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
// as recall@k and MRR over the bundles this repository ships. Ranking
// changes land with these numbers in the PR; the floors below pin the
// current baseline so a regression fails instead of shipping quietly.
//
// **The corpus is two bundles and one base**, imported under a
// run-unique prefix so a shared test database neither collides nor
// pollutes: examples/demo, the eighteen concepts the quick start loads,
// plus kb/bundle, the nine concepts of ochakai's own development
// knowledge, plus the two Japanese concepts defined inline — 29 in all.
//
// The second bundle is here because one was not a base, it was a topic.
// Every question asked of examples/demo alone is a question about a
// shop asked of a corpus entirely about that shop, so sharing 売上 with
// every neighbour is the normal condition and no measurement can
// separate "matched the subject" from "matched Japanese". With two
// domains it can, and the separation has a number: leak, below. It
// found a defect on the first run.
//
// The supplement used to be the only Japanese here, because the demo had
// none and the two-character windows a Japanese question is cut into
// (migration 0036) need Japanese text to be exercised at all. That is
// the arrangement this file should be read as a warning about: what the
// harness measured was a fixture nobody ships, while a Japanese reader
// following the quick start typed a Japanese question at the demo and
// got silence. The demo is written in Japanese now — that is what
// ochak.ai's demo is — so the questions below are asked of the shipped
// bundle in the language it is written in. The supplement is down to
// what the shipped bundles have no home for: the term (解約率 — the
// demo's store sells no subscriptions) and the orthography (a concept
// written in halfwidth katakana and fullwidth latin, which no bundle is,
// because people type carefully). The two that stood in for 売上 and
// 受注 are gone, because the bundle says both itself and a fixture
// competing with the concept it stands in for measures nothing.
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

// evalKBPrefix is the path segment the project's own bundle is loaded
// under, and so the test for which domain a concept or a hit belongs
// to. A real base is not one topic — this one is a shop's analytics and
// ochakai's own development knowledge, sharing nothing but the language
// they are written in.
const evalKBPrefix = "kb"

// inKB reports whether an id relative to the run prefix is in the
// project's own bundle rather than the shop's.
func inKB(id string) bool { return strings.HasPrefix(id, evalKBPrefix+"/") }

// evalDimensions is the order the per-dimension lines are reported in,
// and the list both reports read: a dimension named here with no cases
// under it fails the run rather than printing an empty line.
var evalDimensions = []string{dimQuestion, dimKeyword, dimMixed, dimEnglish,
	dimKatakana, dimOrthography, dimSynonyms, dimShort}

