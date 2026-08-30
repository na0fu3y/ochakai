package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/na0fu3y/ochakai/internal/domain"
	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A directory moves whole or not at all (design doc 0132), and "whole"
// means the three classes of object a client cannot see and therefore
// cannot move for itself: the concepts a human rejected, the concepts
// that were deleted and still hold their ids, and the files no concept's
// namespace covers. If any of them stayed behind, the old directory would
// never empty and an import into it later would collide with a row nobody
// can list — which is the whole reason this operation exists rather than
// being a loop somebody writes in shell.
func TestMovePrefixMovesTheWholeDirectoryIntegration(t *testing.T) {
	lockLiveAttachments(t, testdb.URL(t)) // loose.markdown rides a fake blob store other packages' export scans can't read
	ctx, s := movePrefixStore(t)
	run := testdb.Unique(t, "stit-moveprefix-")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	old, dst := run+"/old", run+"/new"

	// a links at b, so the rewrite has to close inside the directory.
	mkPrefixConcept(t, ctx, s, actor, old+"/a", fmt.Sprintf("見出し [b](/%s/b.md) を参照。", old))
	mkPrefixConcept(t, ctx, s, actor, old+"/b", "本文。")
	mkPrefixConcept(t, ctx, s, actor, old+"/rejected", "却下されたもの。")
	mkPrefixConcept(t, ctx, s, actor, old+"/gone", "消されたもの。")
	// A concept whose id is exactly the directory's name. 0075 §2 allows
	// it and 0132 §3 decides it stays: the caller asked for a directory.
	mkPrefixConcept(t, ctx, s, actor, old, "ディレクトリと同名の concept。")
	// Outside, pointing in: this one did not move and its body changes,
	// so it is the one that takes an "update" revision.
	mkPrefixConcept(t, ctx, s, actor, run+"/outside", fmt.Sprintf("外から [a](/%s/a.md) を指す。", old))
	// A rejection is a deletion carrying its reason (design doc 0135), so
	// this one is a tombstone whose revision holds the ruling — and the
	// move has to carry that history, not just the row.
	if err := s.SoftDelete(ctx, old+"/rejected", actor, nil, "no"); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, old+"/gone", actor, nil, ""); err != nil {
		t.Fatal(err)
	}
	// A file sitting loose in the directory — under no concept's
	// namespace, so the one-concept move would never have reached it.
	// Markdown, because a test database has no bucket and markdown is
	// the half of the bundle that lives in the database (0075 §1); the
	// extension is not ".md", which only a concept may sit at (0100).
	if _, _, err := s.PutFile(ctx, old+"/loose.markdown", "text/markdown", []byte("bytes"), actor); err != nil {
		t.Fatal(err)
	}

	moved, err := s.MovePrefix(ctx, old, dst, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Four concepts (two live, two tombstones — one of them a rejection)
	// and one file. The same-named concept is not one of them.
	if moved != 5 {
		t.Errorf("moved %d objects, want 5", moved)
	}
	for _, id := range []string{dst + "/a", dst + "/b"} {
		if _, err := s.Get(ctx, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for _, id := range []string{dst + "/gone", dst + "/rejected"} {
		if _, err := s.GetTombstone(ctx, id); err != nil {
			t.Errorf("the deleted concept did not move: %s: %v", id, err)
		}
	}
	// The ruling moved with it: the reason lives on the revision that
	// removed the concept, so a move that carried the rows but not the
	// history would lose why it was turned down.
	turned, err := s.ListRevisions(ctx, dst+"/rejected", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turned) == 0 || turned[0].Change != "reject" || turned[0].Note != "no" {
		t.Errorf("the rejection did not move with its reason: %+v", turned)
	}
	for _, id := range []string{old + "/a", old + "/b", old + "/rejected"} {
		if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s is still at its old address (%v)", id, err)
		}
	}
	if _, err := s.GetFileMeta(ctx, dst+"/loose.markdown"); err != nil {
		t.Errorf("the loose file did not move: %v", err)
	}
	if _, err := s.GetFileMeta(ctx, old+"/loose.markdown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the loose file is still at its old path (%v)", err)
	}
	// The concept that shares the directory's name stayed: it is a
	// different address, and nothing said to move it.
	if _, err := s.Get(ctx, old); err != nil {
		t.Errorf("the concept named like the directory moved with it: %v", err)
	}

	// The link that closed inside the directory is repaired, and it cost
	// no "update" revision — a's history is create then move (design doc
	// 0132 §5).
	a, err := s.Get(ctx, dst+"/a")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/" + dst + "/b.md"; !strings.Contains(a.Body, want) {
		t.Errorf("a's body = %q, want it to point at %s", a.Body, want)
	}
	revs, err := s.ListRevisions(ctx, dst+"/a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Change != "move" || revs[1].Change != "create" {
		t.Errorf("a's revisions = %v, want create then move and no update", changes(revs))
	}
	// The referrer outside did not move and its body changed, so it takes
	// one.
	out, err := s.Get(ctx, run+"/outside")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/" + dst + "/a.md"; !strings.Contains(out.Body, want) {
		t.Errorf("the outside referrer's body = %q, want it to point at %s", out.Body, want)
	}
	revs, err = s.ListRevisions(ctx, run+"/outside", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Change != "update" {
		t.Errorf("the outside referrer's revisions = %v, want an update on top of create", changes(revs))
	}
}

