package service

import (
	"context"
	"fmt"
	"io/fs"
	"math"
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
// neither collides nor pollutes, plus one Japanese concept defined
// inline.
//
// The supplement used to be the only Japanese here, because the demo had
// none and the two-character windows a Japanese question is cut into
// (migration 0036) need Japanese text to be exercised at all. That is
// the arrangement this file should be read as a warning about: what the
// harness measured was a fixture nobody ships, while a Japanese reader
// following the quick start typed a Japanese question at the demo and
// got silence. The demo is written in Japanese now — that is what
// ochak.ai's demo is — so the questions below are asked of the shipped
// bundle in the language it is written in. The supplement is down to the
// one term the invented shop has no home for (解約率 — it sells no
// subscriptions); the two that stood in for 売上 and 受注 are gone,
// because the bundle says both itself and a fixture competing with the
// concept it stands in for measures nothing.
//
// The English cases that remain are the ones that stayed English in the
// bundle: column names, enum values, the words the warehouse supplies.
// They are here because a Japanese knowledge base is asked those in
// English and has to answer.
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
	// Questions against the demo bundle, in the language it is written
	// in.
	{query: "なぜ売上が落ちているのか", want: "insights/reading-revenue"},
	{query: "月次売上", want: "queries/sales/monthly-revenue"},
	{query: "チャネル別の売上", want: "queries/sales/revenue-by-channel"},
	{query: "売上とは何か", want: "metrics/revenue"},
	{query: "完了した注文とは", want: "glossary/completed-order"},
	{query: "リピート購入率", want: "metrics/repeat-purchase-rate"},
	{query: "チャネルコードの一覧", want: "references/order-channel-codes"},
	{query: "売上計上のルール", want: "policies/revenue-recognition"},
	{query: "注文テーブルのスキーマ", want: "tables/shop-orders"},
	{query: "BigQuery のクエリはどう実行するか", want: "skills/run-bigquery-query"},
	{query: "月末の着地見込み", want: "insights/着地見込み"},
	// A second pass over the same corpus, phrased the way somebody asks
	// rather than the way a title reads. Fourteen cases put MRR inside
	// its own noise — one case moving from rank 1 to 2 moved it by 0.036
	// — so the set is widened as far as twelve concepts honestly
	// support. Beyond that a case stops being a question anybody has and
	// becomes a restatement of a title, which measures the fixture.
	{query: "売上が下がった理由", want: "insights/reading-revenue"},
	{query: "お盆", want: "insights/reading-revenue"},
	{query: "普通の月はいくらか", want: "insights/reading-revenue"},
	{query: "大口の注文で山ができた", want: "insights/reading-revenue"},
	{query: "パーティションの遅れ", want: "insights/reading-revenue"},
	{query: "季節性", want: "insights/reading-revenue"},
	{query: "売上に税は含まれるか", want: "metrics/revenue"},
	{query: "純売上との違い", want: "metrics/revenue"},
	{query: "どの注文を売上として数えるか", want: "policies/revenue-recognition"},
	{query: "返金は引くのか", want: "policies/revenue-recognition"},
	{query: "キャンセルした注文", want: "glossary/completed-order"},
	{query: "受注と完了の違い", want: "glossary/completed-order"},
	{query: "売上が伸びているチャネル", want: "queries/sales/revenue-by-channel"},
	{query: "今年度の売上を月ごとに", want: "queries/sales/monthly-revenue"},
	{query: "買い直した客の割合", want: "metrics/repeat-purchase-rate"},
	{query: "ゲスト購入", want: "metrics/repeat-purchase-rate"},
	{query: "注文はどこに入っているか", want: "tables/shop-orders"},
	{query: "receipt には何を返すのか", want: "skills/run-bigquery-query"},
	{query: "今月はどうなりそうか", want: "insights/着地見込み"},
	// English, and still against the shipped bundle: the names the
	// warehouse supplies stayed English when the prose became Japanese,
	// which is what a Japanese team's own base looks like. A base that
	// could not be asked `total_price` would have translated the wrong
	// half.
	{query: "total_price column", want: "tables/shop-orders"},
	{query: "web_direct", want: "references/order-channel-codes"},
	{query: "channel_code enum", want: "references/order-channel-codes"},
	{query: "status completed", want: "glossary/completed-order"},
	// The other names the writer gave the metric (design doc 0105).
	// "top line" appears nowhere in the bundle but the synonyms key, so
	// this case answers only when the haystack reads it — and unlike the
	// "net sales" it replaced, it is a name for this metric rather than
	// for the 純売上 the concept spends a paragraph saying it is not.
	{query: "top line", want: "metrics/revenue"},
	// Japanese, against the inline supplement below. 解約 is a
	// two-character term — exactly the shape the trigram index cannot
	// serve and the windowed scan answers — in a corpus that has no other
	// home for it.
	{query: "解約率", want: "ja/metrics/kaiyakuritsu"},
	{query: "解約の分母", want: "ja/metrics/kaiyakuritsu"},
}

