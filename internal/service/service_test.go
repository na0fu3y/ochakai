package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// TestCheckedLimit pins design doc 0064's single rule for every paged or
// windowed read: 0 (unset) is the default, and anything else out of
// [1, max] is refused rather than silently substituted.
func TestCheckedLimit(t *testing.T) {
	cases := []struct {
		name          string
		limit         int
		wantLimit     int
		wantErrSubstr string
	}{
		{"unset uses the default", 0, 10, ""},
		{"in range passes through", 7, 7, ""},
		{"at the max passes through", 50, 50, ""},
		{"over the max is refused", 51, 0, "between 1 and 50"},
		{"negative is refused", -1, 0, "between 1 and 50"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := checkedLimit(c.limit, 10, 50)
			if c.wantErrSubstr == "" {
				if err != nil || got != c.wantLimit {
					t.Fatalf("checkedLimit(%d, 10, 50) = %d, %v; want %d, nil", c.limit, got, err, c.wantLimit)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErrSubstr) {
				t.Fatalf("checkedLimit(%d, 10, 50) error = %v, want it to mention %q", c.limit, err, c.wantErrSubstr)
			}
		})
	}
}

func hit(id string, status domain.Status) domain.SearchHit {
	return domain.SearchHit{Summary: domain.Summary{Type: domain.TypeMetrics, ID: id, Status: status}}
}

// verifiedHit is hit plus a confirmation. The rank boost keys off the
// trust tier the ledger yields rather than the status (design docs 0043
// §3.2, 0046 §3.10).
func verifiedHit(id string) domain.SearchHit {
	h := hit(id, domain.StatusStable)
	h.Trust = domain.TrustHuman
	return h
}

func TestRRFFuseMergesAndRanks(t *testing.T) {
	lexical := []domain.SearchHit{hit("a", domain.StatusDraft), hit("b", domain.StatusDraft)}
	vector := []domain.SearchHit{hit("b", domain.StatusDraft), hit("c", domain.StatusDraft)}
	out := rrfFuse(10, lexical, vector)
	if len(out) != 3 {
		t.Fatalf("want 3 fused hits, got %d", len(out))
	}
	// "b" appears in both lists and must rank first.
	if out[0].ID != "b" {
		t.Errorf("want b first, got %s", out[0].ID)
	}
}

func TestRRFFuseBoostsVerified(t *testing.T) {
	// Same single-list rank; verified must win the tie.
	lexical := []domain.SearchHit{hit("draft-doc", domain.StatusDraft)}
	vector := []domain.SearchHit{verifiedHit("verified-doc")}
	out := rrfFuse(10, lexical, vector)
	if out[0].ID != "verified-doc" {
		t.Errorf("verified concept should outrank draft at equal RRF score, got %s first", out[0].ID)
	}
}

func TestRRFFuseLimit(t *testing.T) {
	var list []domain.SearchHit
	for _, id := range []string{"a", "b", "c", "d"} {
		list = append(list, hit(id, domain.StatusDraft))
	}
	if got := len(rrfFuse(2, list)); got != 2 {
		t.Errorf("want limit 2, got %d", got)
	}
}

// TestSearchRejectsEmptyQuery pins the guard that fires before any store
// access: an empty or whitespace-only query is a client error
// (InvalidInputError → 400). SearchLexical splits the query into
// fragments and an empty query yields none, so without the guard it
// builds zero-fragment SQL and every surface got a Postgres 500.
func TestSearchRejectsEmptyQuery(t *testing.T) {
	s := &Service{}
	var inputErr *InvalidInputError
	for _, q := range []string{"", "   ", " \t\n", "　"} {
		_, err := s.Search(context.Background(), q, store.Filter{}, 10)
		if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "needs a query") {
			t.Errorf("query %q: got %v, want a needs-a-query InvalidInputError", q, err)
		}
	}
}

