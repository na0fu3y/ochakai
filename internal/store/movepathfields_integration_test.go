package store

import (
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// TestMoveRewritesPathFieldsIntegration pins that a move repairs the
// paths a frontmatter names (OKF SPEC §6.2) the way it repairs body
// links: a Metric citing the moved computation in sources, and the
// computation's own executor pointing at a file in its namespace, both
// follow — edited in place, so a comment beside the value survives.
func TestMoveRewritesPathFieldsIntegration(t *testing.T) {
	ctx, s := movePrefixStore(t)
	run := testdb.Unique(t, "stit-movepath-")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	comp, metric, dst := run+"/computations/revenue", run+"/metrics/revenue", run+"/finance/revenue"
	unrelated := run + "/metrics/revenue-note"

	put := func(id, doc string) {
		t.Helper()
		d, _, err := okf.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		k := d.Knowledge
		k.ID, k.Doc, k.CreatedBy = id, doc, actor
		k.Status = k.Lifecycle()
		k.Links = domain.LinksFromBody(id, k.Body)
		if err := s.Create(ctx, &k, false); err != nil {
			t.Fatal(err)
		}
	}
	put(comp, "---\ntype: Attested Computation\nruntime: bigquery\n"+
		"executor: {resource: /"+comp+"/run.py}\n"+
		"attester:\n  resource: ./attesters/eq.py\n---\n\nSELECT 1\n")
	put(metric, "---\ntype: Metric\nsources:\n  - id: def\n"+
		"    resource: /"+comp+".md   # the sanctioned computation\n"+
		"  - resource: all queries in BigQuery project acme\n---\n\nNo link in the body.\n")
	// Spells "revenue" and carries sources, so the query reads it as a
	// candidate; nothing in it points at the move.
	put(unrelated, "---\ntype: Insight\nsources:\n  - resource: https://example.com/revenue\n---\n\nrevenue\n")

	// Scoped to what the move truly rewrites: the unrelated candidate
	// sits outside it and must not refuse the move.
	within := []string{run + "/computations", run + "/metrics/revenue", run + "/finance"}
	if _, err := s.Move(ctx, comp, dst, actor, within); err != nil {
		t.Fatal(err)
	}

	m, err := s.Get(ctx, metric)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Sources[0].Resource; got != "/"+dst+".md" {
		t.Errorf("sources[0].resource = %q, want /%s.md", got, dst)
	}
	if !strings.Contains(m.Doc, "resource: /"+dst+".md   # the sanctioned computation") {
		t.Errorf("the citation was not rewritten in place:\n%s", m.Doc)
	}
	if !strings.Contains(m.Doc, "resource: all queries in BigQuery project acme") {
		t.Errorf("a scope descriptor was rewritten:\n%s", m.Doc)
	}
	revs, err := s.ListRevisions(ctx, metric, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 || revs[0].Change != "update" {
		t.Errorf("the repair left no update revision: %+v", revs)
	}

	c, err := s.Get(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if c.Executor == nil || c.Executor.Resource != "/"+dst+"/run.py" {
		t.Errorf("executor = %+v, want the namespace file at its new path", c.Executor)
	}
	if c.Attester == nil || c.Attester.Resource != "/"+run+"/computations/attesters/eq.py" {
		t.Errorf("attester = %+v, want the relative path made absolute against the old directory", c.Attester)
	}
	if !strings.Contains(c.Doc, "executor: {resource: /"+dst+"/run.py}") {
		t.Errorf("the flow mapping was not kept:\n%s", c.Doc)
	}

	u, err := s.Get(ctx, unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if u.Sources[0].Resource != "https://example.com/revenue" {
		t.Errorf("an unrelated candidate was changed: %+v", u.Sources)
	}

	// A directory move carries the same repair.
	if _, err := s.MovePrefix(ctx, run+"/finance", run+"/money", actor, nil); err != nil {
		t.Fatal(err)
	}
	if m, err = s.Get(ctx, metric); err != nil {
		t.Fatal(err)
	}
	if got := m.Sources[0].Resource; got != "/"+run+"/money/revenue.md" {
		t.Errorf("after the directory move, sources[0].resource = %q", got)
	}
	if c, err = s.Get(ctx, run+"/money/revenue"); err != nil {
		t.Fatal(err)
	}
	if c.Executor == nil || c.Executor.Resource != "/"+run+"/money/revenue/run.py" {
		t.Errorf("after the directory move, executor = %+v", c.Executor)
	}
}
