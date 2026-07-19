package scanner

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ObservePendingAnalysisFiles records the exact source tuple seen by local
// analysis before remote search starts. File-backed matchers (notably the
// inline Chromaprint/AcoustID fallback) can therefore address a new file on
// its very first pass. New, restored, or byte-changed files remain pending so
// a pipeline interruption is rediscovered; unchanged terminal rows keep their
// existing status.
func ObservePendingAnalysisFiles(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, result Result) (int, error) {
	if db == nil {
		return 0, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin scanner source observation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	observed := 0
	for _, root := range result.Inventory.Roots {
		for _, file := range root.Files {
			if file.Class != ClassPrimaryMedia && file.Class != ClassExtraMedia {
				continue
			}
			if _, err := q.ObservePendingLibraryFile(ctx, sqlc.ObservePendingLibraryFileParams{
				LibraryID: lib.ID,
				Path:      file.Path,
				Size:      file.Size,
				Mtime:     pgtype.Timestamptz{Time: file.MTime, Valid: !file.MTime.IsZero()},
			}); err != nil {
				return observed, fmt.Errorf("observe scanner source %q: %w", file.Path, err)
			}
			observed++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return observed, fmt.Errorf("commit scanner source observation: %w", err)
	}
	return observed, nil
}
