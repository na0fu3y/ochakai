package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// hitScore returns the score of id among hits, or -1 when it is absent.
// Assertions go through this rather than indexing hits[0]: the test
// database is shared by every package (see CONTRIBUTING), so another
// test's entry can legitimately tie or outrank this one, and a search
// test that assumes it owns the corpus fails for reasons that have
// nothing to do with search.
func hitScore(hits []domain.SearchHit, id string) float64 {
	for _, h := range hits {
		if h.ID == id {
			return h.Score
		}
	}
	return -1
}

// TestIntegration exercises the store against a real PostgreSQL with
// pgvector. Skipped unless OCHAKAI_TEST_DATABASE_URL is set, e.g.:
//
//	docker run -d --rm -p 55433:5432 -e POSTGRES_PASSWORD=t -e POSTGRES_USER=t -e POSTGRES_DB=t pgvector/pgvector:pg17
//	OCHAKAI_TEST_DATABASE_URL='postgres://t:t@localhost:55433/t?sslmode=disable' go test ./internal/store/
func TestIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	// Hard-delete leftovers from prior runs (soft-deleted rows still
	// conflict on the primary key).
	for _, table := range []string{"object", "knowledge_revision", "knowledge_embedding"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-%'`); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"knowledge_event", "knowledge_usage"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE knowledge_id LIKE 'it-%'`); err != nil {
			t.Fatal(err)
		}
	}

	k := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "it-revenue", Title: "売上",
		Description: "統合テスト用", Status: domain.StatusStable,
		CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEmbedding(ctx, k.ID, "test-model", []float32{1, 0, 0, 0}, false); err != nil {
		t.Fatal(err)
	}

	lex, err := s.SearchLexical(ctx, "売上", Filter{}, 20)
	if err != nil {
		t.Fatalf("SearchLexical: %v", err)
	}
	if hitScore(lex, "it-revenue") < 0 {
		t.Errorf("lexical search missed the entry: %+v", lex)
	}

	// The joined vector query must not have ambiguous column references
	// (knowledge_embedding shares type/id/updated_at with knowledge).
	vec, err := s.SearchVector(ctx, []float32{1, 0, 0, 0}, "test-model", Filter{
		Types: []domain.Type{domain.TypeMetrics}, Statuses: []domain.Status{domain.StatusStable},
	}, 20)
	if err != nil {
		t.Fatalf("SearchVector: %v", err)
	}
	if score := hitScore(vec, "it-revenue"); score < 0.99 {
		t.Errorf("vector search wrong result (score %v): %+v", score, vec)
	}

	// Rejected entries: the ruling round-trips against the tombstone, and
	// no search returns them. Rejecting is a delete carrying its reason —
	// a ruling, not a field of the document (design docs 0043 §3.3,
	// 0135).
	rej := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: "it-revenue-dup", Title: "売上(重複)",
		Description: "統合テスト用", Status: domain.StatusDraft,
		CreatedBy: domain.Actor{Kind: "process", Name: "claude-code"},
	}
	if err := s.Create(ctx, rej, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, rej.ID, domain.Actor{Kind: "human", Name: "test"}, nil, "it-revenue と重複"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, rej.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a rejected entry is still live: %v", err)
	}
	// The lifecycle value is untouched: a rejection is a judgment about
	// the entry, not a stage of it. The reason rides on the revision that
	// recorded the removal, which is what the §9 log publishes.
	got, err := s.GetTombstone(ctx, rej.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusDraft {
		t.Errorf("the tombstone lost its lifecycle value: %+v", got.Status)
	}
	revs, err := s.ListRevisions(ctx, rej.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if revs[0].Change != "reject" || revs[0].Note != "it-revenue と重複" ||
		revs[0].ChangedBy.Name != "test" {
		t.Errorf("the ruling did not round-trip onto the revision: %+v", revs[0])
	}
	def, err := s.SearchLexical(ctx, "売上", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range def {
		if h.ID == rej.ID {
			t.Error("search must not return a rejected entry")
		}
	}

	// Type filters match case-insensitively while storage keeps the written
	// spelling (design doc 0023 §3.3). Typing a type with spaces at a shell
	// is the burden this is meant to relieve, so it has to hold for a free
	// type too — not only for the recommended eight, which the write path
	// canonicalizes anyway.
	free := &domain.Knowledge{
		Type: "Data Contract", ID: "it-contract", Title: "売上契約",
		Description: "統合テスト用", Status: domain.StatusStable,
		CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	if err := s.Create(ctx, free, false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		typ  domain.Type
		want string
	}{
		{"Metric", k.ID}, {"metric", k.ID}, {"METRIC", k.ID},
		{"Data Contract", free.ID}, {"data contract", free.ID}, {"DATA CONTRACT", free.ID},
	} {
		// Scoped to the one entry each case is about. What is under
		// test is whether the type filter matches it, and an unscoped
		// search answers that only while the shared corpus is small
		// enough for a bounded hit list to still contain it — a
		// property of the database, not of the code (issue #278).
		hits, err := s.SearchLexical(ctx, "売上",
			Filter{Types: []domain.Type{tc.typ}, Prefixes: []string{tc.want}}, 10)
		if err != nil {
			t.Fatalf("SearchLexical(type=%q): %v", tc.typ, err)
		}
		found := false
		for _, h := range hits {
			found = found || h.ID == tc.want
		}
		if !found {
			t.Errorf("type=%q must find %q, got %d hits", tc.typ, tc.want, len(hits))
		}
	}
	// A type that only differs by case must not be stored as a second
	// spelling by this test's own writes.
	if got, err := s.Get(ctx, free.ID); err != nil {
		t.Fatal(err)
	} else if got.Type != "Data Contract" {
		t.Errorf("stored spelling = %q, want the spelling as written", got.Type)
	}

	// Usage recording: raw events plus running totals.
	actor := domain.Actor{Kind: "agent", Name: "claude-code"}
	target := []string{k.ID}
	if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	if err := s.RecordEvents(ctx, domain.EventFetched, actor, target); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	// RecordEvents buffers; force the write before reading totals back.
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}
	usage, err := s.Usage(ctx, k.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.SearchHits < 2 || usage.Fetches < 1 || usage.LastUsedAt == nil {
		t.Errorf("usage totals wrong: %+v", usage)
	}

	// Outcome reporting: worked/failed events with a note feed the same
	// totals (issue #41).
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, target[0], "joins dropped 2024 rows"); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if err := s.RecordOutcome(ctx, domain.EventWorked, actor, target[0], ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	usage, err = s.Usage(ctx, k.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Worked < 1 || usage.Failed < 1 {
		t.Errorf("outcome totals wrong: %+v", usage)
	}
	// The note comes back where a reviewer can read it (design doc 0137).
	// Only the report that carried one is here — the worked report above
	// wrote an empty note and must not pad the list, which is why the
	// length of Reports is not the count of reports.
	if len(usage.Reports) != 1 {
		t.Fatalf("want 1 noted report, got %d: %+v", len(usage.Reports), usage.Reports)
	}
	report := usage.Reports[0]
	if report.Note != "joins dropped 2024 rows" {
		t.Errorf("outcome note did not round-trip: %q", report.Note)
	}
	if report.Outcome != domain.EventFailed {
		t.Errorf("report outcome = %q, want %q", report.Outcome, domain.EventFailed)
	}
	if report.By.Kind != actor.Kind || report.By.Name != actor.Name {
		t.Errorf("report actor = %+v, want kind %q name %q", report.By, actor.Kind, actor.Name)
	}
	// Design doc 0065 §4 keeps `via` and `producer` off these rows and
	// 0137 §4 leaves that alone, so the read must not invent them.
	if report.By.Via != "" || report.By.Producer != "" {
		t.Errorf("report actor carries via/producer, which the event rows do not hold: %+v", report.By)
	}
	if report.At.IsZero() {
		t.Error("report carries no time")
	}

	// Newest first, and bounded. Reports past the limit are dropped from
	// the tail, not the head: the list is what happened lately.
	for i := range domain.OutcomeReportLimit + 2 {
		if err := s.RecordOutcome(ctx, domain.EventFailed, actor, k.ID, fmt.Sprintf("later note %d", i)); err != nil {
			t.Fatalf("RecordOutcome: %v", err)
		}
	}
	usage, err = s.Usage(ctx, k.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(usage.Reports) != domain.OutcomeReportLimit {
		t.Errorf("want %d reports, got %d", domain.OutcomeReportLimit, len(usage.Reports))
	}
	for i := 1; i < len(usage.Reports); i++ {
		if usage.Reports[i].At.After(usage.Reports[i-1].At) {
			t.Errorf("reports are not newest first at %d: %v then %v",
				i, usage.Reports[i-1].At, usage.Reports[i].At)
		}
	}
	if last := usage.Reports[len(usage.Reports)-1]; last.Note == "joins dropped 2024 rows" {
		t.Error("the oldest note survived the limit; the tail should be dropped, not the head")
	}

	// ListByVerifiedAt: oldest verification first, deleted excluded by
	// the default filter.
	oldAt := time.Now().UTC().Add(-365 * 24 * time.Hour)
	older := &domain.Knowledge{
		Type: domain.TypeComputations, ID: "it-old-query", Title: "古い検証済みクエリ",
		Status:    domain.StatusStable,
		CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	if err := s.Create(ctx, older, false); err != nil {
		t.Fatal(err)
	}
	backdateVerification(t, ctx, s, older.ID, oldAt)
	list, err := s.ListByVerifiedAt(ctx, Filter{Statuses: []domain.Status{domain.StatusStable}}, nil, 100)
	if err != nil {
		t.Fatalf("ListByVerifiedAt: %v", err)
	}
	if len(list) < 2 || list[0].ID != older.ID {
		t.Errorf("oldest verification must come first: %+v", list)
	}

	// ListByUsage: the draft review feed. Two drafts, unequal demand — the
	// more-searched one ranks first, and each hit carries its usage totals.
	// The feed is scoped by a tag only these two carry: the test database is
	// shared with the other packages' integration tests, which leave live
	// insight drafts behind (run-unique ids, no cleanup) that this filter
	// would otherwise rank against — and those accumulate search hits of
	// their own now that ids are searchable (design doc 0022).
	const feedTag = "it-usage-feed"
	hot := &domain.Knowledge{
		Type: domain.TypeInsights, ID: "it-draft-hot", Title: "よく検索される草案",
		Tags:   []string{feedTag},
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	cold := &domain.Knowledge{
		Type: domain.TypeInsights, ID: "it-draft-cold", Title: "検索されない草案",
		Tags:   []string{feedTag},
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	for _, d := range []*domain.Knowledge{hot, cold} {
		if err := s.Create(ctx, d, false); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []string{hot.ID}); err != nil {
			t.Fatalf("RecordEvents: %v", err)
		}
	}
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, hot.ID, ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}
	feed, err := s.ListByUsage(ctx, Filter{
		Types:    []domain.Type{domain.TypeInsights},
		Statuses: []domain.Status{domain.StatusDraft},
		Tags:     []string{feedTag},
	}, nil, 100)
	if err != nil {
		t.Fatalf("ListByUsage: %v", err)
	}
	if len(feed) < 2 || feed[0].ID != hot.ID {
		t.Errorf("most-searched draft must come first: %+v", feed)
	}
	if feed[0].Usage == nil || feed[0].Usage.SearchHits != 3 || feed[0].Usage.Failed != 1 {
		t.Errorf("hit must carry usage totals (search_hits=3, failed=1): %+v", feed[0].Usage)
	}
	if feed[0].Score != 0 {
		t.Errorf("listing hits carry score 0, got %v", feed[0].Score)
	}
	var sawCold bool
	for _, h := range feed {
		if h.ID == cold.ID {
			sawCold = true
			if h.Usage == nil || h.Usage.SearchHits != 0 {
				t.Errorf("never-searched draft must report 0 search_hits: %+v", h.Usage)
			}
		}
	}
	if !sawCold {
		t.Error("ListByUsage must include never-used drafts (the inventory tail)")
	}

	// ListByFailed: the re-verification feed (design doc 0025). Only entries
	// with a failed outcome report appear, most failures first; ties broken
	// by fewest worked, then verification age (oldest first, never-verified
	// last). Scoped by a unique tag for the same shared-DB reason as above.
	const failTag = "it-failed-feed"
	verifiedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	newFail := func(id, title string, status domain.Status, verified bool) *domain.Knowledge {
		k := &domain.Knowledge{
			Type: domain.TypeComputations, ID: id, Title: title, Tags: []string{failTag},
			Status: status, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
		if verified {
			backdateVerification(t, ctx, s, k.ID, verifiedAt)
		}
		return k
	}
	loud := newFail("it-fail-loud", "3回失敗した検証済み", domain.StatusStable, true)
	quiet := newFail("it-fail-quiet", "1回失敗した検証済み", domain.StatusStable, true)
	draftFail := newFail("it-fail-draft", "1回失敗した草案", domain.StatusDraft, false)
	clean := newFail("it-fail-clean", "失敗のない検証済み", domain.StatusStable, true)
	for range 3 {
		if err := s.RecordOutcome(ctx, domain.EventFailed, actor, loud.ID, ""); err != nil {
			t.Fatalf("RecordOutcome: %v", err)
		}
	}
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, quiet.ID, ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	if err := s.RecordOutcome(ctx, domain.EventFailed, actor, draftFail.ID, ""); err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}
	failFeed, err := s.ListByFailed(ctx, Filter{Tags: []string{failTag}}, nil, 100)
	if err != nil {
		t.Fatalf("ListByFailed: %v", err)
	}
	gotIDs := make([]string, len(failFeed))
	for i, h := range failFeed {
		gotIDs[i] = h.ID
	}
	wantIDs := []string{loud.ID, quiet.ID, draftFail.ID}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Errorf("ListByFailed order = %v, want %v (most failures first; verified before draft on a tie)", gotIDs, wantIDs)
	}
	if len(failFeed) > 0 && (failFeed[0].Usage == nil || failFeed[0].Usage.Failed != 3) {
		t.Errorf("worst entry must carry usage totals (failed=3): %+v", failFeed[0].Usage)
	}
	if len(failFeed) > 0 && failFeed[0].Score != 0 {
		t.Errorf("listing hits carry score 0, got %v", failFeed[0].Score)
	}
	for _, h := range failFeed {
		if h.ID == clean.ID {
			t.Error("ListByFailed must exclude entries with no failed reports")
		}
	}
}

// SoftDelete must survive a database where semantic search was never
// enabled: knowledge_embedding does not exist, and the failed DELETE used
// to abort the surrounding transaction (25P02) before the revision insert.
//
// execTolerateMissingTable is the mechanism, and it is exercised here
// against a table that never exists rather than by dropping the real one.
// The test database is shared by every package (CONTRIBUTING), so dropping
// knowledge_embedding threw away the vectors of whatever else was running
// — and those tests then failed somewhere else entirely, a reembed pass
// reporting work it had just done as still missing.
func TestIntegrationToleratesMissingEmbeddingTable(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}

	// A missing table must leave the transaction usable: without the
	// helper, the failed statement poisons everything after it.
	if err := s.withTx(ctx, func(tx pgx.Tx) error {
		if err := execTolerateMissingTable(ctx, tx,
			`DELETE FROM it_never_created WHERE id=$1`, "x"); err != nil {
			return err
		}
		var one int
		return tx.QueryRow(ctx, `SELECT 1`).Scan(&one)
	}); err != nil {
		t.Fatalf("a missing table must not abort the transaction: %v", err)
	}

	// The caller that depends on it, end to end.
	k := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-delete-me", Title: "delete me",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	_ = s.SoftDelete(ctx, k.ID, k.CreatedBy, nil, "") // clean rerun
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.ID, k.CreatedBy, nil, ""); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := s.Get(ctx, k.ID); err == nil {
		t.Error("entry still visible after SoftDelete")
	}
}

// ListLinkingTo powers get_context's reverse-link expansion: entries
// whose links point at the target must surface — a stored target is
// always the resolved id, whichever of SPEC §6's forms the body wrote —
// and soft-deleted entries must not.
func TestIntegrationListLinkingTo(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-link-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	entries := []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: "it-link-metric", Title: "target", Status: domain.StatusDraft, CreatedBy: actor},
		{Type: domain.TypeInsights, ID: "it-link-bare", Title: "bare link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Target: "it-link-metric", Text: "explains"}}},
		{Type: domain.TypeInsights, ID: "it-link-rel", Title: "relative link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Target: "it-link-metric", Text: "explains"}}},
		{Type: domain.TypeInsights, ID: "it-link-gone", Title: "deleted link", Status: domain.StatusDraft, CreatedBy: actor,
			Links: []domain.Link{{Target: "it-link-metric", Text: "explains"}}},
	}
	for _, k := range entries {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SoftDelete(ctx, "it-link-gone", actor, nil, ""); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListByAddress(ctx, Filter{LinksTo: "it-link-metric"}, nil, 10)
	if err != nil {
		t.Fatalf("the links_to reverse lookup: %v", err)
	}
	ids := map[string]bool{}
	for _, k := range got {
		ids[k.ID] = true
	}
	if len(got) != 2 || !ids["it-link-bare"] || !ids["it-link-rel"] {
		t.Errorf("ListLinkingTo = %v, want it-link-bare and it-link-rel", ids)
	}
}

// A path scope selects a subtree, not a string prefix (design doc 0041
// §2.2). The rows here are the cases that separate the two: a sibling
// directory whose name starts with the scope must stay out, the entry
// sitting at the scope itself must come in, and an id containing "_"
// must not behave like a LIKE wildcard. Several scopes are OR-ed, which
// is the "our team's space and the company-wide one" question the filter
// exists for.
func TestIntegrationPrefixFilterMatchesSegments(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// Both families this test creates, or a second run trips over its own
	// leftovers: soft-deleted rows still hold the primary key.
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM `+table+` WHERE id LIKE 'it-scope%' OR id LIKE 'it-elsewhere%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	ids := []string{
		"it-scope",                  // the entry at the scope itself
		"it-scope/metrics/revenue",  // below it
		"it-scope/deep/a/b/c",       // arbitrarily deep below it
		"it-scope-legacy/churn",     // sibling directory sharing the leading text
		"it-scope_x/underscore",     // "_" is a LIKE wildcard, and must not act like one
		"it-scopeother",             // no separator at all
		"it-elsewhere/company/term", // the second scope in the OR
	}
	for _, id := range ids {
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeMetrics, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor,
		}, false); err != nil {
			t.Fatal(err)
		}
	}

	// Sorted in Go, not left in the query's order: ORDER BY id follows the
	// database's collation, which does not put "/" where byte order would
	// (this very corpus lists it-scope-legacy/churn before
	// it-scope/metrics/revenue). Asserting on that order would make the
	// test agree or disagree depending on the server's locale — the same
	// trap that keeps the filter itself off a range predicate
	// (design doc 0041 §2.5).
	got := func(f Filter) []string {
		t.Helper()
		entries, err := s.ListByAddress(ctx, f, nil, 100)
		if err != nil {
			t.Fatalf("ListByAddress: %v", err)
		}
		var out []string
		for _, k := range entries {
			if strings.HasPrefix(k.ID, "it-scope") || strings.HasPrefix(k.ID, "it-elsewhere") {
				out = append(out, k.ID)
			}
		}
		slices.Sort(out)
		return out
	}

	one := got(Filter{Prefixes: []string{"it-scope"}})
	want := []string{"it-scope", "it-scope/deep/a/b/c", "it-scope/metrics/revenue"}
	if !slices.Equal(one, want) {
		t.Errorf("prefix it-scope = %v, want %v", one, want)
	}

	both := got(Filter{Prefixes: []string{"it-scope/metrics", "it-elsewhere/company"}})
	want = []string{"it-elsewhere/company/term", "it-scope/metrics/revenue"}
	if !slices.Equal(both, want) {
		t.Errorf("two scopes = %v, want %v", both, want)
	}

	// The underscore directory is reachable when it is the scope asked
	// for — the point is that it is not reachable through "it-scope".
	if under := got(Filter{Prefixes: []string{"it-scope_x"}}); !slices.Equal(under, []string{"it-scope_x/underscore"}) {
		t.Errorf("prefix it-scope_x = %v, want the underscore entry", under)
	}

	// A scope naming no directory is empty, not everything: the filter
	// cannot silently degrade to "unfiltered" when a path is misspelled.
	if none := got(Filter{Prefixes: []string{"it-scope/nothing-here"}}); len(none) != 0 {
		t.Errorf("unknown scope returned %v, want nothing", none)
	}
}

// SearchLexical's substring floor must treat the query literally: a query
// of "%" or "_" is not an ILIKE wildcard. Before the escaping fix, "%"
// matched every entry and boosted them all; "_" matched any single
// character. Here only the entry that literally contains "%" may surface.
func TestIntegrationSearchLexicalWildcardEscape(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-wild-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	plain := &domain.Knowledge{Type: domain.TypeMetrics, ID: "it-wild-plain", Title: "純増収益",
		Description: "特殊文字を含まない", Status: domain.StatusDraft, CreatedBy: actor}
	pct := &domain.Knowledge{Type: domain.TypeMetrics, ID: "it-wild-pct", Title: "解約率は5%以内",
		Description: "パーセント記号を含む", Status: domain.StatusDraft, CreatedBy: actor}
	for _, k := range []*domain.Knowledge{plain, pct} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}

	// "%" is a literal search now: it may match only the entry containing
	// one, never the plain entry via a wildcard-driven +0.3 boost.
	hits, err := s.SearchLexical(ctx, "%", Filter{}, 10)
	if err != nil {
		t.Fatalf("SearchLexical(%q): %v", "%", err)
	}
	for _, h := range hits {
		if h.ID == "it-wild-plain" {
			t.Errorf(`"%%" matched it-wild-plain — ILIKE wildcard not escaped: %+v`, hits)
		}
	}
	// "_" likewise must not match an entry that contains no underscore.
	hits, err = s.SearchLexical(ctx, "_", Filter{}, 10)
	if err != nil {
		t.Fatalf("SearchLexical(%q): %v", "_", err)
	}
	for _, h := range hits {
		if h.ID == "it-wild-plain" || h.ID == "it-wild-pct" {
			t.Errorf(`"_" matched %s — ILIKE wildcard not escaped: %+v`, h.ID, hits)
		}
	}
}

