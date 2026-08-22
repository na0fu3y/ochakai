package service

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/httpauth"
	"github.com/na0fu3y/ochakai/internal/okf"
	"github.com/na0fu3y/ochakai/internal/store"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// accessFixture is a base with two teams and a shared glossary, an
// administrator, and one reader who may see one team's subtree only.
// The shape design doc 0109 §1 was written for: knowledge some readers
// must not see, inside a bundle that stays one bundle because the teams
// share the glossary.
type accessFixture struct {
	svc              *Service
	prefix           string
	mine, theirs     string
	shared           string
	admin, reader    domain.Actor
	adminCtx, readCt context.Context
}

// The policy this fixture writes is the whole deployment's, so the
// fixture takes a database of its own rather than a prefix in the shared
// one (testdb.Private): a rule here would otherwise scope the callers in
// every package running beside this one.
func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	ctx := context.Background()
	s, err := store.New(ctx, testdb.Private(t, "acl"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	f := &accessFixture{
		admin:  domain.Actor{Kind: domain.ActorHuman, Name: "ops@example.co.jp"},
		reader: domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"},
	}
	f.svc = &Service{
		Store:  s,
		Log:    slog.New(slog.DiscardHandler),
		Config: &config.Config{Admins: []string{"human:ops@example.co.jp"}},
	}
	f.prefix = testdb.Unique(t, "acl-")
	f.mine = f.prefix + "/growth/revenue"
	f.theirs = f.prefix + "/personnel/salaries"
	f.shared = f.prefix + "/glossary/arr"
	f.adminCtx = httpauth.WithActor(ctx, f.admin)
	f.readCt = httpauth.WithActor(ctx, f.reader)

	for _, id := range []string{f.mine, f.theirs, f.shared} {
		doc := "---\ntype: Metric\ntitle: " + id + "\n---\n\nbody\n"
		d, _, err := okf.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		d.ID = id
		if _, err := f.svc.Create(f.adminCtx, &d.Knowledge, f.admin); err != nil {
			t.Fatalf("seeding %s: %v", id, err)
		}
	}
	// The policy lands last, so the seeding above runs on the deployment
	// every existing one is: no rules, no boundary.
	rules := []domain.AccessRule{
		{Prefix: f.prefix + "/growth", Principal: domain.PrincipalOf(f.reader), MayWrite: true},
		{Prefix: f.prefix + "/glossary", Principal: domain.AnyPrincipal},
	}
	if _, err := f.svc.SetPolicy(f.adminCtx, rules, f.admin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.svc.SetPolicy(f.adminCtx, nil, f.admin); err != nil {
			t.Logf("clearing the policy: %v", err)
		}
	})
	return f
}

// TestScopeHidesWhatItRefusesIntegration is design doc 0109 §4's claim:
// outside the scope the address holds nothing. A 403 would answer the
// question the boundary exists to refuse.
func TestScopeHidesWhatItRefusesIntegration(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.svc.Get(f.readCt, f.mine); err != nil {
		t.Fatalf("reading inside the grant: %v", err)
	}
	if _, err := f.svc.Get(f.readCt, f.shared); err != nil {
		t.Fatalf("reading the shared subtree granted to *: %v", err)
	}
	for _, read := range []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, err := f.svc.Get(f.readCt, f.theirs); return err }},
		{"Usage", func() error { _, err := f.svc.Usage(f.readCt, f.theirs); return err }},
		{"Revisions", func() error { _, err := f.svc.Revisions(f.readCt, f.theirs, 0); return err }},
		{"ObjectHistory", func() error { _, err := f.svc.ObjectHistory(f.readCt, f.theirs+".md", 0); return err }},
		{"ReportOutcome", func() error {
			_, err := f.svc.ReportOutcome(f.readCt, f.theirs, domain.EventWorked, "")
			return err
		}},
	} {
		if err := read.call(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s outside the scope returned %v, want ErrNotFound", read.name, err)
		}
	}
}

// TestScopeRefusesAWriteItAllowsAReadOfIntegration is the other half:
// inside the read scope and outside the write scope, the refusal says so
// — it tells the caller nothing they did not already know.
func TestScopeRefusesAWriteItAllowsAReadOfIntegration(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.svc.Verify(f.readCt, f.mine, f.reader); err != nil {
		t.Fatalf("verifying inside the write grant: %v", err)
	}
	// The glossary is granted read-only to everybody.
	for _, write := range []struct {
		name string
		call func() error
	}{
		{"Verify", func() error { _, err := f.svc.Verify(f.readCt, f.shared, f.reader); return err }},
		{"Reject", func() error { _, err := f.svc.Reject(f.readCt, f.shared, "no", f.reader); return err }},
		{"Delete", func() error { return f.svc.Delete(f.readCt, f.shared, f.reader, nil) }},
	} {
		if err := write.call(); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s on a readable, unwritable concept returned %v, want ErrForbidden", write.name, err)
		}
	}
}