// TestSearchOrListValidation pins the mode rules REST and MCP both
// delegate here, so the two surfaces cannot answer the same request
// differently: an unknown sort is a client error, and a sort combined
// with a query is one for every mode in domain.ListSorts (a feed is a
// queue to work through, not something to rank by relevance). All of it
// runs before any store access — the Service below has none.
func TestSearchOrListValidation(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	var inputErr *InvalidInputError

	_, err := s.SearchOrList(ctx, "", "created_at", "", store.Filter{}, 0)
	if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "invalid sort") {
		t.Errorf("unknown sort: got %v, want an invalid-sort InvalidInputError", err)
	}
	for _, sort := range domain.ListSorts {
		_, err := s.SearchOrList(ctx, "revenue", sort, "", store.Filter{}, 0)
		if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "cannot be combined") {
			t.Errorf("sort=%s with a query: got %v, want a cannot-be-combined InvalidInputError", sort, err)
		}
	}
	if _, err := s.SearchOrList(ctx, "  ", "", "", store.Filter{}, 0); !errors.As(err, &inputErr) ||
		!strings.Contains(err.Error(), "needs a query") {
		t.Errorf("neither query nor sort: got %v, want a needs-a-query InvalidInputError", err)
	}
	// The way out of that error is a listing mode, so the message has to
	// name all of them. It named three for as long as there were three.
	_, err = s.SearchOrList(ctx, "", "", "", store.Filter{}, 0)
	for _, sort := range domain.ListSorts {
		if !strings.Contains(err.Error(), sort) {
			t.Errorf("the needs-a-query message never mentions sort=%s: %v", sort, err)
		}
	}
}

// TestReportOutcomeValidation pins the input checks that run before any
// store access: an unknown outcome and an oversized note are client
// errors (InvalidInputError → 400), never a nil-store panic.
func TestReportOutcomeValidation(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	var inputErr *InvalidInputError

	_, err := s.ReportOutcome(ctx, "queries/q", "misleading", "")
	if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "invalid outcome") {
		t.Errorf("unknown outcome: got %v, want an invalid-outcome InvalidInputError", err)
	}

	_, err = s.ReportOutcome(ctx, "queries/q", domain.EventWorked, strings.Repeat("x", maxOutcomeNote+1))
	if !errors.As(err, &inputErr) || !strings.Contains(err.Error(), "note exceeds") {
		t.Errorf("oversized note: got %v, want a note-exceeds InvalidInputError", err)
	}
}

func TestEmbeddingText(t *testing.T) {
	k := &domain.Knowledge{
		Title:       "Revenue",
		Description: "total sales",
		Tags:        []string{"finance", "kpi"},
		Attrs:       map[string]any{"question": "monthly revenue?"},
		Body:        "body text",
	}
	got := embeddingText(k, embed.ConservativeInputBytes)
	for _, want := range []string{"Revenue", "total sales", "finance kpi", "monthly revenue?", "body text"} {
		if !strings.Contains(got, want) {
			t.Errorf("embeddingText misses %q:\n%s", want, got)
		}
	}
}

// The body is truncated to stay within embedding-model input limits;
// the envelope fields must survive untouched.
func TestEmbeddingTextTruncatesBody(t *testing.T) {
	k := &domain.Knowledge{Title: "T", Body: strings.Repeat("x", embed.ConservativeInputBytes+1000)}
	got := embeddingText(k, embed.ConservativeInputBytes)
	if len(got) > embed.ConservativeInputBytes {
		t.Errorf("embeddingText length = %d, want capped at %d", len(got), embed.ConservativeInputBytes)
	}
	if !strings.HasPrefix(got, "T") {
		t.Errorf("title must lead the text: %q", got[:20])
	}
}

