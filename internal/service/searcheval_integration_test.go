package service

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/embed"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// This file is the search evaluation harness (issue #528): a golden set
// of questions with the concept each one is expected to surface, scored
// as recall@k and MRR over the shipped demo bundle. Ranking changes land
// with these numbers in the PR; the floors below pin the current
// baseline so a regression fails instead of shipping quietly.
//
// The corpus is examples/demo — the same eleven concepts the quick start
// loads — imported under a run-unique prefix so a shared test database
// neither collides nor pollutes, plus three Japanese concepts defined
// inline.
//
// The supplement used to be the only Japanese here, because the demo had
// none and the two-character windows a Japanese question is cut into
// (migration 0036) need Japanese text to be exercised at all. That is
// the arrangement this file should be read as a warning about: what the
// harness measured was a fixture nobody ships, while a Japanese reader
// following the quick start typed a Japanese question at the demo and
// got silence. The demo is bilingual now and the Japanese cases below
// are against it; the supplement stays for the terms the invented shop
// has no home for (解約率 — it sells no subscriptions), which is a
// smaller claim than the one it was making.
//
// Both halves are measured. The lexical run is the search a deployment
// without Vertex AI gets; the fused run adds a vector ranking from the
// stand-in encoder in fakeencoder_test.go and fuses the two exactly as
// the product does, which is the configuration every Google Cloud
// deployment runs (design doc 0080 §1.1) and the one nothing measured
// before. Read them for different things: the lexical number is ranking
// quality, and the fused number is the arithmetic of the merge — the
// stand-in shares the lexical side's vocabulary, so it cannot stand in
// for what a trained encoder is for.

// evalCase is one golden question: a query somebody would actually ask,
// and the concept a good ranking puts in front of them.
type evalCase struct {
	query string
	want  string // id relative to the import prefix
}

// evalCases is the golden set. Keep queries phrased as questions and
// keywords a data agent would send — the point is aboutness, not exact
// title matches (those are pinned separately by the name-bonus tests in
// the store).
var evalCases = []evalCase{
	// Questions against the demo bundle.
	{query: "why is revenue down", want: "insights/reading-revenue"},
	{query: "monthly revenue", want: "queries/sales/monthly-revenue"},
	{query: "revenue by channel", want: "queries/sales/revenue-by-channel"},
	{query: "what does revenue mean", want: "metrics/revenue"},
	{query: "completed order", want: "glossary/completed-order"},
	{query: "repeat purchase rate", want: "metrics/repeat-purchase-rate"},
	{query: "channel codes", want: "references/order-channel-codes"},
	{query: "revenue recognition", want: "policies/revenue-recognition"},
	{query: "orders table schema", want: "tables/shop-orders"},
	{query: "how to run a bigquery query", want: "skills/run-bigquery-query"},
	{query: "when should a revenue drop be escalated", want: "insights/reading-revenue"},
	// A second pass over the same corpus, phrased the way somebody asks
	// rather than the way a title reads. Fourteen cases put MRR inside
	// its own noise — one case moving from rank 1 to 2 moved it by 0.036
	// — so the set is widened as far as thirteen concepts honestly
	// support. Beyond that a case stops being a question anybody has and
	// becomes a restatement of a title, which measures the fixture.
	{query: "august is down 15 percent", want: "insights/reading-revenue"},
	{query: "obon", want: "insights/reading-revenue"},
	{query: "what is a normal month", want: "insights/reading-revenue"},
	{query: "bulk order faking a spike", want: "insights/reading-revenue"},
	{query: "late partition", want: "insights/reading-revenue"},
	{query: "does revenue include tax", want: "metrics/revenue"},
	{query: "which orders count as revenue", want: "policies/revenue-recognition"},
	{query: "are refunds deducted", want: "policies/revenue-recognition"},
	{query: "cancelled orders", want: "glossary/completed-order"},
	{query: "web_direct", want: "references/order-channel-codes"},
	{query: "channel_code enum", want: "references/order-channel-codes"},
	{query: "split revenue by channel", want: "queries/sales/revenue-by-channel"},
	{query: "revenue for the fiscal year by month", want: "queries/sales/monthly-revenue"},
	{query: "how many customers bought again", want: "metrics/repeat-purchase-rate"},
	{query: "guest checkout", want: "metrics/repeat-purchase-rate"},
	{query: "where do orders live", want: "tables/shop-orders"},
	{query: "total_price column", want: "tables/shop-orders"},
	{query: "execute an attested computation", want: "skills/run-bigquery-query"},
	// Japanese, against the shipped bundle. The demo is bilingual the way
	// a Japanese team's own base is — English where the warehouse columns
	// are, Japanese where the judgment is — and the README tells a reader
	// to ask it a Japanese question, so these are the cases that check
	// the claim rather than a fixture standing in for it.
	{query: "なぜ売上が落ちているのか", want: "insights/reading-revenue"},
	{query: "お盆", want: "insights/reading-revenue"},
	{query: "月末の着地見込み", want: "insights/着地見込み"},
	{query: "純売上との違い", want: "metrics/revenue"},
	{query: "受注と完了の違い", want: "glossary/completed-order"},
	// Japanese, against the inline supplement below. 売上 and 解約 are
	// two-character terms — exactly the shape the trigram index cannot
	// serve and the windowed scan answers.
	{query: "売上が下がった理由", want: "ja/insights/uriage-yomikata"},
	{query: "解約率", want: "ja/metrics/kaiyakuritsu"},
	{query: "受注とは", want: "ja/glossary/juchu"},
	{query: "八月の売上", want: "ja/insights/uriage-yomikata"},
	{query: "季節性", want: "ja/insights/uriage-yomikata"},
	{query: "解約の分母", want: "ja/metrics/kaiyakuritsu"},
	{query: "支払いが確定した注文", want: "ja/glossary/juchu"},
}