// TestScopeHidesARulingOutsideItIntegration is the case the reflection
// walk below cannot reach: the curation guards MCP's put_concept runs
// ahead of the write only speak when a human has ruled on the id, so a
// fixture whose out-of-scope concept is an ordinary draft passes without
// ever asking the question. Both of their refusals name the id and say a
// human ruled on it — which is the read 0109 §4 answers with a 404.
func TestScopeHidesARulingOutsideItIntegration(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.svc.Reject(f.adminCtx, f.theirs, "not ours", f.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RefuseIfCurated(f.readCt, f.theirs, "replace"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RefuseIfCurated on a ruled-on concept outside the scope returned %v, want ErrNotFound", err)
	}
	// And again once the ruling is a tombstone, which is the other guard:
	// GetTombstone reads a row Get cannot see, so it needs the check of
	// its own.
	if err := f.svc.Delete(f.adminCtx, f.theirs, f.admin, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RefuseIfRevivingCurated(f.readCt, f.theirs); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RefuseIfRevivingCurated on a deleted ruling outside the scope returned %v, want ErrNotFound", err)
	}
	// The surface that calls both: put_concept lands here for an id it
	// found free, and the answer must be the same 404.
	_, err := f.svc.CreateKeepingCurated(f.readCt,
		&domain.Knowledge{Type: "Metric", ID: f.theirs, Title: "x"}, f.reader)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CreateKeepingCurated outside the scope returned %v, want ErrNotFound", err)
	}
	// Inside the read scope but outside the write scope, both guards say
	// the thing the caller already knows.
	if _, err := f.svc.RefuseIfCurated(f.readCt, f.shared, "replace"); !errors.Is(err, ErrForbidden) {
		t.Errorf("RefuseIfCurated on a readable, unwritable concept returned %v, want ErrForbidden", err)
	}
}