// RecordEvents buffers off the read path (design doc 0029): totals are
// invisible until a flush, and Close drains the buffer so a clean shutdown
// loses nothing.
func TestIntegrationUsageBuffering(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-buf-%'`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM knowledge_usage WHERE knowledge_id LIKE 'it-buf-%'`); err != nil {
		t.Fatal(err)
	}

	actor := domain.Actor{Kind: "agent", Name: "claude-code"}
	k := &domain.Knowledge{Type: domain.TypeMetrics, ID: "it-buf-metric", Title: "計測",
		Status: domain.StatusDraft, CreatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordEvents(ctx, domain.EventSearchHit, actor, []string{k.ID}); err != nil {
		t.Fatalf("RecordEvents: %v", err)
	}
	// Before a flush the event is only in memory — no total yet.
	if u, err := s.Usage(ctx, k.ID); err != nil {
		t.Fatal(err)
	} else if u.SearchHits != 0 {
		t.Errorf("event should be buffered, not yet counted: %+v", u)
	}
	// Close must drain the buffer.
	s.Close()

	s2, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if u, err := s2.Usage(ctx, k.ID); err != nil {
		t.Fatal(err)
	} else if u.SearchHits != 1 {
		t.Errorf("Close did not flush the buffered event: %+v", u)
	}
}