// Japanese is 3 bytes per character in UTF-8, so a byte cap lands inside a
// character unless it backs off. Half a character is not valid UTF-8, and
// the API request carries it as a replacement char.
func TestEmbeddingTextTruncatesOnRuneBoundary(t *testing.T) {
	// 2001 characters * 3 bytes = 6003 bytes: the cap falls mid-character.
	k := &domain.Knowledge{Body: strings.Repeat("売", 2001)}
	got := embeddingText(k, embed.ConservativeInputBytes)
	if !utf8.ValidString(got) {
		t.Errorf("embeddingText produced invalid UTF-8 (length %d)", len(got))
	}
	if len(got) > embed.ConservativeInputBytes {
		t.Errorf("embeddingText length = %d, want <= %d", len(got), embed.ConservativeInputBytes)
	}
	if want := embed.ConservativeInputBytes / 3 * 3; len(got) != want {
		t.Errorf("embeddingText length = %d, want %d (whole characters only)", len(got), want)
	}
}

// The envelope shares the budget with the body. It used to ride on top of
// it, so a deep path plus a paragraph of description could push the text
// past the model's window even though the body itself fit.
func TestEmbeddingTextCapsTheWholeText(t *testing.T) {
	k := &domain.Knowledge{
		ID:          strings.Repeat("a/", 200) + "entry",
		Title:       strings.Repeat("t", 500),
		Description: strings.Repeat("d", 2000),
		Tags:        []string{strings.Repeat("g", 500)},
		Attrs:       map[string]any{"question": strings.Repeat("q", 2000)},
		Body:        strings.Repeat("b", embed.ConservativeInputBytes),
	}
	if got := len(embeddingText(k, embed.ConservativeInputBytes)); got > embed.ConservativeInputBytes {
		t.Errorf("embeddingText length = %d, want the whole text capped at %d", got, embed.ConservativeInputBytes)
	}
}

// The window is a property of the model. Handing gemini-embedding-2's
// 8192-token input the budget sized for a 2048-token model throws away
// three quarters of it, and the concept embeds fine while carrying less of
// itself — a loss nothing reports.
func TestEmbedBytesFollowsTheModel(t *testing.T) {
	conservative := &Service{Embedder: &shrinkEmbedder{}}
	if got := conservative.embedBytes(); got != embed.ConservativeInputBytes {
		t.Errorf("an embedder that does not say = %d, want the floor %d", got, embed.ConservativeInputBytes)
	}
	roomy := &Service{Embedder: limitedEmbedder{bytes: 20000}}
	if got := roomy.embedBytes(); got != 20000 {
		t.Errorf("an embedder that says = %d, want 20000", got)
	}
	k := &domain.Knowledge{Title: "T", Body: strings.Repeat("x", 30000)}
	if got := len(embeddingText(k, roomy.embedBytes())); got != 20000 {
		t.Errorf("text capped at %d, want the model's 20000", got)
	}
}

// limitedEmbedder reports an input window, like Vertex does.
type limitedEmbedder struct{ bytes int }

func (limitedEmbedder) Model() string { return "fake-roomy" }
func (limitedEmbedder) Embed(_ context.Context, _ embed.Task, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}
func (e limitedEmbedder) MaxInputBytes() int { return e.bytes }

func TestTruncateUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"under the cap", "abc", 10, "abc"},
		{"exactly the cap", "abc", 3, "abc"},
		{"ascii cut", "abcdef", 3, "abc"},
		{"cut one byte into a character", "あい", 4, "あ"},
		{"cut two bytes into a character", "あい", 5, "あ"},
		{"cut on a boundary", "あい", 3, "あ"},
		{"cap smaller than one character", "あ", 2, ""},
		{"a real U+FFFD survives", "x�", 4, "x�"},
	} {
		if got := truncateUTF8(tc.in, tc.max); got != tc.want {
			t.Errorf("%s: truncateUTF8(%q, %d) = %q, want %q", tc.name, tc.in, tc.max, got, tc.want)
		}
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	base := func() *domain.Knowledge {
		return &domain.Knowledge{Type: "metric", ID: "revenue", Title: "Revenue"}
	}
	if err := validate(base()); err != nil {
		t.Errorf("valid concept rejected: %v", err)
	}
	// Title is optional (design doc 0022): the id's last segment is the
	// display name when it is absent.
	titleless := base()
	titleless.Title = ""
	if err := validate(titleless); err != nil {
		t.Errorf("titleless concept rejected: %v", err)
	}
	for name, mutate := range map[string]func(*domain.Knowledge){
		"bad type":   func(k *domain.Knowledge) { k.Type = "no/slash" },
		"bad id":     func(k *domain.Knowledge) { k.ID = "UPPER//bad" },
		"index id":   func(k *domain.Knowledge) { k.ID = "sales/index" },
		"log id":     func(k *domain.Knowledge) { k.ID = "sales/log" },
		"bad status": func(k *domain.Knowledge) { k.Status = "published" },
	} {
		k := base()
		mutate(k)
		err := validate(k)
		var invalid *InvalidInputError
		if err == nil || !errors.As(err, &invalid) {
			t.Errorf("%s: want InvalidInputError, got %v", name, err)
		}
	}

	// Rejecting is half the job: the message has to name the vocabulary
	// the writer can use. It named `verified` and `rejected` — a ledger
	// and a ruling since design doc 0043 §§3.2-3.3, not statuses — and
	// never named `stable`, so whoever wrote `published` was told to retry
	// with a value that fails exactly the same way.
	bad := base()
	bad.Status = "published"
	msg := validate(bad).Error()
	for _, s := range domain.Statuses {
		if !strings.Contains(msg, string(s)) {
			t.Errorf("the invalid-status error never names %q: %s", s, msg)
		}
	}
	for _, retired := range []string{"verified", "rejected"} {
		if strings.Contains(msg, retired) {
			t.Errorf("the invalid-status error still offers %q, not a status since design doc 0043: %s", retired, msg)
		}
	}
}

// A write payload's byte-compared keys — the id, and the link targets
// derived from the body — are stored NFC (design doc 0022); content
// fields stay as written.
func TestNormalizeKeys(t *testing.T) {
	nfd := "サンプル" // NFD spelling of サンプル
	k := &domain.Knowledge{
		ID:    "insights/" + nfd,
		Title: nfd,
		Body:  "[用語](/terms/" + nfd + ".md) を参照。",
	}
	normalizeKeys(k)
	deriveLinks(k)
	if k.ID != "insights/サンプル" {
		t.Errorf("ID = %q, want NFC", k.ID)
	}
	if len(k.Links) != 1 || k.Links[0].Target != "terms/サンプル" {
		t.Errorf("links = %v, want one NFC target terms/サンプル", k.Links)
	}
	if k.Title != nfd {
		t.Errorf("title changed to %q; content must stay as written", k.Title)
	}
}

// Links are derived from the body, so a payload that carries them has
// them discarded (design doc 0024) — no route can store an edge the
// prose does not back.
func TestDeriveLinksIgnoresPayloadLinks(t *testing.T) {
	k := &domain.Knowledge{
		ID:    "insights/a",
		Links: []domain.Link{{Target: "metrics/invented"}},
		Body:  "See [revenue](/metrics/revenue.md).",
	}
	deriveLinks(k)
	want := []domain.Link{{Target: "metrics/revenue", Text: "revenue"}}
	if !reflect.DeepEqual(k.Links, want) {
		t.Errorf("links = %v, want %v", k.Links, want)
	}
}

// TestExampleGoldenQueryRegisters guards the shipped example against
// drift: examples/golden-query.md must parse as an OKF document and pass
// validation with its frontmatter landing in attrs — the quickstart
// command has to keep working.
func TestExampleGoldenQueryRegisters(t *testing.T) {
	doc, err := os.ReadFile("../../examples/golden-query.md")
	if err != nil {
		t.Fatal(err)
	}
	k, notes, err := okf.Parse(doc)
	if err != nil {
		t.Fatalf("okf.Parse: %v", err)
	}
	if len(notes) > 0 {
		t.Errorf("the shipped example should parse without reinterpretation: %v", notes)
	}
	k.ID = "queries/monthly-revenue"
	if k.Type != domain.TypeComputations {
		t.Errorf("type = %q, want %q", k.Type, domain.TypeComputations)
	}
	if err := validate(&k.Knowledge); err != nil {
		t.Errorf("example concept rejected: %v", err)
	}
	if q, _ := k.Attrs["question"].(string); q == "" {
		t.Error("attrs.question missing: a verified query without its question is not searchable as one")
	}
	// The computation belongs in the body fence, not in attrs: SPEC §10.2
	// makes the fence the computation when no external file is named, and
	// an attester canonicalizes what a run executed against it. Keeping a
	// second copy under attrs.sql would leave a consumer no way to tell
	// which of the two is authoritative (design doc 0038 §3.3).
	if !strings.Contains(k.Body, "# Computation") || !strings.Contains(k.Body, "SELECT") {
		t.Errorf("the computation must be a # Computation fence in the body, got %q", k.Body)
	}
	if _, ok := k.Attrs["sql"]; ok {
		t.Error("attrs.sql duplicates the body fence: one computation, one home")
	}
}

