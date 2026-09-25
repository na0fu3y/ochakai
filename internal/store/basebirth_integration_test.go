package store

import (
	"context"
	"testing"

	"github.com/na0fu3y/ochakai/internal/testdb"
)

// A base knows whether it was made before gemini-embedding-2 in global
// became the default (design doc 0147 §1.2). The answer decides where an
// operator's text goes, so both halves are held: a base made from nothing
// reads as new, and a base that was already there when the record
// arrived — an upgrade — reads as older, however many times it starts
// after.
func TestIntegrationABaseKnowsWhenItWasMade(t *testing.T) {
	ctx := context.Background()

	t.Run("made from nothing", func(t *testing.T) {
		s := birthStore(ctx, t, "base_new_")
		for range 2 { // a second start must not change the answer
			if err := s.Migrate(ctx, 0); err != nil {
				t.Fatal(err)
			}
			older, err := s.PredatesGlobalEmbedding(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if older {
				t.Fatal("a base this binary made from nothing reads as older: it would embed in its region with the old model")
			}
		}
	})

	t.Run("already there", func(t *testing.T) {
		s := birthStore(ctx, t, "base_old_")
		if err := s.Migrate(ctx, 0); err != nil {
			t.Fatal(err)
		}
		// Back to the shape of a base the release before this one left.
		for _, q := range []string{
			`DROP TABLE base_birth`,
			`DELETE FROM schema_migrations WHERE version >= '` + birthRecorded + `'`,
		} {
			if _, err := s.pool.Exec(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			if err := s.Migrate(ctx, 0); err != nil {
				t.Fatal(err)
			}
			older, err := s.PredatesGlobalEmbedding(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !older {
				t.Fatal("an upgraded base reads as new: its vectors would be stranded and its text sent to global")
			}
		}
	})
}

// birthStore is an empty scoped store, with knowledge_embedding in its
// pre-0010 shape for the reason TestMigrateConcurrent gives: 0010/0011
// probe for it unqualified and would otherwise find public's.
func birthStore(ctx context.Context, t *testing.T, prefix string) *Store {
	t.Helper()
	s := scopedStore(ctx, t, testdb.Unique(t, prefix))
	if _, err := s.pool.Exec(ctx, `CREATE TABLE knowledge_embedding (
		type text NOT NULL, id text NOT NULL, model text, PRIMARY KEY (type, id))`); err != nil {
		t.Fatal(err)
	}
	return s
}