// Move rewrites the id everywhere it is recorded — the row, revisions,
// attachments, usage, embeddings — and rewrites inbound references (link
// targets in both forms, attrs.model) so nothing breaks. The destination
// must be a fresh id, even against a soft-deleted row.
func TestIntegrationMove(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL) // the attachment rides a fake blob store other packages' export scans can't read
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}
	s.UseBlobStore(newFakeBlobStore())
	// A namespace of its own, swept when the test ends (CONTRIBUTING,
	// *Tests and checks*). This test used to delete its own rows by
	// `id LIKE 'it-move-%'` on the way in, which reads as the same thing
	// and is not: a file is an object at its own path and carries no id
	// (design docs 0075 §5, 0100), so the revisions of chart.png below
	// survived every run and the next one collided on
	// knowledge_revision's (path, rev). testdb.Sweep deletes on id *or*
	// path, which is the difference.
	run := testdb.Unique(t, "it-move-")
	var (
		src     = run + "/src/metric"
		sibling = run + "/src/sibling" // same directory as src, so ./metric.md resolves
		dst     = run + "/dst/metric"
		bare    = run + "/bare"
		attrs   = run + "/attrs"
		taken   = run + "/taken"
	)

	actor := domain.Actor{Kind: "human", Name: "test"}
	entries := []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: src, Title: "target", Status: domain.StatusStable, CreatedBy: actor},
		{Type: domain.TypeInsights, ID: bare, Title: "bare link", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "Explains [the metric](/" + src + ".md).",
			Links: []domain.Link{{Target: src, Text: "the metric"}}},
		{Type: domain.TypeInsights, ID: sibling, Title: "relative link", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "Explains [the metric](./metric.md) further.",
			Links: []domain.Link{{Target: src, Text: "the metric"}}},
		{Type: domain.TypeMetrics, ID: attrs, Title: "attrs.model ref", Status: domain.StatusDraft, CreatedBy: actor,
			Attrs: map[string]any{"model": src}},
		{Type: domain.TypeInsights, ID: taken, Title: "occupies destination", Status: domain.StatusDraft, CreatedBy: actor},
	}
	for _, k := range entries {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.PutFile(ctx, src+"/chart.png", "image/png", []byte("png"), actor); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEmbedding(ctx, src, "test-model", []float32{1, 0, 0, 0}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEvents(ctx, domain.EventFetched, actor, []string{src}); err != nil {
		t.Fatal(err)
	}
	// Flush before Move so the event is persisted under the old id and the
	// move carries it (RecordEvents now buffers, design doc 0029).
	if err := s.FlushUsage(ctx); err != nil {
		t.Fatalf("FlushUsage: %v", err)
	}

	// Occupied destinations refuse, live or soft-deleted.
	if _, err := s.Move(ctx, src, taken, actor, nil); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("move onto a live entry: got %v, want ErrAlreadyExists", err)
	}
	if err := s.SoftDelete(ctx, taken, actor, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(ctx, src, taken, actor, nil); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("move onto a soft-deleted entry: got %v, want ErrAlreadyExists", err)
	}

	mover := domain.Actor{Kind: "human", Name: "mover"}
	moved, err := s.Move(ctx, src, dst, mover, nil)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ID != dst {
		t.Errorf("moved.ID = %q", moved.ID)
	}
	// A move rewrites the moved entry's links and every referrer's body,
	// so the mover is who those contents stand by — generated.by in an
	// export (design doc 0036 §3.3).
	if moved.UpdatedBy != mover {
		t.Errorf("moved.UpdatedBy = %v, want %v", moved.UpdatedBy, mover)
	}
	for _, id := range []string{dst, bare, attrs} {
		k, err := s.Get(ctx, id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if k.UpdatedBy != mover {
			t.Errorf("%s updated_by = %v after the move, want %v", id, k.UpdatedBy, mover)
		}
	}
	if _, err := s.Get(ctx, src); !errors.Is(err, ErrNotFound) {
		t.Errorf("old id still resolves: %v", err)
	}
	if _, err := s.Get(ctx, dst); err != nil {
		t.Errorf("new id does not resolve: %v", err)
	}

	// History follows: create + move under the new id.
	revs, err := s.ListRevisions(ctx, dst, 10)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) < 2 || revs[0].Change != "move" {
		t.Errorf("revisions after move: %+v", revs)
	}

	// Attachments and usage follow.
	atts, err := s.ListAttachments(ctx, dst)
	if err != nil || len(atts) != 1 || atts[0].Name != "chart.png" {
		t.Errorf("attachments after move: %v, %v", atts, err)
	}
	usage, err := s.Usage(ctx, dst)
	if err != nil || usage.Fetches < 1 {
		t.Errorf("usage after move: %+v, %v", usage, err)
	}
	var embedded bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM knowledge_embedding WHERE id=$1)`, dst).Scan(&embedded); err != nil || !embedded {
		t.Errorf("embedding did not follow: %v, %v", embedded, err)
	}

	// Inbound references rewritten in the body — the links column follows
	// because it is derived from it (design doc 0024) — each as a
	// revision. Targets are stored as ids; a rewritten link comes out
	// bundle-absolute whichever form it went in as, since that is the one
	// form a later move cannot reinterpret (0024 §3.5).
	for id, wantBody := range map[string]string{
		bare:    "Explains [the metric](/" + dst + ".md).",
		sibling: "Explains [the metric](/" + dst + ".md) further.",
	} {
		k, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if k.Body != wantBody {
			t.Errorf("%s body after move: %q, want %q", id, k.Body, wantBody)
		}
		if len(k.Links) != 1 || k.Links[0].Target != dst {
			t.Errorf("%s links after move: %+v, want target %s", id, k.Links, dst)
		}
		revs, err := s.ListRevisions(ctx, id, 10)
		if err != nil || len(revs) < 2 || revs[0].Change != "update" {
			t.Errorf("%s should carry an update revision for the rewrite: %+v, %v", id, revs, err)
		}
	}
	ka, err := s.Get(ctx, attrs)
	if err != nil {
		t.Fatal(err)
	}
	if ka.Attrs["model"] != dst {
		t.Errorf("attrs.model after move: %v", ka.Attrs["model"])
	}
	backlinks, err := s.ListByAddress(ctx, Filter{LinksTo: dst}, nil, 10)
	if err != nil || len(backlinks) != 2 {
		t.Errorf("backlinks after move: %d, %v", len(backlinks), err)
	}

	// Leave no live attachment behind: its bytes exist only in this
	// test's fake blob store, and export scans every live attachment.
	if err := s.DeleteFile(ctx, dst+"/chart.png", actor); err != nil {
		t.Fatalf("cleanup DeleteFile: %v", err)
	}
}

// Create over a soft-deleted entry revives it — otherwise the ID is dead
// forever (the deleted row still owns the primary key while Update
// refuses deleted rows). Live entries still conflict, and a tombstone
// carrying a ruling is refused on the surfaces that ask for that
// (design doc 0135).
func TestIntegrationCreateRevivesSoftDeleted(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-revive-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	first := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revive-me", Title: "first life",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, first, false); err != nil {
		t.Fatal(err)
	}

	// Live entry: create must still conflict.
	dup := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revive-me", Title: "imposter",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, dup, false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("create over a live entry = %v, want ErrAlreadyExists", err)
	}

	if err := s.SoftDelete(ctx, first.ID, actor, nil, ""); err != nil {
		t.Fatal(err)
	}

	// Soft-deleted entry: create revives it with the new content.
	second := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revive-me", Title: "second life",
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "agent", Name: "claude-code"},
	}
	if err := s.Create(ctx, second, false); err != nil {
		t.Fatalf("create over a soft-deleted entry: %v", err)
	}
	got, err := s.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("revived entry not readable: %v", err)
	}
	if got.Title != "second life" || got.CreatedBy.Name != "claude-code" {
		t.Errorf("revived entry = %+v, want the new content and creator", got)
	}

	// The full lineage stays: create, delete, create.
	var changes []string
	rows, err := s.pool.Query(ctx,
		`SELECT change FROM knowledge_revision WHERE id=$1 ORDER BY rev`, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, c)
	}
	want := []string{"create", "delete", "create"}
	if len(changes) != len(want) {
		t.Fatalf("revisions = %v, want %v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("revisions = %v, want %v", changes, want)
		}
	}

	// A rejected entry leaves an ordinary tombstone: the reason it was
	// turned down is on the revision, and it gates nothing (design doc
	// 0135). Even the guarded create — the surface with no precondition —
	// revives it, because the id stays open to the next writer.
	rejected := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revive-no", Title: "rejected",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, rejected, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, rejected.ID, actor, nil, "duplicate"); err != nil {
		t.Fatal(err)
	}
	again := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revive-no", Title: "try again",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, again, true); err != nil {
		t.Fatalf("a rejected id must stay open to the next writer: %v", err)
	}
	// And the reason is still readable, on the revision that removed it.
	revs, err := s.ListRevisions(ctx, again.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range revs {
		if r.Change == "reject" && r.Note == "duplicate" {
			found = true
		}
	}
	if !found {
		t.Errorf("the reason is not in the history: %+v", revs)
	}
}

// Attachments (design doc 0008): content-addressed blobs, replace-by-path,
// and disappearance from the entry's listing with the soft-deleted entry.
func TestIntegrationAttachments(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Attachment bytes live only in the blob store (design doc 0013).
	s.UseBlobStore(newFakeBlobStore())
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, del := range []string{
		`DELETE FROM attachment WHERE knowledge_id LIKE 'it-att%'`,
		// The files too — an entry's namespace outlives it (0046 §3.3).
		`DELETE FROM object WHERE id LIKE 'it-att%' OR starts_with(path, 'it-att')`,
		`DELETE FROM knowledge_revision WHERE id LIKE 'it-att%'`,
	} {
		if _, err := s.pool.Exec(ctx, del); err != nil {
			t.Fatal(err)
		}
	}
	// And again on the way out, inside the lock this test holds: the
	// rows would otherwise outlive it, and another package's
	// whole-bundle export would resolve this test's file against a blob
	// fake it does not have. Deferred rather than t.Cleanup so it runs
	// before the pool closes.
	defer func() {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM object WHERE id LIKE 'it-att%' OR starts_with(path, 'it-att')`); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	}()

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeInsights, ID: "it-att-reading", Title: "売上の読み方",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}

	path := k.ID + "/weekly.png"
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake image bytes")...)
	att, _, err := s.PutFile(ctx, path, "image/png", png, actor)
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if att.Size != int64(len(png)) || att.SHA256 == "" {
		t.Errorf("attachment metadata wrong: %+v", att)
	}

	got, data, err := s.GetFile(ctx, path)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if string(data) != string(png) || got.MediaType != "image/png" {
		t.Errorf("bytes did not round-trip: %+v", got)
	}

	// Replace by path: same entry, same path, new bytes.
	png2 := append([]byte("\x89PNG\r\n\x1a\n"), []byte("updated bytes")...)
	if _, _, err := s.PutFile(ctx, path, "image/png", png2, actor); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAttachments(ctx, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SHA256 == att.SHA256 {
		t.Errorf("replace should keep one attachment with new content: %+v", list)
	}

	// A replacement is a new blob row, not a mutated one — that is what
	// content addressing buys. The row this path names now must exist:
	// SweepBlobs only deletes what no object and no attachment
	// references, so a hash a live attachment names is the one thing it
	// may never take (sweep.go, design doc 0099).
	//
	// The replaced hash is deliberately not asserted on. It is orphaned
	// the moment the new bytes land, and the sweep is global — it does
	// not know about the namespace this test runs in, so any purge or
	// file delete in another package's tests, running against the shared
	// database, may legitimately reclaim it first. lockLiveAttachments
	// does not help: those tests sweep without taking it.
	var blobs int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM blob WHERE sha256 = $1`, list[0].SHA256).Scan(&blobs); err != nil {
		t.Fatal(err)
	}
	if blobs != 1 {
		t.Errorf("blob rows for the hash the attachment names = %d, want 1", blobs)
	}
	if list[0].SHA256 == att.SHA256 {
		t.Errorf("replacement reused the old hash: %s", att.SHA256)
	}

	if err := s.DeleteFile(ctx, path, actor); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if list, err := s.ListAttachments(ctx, k.ID); err != nil || len(list) != 0 {
		t.Errorf("attachments after delete = %+v, %v, want none", list, err)
	}

	// Attachments of soft-deleted entries drop out of the entry's listing,
	// even though the file object itself is untouched (0046 §3.3: it is
	// the entry's namespace that stops claiming it).
	if _, _, err := s.PutFile(ctx, path, "image/png", png, actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.ID, actor, nil, ""); err != nil {
		t.Fatal(err)
	}
	if list, err := s.ListAttachments(ctx, k.ID); err != nil || len(list) != 0 {
		t.Errorf("attachments of a deleted entry = %+v, %v, want none", list, err)
	}
}

// ListRevisions returns the full audit trail newest-first, including for
// soft-deleted entries; an entry that never existed is ErrNotFound.
func TestIntegrationListRevisions(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-revs-%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	k := &domain.Knowledge{
		Type: domain.TypeTerms, ID: "it-revs-1", Title: "v1",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	k.Title = "v2"
	if err := s.Update(ctx, k, actor, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, k.ID, actor, nil, ""); err != nil {
		t.Fatal(err)
	}

	revs, err := s.ListRevisions(ctx, k.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range revs {
		got = append(got, r.Change)
	}
	want := []string{"delete", "update", "create"} // newest first
	if len(got) != len(want) {
		t.Fatalf("revisions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("revisions = %v, want %v", got, want)
		}
	}
	if revs[0].Rev != 3 || revs[2].Rev != 1 {
		t.Errorf("revs = %d..%d, want 3..1", revs[0].Rev, revs[2].Rev)
	}
	// A revision is the document the entry was, so the title is read out
	// of the frontmatter rather than off a struct field (design doc 0043
	// §3.9).
	if !strings.Contains(revs[1].Document, "title: v2") || !strings.Contains(revs[2].Document, "title: v1") {
		t.Errorf("revision documents do not carry v2, v1:\n%s\n%s", revs[1].Document, revs[2].Document)
	}
	if revs[0].ChangedBy != actor {
		t.Errorf("changed_by = %+v, want %+v", revs[0].ChangedBy, actor)
	}

	if _, err := s.ListRevisions(ctx, "it-revs-never-existed", 50); !errors.Is(err, ErrNotFound) {
		t.Errorf("revisions of a nonexistent entry = %v, want ErrNotFound", err)
	}
}

// fakeBlobStore is an in-memory blob.Store for exercising the external
// blob path (design doc 0011) without GCS.
type fakeBlobStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newFakeBlobStore() *fakeBlobStore { return &fakeBlobStore{m: map[string][]byte{}} }

func (f *fakeBlobStore) Put(_ context.Context, sum, _ string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[sum]; ok {
		return nil // create-only, like GCS with DoesNotExist
	}
	f.m[sum] = append([]byte(nil), data...)
	return nil
}

func (f *fakeBlobStore) Get(_ context.Context, sum string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.m[sum]
	if !ok {
		return nil, fmt.Errorf("fake blob store: %s not found", sum)
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeBlobStore) Delete(_ context.Context, sum string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, sum) // already gone is success, like GCS
	return nil
}

func (f *fakeBlobStore) has(sum string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[sum]
	return ok
}

// search_text is maintained by trigger, so every write path keeps the
// haystack current without remembering to. These are the two paths a
// Go-side implementation would most easily miss: a file attached after
// the entry was written (design doc 0020), and an entry renamed into a
// new id (design doc 0022 put the id in the haystack).
func TestIntegrationSearchTextFollowsAttachmentsAndMoves(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL) // seeds.txt rides a fake blob store other packages' export scans can't read
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	s.UseBlobStore(newFakeBlobStore())
	actor := domain.Actor{Kind: "human", Name: "test"}

	searchText := func(id string) string {
		var got string
		if err := s.pool.QueryRow(ctx, `SELECT search_text FROM object WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read search_text for %s: %v", id, err)
		}
		return got
	}

	// Swept by token rather than by hand: the id-keyed delete this
	// replaces could not reach seeds.txt's revisions, because a file is
	// an object at its own path with no id of its own (design docs 0075
	// §5, 0100) — so they survived, and the next run against the same
	// database collided on knowledge_revision's (path, rev).
	run := testdb.Unique(t, "it-haystack-")
	id, moved := run, run+"-moved"
	k := &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: "haystack", Tags: []string{"ledger"},
		Status: domain.StatusDraft, CreatedBy: actor, Body: "prose",
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{id, "haystack", "ledger", "prose"} {
		if !strings.Contains(searchText(id), want) {
			t.Errorf("search_text misses %q after create: %q", want, searchText(id))
		}
	}

	if _, _, err := s.PutFile(ctx, id+"/seeds.txt", "text/plain", []byte("x"), actor); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchText(id), "seeds.txt") {
		t.Errorf("attaching a file left search_text stale: %q", searchText(id))
	}

	if _, err := s.Move(ctx, id, moved, actor, nil); err != nil {
		t.Fatal(err)
	}
	after := searchText(moved)
	if !strings.Contains(after, moved) {
		t.Errorf("move left the old id in search_text: %q", after)
	}
	if !strings.Contains(after, "seeds.txt") {
		t.Errorf("move dropped the attachment name from search_text: %q", after)
	}

	if err := s.DeleteFile(ctx, moved+"/seeds.txt", actor); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(searchText(moved), "seeds.txt") {
		t.Errorf("detaching left the filename in search_text: %q", searchText(moved))
	}
}