// evalCases is the golden set. Keep queries phrased as questions and
// keywords a data agent would send — the point is aboutness, not exact
// title matches (those are pinned separately by the name-bonus tests in
// the store).
var evalCases = []evalCase{
	// Questions against the demo bundle, in the language it is written
	// in.
	{query: "なぜ売上が落ちているのか", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "月次売上", want: "queries/sales/monthly-revenue", dim: dimKeyword},
	{query: "獲得チャネル別の売上", want: "queries/sales/revenue-by-traffic-source", dim: dimKeyword},
	{query: "売上とは何か", want: "metrics/revenue", dim: dimQuestion},
	{query: "完了した注文とは", want: "glossary/completed-order", dim: dimQuestion},
	{query: "リピート購入率", want: "metrics/repeat-purchase-rate", dim: dimKeyword},
	{query: "返品率", want: "metrics/return-rate", dim: dimKeyword},
	{query: "売上計上のルール", want: "policies/revenue-recognition", dim: dimQuestion},
	{query: "注文テーブルのスキーマ", want: "tables/orders", dim: dimQuestion},
	{query: "BigQuery のクエリはどう実行するか", want: "skills/run-bigquery-query", dim: dimQuestion},
	{query: "オントロジー", want: "glossary/ontology", dim: dimKeyword},
	{query: "アクションはどう実行するか", want: "skills/run-an-action", dim: dimQuestion},
	{query: "返品率の高い商品", want: "actions/review-high-return-products",
		accept: []string{"metrics/return-rate"}, dim: dimKeyword},
	{query: "データセットはどこにあるか", want: "references/thelook-dataset", dim: dimQuestion},
	// A second pass over the same corpus, phrased the way somebody asks
	// rather than the way a title reads. Beyond this kind a case stops
	// being a question anybody has and becomes a restatement of a title,
	// which measures the fixture.
	{query: "売上が下がった理由", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "右肩上がりの成長", want: "insights/reading-revenue",
		accept: []string{"references/thelook-dataset", "metrics/revenue"}, dim: dimKeyword},
	{query: "前年比がプラスなのは成果か", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "先週見た数字と合わない", want: "insights/reading-revenue",
		accept: []string{"references/thelook-dataset", "policies/revenue-recognition"},
		dim:    dimQuestion},
	{query: "過去の月の数字が動く", want: "policies/revenue-recognition",
		accept: []string{"glossary/completed-order", "references/thelook-dataset",
			"metrics/return-rate", "insights/reading-revenue"},
		dim: dimQuestion},
	{query: "売上に税は含まれるか", want: "metrics/revenue", dim: dimQuestion},
	{query: "GMV との違い", want: "metrics/revenue",
		accept: []string{"queries/sales/monthly-bookings"}, dim: dimQuestion},
	{query: "受注ベースの合計", want: "queries/sales/monthly-bookings",
		accept: []string{"glossary/completed-order"}, dim: dimKeyword},
	{query: "どの明細を売上として数えるか", want: "policies/revenue-recognition",
		accept: []string{"glossary/completed-order", "metrics/revenue"}, dim: dimQuestion},
	{query: "キャンセルした注文", want: "glossary/completed-order", dim: dimKeyword},
	{query: "一部返品はどう数えるか", want: "glossary/completed-order",
		accept: []string{"metrics/return-rate", "tables/order-items"}, dim: dimQuestion},
	{query: "注文の金額はどの列にあるか", want: "tables/order-items",
		accept: []string{"tables/orders"}, dim: dimQuestion},
	{query: "数量の列が無いのはなぜか", want: "tables/order-items", dim: dimQuestion},
	{query: "定価と実売の違い", want: "tables/products",
		accept: []string{"tables/order-items"}, dim: dimQuestion},
	{query: "商品カテゴリで売上を割る", want: "tables/products",
		accept: []string{"insights/reading-revenue"}, dim: dimQuestion},
	{query: "会員登録と初回購入は別か", want: "tables/users",
		accept: []string{"metrics/repeat-purchase-rate"}, dim: dimQuestion},
	{query: "遡る期間", want: "metrics/repeat-purchase-rate", dim: dimKeyword},
	{query: "決定草案はどこに書くか", want: "skills/run-an-action", dim: dimQuestion},
	{query: "月の途中の数字の読み方", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "検証済みのクエリはどれか", want: "queries/sales/monthly-revenue",
		accept: []string{"tables/order-items", "metrics/revenue"}, dim: dimQuestion},
	// A third pass: questions whose answer the bundle holds as a fact
	// rather than as a title — the lowercase-'complete' trap, the moving
	// past, the missing money column — and the questions the bundle
	// answers in several linked places at once, which is what accept is
	// for. Each anchors to prose one concept actually carries; none
	// restates a name.
	{query: "今月の売上が少なく見える", want: "insights/reading-revenue", dim: dimQuestion},
	{query: "status に履歴はあるか", want: "glossary/completed-order",
		accept: []string{"tables/orders", "policies/revenue-recognition",
			"references/thelook-dataset"},
		dim: dimQuestion},
	{query: "ゲスト購入はあるか", want: "tables/orders", dim: dimQuestion},
	{query: "候補が0行だったらどうするか", want: "skills/run-an-action", dim: dimQuestion},
	{query: "実行してよいパラメータの範囲", want: "actions/review-high-return-products",
		accept: []string{"skills/run-an-action"}, dim: dimQuestion},
	{query: "クエリの課金は誰持ちか", want: "references/thelook-dataset",
		accept: []string{"skills/run-bigquery-query"}, dim: dimQuestion},
	// Katakana loanwords, which is most of the vocabulary a data team
	// writes in. Every one of these is joined by ー — ロケーション,
	// フィード, キャンペーン — and that mark is where a katakana word
	// used to be cut (migration 0042), so they measure whether a
	// loanword can be searched for as a word at all. Each is anchored
	// where the corpus actually says it: フィード and キャンペーン
	// appear in one concept only, ロケーション in the two that name the
	// dataset's region, プラットフォーム in the one concept that
	// contrasts ochakai with one.
	{query: "ロケーション", want: "skills/run-bigquery-query",
		accept: []string{"references/thelook-dataset"}, dim: dimKatakana},
	{query: "フィード", want: "queries/sales/revenue-by-traffic-source", dim: dimKatakana},
	{query: "キャンペーンの振り返り", want: "queries/sales/revenue-by-traffic-source",
		dim: dimKatakana},
	{query: "プラットフォーム", want: "glossary/ontology", dim: dimKatakana},
	{query: "レポートに書いてよい数字か", want: "metrics/repeat-purchase-rate",
		accept: []string{"queries/sales/monthly-bookings"}, dim: dimKatakana},
	// Warehouse English inside a Japanese sentence, which is how a data
	// agent actually talks to this base: the column name stays English,
	// the question around it does not. scriptRuns is what keeps the two
	// halves findable (store/search.go), and nothing measured it.
	{query: "sale_price はどの表にあるか", want: "tables/order-items",
		accept: []string{"tables/orders"}, dim: dimMixed},
	{query: "status が Complete の明細", want: "glossary/completed-order",
		accept: []string{"tables/order-items", "queries/sales/monthly-revenue"}, dim: dimMixed},
	{query: "traffic_source は何の値か", want: "tables/users",
		accept: []string{"queries/sales/revenue-by-traffic-source"}, dim: dimMixed},
	{query: "num_of_item とは", want: "tables/orders", dim: dimMixed},
	{query: "receipt には何を返すのか", want: "skills/run-bigquery-query",
		accept: []string{"skills/run-an-action"}, dim: dimMixed},
	{query: "min_sold は必須か", want: "actions/review-high-return-products", dim: dimMixed},
	{query: "department は2値か", want: "tables/products", dim: dimMixed},
	// English, and still against the shipped bundle: the names the
	// warehouse supplies stayed English when the prose became Japanese,
	// which is what a Japanese team's own base looks like. A base that
	// could not be asked `sale_price` would have translated the wrong
	// half.
	{query: "sale_price column", want: "tables/order-items", dim: dimEnglish},
	{query: "status Complete", want: "glossary/completed-order",
		accept: []string{"tables/order-items"}, dim: dimEnglish},
	{query: "traffic_source values", want: "tables/users", dim: dimEnglish},
	{query: "thelook_ecommerce dataset", want: "references/thelook-dataset", dim: dimEnglish},
	{query: "order_items schema", want: "tables/order-items", dim: dimEnglish},
	// The whole English question, not only the keyword — the exact input
	// queryFragments was written around (store/search.go names it), sent
	// by the teammate who does not read the base's language. The stemmer
	// drops its stopwords and "revenue" reaches the synonyms key, so
	// what this measures is which of the concepts sharing that one word
	// comes first for a question none of them contains.
	{query: "why is revenue down", want: "insights/reading-revenue",
		accept: []string{"metrics/revenue"}, dim: dimEnglish},
	// Spelling variants of words the bundle writes another way. These
	// are the improvement backlog, kept as measured cases rather than as
	// a to-do: 売り上げ is the okurigana spelling every IME offers
	// first, and its windows (売り, 上げ) share nothing with the 売上
	// the corpus writes — the case rides entirely on the rest of the
	// sentence, which is what a reader typing it gets. オントロジ drops
	// the long vowel and keeps most of its windows, so the n-gram
	// treatment absorbs that variant on its own. Full-width
	// ＢｉｇＱｕｅｒｙ is what a Japanese IME yields mid-sentence; no
	// width folding exists, so the Latin term is lost and the Japanese
	// half of the sentence has to carry the case. A normalization that
	// closes any of these moves this dimension's line without touching
	// the others — that is the report doing its job.
	{query: "売り上げの定義", want: "metrics/revenue", dim: dimOrthography},
	{query: "オントロジとは", want: "glossary/ontology", dim: dimOrthography},
	{query: "ＢｉｇＱｕｅｒｙの実行", want: "skills/run-bigquery-query", dim: dimOrthography},
	// The other names the writer gave the metric (design doc 0105).
	// "top line" appears nowhere in the bundle but the synonyms key, so
	// this case answers only when the haystack reads it. トップライン is
	// the same key's Japanese entry, windowed instead of stemmed — the
	// two spellings take different paths to the same row. "return rate"
	// is the return-rate metric's own synonyms entry, though the action
	// carries the words as a SQL alias too.
	{query: "top line", want: "metrics/revenue", dim: dimSynonyms},
	{query: "トップライン", want: "metrics/revenue", dim: dimSynonyms},
	{query: "return rate", want: "metrics/return-rate",
		accept: []string{"actions/review-high-return-products"}, dim: dimSynonyms},
	// Two-character terms — exactly the shape the trigram index cannot
	// serve and the windowed scan answers. 解約 lives in the inline
	// supplement below, in a corpus with no other home for it; 粗利 and
	// 定価 are the demo's own, a couple of sentences in the products
	// concept about which price column means what.
	// The project's own bundle, in the same base (evalKBPrefix). These
	// are the questions a developer asks ochakai about ochakai, and they
	// are here for two reasons.
	//
	// One is what they are made of: real developer Japanese, written by
	// whoever learned the thing, with the English of the trade left
	// standing inside it — MCP, testdb.Unique, DOC-LINES, ICU. The demo
	// is prose about a store, and however carefully it is written it is
	// written by somebody who knew a search would read it.
	//
	// The other is what they are asked *against*. Every case above is a
	// question about a store, asked of a base that is entirely about
	// that store, where sharing 売上 with every neighbour is the normal
	// condition. A base holding two unrelated domains is what a team
	// actually has, and it is the only arrangement in which a question
	// can be asked whether it stayed in its own — which is the leak
	// measured below.
	//
	// Two of them are deliberately words the store also uses. 行数 is a
	// row count over there and a page's length here; 書き戻す is an
	// outcome reported there and a concept landing in kb/ here. Same
	// characters, unrelated subjects — which is the whole question a
	// two-domain base is able to ask.
	{query: "ツールは少ないほうが安いのか", want: "kb/insights/mcp-bytes-not-tool-count", dim: dimQuestion},
	{query: "semantica との比較", want: "kb/insights/mcp-bytes-not-tool-count", dim: dimQuestion},
	{query: "テストの id はどこから取るか", want: "kb/insights/store-tests-shared-db", dim: dimQuestion},
	{query: "自前の名前空間", want: "kb/insights/store-tests-shared-db", dim: dimKeyword},
	{query: "日本語にすると行数は減るか", want: "kb/insights/translation-costs-lines", dim: dimQuestion},
	{query: "見出しを訳すとリンクが死ぬ", want: "kb/insights/translation-costs-lines", dim: dimQuestion},
	{query: "AI が verify を打ってよいか", want: "kb/policies/ai-human-identity", dim: dimQuestion},
	{query: "匿名で記録されてしまう", want: "kb/policies/ai-human-identity", dim: dimQuestion},
	{query: "Docker が無い環境でインスタンスを立てる", want: "kb/skills/dogfood-instance-without-docker", dim: dimMixed},
	{query: "リモートセッションで正直に言えるチェック", want: "kb/skills/remote-session-checks", dim: dimQuestion},
	{query: "スクリーンショットの撮り直しで踏む罠", want: "kb/skills/webui-screenshots", dim: dimQuestion},
	{query: "ダークスキーム", want: "kb/skills/webui-screenshots", dim: dimKatakana},
	{query: "文書から concept を起こす線引き", want: "kb/skills/extract-into-kb",
		accept: []string{"kb/skills/remote-session-checks"}, dim: dimMixed},
	{query: "学びをどこに書き戻すか", want: "kb/skills/run-the-dogfood-loop", dim: dimQuestion},
	// The trade's English, inside the base that is written in Japanese
	// around it — the same shape as the warehouse's column names above,
	// arrived at from the other domain.
	{query: "testdb.Unique", want: "kb/insights/store-tests-shared-db", dim: dimEnglish},
	{query: "DOC-LINES", want: "kb/insights/translation-costs-lines", dim: dimEnglish},
	{query: "ICU ロケール", want: "kb/skills/webui-screenshots", dim: dimMixed},
	// The same word the reader types and the document spells another
	// way. Until the index and the query agree on one spelling of a
	// character, these are two islands: a normally-written question
	// cannot reach a halfwidth-written document and the reverse holds
	// too, so what fails here is recall rather than rank.
	{query: "オーダー", want: "ja/ops/export-notes", dim: dimOrthography},
	{query: "エクスポート", want: "ja/ops/export-notes", dim: dimOrthography},
	{query: "ETL", want: "ja/ops/export-notes", dim: dimOrthography},
	{query: "解約率", want: "ja/metrics/kaiyakuritsu", dim: dimShort},
	{query: "解約の分母", want: "ja/metrics/kaiyakuritsu", dim: dimShort},
	{query: "粗利", want: "tables/products", dim: dimShort},
	{query: "定価", want: "tables/products",
		accept: []string{"references/thelook-dataset"}, dim: dimShort},
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
	// A concept written the way a system upstream of the reader writes:
	// halfwidth katakana and fullwidth latin, which is what a warehouse
	// export, an IME mid-sentence and anything that came through a
	// fixed-width system produce. No shipped bundle is written this way
	// — they are written by people who type carefully — which is
	// exactly why the supplement carries it (the 解約率 argument: a
	// shape the bundles cannot exercise).
	//
	// The questions asking for it below are spelled normally, because
	// that is how the reader asks.
	"ja/ops/export-notes": "---\n" +
		"type: Insight\n" +
		"title: ｴｸｽﾎﾟｰﾄの綴り\n" +
		"description: 基幹から届く抽出ファイルの表記ゆれ\n" +
		"tags: [ops]\n" +
		"status: draft\n" +
		"---\n\n" +
		"基幹の ｴｸｽﾎﾟｰﾄ は ｵｰﾀﾞｰ 区分を半角で書く。ＥＴＬ を通しても\n" +
		"綴りはそのまま残るので、読む側で揃えることになる。\n",
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
// supplement concepts the bundle had come to duplicate were gone, to 56
// with the dimension labels, to 61 when the katakana dimension was added
// to measure migration 0042, and was rewritten at 68 when the demo
// became the thelook_ecommerce ontology bundle — eighteen concepts over
// a real public dataset, with an action and its executor beside the
// computations. Fourteen put MRR inside its own noise: one case moving
// from rank 1 to rank 2 moved it by 0.036, which is the size of the
// differences anybody was reading. The second pass asks the same corpus
// the way somebody asks rather than the way a title reads —
// 「先週見た数字と合わない」「ゲスト購入はあるか」 — and the third pass
// anchors to facts rather than concepts (the lowercase-'complete' trap,
// the missing money column, the 0-row action run — one concept holds
// several facts somebody asks for separately) and to the spellings the
// corpus does not use (売り上げ, オントロジ, ＢｉｇＱｕｅｒｙ), which is
// where queries actually differ from documents. accept opened the
// questions the bundle deliberately answers in several linked places,
// which single-answer scoring had been excluding as ambiguous.
//
// **Each rewrite moved the numbers, and the runs are not comparable
// across one.** When the demo became Japanese: lexical 0.90 → 0.78 and
// fused 0.85 → 0.77, recall to 1.00 on both halves. At 56 cases:
// lexical 0.80, fused 0.78. Migration 0042 was the first entry that
// moved the number without the set changing under it — at 61 cases
// lexical 0.82 → 0.83, fused flat at 0.80, all of it in the question
// dimension — and the reach measurement below exists because that is so
// much smaller than what the migration actually bought. The thelook
// rewrite then replaced the corpus and the set together, so its numbers
// start a new series again. Two effects still govern a corpus of this
// shape:
//
//   - A monolingual Japanese corpus about one store shares 売上 across
//     every concept, and the concept whose name *is* 売上 takes the name
//     rule (store/search.go) from the insight that answers the question.
//     「なぜ売上が落ちているのか」 can put metrics/revenue first and
//     insights/reading-revenue second, which is the rule working: the
//     README's next line is `ochakai get insights/reading-revenue`.
//   - An English keyword lands in prose that is Japanese around it, so
//     "sale_price column" ties the concepts that all carry the column
//     name and is settled by verification recency and id. A middling
//     rank is what a Japanese base gives an English keyword, and
//     measuring it is why the English cases stayed.
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
	// Lexical, measured at 1.00 / 0.87 over the thelook corpus. The MRR
	// floor is two cases' worth under it: one case slipping from rank 1
	// to rank 2 is 0.007 here, so a floor closer than that fails on
	// noise.
	evalRecallFloor = 0.98
	evalMRRFloor    = 0.89
	// Fused: the same questions with the stand-in encoder on, measured
	// at 1.00 / 0.87. This floor is the one that catches the verified
	// addend going back to 0.002 — that constant is the kind that gets
	// nudged by whoever is looking at one query — so it was re-measured
	// when it was set: at 0.002 the 37-case fused run scored 0.73, and
	// the floor sat between.
	//
	// It read 0.75 against 0.78 measured at 56 cases, 0.74 against 0.77
	// when the demo became Japanese, 0.83 against 0.85 over the
	// bilingual corpus, and before that 0.86 against 0.89. Each move
	// came from the corpus or the set changing under it, not from the
	// merge; what the number certifies is still only that the merge
	// does no harm.
	//
	// The prose rewrite of the demo bundle moved this one and nothing
	// else: the same eighteen concepts saying the same things in plainer
	// Japanese left the lexical run identical (1.00 / 0.87, reach 10.54)
	// and took the fused run from 0.86 to 0.87. Shorter sentences around
	// the same terms is a smaller haystack for the stand-in to reorder,
	// which is the mixed dimension going 0.74 to 0.81 lexically. It is
	// the corpus changing under the merge again, not the merge.
	evalFusedRecallFloor = 0.98
	evalFusedMRRFloor    = 0.87
)

