package store

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// The lexical index (migration 0036) is two halves that have to agree
// with Go about the same thing. queryFragments decides which runs of a
// question get cut into two-character windows; ochakai_cjk_class decides
// which runs of a document do. A character on one side and not the other
// is a term nobody can find — silently, because both halves keep working
// for everything else. These tests are that agreement, and the index
// lookup the whole migration exists for.

// newSearchStore dials the test database the way every other integration
// test in this package does, skipping without OCHAKAI_TEST_DATABASE_URL.
func newSearchStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	dbURL := testdb.URL(t)
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestCJKClassAgreesWithGo walks every code point the two classifiers
// could disagree about and asks the database what it thinks.
//
// The whole range rather than a sample: the ranges are the thing being
// pinned, so a test that checked 売 and 一 would pass on any table that
// happened to contain the common characters and miss the edges — which
// is exactly where a transcribed range list goes wrong.
func TestCJKClassAgreesWithGo(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)

	goSaysCJK := func(r rune) bool {
		return unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
	}
	// The planes these scripts live in, plus latin, Cyrillic and
	// punctuation to catch a class that matches too much.
	spans := [][2]rune{
		{0x0020, 0x0500},   // ASCII, latin-1, latin extended, Cyrillic
		{0x2000, 0x9FFF},   // punctuation through the CJK main block
		{0xF900, 0xFAFF},   // compatibility ideographs
		{0xFF00, 0xFFEF},   // halfwidth and fullwidth forms
		{0x16F00, 0x1B2FF}, // Nushu, kana extensions and supplements
		{0x1F000, 0x1F2FF}, // enclosed ideographic supplement
		{0x20000, 0x2FA1F}, // ideographic extensions B through F
		{0x30000, 0x323AF}, // extensions G and H
	}
	for _, span := range spans {
		var chars []string
		for r := span[0]; r <= span[1]; r++ {
			// Surrogates are not characters and cannot be sent at all.
			if r >= 0xD800 && r <= 0xDFFF {
				continue
			}
			chars = append(chars, string(r))
		}
		// Classified by the same expression the bigram function splits on.
		rows, err := s.pool.Query(ctx, `
			SELECT c, c ~ ('[' || ochakai_cjk_class() || ']') FROM unnest($1::text[]) AS c`, chars)
		if err != nil {
			t.Fatalf("classify U+%04X..U+%04X: %v", span[0], span[1], err)
		}
		mismatched := 0
		for rows.Next() {
			var char string
			var sqlSaysCJK bool
			if err := rows.Scan(&char, &sqlSaysCJK); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			r := []rune(char)[0]
			if sqlSaysCJK == goSaysCJK(r) {
				continue
			}
			mismatched++
			if mismatched <= 8 {
				t.Errorf("U+%04X (%q): ochakai_cjk_class says %v, Go says %v — "+
					"a query fragment and the document's windows would not meet",
					r, char, sqlSaysCJK, goSaysCJK(r))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if mismatched > 8 {
			t.Errorf("...and %d more disagreements in U+%04X..U+%04X", mismatched-8, span[0], span[1])
		}
	}
}

// TestJapaneseSearchUsesTheIndex is the migration's whole point: a
// two-character Japanese term is looked up rather than scanned for.
// Before 0036 this plan was a sequential scan by construction — pg_trgm
// can extract no whole trigram from two characters — and its cost grew
// with the corpus.
//
// The assertion is on the plan rather than on a duration, because a
// timing on a test machine says as much about the machine as about the
// query, and on the plan under enable_seqscan=off rather than on which
// plan the planner prefers, because at two hundred rows it should
// prefer the scan and that preference is not what changed.
//
// What changed is that the predicate became indexable at all. Turning
// enable_seqscan off only makes a scan expensive; it cannot make an
// index serve a predicate it has no entries for, which is why the same
// setting would leave '%売上%' on a sequential scan however costly the
// scan was made. The second assertion is what migration 0016 warns
// about — an index that answers by reading all of itself reads as an
// index scan — so the number of rows the lookup actually returned is
// checked too.
func TestJapaneseSearchUsesTheIndex(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	run := testdb.Unique(t, "tsvidx")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	// Enough rows that the planner has a reason to prefer the index, and
	// bodies that differ so the term stays selective — the case the
	// index is for.
	for i := range 200 {
		body := fmt.Sprintf("これは %d 番目の記録である。原価と客数について書いてある。", i)
		if i == 7 {
			// 鵺鵼 is the term the plan is read for: the test database is
			// shared by every package, so a common word like 売上 is in
			// the index from other tests too and the lookup's row count
			// would say nothing about this corpus.
			body = "売上は八月に下がるが季節性である。鵺鵼と呼ぶ。"
		}
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: fmt.Sprintf("%s/doc-%03d", run, i),
			Title: fmt.Sprintf("記録 %d", i), Body: body,
			Status: domain.StatusStable, CreatedBy: actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatalf("create %s: %v", k.ID, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `ANALYZE object`); err != nil {
		t.Fatal(err)
	}

	plan := explainJapaneseLookup(t, ctx, s)
	if !strings.Contains(plan, "object_search_tsv") {
		t.Errorf("a two-character Japanese term does not reach the index; the plan was:\n%s\n"+
			"migration 0036 exists so this lookup stops reading the table", plan)
	}
	// And it discriminated: the lookup returned a handful of rows rather
	// than the table. Measured against the table's size rather than
	// against 1, because the test database is shared and holds what
	// earlier runs of this same test wrote — the claim is that the index
	// finds the lexeme, not that this run is alone in it.
	m := regexp.MustCompile(`Bitmap Index Scan on object_search_tsv \(actual rows=(\d+) loops`).FindStringSubmatch(plan)
	if m == nil {
		t.Fatalf("no bitmap index scan on object_search_tsv to read row counts from:\n%s", plan)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM object`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	got, _ := strconv.Atoi(m[1])
	if total < 200 {
		t.Fatalf("only %d rows in object: too few for the lookup's selectivity to mean anything", total)
	}
	if got*10 > total {
		t.Errorf("the index lookup returned %d rows of %d for a term almost nothing has: "+
			"it is answering by reading itself, not by finding the lexeme\n%s", got, total, plan)
	}

	// And it still answers. An index the planner likes but that holds the
	// wrong lexemes would pass the assertion above.
	hits, err := s.SearchLexical(ctx, "売上", Filter{Prefixes: []string{run}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.HasSuffix(hits[0].ID, "doc-007") {
		t.Errorf("searching 売上 returned %d hits; want the one concept that says it", len(hits))
	}
}

// explainJapaneseLookup returns the analyzed plan for the two-character
// lookup, with the sequential scan priced out of the running for the
// length of the transaction.
func explainJapaneseLookup(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `
		EXPLAIN (ANALYZE, TIMING OFF, COSTS OFF) SELECT k.id FROM object k
		WHERE k.search_tsv @@ plainto_tsquery('simple', '鵺鵼')`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

// TestEnglishSearchStems pins the recall ILIKE could not have: a plural
// finds the body that says the singular. A search that found nothing was
// recorded as a knowledge gap (design doc 0051), so this defect used to
// reach the curator as "somebody should write this".
func TestEnglishSearchStems(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	run := testdb.Unique(t, "tsvstem")

	k := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: run + "/revenue", Title: "net revenue",
		Body:      "Recognized when the order completes.",
		Status:    domain.StatusStable,
		CreatedBy: domain.Actor{Kind: domain.ActorHuman, Name: "test"},
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"revenue", "revenues", "recognizing an order", "orders"} {
		hits, err := s.SearchLexical(ctx, query, Filter{Prefixes: []string{run}}, 10)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(hits) == 0 {
			t.Errorf("%q found nothing; the stored form stems, so the query has to as well", query)
		}
	}
}

// TestUsageFeedRanksByReadsNotByListings pins what the draft review feed
// orders by. A search hit is recorded for every id in every result set,
// so ordering the promotion queue by it made the ranker's own output the
// demand it was supposed to measure — whatever surfaced kept surfacing,
// and a curator read that back as "people want this".
func TestUsageFeedRanksByReadsNotByListings(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	run := testdb.Unique(t, "usgord")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	// Two drafts. One the ranker keeps listing and nobody opens; one
	// listed half as often and read every time.
	listed, read := run+"/listed", run+"/read"
	for _, id := range []string{listed, read} {
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	for range 20 {
		if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []string{listed}); err != nil {
			t.Fatal(err)
		}
	}
	for range 10 {
		if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []string{read}); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordEvents(ctx, domain.EventFetched, actor, []string{read}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	hits, err := s.ListByUsage(ctx, Filter{Prefixes: []string{run}}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("the feed held %d concepts, want the two this test wrote", len(hits))
	}
	if hits[0].ID != read {
		t.Errorf("the feed puts %q first; want %q — the one that was read, not the one "+
			"the ranker listed twice as often", hits[0].ID, read)
	}
}

// The feeds rank on what happened lately, not on what a concept was once
// worth.
//
// The counters in knowledge_usage only ever grow, so before this a
// concept that was read a hundred times in a knowledge base's first month
// sat at the top of the promotion queue for as long as the deployment
// lived — and a queue whose job is "look at this next" was answering
// "look at what you already looked at". The same shape put a long-ago
// repeat offender above the concept failing this week.
//
// Backdated by writing knowledge_event directly, because that is the one
// thing a test cannot ask the product to do: the running totals and the
// raw rows are written together, and only the raw rows carry a time.
func TestTheFeedsRankOnTheRecentWindowIntegration(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	run := testdb.Unique(t, "decay")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	old, now := run+"/once-hot", run+"/hot-now"
	for _, id := range []string{old, now} {
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// Twenty reads and four failures, all older than the window; against
	// three reads and one failure inside it. Lifetime totals say the old
	// one wins by five to one.
	backdate := fmt.Sprintf("%d days", domain.RecentUsageDays+10)
	for _, e := range []struct {
		event string
		n     int
	}{{domain.EventFetched, 20}, {domain.EventFailed, 4}} {
		for range e.n {
			if _, err := s.pool.Exec(ctx,
				`INSERT INTO knowledge_event (knowledge_id, event, actor_kind, actor_name, at)
				 VALUES ($1, $2, 'human', 'test', now() - $3::interval)`,
				old, e.event, backdate); err != nil {
				t.Fatal(err)
			}
		}
		// The running totals are what a real write would have left behind.
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO knowledge_usage (knowledge_id, event, count, last_at)
			 VALUES ($1, $2, $3, now() - $4::interval)
			 ON CONFLICT (knowledge_id, event) DO UPDATE SET count = knowledge_usage.count + $3`,
			old, e.event, e.n, backdate); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := s.RecordEvents(ctx, domain.EventFetched, actor, []string{now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordEvents(ctx, domain.EventFailed, actor, []string{now}); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatal(err)
	}

	f := Filter{Prefixes: []string{run}}
	usage, err := s.ListByUsage(ctx, f, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 2 {
		t.Fatalf("the usage feed held %d concepts, want the two this test wrote", len(usage))
	}
	if usage[0].ID != now {
		t.Errorf("the promotion queue leads with %s; 20 reads from before the window "+
			"must not outrank 3 inside it", usage[0].ID)
	}
	// The number that explains the order travels with it, and says its
	// window: an order by a count nobody can see reads as a broken sort.
	if got := usage[0].Usage.Recent; got.Fetches != 3 || got.Days != domain.RecentUsageDays {
		t.Errorf("the leading hit carries recent = %+v, want 3 fetches over %d days",
			got, domain.RecentUsageDays)
	}
	if got := usage[1].Usage; got.Recent.Fetches != 0 || got.Fetches != 20 {
		t.Errorf("the aged concept carries %+v, want 0 recent against 20 lifetime", got)
	}

	failed, err := s.ListByFailed(ctx, f, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 {
		t.Fatalf("the re-verification feed held %d concepts, want two", len(failed))
	}
	if failed[0].ID != now {
		t.Errorf("the re-verification feed leads with %s; four failures from before the "+
			"window must not outrank one inside it", failed[0].ID)
	}
}

// The window is counted over raw events, and those are pruned. A window
// longer than the retention would read like a period of time and mean
// "as much as we still have".
func TestTheRecentWindowFitsInsideRetention(t *testing.T) {
	window := time.Duration(domain.RecentUsageDays) * 24 * time.Hour
	if window > eventRetention {
		t.Errorf("the recent window is %s and raw events are kept %s: the counts would be "+
			"silently short of what they claim", window, eventRetention)
	}
}

// A concept is found by the other names its writer gave it (design doc
// 0105). The type vocabulary promises this of a Metric — "its definition
// and synonyms" — and the examples bundle writes them the obvious way,
// while nothing read the key: `synonyms: [net sales, 売上]` sat in attrs
// and neither name found the metric. Both halves of the index are
// exercised, because the Japanese half is where it mattered most: 売上 is
// the word a reader has when `Revenue` is the word the warehouse has.
func TestSynonymsAreSearchable(t *testing.T) {
	ctx := context.Background()
	s := newSearchStore(t, ctx)
	run := testdb.Unique(t, "tsvsyn")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	k := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: run + "/revenue", Title: "Revenue",
		Body:      "Tax-included value of completed orders.",
		Status:    domain.StatusStable,
		CreatedBy: actor,
		Attrs:     map[string]any{"synonyms": []any{"net sales", "売上"}},
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	// One alias, written as the bare string somebody with one writes.
	one := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: run + "/margin", Title: "Margin",
		Status: domain.StatusStable, CreatedBy: actor,
		Attrs: map[string]any{"synonyms": "粗利"},
	}
	if err := s.Create(ctx, one, false); err != nil {
		t.Fatal(err)
	}
	// A producer key that is not a name. It says which model the concept
	// is about; a search for that model must not return this concept,
	// which is what --links-to and --source answer properly.
	other := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: run + "/derived", Title: "Derived",
		Status: domain.StatusStable, CreatedBy: actor,
		Attrs: map[string]any{"model": "zzqqx-model-name"},
	}
	if err := s.Create(ctx, other, false); err != nil {
		t.Fatal(err)
	}

	for query, want := range map[string]string{
		"net sales": run + "/revenue",
		"売上":        run + "/revenue",
		"粗利":        run + "/margin",
	} {
		hits, err := s.SearchLexical(ctx, query, Filter{Prefixes: []string{run}}, 10)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		found := false
		for _, h := range hits {
			if h.ID == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not find %s: a name the writer gave it is a name it answers to", query, want)
		}
	}
	if hits, err := s.SearchLexical(ctx, "zzqqx-model-name", Filter{Prefixes: []string{run}}, 10); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Errorf("a producer key that is not a name reached the haystack: %+v", hits)
	}

	// An edit that removes a name takes it out of the haystack too: the
	// trigger recomputes from the row, so there is no second copy to go
	// stale.
	k.Attrs = map[string]any{"synonyms": []any{"net sales"}}
	if err := s.Update(ctx, k, actor, nil); err != nil {
		t.Fatal(err)
	}
	if hits, err := s.SearchLexical(ctx, "売上", Filter{Prefixes: []string{run}}, 10); err != nil {
		t.Fatal(err)
	} else if len(hits) != 0 {
		t.Errorf("a removed name still answers: %+v", hits)
	}
}