// Move onto an id held by a soft-deleted entry, and the purge that frees
// it. Create revives a tombstone in place (it brings no history of its
// own), but Move cannot: the arriving entry has revisions, and (id, rev)
// is a primary key. Without purge the id would be blocked forever — the
// exact outcome Create's own comment calls out as unacceptable.
func TestIntegrationPurgeFreesIDForMove(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	const src, dst = "it-purge-src", "it-purge-dst"
	for _, id := range []string{src, dst} {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
	}
	create := func(id string) {
		t.Helper()
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeTerms, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	create(src)
	create(dst)

	// A live occupant is a plain conflict.
	if _, err := s.Move(ctx, src, dst, actor, nil); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("move onto a live entry: got %v, want ErrAlreadyExists", err)
	}

	if err := s.SoftDelete(ctx, dst, actor, nil, ""); err != nil {
		t.Fatal(err)
	}
	// Still taken — but the error must say so, or the caller goes looking
	// for an entry they cannot see.
	_, err = s.Move(ctx, src, dst, actor, nil)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("move onto a tombstone: got %v, want ErrAlreadyExists", err)
	}
	if !strings.Contains(err.Error(), "purge") {
		t.Errorf("error does not point at the way out: %v", err)
	}

	// Purging a live entry is refused: delete first, purge second.
	if err := s.Purge(ctx, src, actor); !errors.Is(err, ErrNotDeleted) {
		t.Errorf("purge of a live entry: got %v, want ErrNotDeleted", err)
	}

	if err := s.Purge(ctx, dst, actor); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	// The one record a purge leaves: who destroyed what, and how much
	// history went with it. Everything else about the entry is gone, so
	// without this row nobody can answer "where did it go?".
	var purgedBy string
	var purgedRevs int
	if err := s.pool.QueryRow(ctx,
		`SELECT purged_by_kind || ':' || purged_by_name, revisions
		   FROM knowledge_purge WHERE id = $1 ORDER BY purged_at DESC LIMIT 1`, dst).
		Scan(&purgedBy, &purgedRevs); err != nil {
		t.Fatalf("purge left no audit row: %v", err)
	}
	if purgedBy != actor.Kind+":"+actor.Name {
		t.Errorf("audit row records %q, want %q", purgedBy, actor.Kind+":"+actor.Name)
	}
	if purgedRevs == 0 {
		t.Error("audit row lost the revision count")
	}
	var revs int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_revision WHERE id = $1`, dst).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	if revs != 0 {
		t.Errorf("purge left %d revisions behind", revs)
	}
	if err := s.Purge(ctx, dst, actor); !errors.Is(err, ErrNotFound) {
		t.Errorf("purge of a purged entry: got %v, want ErrNotFound", err)
	}

	// The id is free again.
	moved, err := s.Move(ctx, src, dst, actor, nil)
	if err != nil {
		t.Fatalf("move after purge: %v", err)
	}
	if moved.ID != dst {
		t.Errorf("moved entry id = %q, want %q", moved.ID, dst)
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, dst)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, dst)
}

// A purge and a revival can be in flight at the same time: Create brings a
// tombstone back in place, and the entry it produces is live knowledge
// somebody is now using. The purge must lose that race — it was authorized
// against a tombstone, and hard-deleting the revived entry would destroy in
// one call history that was in use a moment ago, which is exactly what the
// two-step delete-then-purge exists to prevent.
func TestIntegrationPurgeLosesToRevival(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	const id = "it-purge-race"
	cleanup := func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_purge WHERE id = $1`, id)
	}
	cleanup()
	defer cleanup()

	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: id,
		Status: domain.StatusDraft, CreatedBy: actor,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, id, actor, nil, ""); err != nil {
		t.Fatal(err)
	}

	// Stand in for a concurrent Create: it revives the tombstone in place
	// and holds the row until it commits.
	revive, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = revive.Rollback(ctx) }()
	if _, err := revive.Exec(ctx,
		`UPDATE object SET deleted_at = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	purged := make(chan error, 1)
	go func() { purged <- s.Purge(ctx, id, actor) }()

	// Wait for the purge to block on the revival's row lock. Without the
	// wait the purge might not have reached the row yet, and committing
	// first would test a serial order rather than the interleaving.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity
			  WHERE wait_event_type = 'Lock'
			    AND (query LIKE '%object%' OR query LIKE '%knowledge%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("purge never blocked on the revival's lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := revive.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-purged; !errors.Is(err, ErrNotDeleted) {
		t.Errorf("purge racing a revival: got %v, want ErrNotDeleted", err)
	}

	k, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("the revived entry is gone: %v", err)
	}
	if k.ID != id {
		t.Errorf("got entry %q, want %q", k.ID, id)
	}
	// An audit row would claim a destruction that must not have happened.
	var audits int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_purge WHERE id = $1`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Errorf("refused purge left %d audit rows", audits)
	}
}

// Close is the last thing a SIGTERM'd process does with the usage buffer:
// stop the flush loop, drain what is still in it. Events recorded in the
// seconds before shutdown — a whole request's worth on a Cloud Run
// instance scaling to zero — would otherwise never reach the database.
func TestIntegrationCloseFlushesBufferedUsage(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx, 0); err != nil {
		s.Close()
		t.Fatal(err)
	}
	const id = "it-flush-on-close"
	_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_usage WHERE knowledge_id = $1`, id)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_event WHERE knowledge_id = $1`, id)
	actor := domain.Actor{Kind: "human", Name: "test"}
	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: id, Title: "flush", Status: domain.StatusDraft, CreatedBy: actor,
	}, false); err != nil {
		s.Close()
		t.Fatal(err)
	}
	// Buffered, not written: the flush interval is far longer than this
	// test, so only Close can get it to the database.
	if err := s.RecordEvents(ctx, domain.EventFetched, actor, []string{id}); err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()

	verify, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	u, err := verify.Usage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Fetches != 1 {
		t.Errorf("fetches = %d after Close, want 1 — the buffer was dropped", u.Fetches)
	}
	_, _ = verify.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
	_, _ = verify.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
	_, _ = verify.pool.Exec(ctx, `DELETE FROM knowledge_usage WHERE knowledge_id = $1`, id)
	_, _ = verify.pool.Exec(ctx, `DELETE FROM knowledge_event WHERE knowledge_id = $1`, id)
}

// Delegated provenance survives the round trip through both the entry and
// its revisions (design doc 0027). The point of the column is that a
// reader can tell "tanaka, through InsightFlow" from "tanaka" — if Via
// were dropped on the way to the database, the two would be identical and
// the delegation would be a forgery.
func TestIntegrationDelegatedProvenance(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}

	const id = "it-delegated"
	_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
	via := "process:insightflow@example.iam.gserviceaccount.com"
	delegated := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp", Via: via}

	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeInsights, ID: id, Title: "delegated",
		Status: domain.StatusDraft, CreatedBy: delegated,
	}, false); err != nil {
		t.Fatal(err)
	}

	k, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if k.CreatedBy != delegated {
		t.Errorf("created_by = %+v, want %+v", k.CreatedBy, delegated)
	}
	if want := "human:tanaka@example.co.jp via " + via; k.CreatedBy.String() != want {
		t.Errorf("rendered = %q, want %q", k.CreatedBy.String(), want)
	}

	// A verification carries its own delegation, independent of creation.
	verifier := domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.co.jp"}
	k.Status = domain.StatusStable
	if err := s.Update(ctx, k, verifier, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, id, verifier); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy.Via != via {
		t.Errorf("update dropped created_by_via: %+v", got.CreatedBy)
	}
	last := got.LastVerified()
	if last == nil || last.By.Via != "" {
		t.Errorf("a verification must carry its own (absent) delegation: %+v", last)
	}

	revs, err := s.ListRevisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 {
		t.Fatal("no revisions")
	}
	create := revs[len(revs)-1]
	if create.ChangedBy.Via != via {
		t.Errorf("revision changed_by lost the delegation: %+v", create.ChangedBy)
	}

	_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
	_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
}

