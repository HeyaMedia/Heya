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
	return parkInventoryFiles(ctx, db, lib, result, func(file InventoryFile) bool {
		return !claimed[file.RelPath]
	})
}

// ParkUnappliedFiles writes seen-markers for files whose identity reached a
// deliberate terminal apply outcome — skipped or blocked (e.g.
// metadata_mismatch). The identifier refused the apply on purpose; without a
// marker the unit re-detects as changed on every scan and loops
// search-skip → fetch → refuse forever. Parked files stay quiet until their
// bytes change or a review decision forces a scoped rescan.
func ParkUnappliedFiles(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, result Result) (int, error) {
	if db == nil {
		return 0, nil
	}
	refused := map[string]bool{}
	mark := func(key, action string) {
		if action == "skipped" || action == "blocked" {
			refused[key] = true
		}
	}
	for _, a := range result.MovieApply {
		mark(a.Key, a.Action)
	}
	for _, a := range result.TVApply {
		mark(a.Key, a.Action)
	}
	for _, a := range result.MusicApply {
		mark(a.Key, a.Action)
	}
	for _, a := range result.BookApply {
		mark(a.Key, a.Action)
	}
	if len(refused) == 0 {
		return 0, nil
	}

	filesByKey := identityFilesByKey(lib, result)
	rels := map[string]bool{}
	for key := range refused {
		for _, rel := range filesByKey[key] {
			rels[rel] = true
		}
	}
	return parkInventoryFiles(ctx, db, lib, result, func(file InventoryFile) bool {
		return rels[file.RelPath]
	})
}

// parkInventoryFiles upserts unmatched seen-markers for every tracked
// inventory file the filter selects.
func parkInventoryFiles(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, result Result, include func(InventoryFile) bool) (int, error) {
	q := sqlc.New(db)
	parked := 0
	for _, root := range result.Inventory.Roots {
		for _, file := range root.Files {
			if file.Class != ClassPrimaryMedia && file.Class != ClassExtraMedia {
				continue
			}
			if !include(file) {
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

// identityFilesByKey maps each search identity to the relpaths it claims.
func identityFilesByKey(lib sqlc.Library, result Result) map[string][]string {
	files := map[string][]string{}
	switch lib.MediaType {
	case sqlc.MediaTypeMovie:
		for _, m := range result.MovieMatches {
			files[m.Key] = m.Files
		}
	case sqlc.MediaTypeMusic:
		for _, a := range result.MusicArtists {
			files[a.Key] = a.Files
		}
	case sqlc.MediaTypeBook:
		for _, p := range result.BookPlans {
			files[p.Key] = p.Files
		}
	default: // TV-like: tv + anime
		for _, m := range result.TVMatches {
			files[m.Key] = m.Files
		}
	}
	return files
}

// acceptedIdentityRelPaths collects the relpaths of every file claimed by a
// search identity that was accepted with a provider match — the same
// criterion the process_scan worker uses to enqueue fetch_metadata.
func acceptedIdentityRelPaths(lib sqlc.Library, result Result) map[string]bool {
	filesByKey := identityFilesByKey(lib, result)
	claimed := map[string]bool{}
	claim := func(key string) {
		for _, f := range filesByKey[key] {
			claimed[f] = true
		}
	}
	switch lib.MediaType {
	case sqlc.MediaTypeMovie:
		for _, s := range result.MovieSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(s.Key)
			}
		}
	case sqlc.MediaTypeMusic:
		for _, s := range result.MusicSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(s.Key)
			}
		}
	case sqlc.MediaTypeBook:
		for _, s := range result.BookSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(s.Key)
			}
		}
	default: // TV-like: tv + anime
		for _, s := range result.TVSearch {
			if s.Accepted && s.ProviderID != "" {
				claim(s.Key)
			}
		}
	}
	return claimed
}