// packWithinBudget delivers whole concepts or none: a body cut in half
// still looks like a body, and half of an attested computation's SQL
// still looks executable.
func TestPackWithinBudgetKeepsEntriesWhole(t *testing.T) {
	entry := func(id string, bodyBytes int) domain.View {
		k := domain.Knowledge{Type: domain.TypeInsights, ID: id, Title: id,
			Description: "desc " + id, Status: domain.StatusDraft, Body: strings.Repeat("x", bodyBytes)}
		v, err := okf.ViewOf(&k)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	small := entry("insights/small", 100)
	big := entry("insights/big", 5000)
	// Room for the small concept plus the row that names the big one: the
	// outline is inside the budget, so "room for one concept" has to include
	// what saying "and there was another" costs.
	one := serializedSize(&small)
	oneAndARow := one + jsonSize(outlineRow(&big, serializedSize(&big))) + 10

	t.Run("no budget keeps everything", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.View{small, big}, 0)
		if len(kept) != 2 || outline != nil {
			t.Errorf("budget 0 must not truncate: %d kept, %d outlined", len(kept), len(outline))
		}
	})

	t.Run("overflow becomes an outline row", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.View{small, big}, oneAndARow)
		if len(kept) != 1 || kept[0].ID != small.ID {
			t.Fatalf("want only the small concept in full, got %d", len(kept))
		}
		if len(outline) != 1 || outline[0].ID != big.ID {
			t.Fatalf("want the big concept outlined, got %v", outline)
		}
		if outline[0].Bytes != serializedSize(&big) || outline[0].Description != big.Summary.Description {
			t.Errorf("outline row must carry size and description: %+v", outline[0])
		}
		if outline[0].Title != big.Summary.Title {
			t.Errorf("outline title = %q, want the display title %q", outline[0].Title, big.Summary.Title)
		}
	})

	// A model concept carries its whole spec in attrs and can outweigh the
	// entire budget. A prefix cut would let it starve everything below it;
	// greedy packing keeps the rest and names the giant.
	t.Run("an oversized leader does not starve the rest", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.View{big, small}, oneAndARow)
		if len(kept) != 1 || kept[0].ID != small.ID {
			t.Fatalf("want the small concept delivered behind the giant, got %+v", kept)
		}
		if len(outline) != 1 || outline[0].ID != big.ID {
			t.Fatalf("want the giant outlined, got %v", outline)
		}
	})

	// Budgets below one concept outline everything rather than shipping a
	// fragment: an empty concepts list with a populated outline is a usable
	// answer, a half-concept is not. Naming everything is also the floor —
	// a caller cannot raise a budget for concepts it never heard about.
	t.Run("a budget below one concept outlines everything", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.View{small, big}, 1)
		if len(kept) != 0 || len(outline) != 2 {
			t.Errorf("want everything outlined, got %d kept / %d outlined", len(kept), len(outline))
		}
	})

	// The budget governs the response, not half of it. An outline row
	// carries a description — unbounded on the concept — so a budget that
	// only counted delivered concepts left the actual payload unbounded.
	t.Run("the outline is inside the budget", func(t *testing.T) {
		wordy := make([]domain.View, 6)
		for i := range wordy {
			k := domain.Knowledge{Type: domain.TypeInsights,
				ID: fmt.Sprintf("insights/wordy-%d", i), Title: "wordy",
				Description: strings.Repeat("長い説明。", 400), // ~6 kB each
				Status:      domain.StatusDraft, Body: strings.Repeat("x", 900)}
			v, err := okf.ViewOf(&k)
			if err != nil {
				t.Fatal(err)
			}
			wordy[i] = v
		}
		const budget = 4000
		kept, outline := packWithinBudget(wordy, budget)
		total := 0
		for i := range kept {
			total += serializedSize(&kept[i])
		}
		for _, row := range outline {
			total += jsonSize(row)
		}
		if total > budget {
			t.Errorf("response = %d bytes over a budget of %d (%d kept, %d outlined)",
				total, budget, len(kept), len(outline))
		}
		if len(kept)+len(outline) != len(wordy) {
			t.Errorf("every concept must be delivered or named: %d kept, %d outlined, want %d total",
				len(kept), len(outline), len(wordy))
		}
		for _, row := range outline {
			if len(row.Description) > outlineDescriptionBytes {
				t.Errorf("outline description = %d bytes, want <= %d", len(row.Description), outlineDescriptionBytes)
			}
		}
	})

	// The whole document, not just the body: a producer key can be the
	// largest thing a concept carries, and it is in the frontmatter.
	t.Run("size counts the whole document", func(t *testing.T) {
		render := func(k domain.Knowledge) domain.View {
			v, err := okf.ViewOf(&k)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		bare := render(domain.Knowledge{Type: domain.Type("Data Contract"), ID: "contracts/c"})
		withKey := render(domain.Knowledge{Type: domain.Type("Data Contract"), ID: "contracts/c",
			Attrs: map[string]any{"spec": strings.Repeat("y", 2000)}})
		if serializedSize(&withKey) <= serializedSize(&bare)+1000 {
			t.Error("serializedSize ignores what the frontmatter carries")
		}
	})
}

