package scanner

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ParkUnmatchedFiles writes change-detection seen-markers for every tracked
// inventory file that no accepted search identity claims — unmatched,
// needs_review, rejected, ignored, and files the analyzers couldn't plan at
// all. Without a library_files row, the next kickoff re-detects such a file
// as new and re-runs a live provider search for its scope on every scan,
// forever. Parked files stay quiet until their bytes change or a review
// decision enqueues a forced scoped rescan (which bypasses change detection).
//
// Files claimed by accepted identities are deliberately NOT parked: their
// library_files rows are written by the apply phase on success, so a failed
// fetch/apply keeps re-triggering the scope — that self-heal is load-bearing.
//
// Extras (ClassExtraMedia) are never listed in identity file sets; extras
// that belong to an accepted identity get parked here first and overwritten
// moments later when apply attaches them by directory proximity.
func ParkUnmatchedFiles(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, result Result) (int, error) {
	if db == nil {
		return 0, nil
	}
	claimed := acceptedIdentityRelPaths(lib, result)
	q := sqlc.New(db)
	parked := 0
	for _, root := range result.Inventory.Roots {
		for _, file := range root.Files {
			if file.Class != ClassPrimaryMedia && file.Class != ClassExtraMedia {
				continue
			}
			if claimed[file.RelPath] {
				continue
			}
			if err := q.ParkUnmatchedLibraryFile(ctx, sqlc.ParkUnmatchedLibraryFileParams{
				LibraryID: lib.ID,
				Path:      file.Path,
				Size:      file.Size,
				Mtime:     pgtype.Timestamptz{Time: file.MTime, Valid: !file.MTime.IsZero()},
			}); err != nil {
				return parked, err
			}
			parked++
		}
	}
	return parked, nil
}

// acceptedIdentityRelPaths collects the relpaths of every file claimed by a
// search identity that was accepted with a provider match — the same
// criterion the process_scan worker uses to enqueue fetch_metadata.
func acceptedIdentityRelPaths(lib sqlc.Library, result Result) map[string]bool {
	claimed := map[string]bool{}
	claim := func(files []string) {
		for _, f := range files {
			claimed[f] = true
		}
	}
	switch lib.MediaType {
	case sqlc.MediaTypeMovie:
		matches := make(map[string][]string, len(result.MovieMatches))
		for _, m := range result.MovieMatches {
			matches[m.Key] = m.Files
		}
		for _, s := range result.MovieSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(matches[s.Key])
			}
		}
	case sqlc.MediaTypeMusic:
		artists := make(map[string][]string, len(result.MusicArtists))
		for _, a := range result.MusicArtists {
			artists[a.Key] = a.Files
		}
		for _, s := range result.MusicSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(artists[s.Key])
			}
		}
	case sqlc.MediaTypeBook:
		plans := make(map[string][]string, len(result.BookPlans))
		for _, p := range result.BookPlans {
			plans[p.Key] = p.Files
		}
		for _, s := range result.BookSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(plans[s.Key])
			}
		}
	default: // TV-like: tv + anime
		matches := make(map[string][]string, len(result.TVMatches))
		for _, m := range result.TVMatches {
			matches[m.Key] = m.Files
		}
		for _, s := range result.TVSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(matches[s.Key])
			}
		}
	}
	return claimed
}