// japaneseSupplement holds the inline Japanese concepts, keyed by id
// relative to the prefix. Bodies are prose, not keyword lists: the scan
// has to find the terms inside sentences the way it would in a real
// concept.
var japaneseSupplement = map[string]string{
	"ja/metrics/kaiyakuritsu": "---\n" +
		"type: Metric\n" +
		"title: 解約率\n" +
		"description: 月初の契約数に対する当月解約の割合\n" +
		"tags: [retention]\n" +
		"status: draft\n" +
		"---\n\n" +
		"解約率は月初時点の有効契約数を分母に取る。日割りはしない。\n",
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
// The set grew from 14 cases to 36, then to 41 when the demo gained a
// Japanese half, and stands at 37 now that the demo is Japanese and the
// two supplement concepts the bundle had come to duplicate are gone.
// Fourteen put MRR inside its own noise: one case moving from rank 1 to
// rank 2 moved it by 0.036, which is the size of the differences anybody
// was reading. The second pass asks the same corpus the way somebody
// asks rather than the way a title reads — 「お盆」「ゲスト購入」
// 「返金は引くのか」 — and twelve concepts do not honestly support many
// more than that.
//
// **The numbers moved when the demo became Japanese, and the two runs
// are not comparable.** Both the corpus and the questions changed:
// lexical went 0.90 → 0.78 and fused 0.85 → 0.77, while recall went to
// 1.00 on both halves — nothing fell out of the top ten. What the MRR
// is made of is 24 of 37 cases at rank 1 and two at rank 7. The drop is
// the corpus, not the ranking, and it is two effects worth naming:
//
//   - A monolingual Japanese corpus about one shop shares 売上 across
//     every concept, and the concept whose name *is* 売上 takes the name
//     rule (store/search.go) from the insight that answers the question.
//     「なぜ売上が落ちているのか」 puts metrics/revenue first and
//     insights/reading-revenue second, which is the rule working: the
//     README's next line is `ochakai get insights/reading-revenue`.
//   - An English keyword now lands in prose that is Japanese around it,
//     so "total_price column" ties seven concepts at the same score and
//     is settled by verification recency and id. Rank 7 of 10 is what a
//     Japanese base gives an English keyword, and measuring it is why
//     the English cases stayed.
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
	evalK = 10
	// Lexical, measured at 1.00 / 0.78. The MRR floor is two cases'
	// worth under it: one case slipping from rank 1 to rank 2 is 0.014
	// here, so a floor closer than that fails on noise.
	evalRecallFloor = 0.95
	evalMRRFloor    = 0.75
	// Fused: the same questions with the stand-in encoder on, measured
	// at 1.00 / 0.77. This floor is the one that catches the verified
	// addend going back to 0.002 — that constant is the kind that gets
	// nudged by whoever is looking at one query — so it was re-measured
	// against this corpus rather than carried over: at 0.002 the fused
	// run scores 0.73, and the floor sits between.
	//
	// It read 0.83 against 0.85 measured over the bilingual corpus, and
	// before that 0.86 against 0.89. Each move came from the corpus
	// changing under it, not from the merge; what the number certifies is
	// still only that the merge does no harm.
	evalFusedRecallFloor = 0.95
	evalFusedMRRFloor    = 0.74
)

