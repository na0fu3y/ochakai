package service

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
)

// This file is the search evaluation harness (issue #528): a golden set
// of questions with the concept each one is expected to surface, scored
// as recall@k and MRR over the shipped demo bundle. Ranking changes land
// with these numbers in the PR; the floors below pin the current
// baseline so a regression fails instead of shipping quietly.
//
// The corpus is examples/demo — the same ten concepts the quick start
// loads — imported under a run-unique prefix so a shared test database
// neither collides nor pollutes, plus three Japanese concepts defined
// inline: the demo bundle is English, and the two-character windows a
// Japanese question is cut into (migration 0036) need Japanese text to
// be exercised at all.
//
// Embeddings are off in the test environment, so the numbers measure
// the lexical half and the fusion around it. That is the half a
// deployment without Vertex gets, and the half every ranking change
// touches first; the vector half needs its own harness when a fake
// encoder exists.

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
	// Japanese, against the inline supplement below. 売上 and 解約 are
	// two-character terms — exactly the shape the trigram index cannot
	// serve and the windowed scan answers.
	{query: "売上が下がった理由", want: "ja/insights/uriage-yomikata"},
	{query: "解約率", want: "ja/metrics/kaiyakuritsu"},
	{query: "受注とは", want: "ja/glossary/juchu"},
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

// Floors pin the measured baseline: recall@10 = 1.00, MRR = 0.85 as of
// the indexed lexical search (lexical-only, 14 cases). They sit under
// those numbers so ordinary noise passes and a real regression does
// not; a change that moves them — either way — should say so in its PR,
// because that is the point of having them.
//
// MRR was 0.90 when this harness landed, against the ILIKE haystack.
// What took it to 0.85 is not a ranking that got worse but one that
// stopped being decided by an accident: a fragment no document contains
// used to earn the largest possible weight (rarity is the weight), and
// concepts whose *name* contained such a fragment collected half of it.
// English stopwords are exactly those fragments now that the query is
// stemmed, so "by" in a title was worth more than any term anybody
// searched for. Removing it leaves the ties it had been hiding: five of
// these ten concepts score identically for "why is revenue down", and
// the order among them is arbitrary. That is the next thing to fix, and
// it is measured here rather than argued about.
const (
	evalK           = 10
	evalRecallFloor = 0.92 // one miss out of 14 passes; two fail
	evalMRRFloor    = 0.78
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

	if n := loadDemoBundle(t, ctx, svc, prefix, actor); n < 10 {
		t.Fatalf("demo bundle shrank to %d documents; the golden set assumes the quick-start ten", n)
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

	filter := store.Filter{Prefixes: []string{prefix}}
	found := 0
	mrr := 0.0
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
			t.Logf("miss: %q did not surface %s in the top %d; got %v", c.query, c.want, evalK, got)
			continue
		}
		found++
		mrr += 1.0 / float64(rank)
		t.Logf("hit: %q -> %s at rank %d", c.query, c.want, rank)
	}

	recall := float64(found) / float64(len(evalCases))
	mrr /= float64(len(evalCases))
	t.Logf("recall@%d = %.2f (%d/%d), MRR = %.2f, lexical only", evalK, recall, found, len(evalCases), mrr)
	if recall < evalRecallFloor {
		t.Errorf("recall@%d fell to %.2f, under the %.2f baseline: a ranking change made the golden set worse", evalK, recall, evalRecallFloor)
	}
	if mrr < evalMRRFloor {
		t.Errorf("MRR fell to %.2f, under the %.2f baseline: the golden set still surfaces but lower than it did", mrr, evalMRRFloor)
	}
}