// A byte budget only approximates a token count, and the approximation is
// worst for Japanese. When the model rejects the text anyway, the concept
// must not vanish from vector search: shorten and try again, rather than
// log a warning and report the write as a success.
func TestEmbedDocumentShortensOnOverlongInput(t *testing.T) {
	long := strings.Repeat("あ", 2000)

	t.Run("retries shorter until it fits", func(t *testing.T) {
		e := &shrinkEmbedder{acceptUnder: 1200}
		svc := &Service{Embedder: e, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if _, err := svc.embedDocument(context.Background(), long); err != nil {
			t.Fatalf("embedDocument: %v", err)
		}
		if e.calls < 2 {
			t.Errorf("calls = %d, want a retry after the rejection", e.calls)
		}
		if !utf8.ValidString(e.last) {
			t.Error("shortening cut a character in half")
		}
	})

	// An outage is not an overrun: retrying it only adds latency to a
	// write that was going to fall back to trigram search anyway.
	t.Run("does not retry other failures", func(t *testing.T) {
		e := &shrinkEmbedder{fail: errors.New("connection refused")}
		svc := &Service{Embedder: e, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if _, err := svc.embedDocument(context.Background(), long); err == nil {
			t.Fatal("want the error surfaced")
		}
		if e.calls != 1 {
			t.Errorf("calls = %d, want exactly one", e.calls)
		}
	})
}

// shrinkEmbedder accepts input below acceptUnder bytes and reports
// anything longer as over the model's limit.
type shrinkEmbedder struct {
	acceptUnder int
	fail        error
	calls       int
	last        string
}

func (e *shrinkEmbedder) Model() string { return "fake" }

func (e *shrinkEmbedder) Embed(_ context.Context, _ embed.Task, texts []string) ([][]float32, error) {
	e.calls++
	e.last = texts[0]
	if e.fail != nil {
		return nil, e.fail
	}
	if len(texts[0]) >= e.acceptUnder {
		return nil, fmt.Errorf("%w: 400 Bad Request", embed.ErrInputTooLong)
	}
	return [][]float32{{1, 0}}, nil
}

// TestRecommendedTypesAreWritable pins design doc 0038 §4.2: adding a type
// to the vocabulary adds no server behavior. Every recommended type must
// pass the write path carrying nothing but an id and a title — the sole
// exception being the runtime SPEC §10.2 requires on an Attested
// Computation (design doc 0036 §3.10).
//
// This is also the guard the vocabulary change itself needed. Swapping a
// test fixture's type to Attested Computation without giving it a runtime
// compiles, passes every unit test, and only fails where a real write
// happens — which, for the concept types that live in integration tests, is
// nowhere a developer without a database will see.
func TestRecommendedTypesAreWritable(t *testing.T) {
	for _, ty := range domain.Types {
		k := &domain.Knowledge{Type: ty, ID: "probe/concept", Title: "probe"}
		if ty == domain.TypeComputations {
			k.Runtime = "bigquery"
		}
		if err := validate(k); err != nil {
			t.Errorf("recommended type %q is not writable as-is: %v", ty, err)
		}
	}
	bare := &domain.Knowledge{Type: domain.TypeComputations, ID: "probe/concept", Title: "probe"}
	if err := validate(bare); err == nil {
		t.Error("an Attested Computation without a runtime must still be refused")
	}
}

// A path scope is normalized the way a browsed path is, so a directory
// the tree can open can be pasted into a filter unchanged (design doc
// 0041 §2.4): NFC, no trailing slash. The empty scope is the root, which
// narrows nothing, so it drops out rather than becoming an error or —
// worse — a condition matching nothing.
func TestCheckedFilterNormalizesPaths(t *testing.T) {
	nfd := "データ/売上" // デ decomposed, as macOS hands paths back
	nfc := domain.Normalize(nfd)
	if nfd == nfc {
		t.Fatal("test input is already NFC; it cannot show that normalization happens")
	}
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"trailing slash is the same scope", []string{"teams/growth/"}, []string{"teams/growth"}},
		{"decomposed path is composed", []string{nfd}, []string{nfc}},
		{"empty scopes drop out", []string{"", "teams/growth", ""}, []string{"teams/growth"}},
		{"only-empty leaves no condition", []string{"", "/"}, []string{}},
		{"several scopes survive in order", []string{"teams/growth", "company"}, []string{"teams/growth", "company"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkedFilter(store.Filter{Prefixes: tc.in})
			if err != nil {
				t.Fatalf("checkedFilter(%q): %v", tc.in, err)
			}
			if !reflect.DeepEqual(got.Prefixes, tc.want) {
				t.Errorf("checkedFilter(%q) = %q, want %q", tc.in, got.Prefixes, tc.want)
			}
		})
	}
}