// Reach: how many concepts a question touched at all.
//
// recall@k and MRR both read one position — where the right answer
// landed — and are blind to what came with it. Migration 0042 was found
// outside this harness for exactly that reason: ー was a word boundary,
// every katakana question had degraded from a lookup to a prefix scan,
// and the golden set said 1.00 recall and MRR 1.00 on those cases,
// because the right concept was still first. What was wrong sat
// underneath it, and no number here could see it.
//
// Reach is that number. It is the length of the lexical list — every
// concept that matched a fragment at all, which is the floor the
// ranking is built on ("matched something", not a score threshold:
// design doc 0068 §3). A question that reaches the whole corpus has
// ranked it, not searched it, and the reader under a byte budget
// (design doc 0093) pays for the difference.
//
// **Lexical only, and that is not a gap to close later.** Cosine is
// never zero, so the vector half reaches every concept in the corpus by
// construction; a fused reach would be the corpus size for every query
// on every run and would measure nothing at all.
//
// **Read it with the recall floor, never alone.** Reach falls to 1.00
// if the search stops finding anything, and it is the recall floor —
// not this ceiling — that says the answers are still there. The two
// numbers are one measurement: recall says nothing fell out, reach says
// how much came with it.
const (
	// High enough that nothing is truncated by it: a reach that hit its
	// own limit would be the limit's number, not the query's, and the
	// measurement fails rather than reporting it.
	evalReachLimit = 200
	// Measured at 10.52 concepts of the 29 this corpus holds. The
	// ceiling sits two cases' worth over it, the width the floors use,
	// and fails in both directions for the floors' reason: a ceiling
	// nobody lowered when a change narrowed the search is budget the
	// next widening spends without moving a number anybody reads.
	//
	// **It was checked against the defect it was written for.** On the
	// previous demo corpus, before migration 0042, the same measurement
	// read 7.44 of 12 against 6.25 after, with the katakana line at
	// 11.00 of 12 before and 2.60 after — every loanword question
	// touching all but one concept in the base. recall and MRR moved by
	// one point across that fix where reach moved by nine concepts,
	// which is the whole argument for measuring this at all.
	//
	// **It moves when the corpus does, and it has twice.** The thelook
	// rewrite replaced twelve concepts with nineteen (10.54, 56% of the
	// base, against 6.25 and 52% before it), and kb/bundle then took it
	// to twenty-eight (12.59, 45%). None of the three is comparable with
	// the others as an absolute count; what the share says is that a
	// second, unrelated domain is the only one of the two growths that
	// made the search narrower relative to what it was searching, which
	// is what adding concepts nobody's question is about should do.
	// The bundle's own prose moved it a third time, by a hair: the
	// Japanese pass over examples/demo cut abstract nominal subjects and
	// one repeated paragraph, and 10.54 became 10.47 with every rank
	// unchanged. Words nobody searches for are still words the search
	// can match on.
	//
	// And back up to 10.49 when insights/reading-revenue was given the
	// magnitudes it was missing: a longer document is matched by more
	// questions, which is the price of the content and not a ranking
	// change — recall stayed 1.00 and **every one of the 88 cases landed
	// on the rank it landed on before**. Both passes together still leave
	// the search narrower than the 10.54 they started from.
	evalReachCeiling = 10.51

	// Leak: of those, how many came from the other bundle. The two
	// domains share nothing but the language, so a leaked hit is the
	// search matching on Japanese rather than on subject.
	//
	// Measured at 2.87 concepts, against 3.24 when this measurement
	// arrived — **and the difference is the defect it was added to
	// find.** 「AI が verify を打ってよいか」 reached 18 of the store's
	// concepts and the demo's own 「department は2値か」 reached all 28
	// in the base, because queryFragments kept a run of one or two
	// characters whole without asking whether it held a content
	// character: the particles が and は arrived as fragments, and
	// fragmentQuery asks for a one-character run as a prefix, so が:*
	// matched nearly every Japanese document there was. It was migration
	// 0042's defect — a boundary in the wrong place — standing for
	// grammar rather than for vocabulary, and one corpus about one
	// subject could not have shown it: reaching every neighbour is
	// honest when every neighbour is about your question.
	//
	// What the particle fix moved was here and nowhere else: reach
	// 12.59 → 12.04, leak 3.24 → 2.87, recall and MRR unchanged at 1.00
	// and 0.90. Taking a loanword whole (store.minLoanword) moved the
	// same two again — reach 12.04 → 10.75, leak 2.87 → 2.19 — and that
	// one moved nothing else at all: **every one of the 85 cases landed
	// on the rank it landed on before**, while the questions touched an
	// eighth fewer concepts and a quarter fewer from the wrong domain.
	// Three numbers, and only the two this file added last had said
	// anything for three changes running — until migration 0043, where
	// recall moved instead: three questions that could not find their
	// concept at all, because the document spelled it ｵｰﾀﾞｰ and the
	// question spelled it オーダー.
	// The Japanese pass over examples/demo took it 2.14 → 2.10, for the
	// reason above: less prose in the demo is less Japanese for a kb
	// question to match on. Giving reading-revenue its magnitudes put
	// 0.01 of that back (2.10 → 2.11), the mirror of the same reason.
	evalLeakCeiling = 2.13

	// What the dimensions read over this corpus, for the next change to
	// aim at: english 12.83, mixed 12.43, question 11.97, keyword
	// 10.20, katakana 8.80, orthography 7.33, synonyms 5.67, short
	// 2.25. english is the widest and also ranks worst (MRR 0.65),
	// which is where the two measurements agree that something is
	// unfinished: an English column name lands in prose that is
	// Japanese around it, matches the concepts that all carry it, and is
	// settled by tie-breaks rather than by aboutness. mixed is nearly as
	// wide and used to rank as badly; the prose rewrite took it to 0.81
	// without narrowing it, which is the same sentence carrying fewer
	// competing terms rather than fewer concepts. short is the
	// narrowest, which is the windowed scan doing exactly what it is
	// for.
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
// set has been 14, then 36, then 41, then 37, then 56, then 61, then 68.
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

// loadBundle imports every document under a bundle root with the given
// id prefix prepended to its path-derived id, and returns the number
// imported. Root-relative links inside the bodies keep pointing at the
// unprefixed ids; that leaves the link graph dangling, which the lexical
// ranking this harness measures does not read.
func loadBundle(t *testing.T, ctx context.Context, svc *Service, root, prefix string, actor domain.Actor) int {
	t.Helper()
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
		t.Fatalf("load bundle %s: %v", root, err)
	}
	return n
}

