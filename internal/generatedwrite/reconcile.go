package generatedwrite

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ReconcileCounts struct {
	Examined  int
	Recovered int
	Retired   int
	Skipped   int
	Failed    int
}

// Reconcile converges a bounded oldest-first slice. It is intentionally a
// function called by the existing scanner-artifact maintenance job; it never
// creates one River job per sidecar.
func Reconcile(ctx context.Context, pool *pgxpool.Pool, limit int) (ReconcileCounts, error) {
	if pool == nil {
		return ReconcileCounts{}, errors.New("generatedwrite: reconcile database unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 250
	}
	rows, err := pool.Query(ctx, `
		SELECT path
		FROM generated_sidecar_publications
		WHERE (pending_intent_id IS NOT NULL AND pending_lease_expires_at <= now())
		   OR verified_at < now() - interval '24 hours'
		ORDER BY (pending_intent_id IS NULL),
		         COALESCE(pending_lease_expires_at, verified_at), path
		LIMIT $1
	`, limit)
	if err != nil {
		return ReconcileCounts{}, err
	}
	paths := make([]string, 0, limit)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return ReconcileCounts{}, err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReconcileCounts{}, err
	}
	rows.Close()

	counts := ReconcileCounts{}
	var failures []error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return counts, errors.Join(append(failures, err)...)
		}
		acquired, err := TryWithPathLock(ctx, pool, path, func(conn *pgxpool.Conn) error {
			counts.Examined++
			publication, exists, err := LoadPublication(ctx, conn, path)
			if err != nil || !exists {
				return err
			}
			beforePending := publication.Pending != nil
			resolved, userOwned, err := RecoverLocked(ctx, conn, publication)
			if err != nil {
				return err
			}
			if userOwned {
				counts.Retired++
				return nil
			}
			current, currentExists, err := observePath(path)
			if err != nil {
				return err
			}
			if !currentExists {
				if err := ClearPublication(ctx, conn, path); err != nil {
					return err
				}
				counts.Retired++
				return nil
			}
			matches := resolved.Published != nil && resolved.Published.Matches(current.Size, current.SHA256)
			if !matches && resolved.Pending != nil {
				matches = resolved.Pending.Signature.Matches(current.Size, current.SHA256)
			}
			if !matches {
				if err := ClearPublication(ctx, conn, path); err != nil {
					return err
				}
				counts.Retired++
				return nil
			}
			if beforePending && resolved.Pending == nil {
				counts.Recovered++
			}
			if _, err := conn.Exec(ctx, `UPDATE generated_sidecar_publications SET verified_at = now() WHERE path = $1`, path); err != nil {
				return err
			}
			return reconcileMemberships(ctx, conn, path)
		})
		if err != nil {
			counts.Failed++
			failures = append(failures, err)
			if ctx.Err() != nil {
				return counts, errors.Join(append(failures, ctx.Err())...)
			}
			continue
		}
		if !acquired {
			counts.Skipped++
		}
	}
	return counts, errors.Join(failures...)
}

func reconcileMemberships(ctx context.Context, conn *pgxpool.Conn, path string) error {
	ids, err := containingLibraries(ctx, conn, path)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		_, err := conn.Exec(ctx, `DELETE FROM library_generated_sidecars WHERE path = $1`, path)
		return err
	}
	if _, err := conn.Exec(ctx, `DELETE FROM library_generated_sidecars WHERE path = $1 AND NOT (library_id = ANY($2::bigint[]))`, path, ids); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := conn.Exec(ctx, `INSERT INTO library_generated_sidecars (library_id,path) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, path); err != nil {
			return err
		}
	}
	return nil
}