// A scope that could not lead an id is a client error, not something to
// quietly ignore: a misspelling that returned the whole knowledge base
// would read as "this scope holds everything".
func TestCheckedFilterRejectsImpossiblePaths(t *testing.T) {
	for _, bad := range []string{"teams//growth", "//", "teams/.hidden", "teams/g\x00rowth"} {
		var invalid *InvalidInputError
		if _, err := checkedFilter(store.Filter{Prefixes: []string{bad}}); !errors.As(err, &invalid) {
			t.Errorf("checkedFilter(%q) error = %v, want an InvalidInputError", bad, err)
		}
	}
	// A space is not a mistake. Ids are file paths with no prescribed
	// character set (design doc 0019), so a directory named "sales team"
	// is addressable and must be scopeable.
	if got, err := checkedFilter(store.Filter{Prefixes: []string{"teams/sales team"}}); err != nil {
		t.Errorf("checkedFilter with a space in a segment: %v", err)
	} else if !reflect.DeepEqual(got.Prefixes, []string{"teams/sales team"}) {
		t.Errorf("scope with a space = %q", got.Prefixes)
	}
	// A filter with no scopes at all is untouched — the ordinary case
	// must not pay for the check.
	f, err := checkedFilter(store.Filter{Tags: []string{"core"}})
	if err != nil || f.Prefixes != nil {
		t.Errorf("unscoped filter = %+v, %v; want it unchanged", f, err)
	}
}