// TestScopeNarrowsEveryListingIntegration walks the answers assembled
// from more than one query — the ones the search filter does not reach —
// because those are where a boundary leaks by omission rather than by
// decision (0109 §6).
func TestScopeNarrowsEveryListingIntegration(t *testing.T) {
	f := newAccessFixture(t)
	carries := func(t *testing.T, what string, ids []string) {
		t.Helper()
		for _, id := range ids {
			if strings.Contains(id, "/personnel/") {
				t.Errorf("%s carries %s, which is outside the caller's scope", what, id)
			}
		}
	}

	hits, err := f.svc.SearchOrList(f.readCt, "", "usage", "", store.Filter{Prefixes: []string{f.prefix}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(hits.Hits))
	for _, h := range hits.Hits {
		ids = append(ids, h.ID)
	}
	carries(t, "a listing", ids)
	if len(ids) == 0 {
		t.Error("a listing inside the grant came back empty; the narrowing removed too much")
	}

	// Browsing the level above both teams: the granted directory is
	// reachable and the other one is not there at all.
	lvl, err := f.svc.Browse(f.readCt, f.prefix, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range lvl.Dirs {
		if d.Name == "personnel" {
			t.Error("browse names a directory the caller may not read")
		}
	}
	var reachable bool
	for _, d := range lvl.Dirs {
		if d.Name == "growth" {
			reachable = true
		}
	}
	if !reachable {
		t.Error("browse hides a directory the caller was granted, so the tree cannot be walked into it")
	}

	// The log under the shared root, and the backlinks a read carries.
	rows, err := f.svc.LogRows(f.readCt, f.prefix, 0)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(rows))
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	carries(t, "the log", paths)
}

// TestPolicyBelongsToAdministratorsIntegration: the rules name people
// and the directories they may see, and the operations that take the
// bundle as a whole are refused rather than narrowed (0109 §3).
func TestPolicyBelongsToAdministratorsIntegration(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.svc.Policy(f.readCt); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller reading the policy returned %v, want ErrForbidden", err)
	}
	if _, err := f.svc.SetPolicy(f.readCt, nil, f.reader); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller writing the policy returned %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Stats(f.readCt, 0, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller asking for stats returned %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Move(f.readCt, f.mine, f.mine+"-2", f.reader); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller moving a concept returned %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Stats(f.adminCtx, 0, nil); err != nil {
		t.Errorf("an administrator asking for stats: %v", err)
	}
}

// TestNoPolicyIsTheDeploymentThatCameBeforeIntegration is the promise
// every existing deployment rests on: with no rules, nothing here
// applies — not to a caller the configuration never heard of either.
// It reads a database of its own for the same reason the fixture does,
// in the other direction: what it asserts is the absence of a policy,
// and in the shared database a neighbour's rule would be present.
func TestNoPolicyIsTheDeploymentThatCameBeforeIntegration(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(ctx, testdb.Private(t, "aclopen"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: s, Log: slog.New(slog.DiscardHandler), Config: &config.Config{}}
	stranger := httpauth.WithActor(ctx, domain.Actor{Kind: domain.ActorHuman, Name: "stranger@example.com"})
	id := testdb.Unique(t, "acl-open-")
	d, _, err := okf.Parse([]byte("---\ntype: Metric\ntitle: t\n---\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	d.ID = id
	if _, err := svc.Create(stranger, &d.Knowledge, domain.Actor{Kind: domain.ActorHuman, Name: "stranger@example.com"}); err != nil {
		t.Fatalf("writing on a deployment with no policy: %v", err)
	}
	if _, err := svc.Get(stranger, id); err != nil {
		t.Fatalf("reading on a deployment with no policy: %v", err)
	}
	if _, err := svc.Stats(stranger, 0, nil); err != nil {
		t.Fatalf("stats on a deployment with no policy: %v", err)
	}
	// And a first rule cannot be written where nobody could undo it.
	_, err = svc.SetPolicy(stranger, []domain.AccessRule{{Prefix: id, Principal: "human:x"}},
		domain.Actor{Kind: domain.ActorHuman, Name: "stranger@example.com"})
	if err == nil {
		t.Error("a deployment naming no administrator accepted a first rule; nobody could have undone it")
	}
}

// TestEveryWriteIsScopedIntegration is design doc 0109 §6's exhaustiveness
// check, and the shape TestReadOnlyRefusesEveryWrite already uses: the
// list of write methods is derived by reflection rather than written out,
// so a write added later cannot land without either being guarded or
// failing here. It found Delete missing on the first run.
//
// Every write is called against an id outside the caller's scope, where
// the answer is the 404 that hides it (§4). A method that succeeds has
// written into somebody else's subtree.
func TestEveryWriteIsScopedIntegration(t *testing.T) {
	f := newAccessFixture(t)
	reads := map[string]bool{
		// Reads answer for themselves, above; what this test walks is
		// the other half of the surface.
		"Get": true, "Search": true, "SearchOrList": true, "Revisions": true,
		"Usage": true, "Browse": true, "Stats": true, "IndexDocument": true,
		"LogDocument": true, "LogRows": true, "ObjectHistory": true,
		"GetFile": true, "GetFileMeta": true, "FillFiles": true, "Close": true,
		"Policy": true, "RequireAdmin": true,
		// Whole-bundle operations are refused by RequireAdmin rather than
		// by an id, and TestPolicyBelongsToAdministratorsIntegration
		// checks them by name.
		"SetPolicy": true, "Reembed": true, "Move": true,
		// ReportOutcome takes a read grant by decision (0109 §4): it is
		// the machine's observation of a concept it used.
		"ReportOutcome": true,
	}
	arg := func(ty reflect.Type) reflect.Value {
		switch ty {
		case reflect.TypeOf((*context.Context)(nil)).Elem():
			return reflect.ValueOf(f.readCt)
		case reflect.TypeOf(""):
			return reflect.ValueOf(f.theirs)
		case reflect.TypeOf([]byte(nil)):
			return reflect.ValueOf([]byte("bytes"))
		case reflect.TypeOf(domain.Actor{}):
			return reflect.ValueOf(f.reader)
		case reflect.TypeOf(&domain.Knowledge{}):
			return reflect.ValueOf(&domain.Knowledge{Type: "Metric", ID: f.theirs, Title: "x"})
		}
		return reflect.Zero(ty)
	}
	rv := reflect.ValueOf(f.svc)
	checked := 0
	for i := 0; i < rv.NumMethod(); i++ {
		name := rv.Type().Method(i).Name
		if reads[name] {
			continue
		}
		m := rv.Method(i)
		in := make([]reflect.Value, m.Type().NumIn())
		for j := range in {
			in[j] = arg(m.Type().In(j))
		}
		out := m.Call(in)
		if len(out) == 0 {
			continue
		}
		last := out[len(out)-1]
		if last.IsNil() {
			t.Errorf("%s wrote outside the caller's scope and returned no error", name)
			continue
		}
		err := last.Interface().(error)
		// A deployment with no blob store refuses a file write before it
		// consults anything — the 501 restapi's TestAttachWithoutBlobStore
		// pins, which exists so that attaching costs no database access.
		// The scope check sits behind it, so on a test database with no
		// bucket these two never reach it.
		var unsupported *UnsupportedError
		if errors.As(err, &unsupported) {
			continue
		}
		if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, ErrForbidden) {
			t.Errorf("%s outside the scope returned %v, want ErrNotFound or ErrForbidden", name, err)
			continue
		}
		checked++
	}
	if checked < 8 {
		t.Errorf("only %d write methods were reached; the reflection is finding fewer than it should", checked)
	}
}
