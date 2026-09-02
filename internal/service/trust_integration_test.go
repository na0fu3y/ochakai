package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestVerificationStandsUntilTheContentMovesIntegration pins design doc
// 0138 end to end: the trust tier is derived from the verifications that
// stand — the rows at or after the last meaningful change — so an edit or
// a move returns a confirmed concept to unverified, the edited queue
// holds it, and a re-verification takes it back out. The ledger itself
// never shrinks: what lapses is the claim about the current content.
func TestVerificationStandsUntilTheContentMovesIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer@example.co.jp"}

	prefix := uid(t, "standit")
	id := prefix + "/metric"
	filter := store.Filter{Prefixes: []string{prefix}}
	edited := func() int64 {
		t.Helper()
		q, err := svc.Store.QueueCounts(ctx, filter)
		if err != nil {
			t.Fatalf("QueueCounts: %v", err)
		}
		return q.Edited
	}
	trust := func(id string) domain.Trust {
		t.Helper()
		k, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		return k.Trust
	}

	entry := func(body string) *domain.Knowledge {
		return &domain.Knowledge{Type: domain.TypeMetrics, ID: id, Title: "売上", Body: body}
	}
	if _, err := svc.Create(ctx, entry("受注合計。"), human); err != nil {
		t.Fatal(err)
	}
	// A never-verified neighbour: unverified, but with no ledger row it
	// belongs in no queue — the edited queue is "confirmed once, not as
	// it stands", not "unverified".
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeInsights, ID: prefix + "/note", Body: "未検証のまま。"}, human); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Verify(ctx, id, human); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got := trust(id); got != domain.TrustHuman {
		t.Fatalf("after verify: trust = %s, want %s", got, domain.TrustHuman)
	}
	if n := edited(); n != 0 {
		t.Fatalf("after verify: edited queue = %d, want 0", n)
	}

	// An edit replaces the confirmed content, so the confirmation no
	// longer stands: the tier lapses, the ledger row stays, and the
	// concept enters the edited queue.
	if _, _, err := svc.Update(ctx, entry("受注合計。返品は差し引く。"), human, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	k, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if k.Trust != domain.TrustUnverified {
		t.Errorf("after edit: trust = %s, want %s", k.Trust, domain.TrustUnverified)
	}
	if len(k.Verifications) != 1 {
		t.Errorf("after edit: ledger = %d rows, want the history kept", len(k.Verifications))
	}
	if n := edited(); n != 1 {
		t.Errorf("after edit: edited queue = %d, want 1 (the neighbour has no ledger row)", n)
	}

	// The trust filter answers in the same derivation: the lapsed concept
	// is unverified now, and the edited queue is its non-null-verified_at
	// prefix of the verification-age listing.
	hits, err := svc.SearchOrList(ctx, "", "verified_at", "",
		store.Filter{Prefixes: []string{prefix}, Trust: []domain.Trust{domain.TrustUnverified}}, 10)
	if err != nil {
		t.Fatalf("SearchOrList: %v", err)
	}
	if len(hits.Hits) != 2 || hits.Hits[0].ID != id || hits.Hits[0].VerifiedAt == nil {
		t.Errorf("trust=unverified listing = %+v, want the lapsed concept first with its verified_at, then the tail", hits.Hits)
	}

	// The stats tally shares the derivation (a tally that disagreed with
	// the filter would be worse than none).
	st, err := svc.Store.Stats(ctx, time.Now().Add(-time.Hour), []string{prefix})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Concepts.Trust[string(domain.TrustHuman)] != 0 ||
		st.Concepts.Trust[string(domain.TrustUnverified)] != 2 {
		t.Errorf("stats trust after edit = %v, want 0 human-reviewed / 2 unverified", st.Concepts.Trust)
	}
	if st.Queues.Edited != 1 {
		t.Errorf("stats queues.edited = %d, want the feed's own count", st.Queues.Edited)
	}

	// Re-verifying the current content restores the tier and empties the
	// queue — the loop's exit.
	if _, err := svc.Verify(ctx, id, human); err != nil {
		t.Fatalf("re-verify: %v", err)
	}
	if got := trust(id); got != domain.TrustHuman {
		t.Errorf("after re-verify: trust = %s, want %s", got, domain.TrustHuman)
	}
	if n := edited(); n != 0 {
		t.Errorf("after re-verify: edited queue = %d, want 0", n)
	}
}

// TestMoveLapsesVerificationsIntegration pins the move half of design doc
// 0138: a move reassigns who the content stands by (design doc 0036
// §3.3), so the moved concept's verifications no longer stand — and
// neither do a referrer's, whose body the move rewrote.
func TestMoveLapsesVerificationsIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer@example.co.jp"}

	prefix := uid(t, "mvlapse")
	metric, referrer := prefix+"/metric", prefix+"/insight"
	for _, k := range []*domain.Knowledge{
		{Type: domain.TypeMetrics, ID: metric, Title: "売上", Body: "受注合計。"},
		{Type: domain.TypeInsights, ID: referrer, Title: "読み方",
			Body: "[売上](/" + metric + ".md) の注意点。"},
	} {
		if _, err := svc.Create(ctx, k, human); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{metric, referrer} {
		if _, err := svc.Verify(ctx, id, human); err != nil {
			t.Fatalf("Verify(%s): %v", id, err)
		}
	}

	movedTo := prefix + "/net-revenue"
	if _, err := svc.Move(ctx, metric, movedTo, human); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for _, id := range []string{movedTo, referrer} {
		k, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if k.Trust != domain.TrustUnverified {
			t.Errorf("%s after the move: trust = %s, want %s (the move rewrote its content)",
				id, k.Trust, domain.TrustUnverified)
		}
		if len(k.Verifications) == 0 {
			t.Errorf("%s after the move: ledger emptied, want the history kept", id)
		}
	}
	q, err := svc.Store.QueueCounts(ctx, store.Filter{Prefixes: []string{prefix}})
	if err != nil {
		t.Fatal(err)
	}
	if q.Edited != 2 {
		t.Errorf("edited queue after the move = %d, want both lapsed concepts", q.Edited)
	}
}

// TestVerifyStandsOnSkewedClocksIntegration pins the clamp in
// store.Verify: content_changed_at is stamped from the application clock
// and the verification from the database's, and a database running
// behind must not record a fresh verification that fails the standing
// comparison it exists to pass. The test writes a content change dated
// in the near future — a stand-in for the skew — and verifies.
func TestVerifyStandsOnSkewedClocksIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	human := domain.Actor{Kind: domain.ActorHuman, Name: "reviewer@example.co.jp"}

	id := uid(t, "skewit")
	if _, err := svc.Create(ctx, &domain.Knowledge{
		Type: domain.TypeMetrics, ID: id, Title: "clock", Body: "v1"}, human); err != nil {
		t.Fatal(err)
	}
	// Push content_changed_at ahead of any clock the database will read.
	conn, err := pgx.Connect(ctx, testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx,
		`UPDATE object SET content_changed_at = now() + interval '2 seconds' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, id, human); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	k, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if k.Trust != domain.TrustHuman {
		t.Errorf("verification under clock skew does not stand: trust = %s, want %s",
			k.Trust, domain.TrustHuman)
	}
}
