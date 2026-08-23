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
	if _, _, err := f.svc.SetPolicy(f.adminCtx, rules, f.admin, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, _, err := f.svc.SetPolicy(f.adminCtx, nil, f.admin, nil); err != nil {
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
// bundle as a whole are refused rather than narrowed (0109 §3). Stats
// left this list — it is narrowed and says so (design doc 0123), which
// is the split 0109 §3 said could be made later.
func TestPolicyBelongsToAdministratorsIntegration(t *testing.T) {
	f := newAccessFixture(t)
	if _, _, err := f.svc.Policy(f.readCt); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller reading the policy returned %v, want ErrForbidden", err)
	}
	if _, _, err := f.svc.SetPolicy(f.readCt, nil, f.reader, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller writing the policy returned %v, want ErrForbidden", err)
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
	_, _, err = svc.SetPolicy(stranger, []domain.AccessRule{{Prefix: id, Principal: "human:x"}},
		domain.Actor{Kind: domain.ActorHuman, Name: "stranger@example.com"}, nil)
	if err == nil {
		t.Error("a deployment naming no administrator accepted a first rule; nobody could have undone it")
	}
}

// TestFirstRuleBelongsToAnAdministratorIntegration is the other half of
// that floor (design doc 0122, amending 0109 §3): a deployment that does
// name administrators, a policy that is still empty, and a caller who is
// not one of them.
//
// Every caller passes the administrator check while there are no rules —
// 0109 §2 promises exactly that — so this write used to land, and only
// the read on the way back out noticed that the policy it had just
// created did not belong to whoever wrote it. The rows were committed,
// the log said "access policy replaced", and the caller was handed a 403
// with no version and no body: an answer that says nothing was written.
// A caller retrying it would replace the policy again, each time.
func TestFirstRuleBelongsToAnAdministratorIntegration(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(ctx, testdb.Private(t, "aclfirst"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Store:  s,
		Log:    slog.New(slog.DiscardHandler),
		Config: &config.Config{Admins: []string{"human:ops@example.co.jp"}},
	}
	admin := domain.Actor{Kind: domain.ActorHuman, Name: "ops@example.co.jp"}
	adminCtx := httpauth.WithActor(ctx, admin)
	outsider := domain.Actor{Kind: domain.ActorHuman, Name: "tanaka@example.co.jp"}
	outsiderCtx := httpauth.WithActor(ctx, outsider)

	// The outsider may do everything else here: there are no rules, so
	// there is no boundary (0109 §2). Only the policy is not theirs.
	if _, err := svc.Stats(outsiderCtx, 0, nil); err != nil {
		t.Fatalf("stats on a deployment with no policy: %v", err)
	}
	rules := []domain.AccessRule{{Prefix: "aclfirst/growth", Principal: domain.PrincipalOf(outsider), MayWrite: true}}
	if _, _, err := svc.SetPolicy(outsiderCtx, rules, outsider, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("a caller outside OCHAKAI_ADMINS writing the first rule returned %v, want ErrForbidden", err)
	}
	// The refusal has to be a refusal: read as the administrator, who is
	// allowed to see the policy either way, and find it still empty.
	if got, _, err := svc.Policy(adminCtx); err != nil {
		t.Fatalf("reading the policy as the administrator: %v", err)
	} else if len(got) != 0 {
		t.Errorf("the refused write left %d rule(s) behind; a 403 says nothing was written", len(got))
	}
	// The administrator writes the same document, and gets it back —
	// with the version, which is what a conditional replacement needs
	// (0120) and what the refusal above could not have returned.
	stored, version, err := svc.SetPolicy(adminCtx, rules, admin, nil)
	if err != nil {
		t.Fatalf("the administrator writing the first rule: %v", err)
	}
	if len(stored) != 1 || version == "" {
		t.Errorf("the write answered with %d rule(s) and version %q", len(stored), version)
	}
	t.Cleanup(func() {
		if _, _, err := svc.SetPolicy(adminCtx, nil, admin, nil); err != nil {
			t.Logf("clearing the policy: %v", err)
		}
	})
	// And now that a policy exists, the outsider is refused by the check
	// at the top of the operation instead — the same answer, from the
	// side 0109 always covered.
	if _, _, err := svc.SetPolicy(outsiderCtx, nil, outsider, nil); !errors.Is(err, ErrForbidden) {
		t.Errorf("a scoped caller clearing the policy returned %v, want ErrForbidden", err)
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
		case reflect.TypeFor[context.Context]():
			return reflect.ValueOf(f.readCt)
		case reflect.TypeFor[string]():
			return reflect.ValueOf(f.theirs)
		case reflect.TypeFor[[]byte]():
			return reflect.ValueOf([]byte("bytes"))
		case reflect.TypeFor[domain.Actor]():
			return reflect.ValueOf(f.reader)
		case reflect.TypeFor[*domain.Knowledge]():
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
		if _, ok := errors.AsType[*UnsupportedError](err); ok {
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

// The precondition the policy gained (design doc 0120): it is one
// document replaced whole, so two callers that each read it and add a
// rule would otherwise leave only the second one's rule standing.
func TestPolicyPreconditionRefusesTheLostUpdate(t *testing.T) {
	f := newAccessFixture(t)
	rules, version, err := f.svc.Policy(f.adminCtx)
	if err != nil {
		t.Fatal(err)
	}
	if version == "" {
		t.Fatal("a policy read carried no version; there is nothing for If-Match to name")
	}

	// Two administrators read the same policy. The first adds a rule.
	first := append(append([]domain.AccessRule{}, rules...),
		domain.AccessRule{Prefix: f.prefix + "/sales", Principal: "human:sato@example.co.jp", MayWrite: true})
	_, moved, err := f.svc.SetPolicy(f.adminCtx, first, f.admin, &version)
	if err != nil {
		t.Fatalf("the first conditional write should have landed: %v", err)
	}
	if moved == version {
		t.Error("the version did not move after a write that added a rule")
	}

	// The second still holds the version from before, and its write
	// would drop the rule the first one added.
	second := append(append([]domain.AccessRule{}, rules...),
		domain.AccessRule{Prefix: f.prefix + "/support", Principal: "human:suzuki@example.co.jp"})
	if _, _, err := f.svc.SetPolicy(f.adminCtx, second, f.admin, &version); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a stale precondition should be a conflict, got %v", err)
	}
	after, _, err := f.svc.Policy(f.adminCtx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(first) {
		t.Fatalf("the refused write changed the policy: %d rules, want %d", len(after), len(first))
	}

	// Reread, redo, retry: the ordinary way out.
	if _, _, err := f.svc.SetPolicy(f.adminCtx, second, f.admin, &moved); err != nil {
		t.Fatalf("retrying with the version just read: %v", err)
	}
}

// The version names the grants and nothing else, so a rewrite that
// changes no rule leaves a precondition somebody is holding valid —
// what etagOf already promises for a concept, where verifying it does
// not move the hash. granted_at moves on every write and must not count.
func TestPolicyVersionTracksGrantsAndNotTheirTimestamps(t *testing.T) {
	f := newAccessFixture(t)
	rules, version, err := f.svc.Policy(f.adminCtx)
	if err != nil {
		t.Fatal(err)
	}
	if _, again, err := f.svc.SetPolicy(f.adminCtx, rules, f.admin, &version); err != nil {
		t.Fatalf("rewriting the same grants: %v", err)
	} else if again != version {
		t.Errorf("a write that changed no grant moved the version: %s -> %s", version, again)
	}
	// And the caller who held it can still use it.
	if _, _, err := f.svc.SetPolicy(f.adminCtx, rules, f.admin, &version); err != nil {
		t.Errorf("the held precondition stopped being valid: %v", err)
	}
}

// The empty policy has a version like any other, which is what makes the
// first grant on a deployment with no rules a write two automated
// callers can be made to serialize on.
func TestEmptyPolicyHasAVersionThatGuardsTheFirstGrant(t *testing.T) {
	f := newAccessFixture(t)
	if _, _, err := f.svc.SetPolicy(f.adminCtx, nil, f.admin, nil); err != nil {
		t.Fatal(err)
	}
	_, empty, err := f.svc.Policy(f.adminCtx)
	if err != nil {
		t.Fatal(err)
	}
	if empty == "" {
		t.Fatal("an empty policy carried no version; the first grant has nothing to condition on")
	}
	one := []domain.AccessRule{{Prefix: f.prefix + "/growth", Principal: domain.PrincipalOf(f.reader)}}
	if _, _, err := f.svc.SetPolicy(f.adminCtx, one, f.admin, &empty); err != nil {
		t.Fatalf("the first conditional grant: %v", err)
	}
	other := []domain.AccessRule{{Prefix: f.prefix + "/glossary", Principal: domain.AnyPrincipal}}
	if _, _, err := f.svc.SetPolicy(f.adminCtx, other, f.admin, &empty); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a second signup racing on the empty policy should conflict, got %v", err)
	}
}

// Design doc 0123: the loop's numbers are narrowed to what the caller
// can see, and the answer says which subtrees it counted — that
// declaration is what keeps a partial answer from being a second
// meaning for the same words.
func TestScopedStatsCountTheCallersOwnSubtreeAndSayItIntegration(t *testing.T) {
	f := newAccessFixture(t)

	whole, err := f.svc.Stats(f.adminCtx, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if whole.Scope != nil {
		t.Errorf("an administrator asking about the whole bundle declared scope %v, want none", whole.Scope)
	}
	if whole.Misses.Withheld {
		t.Error("misses withheld from an administrator")
	}

	// The reader holds growth and the shared glossary, and nothing else.
	mine, err := f.svc.Stats(f.readCt, 0, nil)
	if err != nil {
		t.Fatalf("a scoped caller asking for stats: %v", err)
	}
	if len(mine.Scope) == 0 {
		t.Fatal("a scoped answer declared no scope; a partial count that does not say so is the thing 0109 §3 refused")
	}
	for _, p := range mine.Scope {
		if !strings.HasPrefix(p, f.prefix+"/") {
			t.Errorf("declared scope %q is outside the caller's grants", p)
		}
	}
	if mine.Concepts.Total >= whole.Concepts.Total {
		t.Errorf("scoped total %d is not smaller than the whole base's %d",
			mine.Concepts.Total, whole.Concepts.Total)
	}

	// The misses are the instance's and have no id to narrow by, so they
	// are withheld rather than shown — and said to be, not zeroed.
	if !mine.Misses.Withheld {
		t.Error("a scoped caller was given the whole instance's unanswered searches")
	}
	if mine.Misses.Count != 0 || len(mine.Misses.Queries) != 0 {
		t.Errorf("withheld misses still carried data: %+v", mine.Misses)
	}

	// An administrator who asks about one subtree gets the same
	// declaration: the field is about the numbers, not about the caller.
	part, err := f.svc.Stats(f.adminCtx, 0, []string{f.prefix + "/growth"})
	if err != nil {
		t.Fatal(err)
	}
	if len(part.Scope) != 1 || part.Scope[0] != f.prefix+"/growth" {
		t.Errorf("an administrator's scoped call declared %v", part.Scope)
	}
}

// A caller who can see none of what these numbers count gets zeros that
// say they are zeros for that reason — an empty declared scope — rather
// than a refusal, for the reason a read outside the scope is a 404.
func TestStatsOutsideEveryGrantCountNothingAndSaySoIntegration(t *testing.T) {
	f := newAccessFixture(t)
	st, err := f.svc.Stats(f.readCt, 0, []string{f.prefix + "/personnel"})
	if err != nil {
		t.Fatalf("asking about a subtree outside every grant: %v", err)
	}
	if st.Scope == nil || len(st.Scope) != 0 {
		t.Errorf("declared scope = %v, want an empty list", st.Scope)
	}
	if st.Concepts.Total != 0 {
		t.Errorf("counted %d concepts the caller cannot see", st.Concepts.Total)
	}
	// The vocabularies still travel with zeros, so a reader never has to
	// tell "none" from "not reported".
	if len(st.Concepts.Status) != len(domain.Statuses) || len(st.Concepts.Trust) != len(domain.Trusts) {
		t.Errorf("the empty answer dropped a vocabulary: %+v", st.Concepts)
	}
}
