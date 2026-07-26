package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

func hit(id string, status domain.Status) domain.SearchHit {
	return domain.SearchHit{Knowledge: domain.Knowledge{Type: domain.TypeMetrics, ID: id, Status: status}}
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
	vector := []domain.SearchHit{hit("verified-doc", domain.StatusVerified)}
	out := rrfFuse(10, lexical, vector)
	if out[0].ID != "verified-doc" {
		t.Errorf("verified entry should outrank draft at equal RRF score, got %s first", out[0].ID)
	}
}

func TestApplyVerificationStampsProvenance(t *testing.T) {
	svc := &Service{}
	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0"}
	agent := domain.Actor{Kind: domain.ActorAgent, Name: "claude-code"}

	verified := &domain.Knowledge{Status: domain.StatusVerified}
	svc.applyVerification(verified, nil, human)
	if verified.VerifiedBy == nil || verified.VerifiedAt == nil {
		t.Fatal("verifying must stamp verified_by/verified_at")
	}
	if verified.RejectedBy != nil || verified.RejectedAt != nil {
		t.Error("verified entry must not carry rejection provenance")
	}

	rejected := &domain.Knowledge{Status: domain.StatusRejected, StatusNote: "duplicate of revenue-v2"}
	svc.applyVerification(rejected, verified, human)
	if rejected.RejectedBy == nil || rejected.RejectedAt == nil {
		t.Fatal("rejecting must stamp rejected_by/rejected_at")
	}
	if rejected.VerifiedBy != nil || rejected.VerifiedAt != nil {
		t.Error("leaving verified must clear verification provenance")
	}

	// A later edit that keeps status=rejected must not re-stamp: the
	// original rejecter stays on record.
	edited := &domain.Knowledge{Status: domain.StatusRejected,
		RejectedBy: rejected.RejectedBy, RejectedAt: rejected.RejectedAt}
	svc.applyVerification(edited, rejected, agent)
	if edited.RejectedBy.Name != "na0" {
		t.Errorf("rejected_by re-stamped to %q, want original na0", edited.RejectedBy.Name)
	}

	// Back to draft clears rejection provenance.
	redraft := &domain.Knowledge{Status: domain.StatusDraft,
		RejectedBy: rejected.RejectedBy, RejectedAt: rejected.RejectedAt}
	svc.applyVerification(redraft, rejected, human)
	if redraft.RejectedBy != nil || redraft.RejectedAt != nil {
		t.Error("leaving rejected must clear rejection provenance")
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
// three quarters of it, and the entry embeds fine while carrying less of
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
		t.Errorf("valid entry rejected: %v", err)
	}
	// Title is optional (design doc 0022): the id's last segment is the
	// display name when it is absent.
	titleless := base()
	titleless.Title = ""
	if err := validate(titleless); err != nil {
		t.Errorf("titleless entry rejected: %v", err)
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
	if k.Type != domain.TypeQueries {
		t.Errorf("type = %q, want %q", k.Type, domain.TypeQueries)
	}
	if err := validate(k); err != nil {
		t.Errorf("example entry rejected: %v", err)
	}
	if q, _ := k.Attrs["question"].(string); q == "" {
		t.Error("attrs.question missing: a golden query without its question is not searchable as one")
	}
	if sql, _ := k.Attrs["sql"].(string); !strings.Contains(sql, "SELECT") {
		t.Errorf("attrs.sql = %q", k.Attrs["sql"])
	}
}

// packWithinBudget delivers whole entries or none: a body cut in half
// still looks like a body, and half of a golden query's SQL still looks
// executable.
func TestPackWithinBudgetKeepsEntriesWhole(t *testing.T) {
	entry := func(id string, bodyBytes int) domain.Knowledge {
		return domain.Knowledge{Type: domain.TypeInsights, ID: id, Title: id,
			Description: "desc " + id, Status: domain.StatusDraft, Body: strings.Repeat("x", bodyBytes)}
	}
	small := entry("insights/small", 100)
	big := entry("insights/big", 5000)
	// Room for the small entry plus the row that names the big one: the
	// outline is inside the budget, so "room for one entry" has to include
	// what saying "and there was another" costs.
	one := serializedSize(&small)
	oneAndARow := one + jsonSize(outlineRow(&big, serializedSize(&big))) + 10

	t.Run("no budget keeps everything", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.Knowledge{small, big}, 0)
		if len(kept) != 2 || outline != nil {
			t.Errorf("budget 0 must not truncate: %d kept, %d outlined", len(kept), len(outline))
		}
	})

	t.Run("overflow becomes an outline row", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.Knowledge{small, big}, oneAndARow)
		if len(kept) != 1 || kept[0].ID != small.ID {
			t.Fatalf("want only the small entry in full, got %d", len(kept))
		}
		if len(outline) != 1 || outline[0].ID != big.ID {
			t.Fatalf("want the big entry outlined, got %v", outline)
		}
		if outline[0].Bytes != serializedSize(&big) || outline[0].Description != big.Description {
			t.Errorf("outline row must carry size and description: %+v", outline[0])
		}
		if outline[0].Title != big.DisplayTitle() {
			t.Errorf("outline title = %q, want the display title %q", outline[0].Title, big.DisplayTitle())
		}
	})

	// A Semantic Model carries its whole spec in attrs and can outweigh the
	// entire budget. A prefix cut would let it starve everything below it;
	// greedy packing keeps the rest and names the giant.
	t.Run("an oversized leader does not starve the rest", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.Knowledge{big, small}, oneAndARow)
		if len(kept) != 1 || kept[0].ID != small.ID {
			t.Fatalf("want the small entry delivered behind the giant, got %+v", kept)
		}
		if len(outline) != 1 || outline[0].ID != big.ID {
			t.Fatalf("want the giant outlined, got %v", outline)
		}
	})

	// Budgets below one entry outline everything rather than shipping a
	// fragment: an empty entries list with a populated outline is a usable
	// answer, a half-entry is not. Naming everything is also the floor —
	// a caller cannot raise a budget for entries it never heard about.
	t.Run("a budget below one entry outlines everything", func(t *testing.T) {
		kept, outline := packWithinBudget([]domain.Knowledge{small, big}, 1)
		if len(kept) != 0 || len(outline) != 2 {
			t.Errorf("want everything outlined, got %d kept / %d outlined", len(kept), len(outline))
		}
	})

	// The budget governs the response, not half of it. An outline row
	// carries a description — unbounded on the entry — so a budget that
	// only counted delivered entries left the actual payload unbounded.
	t.Run("the outline is inside the budget", func(t *testing.T) {
		wordy := make([]domain.Knowledge, 6)
		for i := range wordy {
			wordy[i] = entry(fmt.Sprintf("insights/wordy-%d", i), 900)
			wordy[i].Description = strings.Repeat("長い説明。", 400) // ~6 kB each
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
			t.Errorf("every entry must be delivered or named: %d kept, %d outlined, want %d total",
				len(kept), len(outline), len(wordy))
		}
		for _, row := range outline {
			if len(row.Description) > outlineDescriptionBytes {
				t.Errorf("outline description = %d bytes, want <= %d", len(row.Description), outlineDescriptionBytes)
			}
		}
	})

	// attrs, not just the body: a Semantic Model's spec is the largest
	// payload in the base and lives entirely in attrs.
	t.Run("size counts attrs", func(t *testing.T) {
		bare := domain.Knowledge{Type: domain.TypeModels, ID: "models/m"}
		withSpec := bare
		withSpec.Attrs = map[string]any{"spec": strings.Repeat("y", 2000)}
		if serializedSize(&withSpec) <= serializedSize(&bare)+1000 {
			t.Error("serializedSize ignores attrs")
		}
	})
}

// A byte budget only approximates a token count, and the approximation is
// worst for Japanese. When the model rejects the text anyway, the entry
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
