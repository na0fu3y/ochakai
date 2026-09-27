package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// DirectoryDescriptions returns the description of each directory in
// prefixes that has one (decision 0154).
func (s *Store) DirectoryDescriptions(ctx context.Context, prefixes []string) (map[string]string, error) {
	out := map[string]string{}
	if len(prefixes) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT prefix, description FROM directory_description WHERE prefix = ANY($1)`, prefixes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p, d string
		if err := rows.Scan(&p, &d); err != nil {
			return nil, err
		}
		out[p] = d
	}
	return out, rows.Err()
}

// SetDirectoryDescriptions writes each directory's description; an empty
// one removes it.
func (s *Store) SetDirectoryDescriptions(ctx context.Context, set map[string]string, actor domain.Actor) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		for p, d := range set {
			var err error
			if d == "" {
				_, err = tx.Exec(ctx, `DELETE FROM directory_description WHERE prefix = $1`, p)
			} else {
				_, err = tx.Exec(ctx,
					`INSERT INTO directory_description (prefix, description, updated_by_kind, updated_by_name)
					 VALUES ($1, $2, $3, $4)
					 ON CONFLICT (prefix) DO UPDATE SET description = EXCLUDED.description,
					   updated_by_kind = EXCLUDED.updated_by_kind, updated_by_name = EXCLUDED.updated_by_name,
					   updated_at = now()`,
					p, d, actor.Kind, actor.Name)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// moveDirectoryDescriptions re-keys the descriptions of oldPrefix and
// everything under it, inside a directory move's transaction: a
// directory's description goes where the directory goes.
func moveDirectoryDescriptions(ctx context.Context, tx pgx.Tx, oldPrefix, newPrefix string) error {
	_, err := tx.Exec(ctx,
		`UPDATE directory_description SET prefix = $2 || substr(prefix, length($1) + 1)
		 WHERE prefix = $1 OR starts_with(prefix, $1 || '/')`, oldPrefix, newPrefix)
	return err
}
