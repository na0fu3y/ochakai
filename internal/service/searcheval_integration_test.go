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
//
// Every case carries the dimension of query behaviour it exercises, and
// both runs report a recall/MRR line per dimension beside the
// aggregate. The floors hold only the aggregate — a dimension is a
// handful of cases, all noise — but the per-dimension lines are what
// turn "MRR moved" into "the orthography cases moved", which is the
// difference between knowing a change landed and knowing where to aim
// the next one.

// evalCase is one golden question: a query somebody would actually ask,
// the concept a good ranking puts in front of them, and the dimension of
// query behaviour the case exercises.
type evalCase struct {
	query string
	want  string // id relative to the import prefix
	// accept names the other concepts that answer the same question, for
	// the questions whose answer the bundle deliberately keeps in more
	// than one place — the 経理 mismatch is a policy, a definition and a
	// month-end note at once, and the bundle says so by linking them.
	// The rank scored is the first acceptable concept (want or accept):
	// a case that punishes one correct answer for not being the chosen
	// one measures tie order, not ranking.
	accept []string
	// dim assigns the case to one line of the per-dimension report the
	// scorer logs. The aggregate says whether a ranking change moved
	// anything; the dimension says where, which is what makes the next
	// change targetable. Dimensions are diagnostic only — a handful of
	// cases is inside its own noise, so no floor reads them.
	dim string
}

// The dimensions, in the order a reader meets them below. A new case
// takes the dimension that names what makes it hard, not a new label: a
// report of one-case dimensions says nothing.
const (
	dimQuestion    = "question"    // natural questions, corpus language
	dimKeyword     = "keyword"     // bare terms and jargon lookups
	dimMixed       = "mixed"       // warehouse English inside a Japanese sentence
	dimEnglish     = "english"     // English asked of a Japanese base
	dimKatakana    = "katakana"    // loanwords joined by the ー mark
	dimOrthography = "orthography" // spelling variants of the same word
	dimSynonyms    = "synonyms"    // names only the synonyms key holds
	dimShort       = "short"       // two-character terms below trigram
)