// get_context is documented to take a data question, and the recall hook
// feeds it a raw user prompt — so a question, not a keyword, is the
// flagship input. Matching the query as a whole cannot serve one: a
// question appears verbatim in no body, and trigram similarity between a
// short query and a long document is 0.01-0.1 (for Japanese, where the
// three-character windows of a question and a body rarely align, often
// exactly 0). Before fragment matching this search returned nothing at
// all for either language.
//
// Recall alone is not enough, though. Split a question into fragments and
// most of them are grammar — the particle windows ている / のか, the words
// why / is / down — which nearly every entry contains. Counting matches
// would rank an entry sharing three function words above the one sharing
// the actual subject. So this plants a corpus where the grammar really is
// common and checks the ordering, not just the presence.
func TestIntegrationLexicalSearchAnswersQuestions(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	// Registered rather than deferred, and before the fixture cleanup
	// below: a deferred Close runs before any t.Cleanup, which would leave
	// the delete swallowing "pool closed" and the corpus in the database
	// for every later run to search through. Cleanups are LIFO, so this
	// one runs last.
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	// The corpus is the fixture: the assertions are about how these 22
	// entries rank against each other, and a search over the whole table
	// answers a different question. A database that other tests have run
	// against holds hundreds of unrelated rows, and the English question —
	// four common words — matches enough of them to push the subject past
	// the limit, failing recall on a corpus the test never planted. So
	// every entry carries a run tag and every search is scoped to it,
	// which is what the rest of this file does (see the 0037 feeds).
	run := testdb.Unique(t, "it-q-")
	mine := Filter{Tags: []string{run}}

	target, decoy := run+"/target", run+"/decoy"
	ids := []string{target, decoy}
	for i := range 20 {
		ids = append(ids, fmt.Sprintf("%s/filler-%d", run, i))
	}
	clean := func() {
		for _, id := range ids {
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		}
	}
	t.Cleanup(clean)
	create := func(id, title, body string) {
		t.Helper()
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: title, Tags: []string{run},
			Status: domain.StatusDraft, CreatedBy: actor, Body: body,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// The target shares the subject and nothing else; the decoy and the
	// filler share the grammar and nothing else.
	create(target, "売上の読み方", "売上は前年同期比で見ないと誤読する。monthly revenue by quarter.")
	create(decoy, "棚卸の運用", "棚卸は毎月行っているのか、四半期ごとなのか。why is this down to the site?")
	for i := range 20 {
		create(fmt.Sprintf("%s/filler-%d", run, i), fmt.Sprintf("運用メモ %d", i),
			"これは毎日行っているのかを記録している。why is it down again, and is it down for long?")
	}

	rank := func(hits []domain.SearchHit, id string) int {
		for i, h := range hits {
			if h.ID == id {
				return i
			}
		}
		return -1
	}
	for _, tc := range []struct {
		query   string
		outrank bool // must the subject beat the grammar-only entry?
		why     string
	}{
		// Recall is the regression: whole-query matching found none of
		// these. Ranking is promised only where lexical search can
		// honestly deliver it.
		{"なぜ売上が下がっているのか", true, "Japanese question: similarity is 0; content windows carry it"},
		{"売上", true, "short Japanese keyword"},
		{"revenue", true, "English keyword"},
		// The decoy really does contain three of this question's four
		// words (why / is / down) against the subject's one. Ranking it
		// higher is what a bag of words does, not a defect to paper over:
		// English has no equivalent of the kana/kanji split to lean on.
		// Hybrid mode is the answer — which is why the README recommends
		// embeddings — so here we require only that the subject is found.
		{"why is revenue down", false, "English question: recall only"},
	} {
		hits, err := s.SearchLexical(ctx, tc.query, mine, 50)
		if err != nil {
			t.Fatalf("SearchLexical(%q): %v", tc.query, err)
		}
		got, other := rank(hits, target), rank(hits, decoy)
		if got < 0 {
			t.Errorf("SearchLexical(%q) did not find %s among %d hits (%s)",
				tc.query, target, len(hits), tc.why)
			continue
		}
		if tc.outrank && other >= 0 && other < got {
			t.Errorf("SearchLexical(%q) ranked the grammar-only entry above the subject (%s)",
				tc.query, tc.why)
		}
	}

	// A query that shares nothing must still come back empty: fragment
	// matching widens recall, it does not turn the floor off.
	hits, err := s.SearchLexical(ctx, "zzzznothingmatchesthiszzzz", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	if rank(hits, target) >= 0 || rank(hits, decoy) >= 0 {
		t.Errorf("unrelated query matched a planted entry (%d hits)", len(hits))
	}
}

// A keyword search is a search for a name. Ask for 売上 and the concept
// *called* 売上 is the answer; the twenty that mention it in a body are
// context, however often they say the word.
//
// Fragment scoring alone cannot tell those apart. The haystack is one
// column — id, title, description, tags, body, filenames concatenated —
// so an entry whose whole name is the query and an entry that says the
// word once in five kilobytes both contain every fragment and both
// contain the query verbatim: identical scores, and the order between
// them is whatever the scan happened to produce. This plants exactly that
// tie and requires the name to break it.
func TestIntegrationLexicalSearchRanksTheNamedConceptFirst(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	run := testdb.Unique(t, "it-name-")
	mine := Filter{Tags: []string{run}}
	// Two shapes of name, because an entry has two (design doc 0022): the
	// id's last segment when no title is set, and the title when one is.
	byID, byTitle := run+"/売上", run+"/cogs"
	titled := run + "/revenue-howto"
	ids := []string{byID, byTitle, titled}
	for i := range 20 {
		ids = append(ids, fmt.Sprintf("%s/filler-%d", run, i))
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		}
	})
	create := func(id, title, body string) {
		t.Helper()
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: title, Tags: []string{run},
			Status: domain.StatusDraft, CreatedBy: actor, Body: body,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// Written worst-first on purpose. Tied scores come back in whatever
	// order the scan produced, which is insertion order, so a fixture that
	// plants the answer first passes without ranking anything — this one
	// fails unless the score really separates them.
	//
	// The fillers say both words far more often than the definitions do,
	// which is also the point: frequency in a body is not aboutness.
	for i := range 20 {
		create(fmt.Sprintf("%s/filler-%d", run, i), fmt.Sprintf("月次レポート %d", i),
			strings.Repeat("今月の売上と原価の内訳、売上の推移、原価の推移。", 8))
	}
	create(titled, "売上の読み方", "売上は前年同期比で見ないと誤読する。原価と併せて読む。")
	create(byID, "", "全社の売上をどう数えるかの定義。")
	create(byTitle, "原価", "原価に何を含めるかの定義。")

	rank := func(hits []domain.SearchHit, id string) int {
		for i, h := range hits {
			if h.ID == id {
				return i
			}
		}
		return -1
	}
	for _, tc := range []struct{ query, first, why string }{
		{"売上", byID, "the id's last segment is the concept's name"},
		{"原価", byTitle, "the title is the concept's name"},
	} {
		hits, err := s.SearchLexical(ctx, tc.query, mine, 50)
		if err != nil {
			t.Fatalf("SearchLexical(%q): %v", tc.query, err)
		}
		if len(hits) == 0 || hits[0].ID != tc.first {
			got := "nothing"
			if len(hits) > 0 {
				got = hits[0].ID
			}
			t.Errorf("SearchLexical(%q) ranked %s first, want %s (%s)",
				tc.query, got, tc.first, tc.why)
		}
	}

	// A name the query only partly matches is still a name: "売上の読み方"
	// is more about 売上 than a report that lists the word eight times.
	hits, err := s.SearchLexical(ctx, "売上", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		filler := fmt.Sprintf("%s/filler-%d", run, i)
		if got, other := rank(hits, titled), rank(hits, filler); other >= 0 && other < got {
			t.Fatalf("SearchLexical(売上) ranked %s (mentions only) above %s (named)", filler, titled)
		}
	}
}

// A compound question — 社内用語 chained by の, the input the recall hook
// actually feeds get_context — used to hand the whole ranking to whichever
// prose co-mentioned the most of its terms: the score is fragment
// coverage, a meeting memo saying all three terms covers everything, and
// each term's definition covers only its own third. The name rule now
// reads the query's terms (QueryTerms), so the definitions a question
// names outrank the prose that merely says its words.
func TestIntegrationLexicalSearchLiftsWhatACompoundQueryNames(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	run := testdb.Unique(t, "it-compound-")
	mine := Filter{Tags: []string{run}}
	// The three names a compound question chains: one by title, one by
	// filename, one mixed-script (design doc 0022 for the two names).
	ecJigyo, kaiin, keizoku := run+"/EC事業", run+"/アクティブ会員", run+"/keizoku"
	definitions := []string{ecJigyo, kaiin, keizoku}
	ids := append([]string{}, definitions...)
	for i := range 6 {
		ids = append(ids, fmt.Sprintf("%s/memo-%d", run, i))
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		}
	})
	create := func(id, title, body string) {
		t.Helper()
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: title, Tags: []string{run},
			Status: domain.StatusDraft, CreatedBy: actor, Body: body,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// Worst-first, as above: the memos say every term the question asks
	// for — and 計算, which no definition says — so coverage alone scores
	// them near 1.0 and each definition near a third of that.
	for i := range 6 {
		create(fmt.Sprintf("%s/memo-%d", run, i), fmt.Sprintf("週次定例メモ %d", i),
			"EC事業のアクティブ会員は微増、継続率は目標圏内で推移。"+
				"アクティブ会員の獲得施策は継続、EC事業の継続率が下がったら見直す。"+
				"来週も同じ枠で計算して報告する。")
	}
	create(keizoku, "継続率", "前月アクティブだった会員のうち当月もアクティブである会員の割合。")
	create(kaiin, "", "集計日から30日以内にログインした会員と定義する。")
	create(ecJigyo, "", "自社ECサイト経由の受注だけを集計するセグメント。")

	hits, err := s.SearchLexical(ctx, "EC事業のアクティブ会員の継続率を計算して", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	rank := func(id string) int {
		for i, h := range hits {
			if h.ID == id {
				return i
			}
		}
		return -1
	}
	for _, def := range definitions {
		for i := range 6 {
			memo := fmt.Sprintf("%s/memo-%d", run, i)
			if d, m := rank(def), rank(memo); d < 0 || (m >= 0 && m < d) {
				t.Errorf("SearchLexical(compound) ranked %s (co-mentions every term) "+
					"above %s (named by one)", memo, def)
			}
		}
	}
}

// Verify is the exit from both review feeds (design doc 0025 §6). It
// stamps a fresh verified_at even when the entry is already verified —
// which Update cannot do — and an entry whose last failure report predates
// that stamp leaves the re-verification feed.
func TestIntegrationVerifyClearsTheReviewFeed(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	const id = "it-verify-feed"
	for _, table := range []string{"object", "knowledge_revision", "knowledge_verification"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"knowledge_event", "knowledge_usage"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE knowledge_id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer@example.com"}
	agent := domain.Actor{Kind: domain.ActorProcess, Name: "claude-code"}
	k := &domain.Knowledge{Type: domain.TypeComputations, ID: id, Title: "月次売上",
		Status: domain.StatusDraft, CreatedBy: agent}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}

	verified, err := s.Verify(ctx, id, human)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The ledger gained a row; the document did not move. Confirming an
	// entry and publishing it are different acts (design doc 0043 §3.2).
	if len(verified.Verifications) != 1 {
		t.Fatalf("Verify did not append: %+v", verified.Verifications)
	}
	if verified.Status != domain.StatusDraft {
		t.Errorf("Verify moved the lifecycle status to %q; it must leave the document alone", verified.Status)
	}
	first := verified.LastVerified().At

	// An agent runs the query, gets a wrong number, and says so. The entry
	// is now in the re-verification feed.
	if err := s.RecordOutcome(ctx, domain.EventFailed, agent, id, "returned last month"); err != nil {
		t.Fatal(err)
	}
	inFeed := func() bool {
		hits, err := s.ListByFailed(ctx, Filter{}, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(hits, func(h domain.SearchHit) bool { return h.ID == id })
	}
	if !inFeed() {
		t.Fatal("a failed report should put the entry in the re-verification feed")
	}

	// An Update that changes nothing cannot answer the report: it writes
	// nothing at all, which is exactly why Verify exists.
	same := *verified
	if err := s.Update(ctx, &same, human, nil); err != nil {
		t.Fatal(err)
	}
	if !inFeed() {
		t.Error("an update must not silently clear the feed")
	}

	// Re-verifying does.
	again, err := s.Verify(ctx, id, human)
	if err != nil {
		t.Fatalf("re-Verify: %v", err)
	}
	// Re-verifying appends rather than overwriting: the record answers
	// "how often, and by whom", not just "when last".
	if len(again.Verifications) != 2 {
		t.Fatalf("re-verification replaced the ledger instead of appending: %+v", again.Verifications)
	}
	if !again.LastVerified().At.After(first) {
		t.Fatalf("re-verification did not move the newest verification: %v -> %v", first, again.LastVerified().At)
	}
	if inFeed() {
		t.Error("an entry verified after its last failure report must leave the feed")
	}
	// The lifetime total is untouched: presence in the feed says
	// "unresolved", the count says "how bad it has been".
	if u, err := s.Usage(ctx, id); err != nil {
		t.Fatal(err)
	} else if u.Failed != 1 {
		t.Errorf("verification must not rewrite usage totals: failed=%d, want 1", u.Failed)
	}

	// A fresh failure brings it back.
	if err := s.RecordOutcome(ctx, domain.EventFailed, agent, id, "still wrong"); err != nil {
		t.Fatal(err)
	}
	if !inFeed() {
		t.Error("a failure newer than the verification must reopen the entry")
	}

	revs, err := s.ListRevisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 || revs[0].Change != "verify" {
		t.Errorf("verification must be recorded as a revision, got %+v", revs)
	}
}

// A move rewrites the links that point at the moved entry — and must also
// keep the moved entry's own outbound links pointing where they pointed.
// Relative targets are resolved against the entry's id, so changing the
// directory silently re-aims them (design doc 0024 §3.5).
func TestIntegrationMoveKeepsOutboundRelativeLinks(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-relmove%'`); err != nil {
			t.Fatal(err)
		}
	}

	actor := domain.Actor{Kind: "human", Name: "test"}
	const neighbour = "it-relmove-src/gross"
	const mover = "it-relmove-src/revenue"
	const dest = "it-relmove-dst/revenue"
	for _, k := range []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: neighbour, Title: "gross", Status: domain.StatusDraft, CreatedBy: actor},
		{Type: domain.TypeMetrics, ID: mover, Title: "revenue", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "Net of [gross](./gross.md).",
			Links: []domain.Link{{Target: neighbour, Text: "gross"}}},
	} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"object", "knowledge_revision"} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-relmove%'`)
		}
	})

	if _, err := s.Move(ctx, mover, dest, actor, nil); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Get(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved.Links) != 1 || moved.Links[0].Target != neighbour {
		t.Errorf("the moved entry's edge moved with it: %+v", moved.Links)
	}
	// The stored links column and what the body says must agree: the
	// column is a derivation, and the next write re-derives it.
	derived := domain.LinksFromBody(moved.ID, moved.Body)
	if len(derived) != 1 || derived[0].Target != neighbour {
		t.Errorf("body re-derives to %+v, want a link to %s (body: %q)", derived, neighbour, moved.Body)
	}
	// Backlinks are the same edge read the other way round.
	back, err := s.ListByAddress(ctx, Filter{LinksTo: neighbour}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].ID != dest {
		t.Errorf("backlinks from %s = %+v, want the moved entry", neighbour, back)
	}
}

