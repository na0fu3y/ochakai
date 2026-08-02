package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// One address space, two kinds of object (design doc 0046 §§3.1, 3.5),
// and the two rules that fall out of it:
//
//   - a path prefix is a string, not a LIKE pattern. An id may contain
//     "_", which LIKE reads as "any single character" — browsing has
//     avoided LIKE for that reason since 0014 and the search filters
//     since 0041, and the statements that move, purge and attribute
//     files are the same rule one level down. Getting it wrong here does
//     not misreport a listing: it moves and deletes another entry's
//     files.
//   - a revision is an event about an object, keyed by its path, so a
//     file's history is in the same ledger as a concept's and comes out
//     in the same log.
func newObjectPathStore(t *testing.T) (*Store, context.Context, string) {
	t.Helper()
	dbURL := os.Getenv("OCHAKAI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("OCHAKAI_TEST_DATABASE_URL not set")
	}
	// Every test here holds a live file riding a fake blob store, which
	// another package's whole-bundle export cannot read.
	lockLiveAttachments(t, dbURL)
	ctx := context.Background()
	s, err := New(ctx, dbURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.UseBlobStore(newFakeBlobStore())
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// A run-unique root keeps reruns and the other tests sharing this
	// database out of the assertions, and lets the cleanup be a prefix.
	root := testdb.Unique(t, "it-op")
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM object WHERE starts_with(path, $1)`,
			`DELETE FROM knowledge_revision WHERE starts_with(path, $1)`,
			`DELETE FROM knowledge_purge WHERE starts_with(id, $1)`,
		} {
			if _, err := s.pool.Exec(context.Background(), q, root); err != nil {
				t.Errorf("cleaning up %s: %v", root, err)
			}
		}
	})
	return s, ctx, root
}

func mkConcept(t *testing.T, s *Store, ctx context.Context, id string) {
	t.Helper()
	k := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: id, Title: "t:" + id,
		Status: domain.StatusDraft, CreatedBy: domain.Actor{Kind: "human", Name: "test"},
	}
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
}

// A "_" in an id is a character, and the entry beside it keeps its
// files. Every statement that reads or writes a namespace is exercised:
// attribution, the move that carries a namespace along, and the purge
// that destroys one.
func TestIntegrationUnderscoreIsNotAWildcard(t *testing.T) {
	s, ctx, root := newObjectPathStore(t)
	actor := domain.Actor{Kind: "human", Name: "test"}
	// "sales_2024" and "salesX2024" differ only where LIKE would read
	// the "_" as any character.
	mine, theirs := root+"/sales_2024", root+"/salesX2024"
	mkConcept(t, s, ctx, mine)
	mkConcept(t, s, ctx, theirs)
	if _, _, err := s.PutFile(ctx, theirs+"/chart.png", "image/png", []byte("theirs"), actor); err != nil {
		t.Fatal(err)
	}

	t.Run("attribution", func(t *testing.T) {
		got, err := s.ListAttachments(ctx, mine)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("%s claims %d file(s) that live under %s: %+v", mine, len(got), theirs, got)
		}
		got, err = s.ListAttachments(ctx, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("%s has %d files, want its own one", theirs, len(got))
		}
	})

	t.Run("move", func(t *testing.T) {
		if _, err := s.Move(ctx, mine, root+"/moved", actor); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetFileMeta(ctx, theirs+"/chart.png"); err != nil {
			t.Errorf("moving %s took %s's file with it: %v", mine, theirs, err)
		}
	})

	t.Run("purge", func(t *testing.T) {
		if err := s.SoftDelete(ctx, root+"/moved", actor, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Purge(ctx, root+"/moved", actor); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetFileMeta(ctx, theirs+"/chart.png"); err != nil {
			t.Errorf("purging %s deleted %s's file: %v", mine, theirs, err)
		}
	})
}

// A file's history is in the ledger the concepts use (design doc 0046
// §3.1), so the generated log carries it — at the root, where a ledger
// row with no concept id used to end the read with a scan error, and
// under the directory the file lives in, where keying the query on the
// id left it out entirely.
func TestIntegrationLogCarriesFiles(t *testing.T) {
	s, ctx, root := newObjectPathStore(t)
	actor := domain.Actor{Kind: "human", Name: "test"}
	mkConcept(t, s, ctx, root+"/revenue")
	if _, _, err := s.PutFile(ctx, root+"/revenue/chart.png", "image/png", []byte("bytes"), actor); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ListRevisionsUnder(ctx, "", 1000); err != nil {
		t.Fatalf("the root log has to survive a file: %v", err)
	}

	rows, err := s.ListRevisionsUnder(ctx, root, 100)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	want := []string{root + "/revenue.md", root + "/revenue/chart.png"}
	for _, w := range want {
		if !containsPath(paths, w) {
			t.Errorf("the log under %s is missing %s: %v", root, w, paths)
		}
	}

	// The file's own path addresses its own history, the way a concept's
	// does — the prefix is matched as a string, so "revenue" does not
	// answer for "revenueX".
	rows, err = s.ListRevisionsUnder(ctx, root+"/revenue", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("the concept and its file are %d rows, want 2: %+v", len(rows), rows)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// A file's history follows the file when its namespace moves. Left
// behind, it would report changes at a path holding nothing and go
// missing from the log of the directory the file actually arrived in.
func TestIntegrationFileHistoryFollowsAMove(t *testing.T) {
	s, ctx, root := newObjectPathStore(t)
	actor := domain.Actor{Kind: "human", Name: "test"}
	mkConcept(t, s, ctx, root+"/before")
	if _, _, err := s.PutFile(ctx, root+"/before/chart.png", "image/png", []byte("bytes"), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(ctx, root+"/before", root+"/after", actor); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRevisionsUnder(ctx, root+"/after", 100)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	if !containsPath(paths, root+"/after/chart.png") {
		t.Errorf("the file's history stayed behind: %v", paths)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, root+"/before/") {
			t.Errorf("a revision still names the old path: %v", paths)
			break
		}
	}
}

// A file already holding a path is "something is there", not a 500 with
// a constraint name in it: the two kinds share one address space, and
// the caller who meant to write a concept has to be told the address is
// taken (design doc 0046 §3.5).
func TestIntegrationConceptCannotLandOnAFile(t *testing.T) {
	s, ctx, root := newObjectPathStore(t)
	actor := domain.Actor{Kind: "human", Name: "test"}
	if _, _, err := s.PutFile(ctx, root+"/notes.md", "text/markdown", []byte("carries no type"), actor); err != nil {
		t.Fatal(err)
	}
	k := &domain.Knowledge{
		Type: domain.TypeMetrics, ID: root + "/notes", Title: "notes",
		Status: domain.StatusDraft, CreatedBy: actor,
	}
	err := s.Create(ctx, k, false)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("creating a concept where a file lives = %v, want ErrAlreadyExists", err)
	}
	// And the file is still there, unchanged.
	if _, err := s.GetFileMeta(ctx, root+"/notes.md"); err != nil {
		t.Errorf("the file did not survive the refused create: %v", err)
	}
}

// A directory holds three kinds of thing and the listing carries all
// three (design doc 0046 §3.7). The one that was missing is the file: a
// file is an object in the bundle rather than a property of an entry
// (§3.3), so a directory can hold one nothing shows, and a listing that
// leaves it out describes a directory that is not there.
//
// What the listing still does not do is invent a subdirectory for a
// directory of pure data. OKF's §8 listing is navigation between concept
// documents, and a count mixing the kinds would misdescribe every
// directory that has both.
func TestIntegrationBrowseListsTheFilesInADirectory(t *testing.T) {
	s, ctx, root := newObjectPathStore(t)
	actor := domain.Actor{Kind: "human", Name: "test"}
	mkConcept(t, s, ctx, root+"/revenue")
	// One file beside the concept, one under it, one in a directory that
	// holds nothing else.
	for _, p := range []string{root + "/seed.csv", root + "/revenue/chart.png", root + "/seeds/orders.csv"} {
		if _, _, err := s.PutFile(ctx, p, "text/plain", []byte("bytes here"), actor); err != nil {
			t.Fatal(err)
		}
	}

	lvl, err := s.Browse(ctx, root+"/")
	if err != nil {
		t.Fatal(err)
	}
	if len(lvl.Files) != 1 || lvl.Files[0].Name != "seed.csv" || lvl.Files[0].Path != root+"/seed.csv" {
		t.Errorf("files at this level = %+v, want seed.csv alone", lvl.Files)
	}
	if lvl.Files[0].Size == 0 || lvl.Files[0].MediaType == "" {
		t.Errorf("a file line has nothing to describe it: %+v", lvl.Files[0])
	}
	if len(lvl.Concepts) != 1 || lvl.Concepts[0].ID != root+"/revenue" {
		t.Errorf("concepts = %+v", lvl.Concepts)
	}
	// "revenue" is a subdirectory because a file sits under it, but it
	// holds no concept, so it is not one of these — and neither is
	// "seeds". Directories exist here through the concepts in them.
	for _, d := range lvl.Dirs {
		t.Errorf("a directory of files was listed as a subdirectory: %+v", d)
	}

	// Asked about directly, a directory of pure data lists what it holds.
	lvl, err = s.Browse(ctx, root+"/seeds/")
	if err != nil {
		t.Fatal(err)
	}
	if len(lvl.Files) != 1 || lvl.Files[0].Name != "orders.csv" {
		t.Errorf("a directory of files lists nothing: %+v", lvl)
	}

	// A deleted file leaves the listing with it.
	if err := s.DeleteFile(ctx, root+"/seed.csv", actor); err != nil {
		t.Fatal(err)
	}
	lvl, err = s.Browse(ctx, root+"/")
	if err != nil {
		t.Fatal(err)
	}
	if len(lvl.Files) != 0 {
		t.Errorf("a deleted file is still listed: %+v", lvl.Files)
	}
}