// japaneseSupplement holds the inline Japanese concepts, keyed by id
// relative to the prefix. Bodies are prose, not keyword lists: the scan
// has to find the terms inside sentences the way it would in a real
// concept.
var japaneseSupplement = map[string]string{
	"ja/insights/uriage-yomikata": "---\n" +
		"type: Insight\n" +
		"title: 売上の読み方\n" +
		"description: 季節性と、下がって見えるが問題ではない月\n" +
		"tags: [sales, seasonality]\n" +
		"status: stable\n" +
		"---\n\n" +
		"売上は毎年8月に下がるが、これは季節性であって異常ではない。\n" +
		"二か月連続で前年同月を大きく割ったときだけ調べる価値がある。\n",
	"ja/metrics/kaiyakuritsu": "---\n" +
		"type: Metric\n" +
		"title: 解約率\n" +
		"description: 月初の契約数に対する当月解約の割合\n" +
		"tags: [retention]\n" +
		"status: draft\n" +
		"---\n\n" +
		"解約率は月初時点の有効契約数を分母に取る。日割りはしない。\n",
	"ja/glossary/juchu": "---\n" +
		"type: Glossary Term\n" +
		"title: 受注\n" +
		"description: 支払いが確定した注文\n" +
		"tags: [sales]\n" +
		"status: stable\n" +
		"---\n\n" +
		"受注は支払い確定時点で数える。キャンセルは受注から差し引く。\n",
}

// evalVerified is the one concept the harness confirms before scoring.
//
// A confirmation moves a concept in both halves of the search — the
// store's own boost in the lexical ranking, and the addend rrfFuse puts
// on a fused score — and for as long as the corpus was uniformly
// unverified, neither of those rules was exercised by any number on this
// page. So one concept is confirmed, and it is deliberately one that
// answers three of the questions below and is a plausible wrong answer to
// eight more: whatever a confirmation is worth, it shows up here as those
// eight moving.
//
// That is how the fused addend came to be written in ranks. At 0.002 it
// was 7.6 rank positions, and turning this one line on measured it: fused
// MRR 0.83, against 0.89 for the same corpus with nothing confirmed.
// Confirming a concept made the ranking worse, because the addend was
// carrying revenue-recognition over the concepts that actually answered
// those eight questions, on the strength of having been checked. At one
// rank position the same line reads 0.90 — a confirmation settles the
// questions it ties on and leaves the rest alone.
const evalVerified = "policies/revenue-recognition"