// A move that rewrites the moved entry's own body — relative links
// absolutized because the directory changed (design doc 0024 §3.5) — is
// still one logical operation. The value Move returns, the stored row,
// and the entry's terminal "move" revision must all carry the same final
// body, links, attrs, and timestamp; the moved entry gets no separate
// "update" revision (only its referrers do). This is the regression test
// for Move returning the pre-rewrite body with a stale updated_at.
func TestIntegrationMoveSelfRewriteIsOneRevision(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-selfmove%'`); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for _, table := range []string{"object", "knowledge_revision"} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-selfmove%'`)
		}
	}()

	actor := domain.Actor{Kind: "human", Name: "test"}
	const neighbour = "it-selfmove-src/gross"
	const mover = "it-selfmove-src/revenue"
	const referrer = "it-selfmove-src/note"
	const dest = "it-selfmove-dst/revenue"
	for _, k := range []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: neighbour, Title: "gross", Status: domain.StatusDraft, CreatedBy: actor},
		{Type: domain.TypeMetrics, ID: mover, Title: "revenue", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "Net of [gross](./gross.md).",
			Links: []domain.Link{{Target: neighbour, Text: "gross"}}},
		{Type: domain.TypeInsights, ID: referrer, Title: "note", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "See [revenue](./revenue.md).",
			Links: []domain.Link{{Target: mover, Text: "revenue"}}},
	} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
	}

	returned, err := s.Move(ctx, mover, dest, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.Get(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}

	// (1) The return value is the stored row: the rewrite that the move
	// performed on the entry's own body is visible in what Move hands back.
	if !strings.Contains(stored.Body, "/"+neighbour+".md") {
		t.Fatalf("the move did not absolutize the entry's own relative link: %q", stored.Body)
	}
	if returned.Body != stored.Body {
		t.Errorf("Move returned body %q, stored body is %q", returned.Body, stored.Body)
	}
	if !returned.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Errorf("Move returned updated_at %v, stored row has %v — the returned value cannot serve as If-Match", returned.UpdatedAt, stored.UpdatedAt)
	}
	if len(returned.Links) != 1 || returned.Links[0].Target != neighbour {
		t.Errorf("Move returned links %+v, want the re-derived link to %s", returned.Links, neighbour)
	}

	// (2) One operation, one revision: history is create then move, the
	// move snapshot is the stored row, and nothing trails it.
	revs, err := s.ListRevisions(ctx, dest, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Change != "move" || revs[1].Change != "create" {
		changes := make([]string, len(revs))
		for i, r := range revs {
			changes[i] = r.Change
		}
		t.Fatalf("revisions (newest first) = %v, want [move create] — the moved entry must not get its own trailing \"update\"", changes)
	}
	if !strings.Contains(revs[0].Document, stored.Body) {
		t.Errorf("move revision document contradicts the stored row %q:\n%s", stored.Body, revs[0].Document)
	}
	if want := stored.UpdatedAt.UTC().Format(time.RFC3339); !strings.Contains(revs[0].Document, want) {
		t.Errorf("move revision document does not carry generated.at %s:\n%s", want, revs[0].Document)
	}
	if revs[0].ChangedAt.Before(revs[1].ChangedAt) {
		t.Errorf("revision changed_at runs backwards: rev %d at %v, rev %d at %v",
			revs[1].Rev, revs[1].ChangedAt, revs[0].Rev, revs[0].ChangedAt)
	}

	// (3) Referrers keep their own "update" revision — their rewrite is a
	// change to them, distinct from the move.
	refRevs, err := s.ListRevisions(ctx, referrer, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(refRevs) != 2 || refRevs[0].Change != "update" {
		t.Fatalf("referrer revisions = %+v, want an \"update\" on top of \"create\"", refRevs)
	}
	if !strings.Contains(refRevs[0].Document, "/"+dest+".md") {
		t.Errorf("referrer's update revision still points at the old id:\n%s", refRevs[0].Document)
	}
}

// An export is streamed, so its reads happen at different moments. The
// snapshot is what keeps the archive a picture of one instant: an entry
// deleted after the id list was taken still has to be in the bundle the
// index promises, or the archive contradicts itself.
func TestIntegrationExportSnapshotIsConsistent(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	const doomed = "it-exportsnap/doomed"
	for _, table := range []string{"object", "knowledge_revision"} {
		if _, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-exportsnap%'`); err != nil {
			t.Fatal(err)
		}
	}
	actor := domain.Actor{Kind: "human", Name: "test"}
	if err := s.Create(ctx, &domain.Knowledge{Type: domain.TypeInsights, ID: doomed,
		Title: "deleted mid-export", Status: domain.StatusDraft, CreatedBy: actor}, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"object", "knowledge_revision"} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE id LIKE 'it-exportsnap%'`)
		}
	})

	snap, err := s.BeginExport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close(ctx)
	rows, err := snap.IndexRows(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(rows, func(k domain.Knowledge) bool { return k.ID == doomed }) {
		t.Fatalf("%s missing from the index rows", doomed)
	}

	// The world moves on while the archive is being written.
	if err := s.SoftDelete(ctx, doomed, actor, nil, ""); err != nil {
		t.Fatal(err)
	}

	entries, err := snap.ListByIDs(ctx, []string{doomed})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != doomed {
		t.Errorf("the entry the index promised is missing from the bundle: %+v", entries)
	}
	// And the snapshot is only the export's: the live database has moved.
	if _, err := s.Get(ctx, doomed); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

// The curated-tombstone guard lives in the create transaction, not only
// in the service pre-check (design doc 0015 §3.1): a ruling that lands
// after the check must beat the create, not be erased by it. Reviving a
// draft tombstone stays allowed either way, and the unguarded call — the
// human surfaces — still revives anything.
func TestIntegrationCreateKeepsCuratedTombstones(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := t.Context()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	// One of the two rulings is a ledger, so the guard has to read it: a
	// ruled-on tombstone is a draft as far as the status column knows
	// (design doc 0043 §3.2), and a guard that only compared statuses
	// would quietly stop protecting it. A rejection is not among them —
	// it is the delete, and it leaves the id open (design doc 0135),
	// which the note case below is the test for.
	tombstone := func(t *testing.T, id string, rule func(id string), rejectNote string) {
		t.Helper()
		k := &domain.Knowledge{Type: domain.TypeTerms, ID: id, Title: id,
			Status: domain.StatusDraft, CreatedBy: actor}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatal(err)
		}
		rule(id)
		if err := s.SoftDelete(ctx, id, actor, nil, rejectNote); err != nil {
			t.Fatal(err)
		}
	}
	revive := func(id string) *domain.Knowledge {
		return &domain.Knowledge{Type: domain.TypeTerms, ID: id, Title: "revived",
			Status: domain.StatusDraft, CreatedBy: actor}
	}

	for _, tc := range []struct {
		ruling domain.Ruling
		rule   func(id string)
		note   string
	}{
		{domain.RulingVerified, func(id string) {
			if _, err := s.Verify(ctx, id, actor); err != nil {
				t.Fatal(err)
			}
		}, ""},
		{domain.RulingDeprecated, func(id string) {
			k, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			k.Status, k.StatusNote = domain.StatusDeprecated, "why"
			if err := s.Update(ctx, k, actor, nil); err != nil {
				t.Fatal(err)
			}
		}, ""},
	} {
		t.Run("guarded create refuses a "+string(tc.ruling)+" tombstone", func(t *testing.T) {
			id := testdb.Unique(t, "it-tomb-"+string(tc.ruling)+"-")
			tombstone(t, id, tc.rule, tc.note)
			if err := s.Create(ctx, revive(id), true); !errors.Is(err, ErrCuratedTombstone) {
				t.Fatalf("create = %v, want ErrCuratedTombstone", err)
			}
			// The ruling is intact: nothing was revived under it.
			old, err := s.GetTombstone(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if old.Ruling() != tc.ruling {
				t.Errorf("tombstone ruling = %q, want %q", old.Ruling(), tc.ruling)
			}
			// The human surfaces still revive it.
			if err := s.Create(ctx, revive(id), false); err != nil {
				t.Errorf("unguarded create must still revive: %v", err)
			}
			// A create is a new entry, so the old rulings do not carry
			// over onto content nobody judged.
			back, err := s.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if back.Curated() {
				t.Errorf("a revived entry kept a ruling: %+v", back.Ruling())
			}
		})
	}

	t.Run("guarded create still revives a draft tombstone", func(t *testing.T) {
		id := testdb.Unique(t, "it-tomb-draft-")
		tombstone(t, id, func(string) {}, "")
		if err := s.Create(ctx, revive(id), true); err != nil {
			t.Fatalf("draft tombstone must stay revivable: %v", err)
		}
	})

	t.Run("guarded create over a live entry is still ErrAlreadyExists", func(t *testing.T) {
		id := testdb.Unique(t, "it-tomb-live-")
		if err := s.Create(ctx, revive(id), false); err != nil {
			t.Fatal(err)
		}
		if err := s.Create(ctx, revive(id), true); !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("create over a live entry = %v, want ErrAlreadyExists", err)
		}
	})
}

// A move rewrites every entry that referred to the old id, and it does so
// by reading the referrer, repairing its body in Go, and writing the whole
// row back. Without a lock on that read, an edit committing in the window
// is reverted by the write-back -- and recorded as an ordinary "update"
// revision, so the history shows a change but not that one was lost.
func TestIntegrationMoveDoesNotClobberAConcurrentEdit(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	run := testdb.Unique(t, "it-clobber-")
	target, moved, referrer := run+"/metric", run+"/renamed", run+"/insight"

	for _, k := range []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: target, Title: "target", Status: domain.StatusDraft, CreatedBy: actor},
		{Type: domain.TypeInsights, ID: referrer, Title: "referrer", Status: domain.StatusDraft, CreatedBy: actor,
			Body:  "Explains [the metric](/" + target + ".md).",
			Links: domain.LinksFromBody(referrer, "Explains [the metric](/"+target+".md).")},
	} {
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatalf("create %s: %v", k.ID, err)
		}
	}

	// A human edit to the referrer, committed while the move is in flight.
	edit, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = edit.Rollback(ctx) }()
	const added = "A sentence the move must not eat."
	if _, err := edit.Exec(ctx,
		`UPDATE object SET body = body || $2, updated_at = now() WHERE id = $1`,
		referrer, "\n\n"+added); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.Move(ctx, target, moved, actor, nil)
		done <- err
	}()

	// Let the move reach the referrer and block there.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM pg_stat_activity
			  WHERE wait_event_type = 'Lock'
			    AND (query LIKE '%object%' OR query LIKE '%knowledge%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("move finished without blocking: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("move never blocked on the concurrent edit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := edit.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Move: %v", err)
	}

	got, err := s.Get(ctx, referrer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, added) {
		t.Errorf("the move reverted a concurrent edit; body = %q", got.Body)
	}
	// And it still did its own job: the reference follows the move.
	if !strings.Contains(got.Body, moved) {
		t.Errorf("reference was not rewritten to %s; body = %q", moved, got.Body)
	}
}

// The two lookups design doc 0037 adds: the feed of entries past the
// expiry their author declared, and the reverse of sources[].resource.
//
// Every query here is narrowed by a tag unique to this run. The test
// database is shared (see hitScore above), and both of these are listing
// modes over the whole corpus — without the tag, another package's stale
// entry is indistinguishable from a bug here.
func TestStaleFeedAndSourceLookupIntegration(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}

	run := testdb.Unique(t, "it37-")
	mine := Filter{Tags: []string{run}}
	actor := domain.Actor{Kind: "human", Name: "test"}
	policy := "https://wiki.example/" + run + "/revenue-recognition"
	other := "https://wiki.example/" + run + "/close-calendar"

	mk := func(name, staleAfter string, sources ...domain.Source) string {
		id := run + "/" + name
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: name, Body: "b", Tags: []string{run},
			Status: domain.StatusDraft, StaleAfter: staleAfter, Sources: sources,
			CreatedBy: actor, UpdatedBy: actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return id
	}
	ids := func(entries []domain.Knowledge) []string {
		out := make([]string, len(entries))
		for i, k := range entries {
			out[i] = k.ID
		}
		return out
	}

	now := time.Now().UTC()
	longAgo := now.AddDate(0, 0, -30).Format(domain.DateLayout)
	yesterday := now.AddDate(0, 0, -1).Format(domain.DateLayout)
	tomorrow := now.AddDate(0, 0, 1).Format(domain.DateLayout)

	overdue := mk("overdue", longAgo, domain.Source{Resource: policy})
	justPast := mk("just-past", yesterday, domain.Source{Resource: policy, ID: "rev"})
	today := mk("today", now.Format(domain.DateLayout))
	mk("future", tomorrow, domain.Source{Resource: policy})
	mk("undated", "", domain.Source{Resource: other})

	// Most overdue first. The future declaration and the undated entry are
	// absent: a date that has not come due is not work (0037 §2.1), and
	// today counts as stale because the rule is today >= stale_after.
	stale, err := s.ListByStaleAfter(ctx, mine, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{overdue, justPast, today}; !slices.Equal(ids(stale), want) {
		t.Errorf("stale feed = %v, want %v", ids(stale), want)
	}

	// The reverse lookup, matched on resource and ordered by id.
	cited, err := s.ListByAddress(ctx, Filter{Tags: []string{run}, Source: policy}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{run + "/future", justPast, overdue}; !slices.Equal(ids(cited), want) {
		t.Errorf("source lookup = %v, want %v", ids(cited), want)
	}

	// Exact, on resource only: a prefix, a longer string, and the
	// per-document source id all match nothing (0037 §2.3).
	for _, miss := range []string{"https://wiki.example/" + run, policy + "#section", "rev"} {
		hits, err := s.ListByAddress(ctx, Filter{Tags: []string{run}, Source: miss}, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 0 {
			t.Errorf("source %q matched %v; matching is exact on resource", miss, ids(hits))
		}
	}

	// The two compose: "cites this and is overdue" is one question.
	both, err := s.ListByStaleAfter(ctx, Filter{Tags: []string{run}, Source: policy}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{overdue, justPast}; !slices.Equal(ids(both), want) {
		t.Errorf("source + stale feed = %v, want %v", ids(both), want)
	}

	// A resource carrying a quote and a backslash cannot break out of the
	// jsonb literal the filter builds.
	odd := `https://x.test/a"b\c`
	quoted := mk("quoted", "", domain.Source{Resource: odd})
	hits, err := s.ListByAddress(ctx, Filter{Tags: []string{run}, Source: odd}, nil, 100)
	if err != nil {
		t.Fatalf("resource with a quote: %v", err)
	}
	if want := []string{quoted}; !slices.Equal(ids(hits), want) {
		t.Errorf("quoted resource lookup = %v, want %v", ids(hits), want)
	}
}

// backdateVerification writes a verification at an arbitrary time. Verify
// stamps the database clock on purpose (the failure feed compares the two
// and they must not disagree), so a feed test that needs an old or a
// staggered verification reaches past it.
func backdateVerification(t *testing.T, ctx context.Context, s *Store, id string, at time.Time) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO knowledge_verification (id, seq, by_kind, by_name, at)
		 VALUES ($1, COALESCE((SELECT MAX(seq) FROM knowledge_verification WHERE id=$1), 0) + 1,
		         'human', 'test', $2)`, id, at); err != nil {
		t.Fatal(err)
	}
}

// A move carries the rulings with the id (design doc 0021 moves an
// entry's whole record), and a purge takes them with everything else —
// otherwise a freed id would hand its next occupant a verification of
// content nobody verified.
func TestIntegrationMoveAndPurgeCarryTheLedgers(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	from := testdb.Unique(t, "it-ledger-move-")
	to := from + "-moved"

	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: from, Title: from, Status: domain.StatusDraft, CreatedBy: actor,
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, from, actor); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Move(ctx, from, to, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved.Verifications) != 1 {
		t.Errorf("move dropped the ledger: %+v", moved.Verifications)
	}

	// The rejection ledger is a tombstone's (design doc 0135), so this is
	// where a row lands in it — and the purge below has to reach it too.
	if err := s.SoftDelete(ctx, to, actor, nil, "on second thoughts"); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(ctx, to, actor); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"knowledge_verification"} {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE id = $1`, to).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("purge left %d row(s) in %s", n, table)
		}
	}
}