// evalCases is the golden set. Keep queries phrased as questions and
// keywords a data agent would send — the point is aboutness, not exact
// title matches (those are pinned separately by the name-bonus tests in
// the store).
var evalCases = []evalCase{
	// Questions against the demo bundle, in the language it is written
	// in.
	{query: "なぜ売上が落ちているのか", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "月次売上", want: "queries/sales/monthly-revenue", dim: dimKeyword},
	{query: "チャネル別の売上", want: "queries/sales/revenue-by-channel", dim: dimKeyword},
	{query: "売上とは何か", want: "metrics/revenue", dim: dimQuestion},
	{query: "完了した注文とは", want: "glossary/completed-order", dim: dimQuestion},
	{query: "リピート購入率", want: "metrics/repeat-purchase-rate", dim: dimKeyword},
	{query: "チャネルコードの一覧", want: "references/order-channel-codes", dim: dimQuestion},
	{query: "売上計上のルール", want: "policies/revenue-recognition", dim: dimQuestion},
	{query: "注文テーブルのスキーマ", want: "tables/shop-orders", dim: dimQuestion},
	{query: "BigQuery のクエリはどう実行するか", want: "skills/run-bigquery-query", dim: dimQuestion},
	{query: "月末の着地見込み", want: "insights/着地見込み", dim: dimKeyword},
	// A second pass over the same corpus, phrased the way somebody asks
	// rather than the way a title reads. Fourteen cases put MRR inside
	// its own noise — one case moving from rank 1 to 2 moved it by 0.036
	// — so the set is widened as far as twelve concepts honestly
	// support. Beyond that a case stops being a question anybody has and
	// becomes a restatement of a title, which measures the fixture.
	{query: "売上が下がった理由", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "お盆", want: "insights/reading-revenue", dim: dimKeyword},
	{query: "普通の月はいくらか", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "大口の注文で山ができた", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "パーティションの遅れ", want: "insights/reading-revenue",
		accept: []string{"tables/shop-orders"}, dim: dimQuestion},
	{query: "季節性", want: "insights/reading-revenue", dim: dimKeyword},
	{query: "売上に税は含まれるか", want: "metrics/revenue", dim: dimQuestion},
	{query: "純売上との違い", want: "metrics/revenue", dim: dimQuestion},
	{query: "どの注文を売上として数えるか", want: "policies/revenue-recognition",
		accept: []string{"glossary/completed-order"}, dim: dimQuestion},
	{query: "返金は引くのか", want: "policies/revenue-recognition",
		accept: []string{"metrics/revenue"}, dim: dimQuestion},
	{query: "キャンセルした注文", want: "glossary/completed-order", dim: dimKeyword},
	{query: "受注と完了の違い", want: "glossary/completed-order", dim: dimQuestion},
	{query: "売上が伸びているチャネル", want: "queries/sales/revenue-by-channel", dim: dimQuestion},
	{query: "今年度の売上を月ごとに", want: "queries/sales/monthly-revenue", dim: dimQuestion},
	{query: "買い直した客の割合", want: "metrics/repeat-purchase-rate", dim: dimQuestion},
	{query: "ゲスト購入", want: "metrics/repeat-purchase-rate", dim: dimKeyword},
	{query: "注文はどこに入っているか", want: "tables/shop-orders", dim: dimQuestion},
	{query: "receipt には何を返すのか", want: "skills/run-bigquery-query", dim: dimMixed},
	{query: "今月はどうなりそうか", want: "insights/着地見込み", dim: dimQuestion},
	// A third pass, added with the dimension report: questions whose
	// answer the bundle holds as a fact rather than as a title — the
	// payday spike, the 与信 hold, the split shipment — and the questions
	// the bundle answers in several linked places at once, which is what
	// accept is for. Each anchors to prose one concept actually carries;
	// none restates a name.
	{query: "今日の売上が少なく見える", want: "tables/shop-orders",
		accept: []string{"insights/reading-revenue"}, dim: dimQuestion},
	{query: "経理の締めと合わないのはなぜか", want: "policies/revenue-recognition",
		accept: []string{"metrics/revenue", "glossary/completed-order", "insights/着地見込み"},
		dim:    dimQuestion},
	{query: "月がずれる", want: "tables/shop-orders",
		accept: []string{"queries/sales/monthly-revenue", "insights/着地見込み"}, dim: dimQuestion},
	{query: "一部返品された注文はどう数えるか", want: "glossary/completed-order",
		accept: []string{"policies/revenue-recognition", "metrics/revenue"}, dim: dimQuestion},
	{query: "アプリからの注文はどのコードか", want: "references/order-channel-codes", dim: dimQuestion},
	{query: "どのサービスアカウントで実行するか", want: "skills/run-bigquery-query", dim: dimQuestion},
	{query: "25日の山", want: "insights/着地見込み", dim: dimKeyword},
	{query: "分割出荷", want: "glossary/completed-order", dim: dimKeyword},
	{query: "遡る期間", want: "metrics/repeat-purchase-rate", dim: dimKeyword},
	// Katakana loanwords, which is most of the vocabulary a data team
	// writes in: the terms below are the only place each word appears in
	// the corpus, so a case answers only if the term was looked up as a
	// term. Every one of them is joined by ー — テーブル, ロード,
	// カレンダー — and that mark is where a katakana word is cut, so
	// these measure whether a loanword can be searched for at all.
	{query: "ロケーション", want: "skills/run-bigquery-query", dim: dimKatakana},
	{query: "データセット", want: "skills/run-bigquery-query", dim: dimKatakana},
	{query: "毎時ロード", want: "tables/shop-orders", dim: dimKatakana},
	{query: "カレンダー", want: "insights/reading-revenue",
		accept: []string{"insights/着地見込み", "queries/sales/monthly-revenue"}, dim: dimKatakana},
	{query: "フィード", want: "queries/sales/revenue-by-channel", dim: dimKatakana},
	// Warehouse English inside a Japanese sentence, which is how a data
	// agent actually talks to this base: the column name stays English,
	// the question around it does not. scriptRuns is what keeps the two
	// halves findable (store/search.go), and nothing measured it.
	{query: "channel_code が null の行", want: "tables/shop-orders",
		accept: []string{"queries/sales/revenue-by-channel", "references/order-channel-codes"},
		dim:    dimMixed},
	{query: "created_at はどのタイムゾーンか", want: "tables/shop-orders", dim: dimMixed},
	{query: "from_date は必須か", want: "queries/sales/revenue-by-channel", dim: dimMixed},
	// English, and still against the shipped bundle: the names the
	// warehouse supplies stayed English when the prose became Japanese,
	// which is what a Japanese team's own base looks like. A base that
	// could not be asked `total_price` would have translated the wrong
	// half.
	{query: "total_price column", want: "tables/shop-orders", dim: dimEnglish},
	{query: "web_direct", want: "references/order-channel-codes", dim: dimEnglish},
	{query: "channel_code enum", want: "references/order-channel-codes", dim: dimEnglish},
	{query: "status completed", want: "glossary/completed-order", dim: dimEnglish},
	// The whole English question, not only the keyword — the exact input
	// queryFragments was written around (store/search.go names it), sent
	// by the teammate who does not read the base's language. The stemmer
	// drops its stopwords and "revenue" reaches the synonyms key, so
	// what this measures is which of the concepts sharing that one word
	// comes first for a question none of them contains.
	{query: "why is revenue down", want: "insights/reading-revenue",
		accept: []string{"metrics/revenue"}, dim: dimEnglish},
	{query: "customer_id is null", want: "tables/shop-orders", dim: dimEnglish},
	// Spelling variants of words the bundle writes another way. These
	// are the improvement backlog, kept as measured cases rather than as
	// a to-do: 売り上げ is the okurigana spelling every IME offers
	// first, and its windows (売り, 上げ) share nothing with the 売上
	// the corpus writes — the case rides entirely on the rest of the
	// sentence, which is what a reader typing it gets. チャンネル shares
	// two of its four windows with チャネル, so the n-gram treatment
	// absorbs that variant on its own. Full-width ＢｉｇＱｕｅｒｙ is
	// what a Japanese IME yields mid-sentence; no width folding exists,
	// so the Latin term is lost and the Japanese half of the sentence
	// has to carry the case. A normalization that closes any of these
	// moves this dimension's line without touching the others — that is
	// the report doing its job.
	{query: "売り上げの定義", want: "metrics/revenue", dim: dimOrthography},
	{query: "チャンネルごとの売上", want: "queries/sales/revenue-by-channel", dim: dimOrthography},
	{query: "ＢｉｇＱｕｅｒｙの実行", want: "skills/run-bigquery-query", dim: dimOrthography},
	// The other names the writer gave the metric (design doc 0105).
	// "top line" appears nowhere in the bundle but the synonyms key, so
	// this case answers only when the haystack reads it — and unlike the
	// "net sales" it replaced, it is a name for this metric rather than
	// for the 純売上 the concept spends a paragraph saying it is not.
	// トップライン is the same key's Japanese entry, windowed instead of
	// stemmed — the two spellings take different paths to the same row.
	{query: "top line", want: "metrics/revenue", dim: dimSynonyms},
	{query: "トップライン", want: "metrics/revenue", dim: dimSynonyms},
	// Two-character terms — exactly the shape the trigram index cannot
	// serve and the windowed scan answers. 解約 lives in the inline
	// supplement below, in a corpus with no other home for it; 与信 is
	// the demo's own, one concept's single sentence about credit holds.
	{query: "解約率", want: "ja/metrics/kaiyakuritsu", dim: dimShort},
	{query: "解約の分母", want: "ja/metrics/kaiyakuritsu", dim: dimShort},
	{query: "与信", want: "glossary/completed-order", dim: dimShort},
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
// Japanese half, to 37 when the demo became Japanese and the two
// supplement concepts the bundle had come to duplicate were gone, and
// stands at 61: 56 with the dimension labels, and five more when the
// katakana dimension was added to measure migration 0042. Fourteen put MRR inside its
// own noise: one case moving from rank 1 to rank 2 moved it by 0.036,
// which is the size of the differences anybody was reading. The second
// pass asks the same corpus the way somebody asks rather than the way a
// title reads — 「お盆」「ゲスト購入」「返金は引くのか」 — and twelve
// concepts do not honestly support many more *of that kind*. The third
// pass got past that limit two ways: by anchoring to facts rather than
// concepts (与信, 分割出荷, the 25日 spike — one concept holds several
// facts somebody asks for separately), and by asking the same fact in
// the spellings the corpus does not use (売り上げ, チャンネル,
// ＢｉｇＱｕｅｒｙ), which is where queries actually differ from
// documents. accept opened the questions the bundle deliberately
// answers in several linked places, which single-answer scoring had
// been excluding as ambiguous.
//
// **The numbers moved when the demo became Japanese, and the two runs
// are not comparable.** Both the corpus and the questions changed:
// lexical went 0.90 → 0.78 and fused 0.85 → 0.77, while recall went to
// 1.00 on both halves — nothing fell out of the top ten. They moved
// again at 56 cases (lexical 0.78 → 0.80, fused 0.77 → 0.78), and
// again the runs are not comparable: the set changed under the number,
// partly because accept stopped charging the multi-homed questions to
// the ranking.
//
// **Migration 0042 is the first entry here that moved the number
// without the set changing under it**: at 61 cases, lexical went 0.82 →
// 0.83 and fused stayed 0.80, all of it in the question dimension (0.76
// → 0.79). That is the honest size of it, and it is much smaller than
// what the migration actually bought — the golden set scores rank, and
// what a loanword lookup fixes is the candidate set: over this corpus
// the eight katakana probes went from returning all twelve concepts
// each to returning 2.6 on average. A twelve-concept corpus cannot
// charge for the nine wrong answers underneath a right one, which is
// this harness's own limit written down.
//
// The 37-case story below still names the two effects that govern this
// corpus:
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
	// Lexical, measured at 1.00 / 0.83. The MRR floor is two cases'
	// worth under it: one case slipping from rank 1 to rank 2 is 0.008
	// here, so a floor closer than that fails on noise.
	evalRecallFloor = 0.97
	evalMRRFloor    = 0.80
	// Fused: the same questions with the stand-in encoder on, measured
	// at 1.00 / 0.80. This floor is the one that catches the verified
	// addend going back to 0.002 — that constant is the kind that gets
	// nudged by whoever is looking at one query — so it was re-measured
	// when it was set: at 0.002 the 37-case fused run scored 0.73, and
	// the floor sat between.
	//
	// It read 0.75 against 0.78 measured at 56 cases, 0.74 against 0.77
	// when the demo became Japanese,
	// 0.83 against 0.85 over the bilingual corpus, and before that 0.86
	// against 0.89. Each move came from the corpus or the set changing
	// under it, not from the merge; what the number certifies is still
	// only that the merge does no harm.
	evalFusedRecallFloor = 0.97
	evalFusedMRRFloor    = 0.77
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
// reasons in cases: one case slipping from rank 1 to rank 2 is 0.009
// here, one case leaving the top k is 0.018, and the floors above were
// chosen as about two cases under what was measured. A tolerance written
// as 0.05 would have to be rewritten every time the set grows, and the
// set has been 14, then 36, then 41, then 37, then 56, then 61.
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
	filter := store.Filter{Prefixes: []string{prefix}}
	recall, mrr := scoreGoldenSet(t, lexical, prefix, func(query string) []domain.SearchHit {
		hits, _, err := svc.Search(ctx, query, filter, evalK)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return hits
	})
	checkEvalFloors(t, lexical, recall, mrr, evalRecallFloor, evalMRRFloor)

	// The same questions with the vector half on, which is the
	// configuration a Google Cloud deployment runs.
	fusedRecall, fusedMRR := scoreFusedGoldenSet(t, ctx, svc, prefix)
	checkEvalFloors(t, "fused", fusedRecall, fusedMRR, evalFusedRecallFloor, evalFusedMRRFloor)
}