// Floors pin the measured baseline so a regression fails instead of
// shipping quietly. They sit under the numbers the harness reports, and
// a change that moves those numbers — either way — says so in its PR,
// because that is the point of having them.
//
// MRR dipped to 0.85 once, when a fragment no document contains stopped
// earning the largest possible weight (rarity is the weight) and the
// names that had been collecting half of it stopped ranking by an
// accident. That change did not make the ranking worse; it uncovered the
// ties the accident had been hiding — five concepts scored identically
// for "why is revenue down", and the order among them was whatever the
// scan produced. Those ties are now broken by verification recency and
// then by id (store/search.go).
//
// The set grew from 14 cases to 36, and to 41 when the demo gained its
// Japanese half. Fourteen put MRR inside its own noise: one case moving
// from rank 1 to rank 2 moved it by 0.036, which is the size of the
// differences anybody was reading. The second pass asks the same corpus
// the way somebody asks rather than the way a title reads — "obon",
// "does revenue include tax", "guest checkout" — and fourteen concepts
// do not honestly support many more than that. The five added last are
// the ones the README now tells a Japanese reader to type.
//
// The fused floors are separate. A stand-in that shares the lexical
// side's vocabulary can only reorder a list the words already reached,
// so fusion neither adds a concept here nor loses one, and what the
// number certifies is that the merge does no harm: RRF's arithmetic, the
// named-concept sort key surviving it, the confirmation addend, the
// tie-breaks under all of it. Dropping the lexical list from the fuse
// takes MRR from 0.90 to 0.80, which is how it is known the floor has
// teeth. What a trained encoder buys is matching text that shares no
// vocabulary at all, and no number on this page says how much that is.
const (
	evalK           = 10
	evalRecallFloor = 0.92
	evalMRRFloor    = 0.78
	// Fused: the same questions with the stand-in encoder on, measured
	// at 1.00 / 0.85. This floor sits above the 0.79 the fused ranking
	// scores when the verified addend goes back to 0.002, which is the
	// regression it exists to catch — that constant is the kind that gets
	// nudged by whoever is looking at one query.
	//
	// It was 0.86 against 0.89 measured, and both numbers moved when the
	// demo bundle gained its Japanese half: 0.89 → 0.878 over the same
	// 36 cases, which is one more concept competing in a corpus of
	// thirteen, and 0.878 → 0.85 from the five Japanese cases added
	// beside them. Those five are rank 1 or 2 in the lexical run, which
	// held at 0.90 — what they are low in is the *stand-in* encoder's
	// ranking, and a fake that hashes the lexical side's vocabulary has
	// no opinion about Japanese worth reading. The regression this floor
	// guards was re-measured against the same corpus rather than assumed
	// to have stayed at 0.83.
	evalFusedRecallFloor = 0.92
	evalFusedMRRFloor    = 0.83
)

// loadDemoBundle imports every document under examples/demo with the
// run-unique prefix prepended to its path-derived id, and returns the
// number imported. Root-relative links inside the bodies keep pointing
// at the unprefixed ids; that leaves the link graph dangling, which the
// lexical ranking this harness measures does not read.
func loadDemoBundle(t *testing.T, ctx context.Context, svc *Service, prefix string, actor domain.Actor) int {
	t.Helper()
	root := filepath.Join("..", "..", "examples", "demo")
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc, _, err := okf.Parse(body)
		if err != nil {
			return err
		}
		doc.ID = prefix + "/" + strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		if _, _, _, err := svc.Put(ctx, &doc.Knowledge, actor, nil); err != nil {
			return err
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("load demo bundle: %v", err)
	}
	return n
}

func TestSearchEvalIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "eval"}
	prefix := uid(t, "svceval")

	if n := loadDemoBundle(t, ctx, svc, prefix, actor); n < 11 {
		t.Fatalf("demo bundle shrank to %d documents; the golden set assumes the quick-start eleven", n)
	}
	for id, doc := range japaneseSupplement {
		d, _, err := okf.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("parse %s: %v", id, err)
		}
		d.ID = prefix + "/" + id
		if _, _, _, err := svc.Put(ctx, &d.Knowledge, actor, nil); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	if _, err := svc.Verify(ctx, prefix+"/"+evalVerified, actor); err != nil {
		t.Fatalf("verify %s: %v", evalVerified, err)
	}

	recall, mrr := scoreGoldenSet(t, ctx, svc, prefix, "lexical only")
	if recall < evalRecallFloor {
		t.Errorf("recall@%d fell to %.2f, under the %.2f baseline: a ranking change made the golden set worse", evalK, recall, evalRecallFloor)
	}
	if mrr < evalMRRFloor {
		t.Errorf("MRR fell to %.2f, under the %.2f baseline: the golden set still surfaces but lower than it did", mrr, evalMRRFloor)
	}

	// The same questions with the vector half on, which is the
	// configuration a Google Cloud deployment runs.
	scoreFusedGoldenSet(t, ctx, svc, prefix)
}

// scoreGoldenSet runs every case and reports recall@k and MRR, naming
// the configuration in the line it logs — a number without its
// configuration beside it is the thing this harness exists to stop.
func scoreGoldenSet(t *testing.T, ctx context.Context, svc *Service, prefix, config string) (recall, mrr float64) {
	t.Helper()
	filter := store.Filter{Prefixes: []string{prefix}}
	found := 0
	for _, c := range evalCases {
		hits, err := svc.Search(ctx, c.query, filter, evalK)
		if err != nil {
			t.Fatalf("search %q: %v", c.query, err)
		}
		rank := 0
		for i, h := range hits {
			if h.ID == prefix+"/"+c.want {
				rank = i + 1
				break
			}
		}
		if rank == 0 {
			got := make([]string, 0, len(hits))
			for _, h := range hits {
				got = append(got, strings.TrimPrefix(h.ID, prefix+"/"))
			}
			t.Logf("miss (%s): %q did not surface %s in the top %d; got %v", config, c.query, c.want, evalK, got)
			continue
		}
		found++
		mrr += 1.0 / float64(rank)
		t.Logf("hit (%s): %q -> %s at rank %d", config, c.query, c.want, rank)
	}
	recall = float64(found) / float64(len(evalCases))
	mrr /= float64(len(evalCases))
	t.Logf("recall@%d = %.2f (%d/%d), MRR = %.2f, %s", evalK, recall, found, len(evalCases), mrr, config)
	return recall, mrr
}