func TestSearchEvalIntegration(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t, ctx)
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "eval"}
	prefix := uid(t, "svceval")

	if n := loadBundle(t, ctx, svc, filepath.Join("..", "..", "examples", "demo"), prefix, actor); n < 18 {
		t.Fatalf("demo bundle shrank to %d documents; the golden set assumes the quick-start eighteen", n)
	}
	// The project's own bundle, in the same base. Two unrelated domains
	// under one search is what a team's base actually looks like, and it
	// is the only arrangement in which the leak measurement below means
	// anything (evalKBPrefix).
	if n := loadBundle(t, ctx, svc, filepath.Join("..", "..", "kb", "bundle"),
		prefix+"/"+evalKBPrefix, actor); n < 9 {
		t.Fatalf("kb bundle shrank to %d documents; the golden set assumes nine", n)
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

	// What the ranking numbers above cannot see: how much each question
	// touched on its way to the answer.
	corpus, err := svc.SearchOrList(ctx, "", "verified_at", "", filter, 1000)
	if err != nil {
		t.Fatalf("count the corpus: %v", err)
	}
	reach, leak := measureReach(t, ctx, svc, prefix, len(corpus.Hits))
	checkEvalCeiling(t, "reach", reach, evalReachCeiling)
	checkEvalCeiling(t, "leak", leak, evalLeakCeiling)

	// The same questions with the vector half on, which is the
	// configuration a Google Cloud deployment runs.
	fusedRecall, fusedMRR := scoreFusedGoldenSet(t, ctx, svc, prefix)
	checkEvalFloors(t, "fused", fusedRecall, fusedMRR, evalFusedRecallFloor, evalFusedMRRFloor)
}