// The version is the content's, not the row's (design doc 0043 §3.4).
// This is the invariant the whole change turns on: until 0.16 the ETag
// was updated_at, so verifying an entry — or attaching a file to it —
// moved a version whose content nobody had touched, and an editor's held
// If-Match died for a reason unrelated to their edit.
func TestIntegrationContentHashMovesOnlyWithContent(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	id := testdb.Unique(t, "it-hash-")
	k := &domain.Knowledge{Type: domain.TypeTerms, ID: id, Title: "first",
		Status: domain.StatusDraft, Body: "One.", CreatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if k.ContentHash == "" {
		t.Fatal("create returned no version")
	}
	original := k.ContentHash

	for _, tc := range []struct {
		what string
		do   func() error
	}{
		{"verifying", func() error { _, err := s.Verify(ctx, id, actor); return err }},
	} {
		if err := tc.do(); err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		read, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if read.ContentHash != original {
			t.Errorf("%s moved the version %q -> %q; a ruling is not an edit",
				tc.what, original, read.ContentHash)
		}
	}

	// An edit does move it, and the old version stops matching.
	read, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	read.Body = "Two."
	if err := s.Update(ctx, read, actor, &original); err != nil {
		t.Fatalf("an update against the unchanged version must be accepted: %v", err)
	}
	if read.ContentHash == original {
		t.Error("an edit left the version where it was")
	}
	stale := original
	read.Body = "Three."
	if err := s.Update(ctx, read, actor, &stale); !errors.Is(err, ErrConflict) {
		t.Errorf("update with a stale version = %v, want ErrConflict", err)
	}

	// Two entries that say the same thing hash alike: the version is a
	// statement about content, not about which row holds it.
	twin := &domain.Knowledge{Type: domain.TypeTerms, ID: id + "-twin", Title: "first",
		Status: domain.StatusDraft, Body: "One.", CreatedBy: domain.Actor{Kind: domain.ActorProcess, Name: "other"}}
	if err := s.Create(ctx, twin, false); err != nil {
		t.Fatal(err)
	}
	if twin.ContentHash != original {
		t.Errorf("identical content hashed differently: %q vs %q", twin.ContentHash, original)
	}
}

// The stored document and the columns beside it are one value written
// twice: the columns are the index derived from the document (design doc
// 0043 §3.1), so re-rendering what a read returns must reproduce exactly
// what is stored. If these drift, the index is no longer derived.
func TestIntegrationStoredDocumentMatchesTheIndex(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	id := testdb.Unique(t, "it-storeddoc-")
	count := 7
	k := &domain.Knowledge{
		Type: domain.TypeComputations, ID: id, Title: "月次売上", Description: "説明",
		Resource: "bq://p.d.t", Tags: []string{"finance", "core"},
		Status: domain.StatusStable, StatusNote: "checked", StaleAfter: "2027-01-31",
		Runtime: "bigquery", Computation: "queries/rev.sql",
		Sources: []domain.Source{{Resource: "https://example.test/policy", ID: "pol",
			UsageCount: &count, LastModified: "2026-05-30"}},
		UsageWindow: &domain.UsageWindow{From: "2026-06-01", To: "2026-06-30"},
		Parameters:  []domain.Parameter{{Name: "year", Type: "integer", Required: true}},
		Executor:    &domain.Executor{Resource: "run.md", Receipt: []string{"job_id"}},
		Attester:    &domain.Attester{Resource: "check.py"},
		Attrs:       map[string]any{"question": "月次の売上は?", "owner": "finance"},
		Body:        "本文。[売上](/metrics/revenue.md) を見る。", CreatedBy: actor,
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id) })

	var stored string
	if err := s.pool.QueryRow(ctx, `SELECT doc FROM object WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	read, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// From the index columns alone: the entry was written as a struct, so
	// what is stored is the canonical rendering, and dropping the doc the
	// read carried is what keeps this a check on the index rather than a
	// comparison of the column with itself.
	fromIndex := *read
	fromIndex.Doc = ""
	rendered, hash, err := storedDoc(&fromIndex)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != stored {
		t.Errorf("the index no longer reproduces the stored document:\n--- stored ---\n%s\n--- from the index ---\n%s", stored, rendered)
	}
	if hash != read.ContentHash {
		t.Errorf("stored hash %q does not match the document's %q", read.ContentHash, hash)
	}
}

// A producer key inside a source or a parameter survives storage, which
// is the point of keeping it (design doc 0043 §3.6): the document is the
// stored form, so a key dropped on the way in is a key no later release
// can recover. It has to round-trip through the jsonb index columns as
// well as through the document.
func TestIntegrationProducerKeysInsideObjectsSurviveStorage(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	id := testdb.Unique(t, "it-extra-")
	k := &domain.Knowledge{
		Type: domain.TypeComputations, ID: id, Title: "extras", Runtime: "bigquery",
		Status: domain.StatusDraft, CreatedBy: actor,
		Sources: []domain.Source{{Resource: "policies/revenue.md",
			Extra: domain.Extra{"license": "CC-BY"}}},
		Parameters: []domain.Parameter{{Name: "year", Type: "integer",
			Extra: domain.Extra{"ui_hint": "dropdown"}}},
		Executor: &domain.Executor{Resource: "run.md", Receipt: []string{"job_id"},
			Extra: domain.Extra{"timeout_s": float64(300)}},
		Attester: &domain.Attester{Resource: "check.py",
			Extra: domain.Extra{"language": "python"}},
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id) })

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sources[0].Extra["license"] != "CC-BY" || got.Parameters[0].Extra["ui_hint"] != "dropdown" {
		t.Errorf("producer keys lost: %+v %+v", got.Sources[0].Extra, got.Parameters[0].Extra)
	}
	if got.Executor.Extra["timeout_s"] != float64(300) || got.Attester.Extra["language"] != "python" {
		t.Errorf("contract producer keys lost: %+v %+v", got.Executor.Extra, got.Attester.Extra)
	}
	// The stored document carries them too, inline — and a write that
	// changes nothing else is still a no-op, so they are part of the
	// content the hash is over.
	var doc string
	if err := s.pool.QueryRow(ctx, `SELECT doc FROM object WHERE id = $1`, id).Scan(&doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "license: CC-BY") {
		t.Errorf("the stored document dropped a producer key:\n%s", doc)
	}
	before := got.ContentHash
	got.Sources[0].Extra["license"] = "MIT"
	if err := s.Update(ctx, got, actor, nil); err != nil {
		t.Fatal(err)
	}
	if got.ContentHash == before {
		t.Error("changing a producer key left the version where it was; it is content")
	}
}

// The frontmatter index is what makes the query surface additive: a key
// needs no column of its own to be askable, so the day OKF adds one it
// is queryable with no migration (design doc 0046 §3.11). Which keys a
// caller may name is the service's question, not the store's (design doc
// 0047 §2.3) — the index answers any of them, and the test asks with keys
// deliberately outside every vocabulary the program knows, to pin that.
func TestIntegrationFrontmatterFilter(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	prefix := testdb.Unique(t, "it-fm-")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}

	// Written as documents, because the index is derived from the
	// document and not from the fields beside it.
	docs := map[string]string{
		"owned": "---\ntype: Metric\nowner: finance\nsystems:\n  - dbt\n  - looker\n" +
			// YAML types these: a boolean, a number and a list of numbers,
			// none of which is text by the time it reaches the index.
			"required: true\nusage_count: 5\nports:\n  - 80\n  - 443\n---\n\n本文。\n",
		"other":  "---\ntype: Metric\nowner: growth\nrequired: false\nusage_count: 12\n---\n\n本文。\n",
		"quoted": "---\ntype: Metric\nowner: ops\nrequired: \"true\"\ncode: \"007\"\n---\n\n本文。\n",
		"silent": "---\ntype: Metric\n---\n\n本文。\n",
	}
	for name, text := range docs {
		d, _, err := okf.Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		d.ID = prefix + "/" + name
		d.CreatedBy, d.UpdatedBy = actor, actor
		// What the service does for a document that names no status: the
		// index column holds what a reader reads (design doc 0046 §3.9).
		d.Status = d.Lifecycle()
		if err := s.Create(ctx, &d.Knowledge, false); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, d.ID) })
	}

	find := func(fm map[string]string) []string {
		t.Helper()
		// Any listing takes the same filter; the verification-age feed is
		// the one that needs no query and no other precondition.
		hits, err := s.ListByVerifiedAt(ctx, Filter{Prefixes: []string{prefix}, Frontmatter: fm}, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(hits))
		for i := range hits {
			ids[i] = strings.TrimPrefix(hits[i].ID, prefix+"/")
		}
		sort.Strings(ids)
		return ids
	}

	for _, tc := range []struct {
		name string
		fm   map[string]string
		want []string
	}{
		{"a scalar key ochakai does not know", map[string]string{"owner": "finance"}, []string{"owned"}},
		{"a member of a list", map[string]string{"systems": "dbt"}, []string{"owned"}},
		{"a value nobody wrote", map[string]string{"owner": "nobody"}, nil},
		{"a key nobody wrote", map[string]string{"unheard-of": "x"}, nil},
		{"two keys are AND-ed", map[string]string{"owner": "finance", "systems": "looker"}, []string{"owned"}},
		{"one of them missing excludes the entry", map[string]string{"owner": "finance", "systems": "airflow"}, nil},
		// The store indexes every key alike and answers for any of them.
		// Refusing the five ochakai reads through a column of its own is
		// the service's job (design doc 0047), not this layer's: the
		// filter is the mechanism, and where a caller may point it is a
		// contract about the query surface.
		{"a key the spec does define, indexed like any other", map[string]string{"type": "Metric"},
			[]string{"other", "owned", "quoted", "silent"}},
		// A boolean and a number are askable in the spelling the document
		// used, without the caller knowing how YAML typed it. Text is the
		// only thing a query string can carry, so the filter tries the
		// typed reading beside it.
		{"a boolean key", map[string]string{"required": "true"}, []string{"owned", "quoted"}},
		{"the other boolean", map[string]string{"required": "false"}, []string{"other"}},
		{"a number key", map[string]string{"usage_count": "5"}, []string{"owned"}},
		{"a number nobody wrote", map[string]string{"usage_count": "6"}, nil},
		{"a member of a list of numbers", map[string]string{"ports": "443"}, []string{"owned"}},
		// The text reading does not go away: a producer who quoted the
		// value wrote a string, and the same question finds it. Nor does
		// it turn text that looks numeric into a number — "007" is a code.
		{"a quoted number stays text", map[string]string{"code": "007"}, []string{"quoted"}},
		{"a typed key still AND-s", map[string]string{"required": "true", "owner": "finance"},
			[]string{"owned"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := find(tc.fm); !slices.Equal(got, tc.want) {
				t.Errorf("fm=%v matched %v, want %v", tc.fm, got, tc.want)
			}
		})
	}
}

// A row is at the path its concept lives at, and stays there through a
// move (design doc 0046 §§2.1, 3.1). The path is the key of the bundle,
// so a write that set the id and left the path behind would put the
// entry at an address nothing resolves — and the revision ledger, which
// counts by path now, would start a second history for it.
func TestIntegrationObjectIsKeyedByBundlePath(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "t"}
	id := testdb.Unique(t, "it-path-") + "/revenue"
	moved := id + "-moved"
	defer func() {
		for _, i := range []string{id, moved} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, i)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, i)
		}
	}()

	k := &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "売上",
		Status: domain.StatusDraft, Body: "受注合計。", CreatedBy: actor, UpdatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	pathOf := func(id string) string {
		t.Helper()
		var p string
		if err := s.pool.QueryRow(ctx, `SELECT path FROM object WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatalf("no row at id %s: %v", id, err)
		}
		return p
	}
	if got, want := pathOf(id), domain.ConceptPath(id); got != want {
		t.Errorf("created at path %q, want %q", got, want)
	}

	if _, err := s.Move(ctx, id, moved, actor, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := pathOf(moved), domain.ConceptPath(moved); got != want {
		t.Errorf("after the move the row is at %q, want %q", got, want)
	}
	// One history, carried to the new path rather than restarted there.
	var revs int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_revision WHERE path = $1`,
		domain.ConceptPath(moved)).Scan(&revs); err != nil {
		t.Fatal(err)
	}
	if revs != 2 { // create, move
		t.Errorf("revisions at the new path: got %d, want 2", revs)
	}
	var stale int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM knowledge_revision WHERE path = $1`,
		domain.ConceptPath(id)).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Errorf("%d revisions were left at the old path", stale)
	}
}

// A file is an object in the bundle, and which entry it belongs to is
// derived (design doc 0046 §§3.3, 3.13): from the path when it sits
// under the entry's own namespace, and from the body when the entry
// points at it. Both halves, and the two things that must not happen —
// a file showing up where entries are listed, and a file outliving the
// entry whose namespace it was in.
func TestIntegrationFilesAreObjectsAttributedByPathOrBody(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL) // live files, on a blob fake only this test can read
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.UseBlobStore(newFakeBlobStore())
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "t"}
	base := testdb.Unique(t, "it-files-")
	id := base + "/revenue"
	defer func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE path LIKE $1 || '%'`, base)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE path LIKE $1 || '%'`, base)
	}()

	// The body shows one file from the entry's own namespace and links
	// one that lives elsewhere in the bundle.
	body := fmt.Sprintf("![chart](revenue/chart.png)\n\n元データは [CSV](/%s/seeds/orders.csv)。\n", base)
	k := &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "売上",
		Status: domain.StatusDraft, Body: body, CreatedBy: actor, UpdatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutFile(ctx, id+"/chart.png", "image/png", []byte("png"), actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutFile(ctx, base+"/seeds/orders.csv", "text/plain", []byte("a,b\n"), actor); err != nil {
		t.Fatal(err)
	}

	atts, err := s.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 {
		t.Fatalf("attributed files = %+v, want the one under the namespace and the one the body links", atts)
	}
	// Each reports where it lives, which is what an export puts it back
	// as — the one under the namespace and the one elsewhere alike.
	byName := map[string]domain.File{}
	for _, a := range atts {
		byName[a.Name] = a
	}
	if got, want := byName["chart.png"].Path, id+"/chart.png"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := byName["orders.csv"].Path, base+"/seeds/orders.csv"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	// It is stored where the body says, not where the entry is.
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM object WHERE path = $1 AND id IS NULL`, base+"/seeds/orders.csv").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("the linked file is not at the path the body names")
	}

	// A file is not an entry. Nothing that lists or searches entries may
	// return one — the failure this guards against is silent.
	hits, err := s.SearchLexical(ctx, "chart", Filter{Prefixes: []string{base}}, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ID != id {
			t.Errorf("search returned a non-entry: %q", h.ID)
		}
	}
	lvl, err := s.Browse(ctx, base, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range lvl.Concepts {
		if e.ID != id {
			t.Errorf("browse listed a non-entry: %q", e.ID)
		}
	}
	for _, d := range lvl.Dirs {
		if d.Name == "seeds" && d.Count != 0 {
			t.Errorf("a directory of files counts as %d entries", d.Count)
		}
	}

	// Detaching takes the object with it, and the entry stops naming it.
	if err := s.DeleteFile(ctx, base+"/seeds/orders.csv", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM object WHERE path = $1`, base+"/seeds/orders.csv").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the file object outlived the detach")
	}
}