// evalFloorSlackCases is how far a floor may sit under what was measured
// before that gap is itself a failure, counted in cases.
//
// The floors above check one direction: under them fails, over them is
// free. That lets an improvement bank quietly — a ranking change that
// lifts MRR and leaves the floor where it was hands the next regression
// a gap to fall into, and a number nobody moved is a number nobody
// reads. The line ceilings found the same hole from the other side and
// closed it the same way: d28c3c8 shortened the deploy guide, raised
// DOC-LINES for the room the fold needed, and left 28 lines nobody
// returned, which is why DOC-LINES-SLACK and RECORD-CORPUS-LINES-SLACK
// exist (surface_test.go, CONTRIBUTING.md). **The comment above already
// states the rule this closes** — "a change that moves those numbers —
// either way — says so in its PR" — and nothing could check the
// either-way half of it.
//
// Two cases, and derived from the size of the set rather than declared
// as a score, because everything on this page that reasons about noise
// reasons in cases: one case slipping from rank 1 to rank 2 is 0.014
// here, one case leaving the top k is 0.027, and the floors above were
// chosen as about two cases under what was measured. A tolerance written
// as 0.05 would have to be rewritten every time the set grows, and the
// set has been 14, then 36, then 41, then 37.
const evalFloorSlackCases = 2

// checkEvalFloors holds one configuration's numbers to their floors in
// both directions: under a floor is the regression the floor exists to
// catch, and further over it than evalFloorSlackCases allows is a floor
// that was not raised when the ranking improved.
func checkEvalFloors(t *testing.T, config string, recall, mrr, recallFloor, mrrFloor float64) {
	t.Helper()
	cases := float64(len(evalCases))
	slack := evalFloorSlackCases / cases
	for _, m := range []struct {
		name       string
		got, floor float64
	}{
		{fmt.Sprintf("recall@%d", evalK), recall, recallFloor},
		{"MRR", mrr, mrrFloor},
	} {
		switch {
		case m.got < m.floor:
			t.Errorf("%s (%s) fell to %.2f, under the %.2f baseline: a ranking change made the golden set worse",
				m.name, config, m.got, m.floor)
		case m.got-m.floor > slack:
			// Rounded up, because the floor is written to two decimals
			// and rounding the other way would advise a number that
			// fails this same check on the next run.
			want := math.Ceil((m.got-slack)*100) / 100
			t.Errorf(`%s (%s) measures %.2f against a floor of %.2f — %.1f cases' worth of headroom, wider than the %d
evalFloorSlackCases this file allows. Raise the floor to at least %.2f in the PR that
earned the improvement: a floor left under a number that moved up is budget the next
regression spends without moving anything anybody reads.`,
				m.name, config, m.got, m.floor, (m.got-m.floor)*cases, evalFloorSlackCases, want)
		}
	}
}

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

	const lexical = "lexical only"
	recall, mrr := scoreGoldenSet(t, ctx, svc, prefix, lexical)
	checkEvalFloors(t, lexical, recall, mrr, evalRecallFloor, evalMRRFloor)

	// The same questions with the vector half on, which is the
	// configuration a Google Cloud deployment runs.
	fusedRecall, fusedMRR := scoreFusedGoldenSet(t, ctx, svc, prefix)
	checkEvalFloors(t, "fused", fusedRecall, fusedMRR, evalFusedRecallFloor, evalFusedMRRFloor)
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
func scoreFusedGoldenSet(t *testing.T, ctx context.Context, svc *Service, prefix string) (recall, mrr float64) {
	t.Helper()
	dim, ok := embed.Dimension("gemini-embedding-001")
	if !ok {
		t.Fatal("the product no longer knows its own embedding width")
	}
	enc := fakeEncoder{dim: dim}
	corpus := embedCorpus(t, ctx, svc, prefix, enc)
	if len(corpus) < 12 {
		t.Fatalf("embedded %d concepts, want the whole corpus", len(corpus))
	}

	filter := store.Filter{Prefixes: []string{prefix}}
	found := 0
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
	recall = float64(found) / float64(len(evalCases))
	mrr /= float64(len(evalCases))
	t.Logf("recall@%d = %.2f (%d/%d), MRR = %.2f, lexical + vector (stand-in encoder)",
		evalK, recall, found, len(evalCases), mrr)
	return recall, mrr
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