// measureReach reports the mean number of concepts a question reached,
// with a line per dimension beside it — the same shape the scorer
// reports, so the two are read together.
//
// The worst case is logged by name. A mean says the search narrowed;
// the question sitting at the top of it says what to look at next, and
// that is the line that sent this harness to the prolonged sound mark.
func measureReach(t *testing.T, ctx context.Context, svc *Service, prefix string, corpus int) (reach, leak float64) {
	t.Helper()
	filter := store.Filter{Prefixes: []string{prefix}}
	type tally struct{ cases, reached int }
	byDim := map[string]*tally{}
	total, worst, worstQuery := 0, 0, ""
	leaked, leakWorst, leakWorstQuery := 0, 0, ""
	for _, c := range evalCases {
		hits, err := svc.Store.SearchLexical(ctx, c.query, filter, evalReachLimit)
		if err != nil {
			t.Fatalf("reach %q: %v", c.query, err)
		}
		// How far outside its own domain the question went. The two
		// bundles share nothing but the language they are written in, so
		// a hit from the other one is the search matching on Japanese
		// rather than on subject — the failure the demo alone cannot
		// show, because there every concept is about the same shop and
		// a wide reach is honest.
		crossed := 0
		for _, h := range hits {
			if inKB(strings.TrimPrefix(h.ID, prefix+"/")) != inKB(c.want) {
				crossed++
			}
		}
		leaked += crossed
		if crossed > leakWorst {
			leakWorst, leakWorstQuery = crossed, c.query
		}
		if len(hits) >= evalReachLimit {
			t.Fatalf("%q reached %d concepts, filling the limit this is measured at: "+
				"raise evalReachLimit, or the number is the limit's and not the query's",
				c.query, len(hits))
		}
		total += len(hits)
		if len(hits) > worst {
			worst, worstQuery = len(hits), c.query
		}
		d := byDim[c.dim]
		if d == nil {
			d = &tally{}
			byDim[c.dim] = d
		}
		d.cases++
		d.reached += len(hits)
	}
	for _, dim := range evalDimensions {
		d := byDim[dim]
		t.Logf("  %-11s reach = %.2f concepts", dim, float64(d.reached)/float64(d.cases))
	}
	mean := float64(total) / float64(len(evalCases))
	t.Logf("reach = %.2f concepts of %d (widest: %q at %d), lexical only", mean, corpus, worstQuery, worst)
	leak = float64(leaked) / float64(len(evalCases))
	t.Logf("leak  = %.2f concepts from the other domain (widest: %q at %d)", leak, leakWorstQuery, leakWorst)
	return mean, leak
}