// Every actor a surface reads back carries its producer, through creation,
// update, verification, rejection and the revision ledger (design doc
// 0052 §3.4). The identity beside it never moves: the self-declaration is
// admissible precisely because the authenticated name stays in the row.
func TestIntegrationProducerRidesEveryActor(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}

	const id = "it-producer"
	cleanup := func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_verification WHERE id = $1`, id)
	}
	cleanup()
	defer cleanup()

	agent := domain.Actor{
		Kind: domain.ActorHuman, Name: "tanaka@example.co.jp",
		Via:      "process:insightflow@example.iam.gserviceaccount.com",
		Producer: "insightflow/1.4.0",
	}
	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeInsights, ID: id, Title: "producer",
		Status: domain.StatusDraft, CreatedBy: agent, UpdatedBy: agent,
	}, false); err != nil {
		t.Fatal(err)
	}

	k, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if k.CreatedBy != agent || k.UpdatedBy != agent {
		t.Errorf("created_by / updated_by = %+v / %+v, want %+v", k.CreatedBy, k.UpdatedBy, agent)
	}
	want := "human:tanaka@example.co.jp via process:insightflow@example.iam.gserviceaccount.com" +
		" using insightflow/1.4.0"
	if k.CreatedBy.String() != want {
		t.Errorf("rendered = %q, want %q", k.CreatedBy.String(), want)
	}

	// A later write declares its own producer, or none. The entry's
	// creation keeps saying what wrote it.
	editor := domain.Actor{Kind: domain.ActorProcess, Name: "sync@example.iam.gserviceaccount.com",
		Producer: "ochakai-cli/0.15.0"}
	k.Title = "producer, edited"
	k.UpdatedBy = editor // what the service sets when a write changes content
	if err := s.Update(ctx, k, editor, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy.Producer != agent.Producer {
		t.Errorf("update rewrote created_by_producer: %+v", got.CreatedBy)
	}
	if got.UpdatedBy.Producer != editor.Producer {
		t.Errorf("updated_by producer = %q, want %q", got.UpdatedBy.Producer, editor.Producer)
	}

	human := domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.co.jp"}
	if _, err := s.Verify(ctx, id, human); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// A person who declared nothing records nothing — the field is not
	// inherited from the entry or from an earlier write.
	if last := got.LastVerified(); last == nil || last.By.Producer != "" {
		t.Errorf("verification producer = %+v, want empty", last)
	}

	revs, err := s.ListRevisions(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 {
		t.Fatal("no revisions")
	}
	if create := revs[len(revs)-1]; create.ChangedBy.Producer != agent.Producer {
		t.Errorf("revision changed_by lost the producer: %+v", create.ChangedBy)
	}

	log, err := s.ListRevisionsUnder(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) == 0 || log[len(log)-1].ChangedBy.Producer != agent.Producer {
		t.Errorf("log row lost the producer: %+v", log)
	}

	// A ruling carries the producer too, and the ruling that ends a
	// concept is its deletion (design doc 0135), so the tombstone is
	// where this one is read back from.
	rejecter := domain.Actor{Kind: domain.ActorHuman, Name: "na0@example.co.jp", Producer: "ochakai-webui/0.15.0"}
	turned := testdb.Unique(t, "it-prod-no-")
	if err := s.Create(ctx, &domain.Knowledge{
		Type: domain.TypeTerms, ID: turned, Title: turned, Status: domain.StatusDraft, CreatedBy: agent,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, turned, rejecter, nil, "not yet"); err != nil {
		t.Fatal(err)
	}
	turnedRevs, err := s.ListRevisions(ctx, turned, 10)
	if err != nil {
		t.Fatal(err)
	}
	if turnedRevs[0].Change != "reject" || turnedRevs[0].ChangedBy.Producer != rejecter.Producer ||
		turnedRevs[0].Note != "not yet" {
		t.Errorf("ruling revision = %+v, want the producer and the reason carried", turnedRevs[0])
	}
}

// A file's path is the whole of where it lives (design doc 0046 §3.3).
// It used to take two fields: a name, and an okf_path that said "not
// where ochakai would have put it" — a second identity for the same
// object, and one that could disagree with the row it described.
func TestIntegrationAFileReportsItsPath(t *testing.T) {
	dbURL := testdb.URL(t)
	lockLiveAttachments(t, dbURL) // live files, on a blob fake only this test can read
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	s.UseBlobStore(newFakeBlobStore())
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "t"}
	base := testdb.Unique(t, "it-path-")
	id := base + "/revenue"
	defer func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE path LIKE $1 || '%'`, base)
		_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE path LIKE $1 || '%'`, base)
	}()

	body := fmt.Sprintf("![chart](revenue/chart.png)\n\n[CSV](/%s/seeds/orders.csv)\n", base)
	k := &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "it path",
		Status: domain.StatusDraft, Body: body, CreatedBy: actor, UpdatedBy: actor}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutFile(ctx, id+"/chart.png", "image/png", []byte("png"), actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutFile(ctx, base+"/seeds/orders.csv", "text/plain", []byte("a,b\n"), actor); err != nil {
		t.Fatal(err)
	}

	atts, err := s.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range atts {
		got[a.Name] = a.Path
	}
	// Both report where they are. The one under the entry's namespace is
	// not a special case with an empty field — it has an address like
	// everything else, and it is the address an export writes it back at.
	want := map[string]string{
		"chart.png":  id + "/chart.png",
		"orders.csv": base + "/seeds/orders.csv",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
	for _, a := range atts {
		if okf.FilePath(id, &a) != a.Path {
			t.Errorf("the export path of %s disagrees with its own: %q vs %q",
				a.Name, okf.FilePath(id, &a), a.Path)
		}
	}
}

// TestIntegrationQueueCounts pins the property that makes the counts
// worth having: each one is the size of the feed it names, measured
// under the same filter (design doc 0049). A count that drifted from its
// feed would be worse than no count — it would send a reviewer to an
// empty page, or leave work invisible.
func TestIntegrationQueueCounts(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}

	// Scoped by prefix, both because the test database is shared and
	// because scoping is the only filter the counts take.
	run := testdb.Unique(t, "it49-")
	mine := Filter{Prefixes: []string{run}}
	actor := domain.Actor{Kind: "human", Name: "test"}
	mk := func(name string, status domain.Status, staleAfter string) string {
		id := run + "/" + name
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: name, Body: "b",
			Status: status, StaleAfter: staleAfter, CreatedBy: actor, UpdatedBy: actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return id
	}
	fail := func(id string) {
		if err := s.RecordOutcome(ctx, domain.EventFailed, actor, id, ""); err != nil {
			t.Fatalf("RecordOutcome %s: %v", id, err)
		}
	}
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(domain.DateLayout)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(domain.DateLayout)

	mk("draft-idle", domain.StatusDraft, "")
	fail(mk("draft-failed", domain.StatusDraft, ""))
	broken := mk("verified-broken", domain.StatusStable, "")
	backdateVerification(t, ctx, s, broken, time.Now().UTC().Add(-30*24*time.Hour))
	fail(broken)
	answered := mk("verified-answered", domain.StatusStable, "")
	fail(answered)
	mk("expired", domain.StatusStable, yesterday)
	mk("not-yet", domain.StatusStable, tomorrow)
	rejected := mk("rejected-draft", domain.StatusDraft, "")
	if err := s.SoftDelete(ctx, rejected, actor, nil, "no"); err != nil {
		t.Fatal(err)
	}
	// The answer to a failure report is a verification, and it must land
	// after the report to count as one: this is the entry that proves the
	// failed count is a queue and not a ledger.
	if _, err := s.Verify(ctx, answered, actor); err != nil {
		t.Fatal(err)
	}

	got, err := s.QueueCounts(ctx, mine)
	if err != nil {
		t.Fatalf("QueueCounts: %v", err)
	}
	// Edited counts verified-broken: its verification is backdated to
	// before the content's own change time, so it does not stand for the
	// current content (design doc 0138) — the same entry sits in two
	// queues, which Total() counts as two places to work.
	want := domain.QueueCounts{Drafts: 2, Failed: 2, StaleAfter: 1, Edited: 1}
	if got != want {
		t.Errorf("QueueCounts = %+v, want %+v", got, want)
	}

	// Same filter, same numbers as the feeds themselves.
	drafts, err := s.ListByUsage(ctx, Filter{Prefixes: []string{run},
		Statuses: []domain.Status{domain.StatusDraft}}, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	reported, err := s.ListByFailed(ctx, mine, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := s.ListByStaleAfter(ctx, mine, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(drafts)) != got.Drafts || int64(len(reported)) != got.Failed ||
		int64(len(expired)) != got.StaleAfter {
		t.Errorf("counts %+v disagree with the feeds (%d drafts, %d reported wrong, %d past expiry)",
			got, len(drafts), len(reported), len(expired))
	}
}

// When the text cannot tell two concepts apart, something still has to
// decide which one an agent reading the top of the list under a byte
// budget sees. It used to be whatever order the scan produced. Now it is
// the loop's own signal: the concept somebody confirmed most recently
// goes first, and the id closes the order so two concepts equal in every
// way still arrive the same way twice.
func TestIntegrationLexicalSearchBreaksTiesByVerification(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	run := testdb.Unique(t, "it-tie-")
	mine := Filter{Tags: []string{run}}
	// Three concepts carrying the identical body and no distinguishing
	// name: every term in the query lands in all three, so the score is
	// the same number three times over.
	ids := []string{run + "/gamma", run + "/beta", run + "/alpha"}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_verification WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		}
	})
	// Created in an order that is neither the id order nor the answer, so
	// insertion order passing by accident is not one of the outcomes.
	for _, id := range ids {
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Tags: []string{run},
			Status: domain.StatusDraft, CreatedBy: actor,
			Body: "四半期の粗利の推移をどう読むか。",
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	hits, err := s.SearchLexical(ctx, "粗利", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want the 3 tied concepts", len(hits))
	}
	for _, h := range hits[1:] {
		if h.Score != hits[0].Score {
			t.Fatalf("the fixture is meant to tie: scores %v", hits)
		}
	}
	// Nothing confirmed yet: the id closes the order.
	if hits[0].ID != run+"/alpha" || hits[2].ID != run+"/gamma" {
		t.Errorf("unconfirmed ties = %s, %s, %s; want alpha, beta, gamma",
			hits[0].ID, hits[1].ID, hits[2].ID)
	}
	// Confirm the one the id order puts last: recency outranks the id.
	if _, err := s.Verify(ctx, run+"/gamma", actor); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchLexical(ctx, "粗利", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	if hits[0].ID != run+"/gamma" {
		t.Errorf("after confirming gamma the order is %s first; want gamma — "+
			"the concept somebody checked most recently leads a tie", hits[0].ID)
	}
}

// A hit says which concept matched; the snippet says why. It is filled
// only when the reason is not already on the row — a concept whose title
// or description carries the query needs no passage, because the caller
// can read the match without one.
func TestIntegrationLexicalSearchCarriesTheMatchingPassage(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	actor := domain.Actor{Kind: "human", Name: "test"}

	run := testdb.Unique(t, "it-snip-")
	mine := Filter{Tags: []string{run}}
	inBody, inTitle := run+"/insights/reading", run+"/metrics/named"
	t.Cleanup(func() {
		for _, id := range []string{inBody, inTitle} {
			_, _ = s.pool.Exec(ctx, `DELETE FROM object WHERE id = $1`, id)
			_, _ = s.pool.Exec(ctx, `DELETE FROM knowledge_revision WHERE id = $1`, id)
		}
	})
	create := func(id, title, desc, body string) {
		t.Helper()
		if err := s.Create(ctx, &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: title, Description: desc,
			Tags: []string{run}, Status: domain.StatusDraft, CreatedBy: actor, Body: body,
		}, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	create(inBody, "八月の読み方", "季節性の説明",
		"毎年のことなので調査には値しない。"+strings.Repeat("前置きが長い。", 30)+
			"棚卸資産の回転が落ちるのは在庫の積み増しによる。")
	create(inTitle, "棚卸資産の定義", "", "定義の本文。")

	hits, err := s.SearchLexical(ctx, "棚卸資産", mine, 50)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.SearchHit{}
	for _, h := range hits {
		byID[h.ID] = h
	}
	body, ok := byID[inBody]
	if !ok {
		t.Fatalf("the concept matching in its body is not in the hits: %+v", hits)
	}
	if !strings.Contains(body.Snippet, "棚卸資産") {
		t.Errorf("snippet = %q, want the passage where the query landed", body.Snippet)
	}
	if !strings.HasPrefix(body.Snippet, "…") {
		t.Errorf("snippet = %q, want a mark saying the passage was cut", body.Snippet)
	}
	named, ok := byID[inTitle]
	if !ok {
		t.Fatalf("the concept matching in its title is not in the hits: %+v", hits)
	}
	if named.Snippet != "" {
		t.Errorf("a concept whose title is the query carries snippet %q; the row already shows the match",
			named.Snippet)
	}
}

// The feed reads stale_after as the moment it is, not as the day it falls
// in (design doc 0133). Two entries expiring the same day, an hour either
// side of now, are the case a date column could not tell apart: both were
// stale from midnight, so half the feed was work nobody had to do yet.
//
// This is also what keeps the comparison free of timezones. now() and the
// column are both absolute, so neither side depends on the session's
// TimeZone — which the date column had to spell out by hand to avoid.
func TestIntegrationStaleFeedReadsTheTimeOfDay(t *testing.T) {
	dbURL := testdb.URL(t)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 4); err != nil {
		t.Fatal(err)
	}

	run := testdb.Unique(t, "it133-")
	actor := domain.Actor{Kind: "human", Name: "test"}
	mk := func(name, staleAfter string) string {
		id := run + "/" + name
		k := &domain.Knowledge{
			Type: domain.TypeInsights, ID: id, Title: name, Body: "b", Tags: []string{run},
			Status: domain.StatusDraft, StaleAfter: staleAfter,
			CreatedBy: actor, UpdatedBy: actor,
		}
		if err := s.Create(ctx, k, false); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return id
	}

	now := time.Now().UTC()
	past := mk("an-hour-ago", now.Add(-time.Hour).Format(time.RFC3339))
	mk("in-an-hour", now.Add(time.Hour).Format(time.RFC3339))

	stale, err := s.ListByStaleAfter(ctx, Filter{Tags: []string{run}}, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(stale))
	for i, k := range stale {
		got[i] = k.ID
	}
	if want := []string{past}; !slices.Equal(got, want) {
		t.Errorf("stale feed = %v, want %v — only the moment that has passed", got, want)
	}
	// And the moment survives the round trip through the column, rather
	// than reading back as the day it fell in.
	if len(stale) == 1 && stale[0].StaleAfter != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Errorf("stale_after read back as %q, want the instant that was written", stale[0].StaleAfter)
	}
}