// scoreFusedGoldenSet re-runs the golden set with a vector ranking
// beside the lexical one, fused exactly as the product fuses them.
//
// **The second ranking is built here rather than in the database, and
// that is a deliberate limit.** Turning the store's vector half on means
// migrating the vector schema and filling it, and both are global: the
// column has one width for the whole database, and a reembed pass walks
// the whole corpus. The packages beside this one run against the same
// throwaway server, so doing either from here dropped a neighbour's
// vectors mid-run and reembedded concepts another test was deleting.
// A harness that makes the suite flaky is not measuring anything.
//
// So the vector list is computed in process — the same encoder over the
// same concepts, ranked by cosine — and handed to the same rrfFuse the
// service calls. What that measures is the merge: RRF's arithmetic, the
// named-concept sort key surviving it, the verification tie-break under
// it. What it does not measure is the SQL that produces the vector list
// in production, which the store's own vector tests cover.
func scoreFusedGoldenSet(t *testing.T, ctx context.Context, svc *Service, prefix string) {
	t.Helper()
	dim, ok := embed.Dimension("gemini-embedding-001")
	if !ok {
		t.Fatal("the product no longer knows its own embedding width")
	}
	enc := fakeEncoder{dim: dim}
	corpus := embedCorpus(t, ctx, svc, prefix, enc)
	if len(corpus) < 13 {
		t.Fatalf("embedded %d concepts, want the whole corpus", len(corpus))
	}

	filter := store.Filter{Prefixes: []string{prefix}}
	found, mrr := 0, 0.0
	for _, c := range evalCases {
		lexical, err := svc.Store.SearchLexical(ctx, c.query, filter, evalK*2)
		if err != nil {
			t.Fatalf("search %q: %v", c.query, err)
		}
		vectors, err := enc.Embed(ctx, embed.TaskQuery, []string{c.query})
		if err != nil {
			t.Fatal(err)
		}
		hits := rrfFuse(c.query, evalK, lexical, nearest(corpus, vectors[0], evalK*2))
		rank := 0
		for i, h := range hits {
			if h.ID == prefix+"/"+c.want {
				rank = i + 1
				break
			}
		}
		if rank == 0 {
			t.Logf("miss (fused): %q did not surface %s in the top %d", c.query, c.want, evalK)
			continue
		}
		found++
		mrr += 1.0 / float64(rank)
		// Logged like the lexical half's hits: without the per-case rank
		// on this side, a fused number that moves says only that
		// something did, and the only way to find out which case was to
		// add this line and run it again.
		t.Logf("hit (fused): %q -> %s at rank %d", c.query, c.want, rank)
	}
	recall := float64(found) / float64(len(evalCases))
	mrr /= float64(len(evalCases))
	t.Logf("recall@%d = %.2f (%d/%d), MRR = %.2f, lexical + vector (stand-in encoder)",
		evalK, recall, found, len(evalCases), mrr)
	if recall < evalFusedRecallFloor {
		t.Errorf("fused recall@%d fell to %.2f, under the %.2f baseline", evalK, recall, evalFusedRecallFloor)
	}
	if mrr < evalFusedMRRFloor {
		t.Errorf("fused MRR fell to %.2f, under the %.2f baseline", mrr, evalFusedMRRFloor)
	}
}

// embedded is one concept with the vector the stand-in encoder gave it.
type embedded struct {
	hit domain.SearchHit
	vec []float32
}

// embedCorpus reads every concept under the prefix and embeds it, the
// way a write would have.
func embedCorpus(t *testing.T, ctx context.Context, svc *Service, prefix string, enc fakeEncoder) []embedded {
	t.Helper()
	listing, err := svc.SearchOrList(ctx, "", "verified_at", "",
		store.Filter{Prefixes: []string{prefix}}, 1000)
	if err != nil {
		t.Fatalf("list corpus: %v", err)
	}
	out := make([]embedded, 0, len(listing.Hits))
	for _, h := range listing.Hits {
		k, err := svc.Get(ctx, h.ID)
		if err != nil {
			t.Fatalf("get %s: %v", h.ID, err)
		}
		vecs, err := enc.Embed(ctx, embed.TaskDocument,
			[]string{k.ID + " " + k.Title + " " + k.Description + " " + k.Body})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, embedded{hit: h, vec: vecs[0]})
	}
	return out
}

// nearest ranks the corpus by cosine against the query vector, as the
// store's vector search does, and returns the top n.
func nearest(corpus []embedded, query []float32, n int) []domain.SearchHit {
	type scored struct {
		hit domain.SearchHit
		sim float64
	}
	ranked := make([]scored, 0, len(corpus))
	for _, c := range corpus {
		var dot float64
		for i := range query {
			dot += float64(query[i]) * float64(c.vec[i])
		}
		ranked = append(ranked, scored{hit: c.hit, sim: dot}) // both sides are unit vectors
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].sim > ranked[j].sim })
	out := make([]domain.SearchHit, 0, n)
	for _, r := range ranked {
		if len(out) == n {
			break
		}
		r.hit.Score = r.sim
		out = append(out, r.hit)
	}
	return out
}
