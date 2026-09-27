package okf

import (
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
)

func movedTo(id string) func(string) (string, bool) {
	moves := domain.ConceptMoves("computations/revenue", "finance/revenue")
	return func(v string) (string, bool) { return domain.MovePathValue(id, v, moves, false) }
}

// A path is swapped as a token: every other byte of the document — the
// indentation, the comments, the quoting of the values beside it, the
// body — is what the writer left.
func TestRewritePathFieldsSwapsOnlyTheToken(t *testing.T) {
	doc := `---
type: Attested Computation
# the contract
title: Revenue
resource: /computations/revenue/table.md
sources:
  - id: policy
    resource: ../computations/revenue.md   # the definition
    last_modified: 2026-07-24
  - resource: all queries in BigQuery project acme
  - resource: "/computations/revenue.md"
  - resource: '../computations/revenue/it''s.md'
runtime: bigquery
executor: {resource: /computations/revenue/run.py, receipt: [rows]}
attester:
  resource: https://example.com/attester.py
x-owner: finance
---

See [the check](/computations/revenue/run.py).
`
	want := strings.NewReplacer(
		"resource: /computations/revenue/table.md", "resource: /finance/revenue/table.md",
		"resource: ../computations/revenue.md   # the definition", "resource: /finance/revenue.md   # the definition",
		`resource: "/computations/revenue.md"`, `resource: "/finance/revenue.md"`,
		`resource: '../computations/revenue/it''s.md'`, `resource: '/finance/revenue/it''s.md'`,
		"executor: {resource: /computations/revenue/run.py,", "executor: {resource: /finance/revenue/run.py,",
	).Replace(doc)
	out, changed, err := RewritePathFields([]byte(doc), movedTo("metrics/revenue"))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	d, _, err := Parse(out)
	if err != nil {
		t.Fatalf("the rewritten document does not parse: %v", err)
	}
	if d.Executor == nil || d.Executor.Resource != "/finance/revenue/run.py" {
		t.Errorf("executor = %+v", d.Executor)
	}
	if again, changed, _ := RewritePathFields(out, movedTo("metrics/revenue")); changed || string(again) != string(out) {
		t.Error("a second pass changed a document with nothing left to move")
	}
}

// A value the token swap cannot find on one line — a block scalar — is
// still rewritten, by rendering its key's block again.
func TestRewritePathFieldsRendersWhatItCannotSwap(t *testing.T) {
	doc := "---\ntype: Metric\ncomputation: >-\n  /computations/revenue.md\ntitle: Revenue\n---\n\nbody\n"
	out, changed, err := RewritePathFields([]byte(doc), movedTo("metrics/revenue"))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	d, _, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if d.Computation != "/finance/revenue.md" {
		t.Errorf("computation = %q\n%s", d.Computation, out)
	}
	if !strings.Contains(string(out), "title: Revenue\n") || !strings.HasSuffix(string(out), "\n---\n\nbody\n") {
		t.Errorf("more than the computation block moved:\n%s", out)
	}
}