// A refused move has changed nothing. Each refusal is checked by reading
// the directory back afterwards, because the failure this guards against
// is not the error — it is the half-moved directory behind it (design doc
// 0132 §4).
func TestMovePrefixRefusesWholeIntegration(t *testing.T) {
	ctx, s := movePrefixStore(t)
	run := testdb.Unique(t, "stit-moveprefix-refuse-")
	actor := domain.Actor{Kind: domain.ActorHuman, Name: "test"}
	old, dst := run+"/old", run+"/new"
	mkPrefixConcept(t, ctx, s, actor, old+"/a", "本文。")
	mkPrefixConcept(t, ctx, s, actor, old+"/b", "本文。")

	t.Run("destination holds objects", func(t *testing.T) {
		mkPrefixConcept(t, ctx, s, actor, dst+"/squatter", "先客。")
		if _, err := s.MovePrefix(ctx, old, dst, actor, nil); !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("move onto an occupied directory = %v, want ErrAlreadyExists", err)
		}
		if _, err := s.Get(ctx, old+"/a"); err != nil {
			t.Errorf("a refused move moved something: %v", err)
		}
	})

	t.Run("nothing under the prefix", func(t *testing.T) {
		if _, err := s.MovePrefix(ctx, run+"/absent", run+"/elsewhere", actor, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("moving an empty directory = %v, want ErrNotFound", err)
		}
	})

	t.Run("a referrer outside what the caller may write", func(t *testing.T) {
		// The referrer sits outside the caller's scope, so the rewrite
		// this move would have to make reaches somewhere it may not
		// write: refused whole, never narrowed (design doc 0129 §2).
		mkPrefixConcept(t, ctx, s, actor, run+"/other/watcher", fmt.Sprintf("外から [a](/%s/a.md) を指す。", old))
		within := []string{old, run + "/moved"}
		if _, err := s.MovePrefix(ctx, old, run+"/moved", actor, within); !errors.Is(err, ErrOutsideScope) {
			t.Fatalf("move with an out-of-scope referrer = %v, want ErrOutsideScope", err)
		}
		if _, err := s.Get(ctx, old+"/a"); err != nil {
			t.Errorf("a refused move moved something: %v", err)
		}
		if _, err := s.Get(ctx, old+"/b"); err != nil {
			t.Errorf("a refused move moved something: %v", err)
		}
		// The administrator's move is unbounded and is how it gets done.
		if _, err := s.MovePrefix(ctx, old, run+"/moved", actor, nil); err != nil {
			t.Errorf("the unbounded move: %v", err)
		}
	})
}

func movePrefixStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx := context.Background()
	s, err := New(ctx, testdb.URL(t), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// A file needs somewhere for its bytes to go, and a test database has
	// no bucket; the move is about the addresses, not the bytes.
	s.UseBlobStore(memPrefixBlobs{})
	return ctx, s
}

// memPrefixBlobs is the smallest blob.Store that lets a file exist.
type memPrefixBlobs map[string][]byte

func (m memPrefixBlobs) Put(_ context.Context, sum, _ string, data []byte) error {
	m[sum] = data
	return nil
}

func (m memPrefixBlobs) Get(_ context.Context, sum string) ([]byte, error) {
	data, ok := m[sum]
	if !ok {
		return nil, fmt.Errorf("mem blob store: %s not found", sum)
	}
	return data, nil
}

func (m memPrefixBlobs) Delete(_ context.Context, sum string) error {
	delete(m, sum)
	return nil
}

func mkPrefixConcept(t *testing.T, ctx context.Context, s *Store, actor domain.Actor, id, body string) {
	t.Helper()
	k := &domain.Knowledge{
		Type: domain.TypeInsights, ID: id, Title: "草案", Status: domain.StatusDraft,
		Body: body, CreatedBy: actor,
	}
	k.Links = domain.LinksFromBody(id, body)
	if err := s.Create(ctx, k, false); err != nil {
		t.Fatal(err)
	}
}

func changes(revs []domain.Revision) []string {
	out := make([]string, len(revs))
	for i, r := range revs {
		out[i] = r.Change
	}
	return out
}