// The "fm." prefix carries the OKF keys nothing else asks about, and only
// those (design doc 0047). A key with a column behind it answers a
// different question through the column than through the jsonb path —
// `status=stable` matches a document that says nothing, `fm.status`
// never does — and the caller cannot see which one they asked, so the
// spelling that would be a coin toss is refused instead.
func TestCheckedFilterRefusesFrontmatterKeysWithAFilter(t *testing.T) {
	for _, key := range []string{"type", "status", "tags", "sources", "stale_after"} {
		var invalid *InvalidInputError
		_, err := checkedFilter(store.Filter{Frontmatter: map[string]string{key: "x"}})
		if !errors.As(err, &invalid) {
			t.Errorf("fm.%s error = %v, want an InvalidInputError", key, err)
			continue
		}
		// The refusal is only useful if it names the filter to use, so
		// the caller's next request is the right one.
		if !strings.Contains(err.Error(), domain.FilterOwnedKeys[key]) {
			t.Errorf("fm.%s error = %q, want it to name %q", key, err, domain.FilterOwnedKeys[key])
		}
	}
	// An OKF key with no filter of its own is what the prefix is for, and
	// passes through untouched.
	for _, key := range domain.AskableFrontmatterKeys() {
		f, err := checkedFilter(store.Filter{Frontmatter: map[string]string{key: "x"}})
		if err != nil {
			t.Errorf("fm.%s: %v, want it answered", key, err)
		} else if f.Frontmatter[key] != "x" {
			t.Errorf("fm.%s was dropped from the filter", key)
		}
	}
}

// The filter vocabulary is OKF's (design doc 0047 §2.2). A producer's own
// key is stored and handed back exactly as written — SPEC §4.1 requires
// that, and design doc 0046 §3.2 keeps it — but it is not something to
// ask for, so naming one is a refusal rather than a query that quietly
// finds nothing. A wrong answer that looks like a valid one is the shape
// this whole vocabulary exists to avoid.
func TestCheckedFilterRefusesKeysOKFDoesNotDefine(t *testing.T) {
	// "tag" and "source" are the singular spellings a producer may well
	// have written; "Status" differs from an OKF key only in case. None
	// of them is an OKF key, so all three land here rather than being
	// second-guessed into the filter beside them.
	for _, key := range []string{"owner", "question", "tag", "source", "Status", "attrs"} {
		var invalid *InvalidInputError
		_, err := checkedFilter(store.Filter{Frontmatter: map[string]string{key: "x"}})
		if !errors.As(err, &invalid) {
			t.Errorf("fm.%s error = %v, want an InvalidInputError", key, err)
			continue
		}
		// The refusal lists what can be asked, so the caller does not
		// have to go and read a document to find out.
		if !strings.Contains(err.Error(), "resource") {
			t.Errorf("fm.%s error = %q, want it to list the askable keys", key, err)
		}
	}
}

// Every askable key is an OKF envelope key, and no key with a filter of
// its own is askable. The two lists are derived from one, so this holds
// the derivation rather than a transcription of it — the day OKF adds a
// key, EnvelopeKeys is the only place that has to learn the spelling
// (design doc 0046 §3.11).
func TestAskableKeysAreTheOKFKeysWithNoFilterOfTheirOwn(t *testing.T) {
	askable := domain.AskableFrontmatterKeys()
	if !slices.IsSorted(askable) {
		t.Errorf("askable keys = %v, want them sorted", askable)
	}
	for _, key := range askable {
		if !slices.Contains(domain.EnvelopeKeys, key) {
			t.Errorf("askable key %q is not an OKF envelope key", key)
		}
		if use, ok := domain.FilterOwnedKeys[key]; ok {
			t.Errorf("askable key %q is already asked by %s", key, use)
		}
	}
	if got, want := len(askable), len(domain.EnvelopeKeys)-len(domain.FilterOwnedKeys); got != want {
		t.Errorf("askable keys = %d, want %d", got, want)
	}
}