// rank is the position of the first acceptable concept — want, or one of
// accept — in the hits, or zero when none is in them. The first
// acceptable, not want's own: for the questions the bundle answers in
// several linked places, want records the best entry point and accept
// keeps the case from punishing the others for existing.
func (c evalCase) rank(prefix string, hits []domain.SearchHit) int {
	acceptable := map[string]bool{prefix + "/" + c.want: true}
	for _, id := range c.accept {
		acceptable[prefix+"/"+id] = true
	}
	for i, h := range hits {
		if acceptable[h.ID] {
			return i + 1
		}
	}
	return 0
}

// scoreGoldenSet runs every case through search and reports recall@k and
// MRR, naming the configuration in the line it logs — a number without
// its configuration beside it is the thing this harness exists to stop.
// Both configurations run through this one loop, so what a hit is and
// what gets logged cannot drift between them.
//
// Beside the aggregate it logs one line per dimension. The aggregate
// says whether a ranking change moved anything; the dimension lines say
// where — an orthography change that costs the mixed-script cases shows
// up as two lines moving in opposite directions, which the aggregate
// would have averaged into silence. They carry no floors: the smallest
// dimension is two cases, and everything on this page that reasons about
// noise reasons in cases.
func scoreGoldenSet(t *testing.T, config, prefix string, search func(query string) []domain.SearchHit) (recall, mrr float64) {
	t.Helper()
	type tally struct {
		cases, found int
		rr           float64
	}
	byDim := map[string]*tally{}
	found := 0
	for _, c := range evalCases {
		d := byDim[c.dim]
		if d == nil {
			d = &tally{}
			byDim[c.dim] = d
		}
		d.cases++
		hits := search(c.query)
		rank := c.rank(prefix, hits)
		if rank == 0 {
			got := make([]string, 0, len(hits))
			for _, h := range hits {
				got = append(got, strings.TrimPrefix(h.ID, prefix+"/"))
			}
			t.Logf("miss (%s, %s): %q did not surface %s in the top %d; got %v",
				config, c.dim, c.query, c.want, evalK, got)
			continue
		}
		found++
		mrr += 1.0 / float64(rank)
		d.found++
		d.rr += 1.0 / float64(rank)
		// The id that answered, not want: when an accept answers first,
		// the line says which concept the reader actually met.
		t.Logf("hit (%s, %s): %q -> %s at rank %d",
			config, c.dim, c.query, strings.TrimPrefix(hits[rank-1].ID, prefix+"/"), rank)
	}
	recall = float64(found) / float64(len(evalCases))
	mrr /= float64(len(evalCases))
	for _, dim := range []string{dimQuestion, dimKeyword, dimMixed, dimEnglish,
		dimKatakana, dimOrthography, dimSynonyms, dimShort} {
		d := byDim[dim]
		if d == nil {
			t.Fatalf("dimension %s has no cases; drop it from this list or give it one", dim)
		}
		t.Logf("  %-11s recall@%d = %d/%d, MRR = %.2f (%s)",
			dim, evalK, d.found, d.cases, d.rr/float64(d.cases), config)
	}
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
	return scoreGoldenSet(t, "fused", prefix, func(query string) []domain.SearchHit {
		lexical, err := svc.Store.SearchLexical(ctx, query, filter, evalK*2)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		vectors, err := enc.Embed(ctx, embed.TaskQuery, []string{query})
		if err != nil {
			t.Fatal(err)
		}
		return rrfFuse(query, evalK, lexical, nearest(corpus, vectors[0], evalK*2))
	})
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