// checkEvalCeiling holds a measured width to its ceiling in both
// directions, the way checkEvalFloors holds a floor: over it is the
// widening the ceiling exists to catch, and further under it than
// evalFloorSlackCases allows is a ceiling nobody lowered when the
// search narrowed.
func checkEvalCeiling(t *testing.T, name string, got, ceiling float64) {
	t.Helper()
	slack := evalFloorSlackCases / float64(len(evalCases))
	switch {
	case got > ceiling:
		t.Errorf(`%s rose to %.2f concepts, over the %.2f ceiling: a change widened what every
question touches. Recall and MRR can both be unmoved while this happens — the right
answer stays where it was and more wrong ones arrive under it.`, name, got, ceiling)
	case ceiling-got > slack:
		// Rounded down, which is the floors' rule read in the other
		// direction: a floor is advised upward and a ceiling downward,
		// because both have to stay on the measurement's side of the
		// number. Rounding a ceiling up advises a value that fails this
		// same check on the next run.
		want := math.Floor((got+slack)*100) / 100
		t.Errorf(`%s measures %.2f concepts against a ceiling of %.2f — %.1f cases' worth of room,
wider than the %d evalFloorSlackCases this file allows. Lower the ceiling to at most
%.2f in the PR that earned it.`,
			name, got, ceiling, (ceiling-got)*float64(len(evalCases)), evalFloorSlackCases, want)
	}
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
	for _, dim := range evalDimensions {
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
