package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/matcher"
	"github.com/karbowiak/kura/internal/scanner"
	"github.com/karbowiak/kura/internal/vfs"
	"github.com/karbowiak/kura/internal/worker"
)

var validMediaTypes = map[string]sqlc.MediaType{
	"movie":   sqlc.MediaTypeMovie,
	"tv":      sqlc.MediaTypeTv,
	"music":   sqlc.MediaTypeMusic,
	"book":    sqlc.MediaTypeBook,
	"comic":   sqlc.MediaTypeComic,
	"podcast": sqlc.MediaTypePodcast,
	"radio":   sqlc.MediaTypeRadio,
}

func ParseMediaType(s string) (sqlc.MediaType, error) {
	mt, ok := validMediaTypes[s]
	if !ok {
		return "", fmt.Errorf("invalid media type %q (valid: movie, tv, music, book, comic, podcast, radio)", s)
	}
	return mt, nil
}

func (a *App) CreateLibrary(ctx context.Context, name string, mediaType sqlc.MediaType, paths []string, userID int64) (sqlc.Library, error) {
	for _, p := range paths {
		if vfs.IsSMBPath(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return sqlc.Library{}, fmt.Errorf("path %q: %w", p, err)
		}
		if !info.IsDir() {
			return sqlc.Library{}, fmt.Errorf("path %q is not a directory", p)
		}
	}

	q := sqlc.New(a.DB)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         name,
		MediaType:    mediaType,
		Paths:        paths,
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
	})
	if err != nil {
		return sqlc.Library{}, fmt.Errorf("creating library: %w", err)
	}

	return lib, nil
}

func (a *App) ListLibraries(ctx context.Context) ([]sqlc.Library, error) {
	q := sqlc.New(a.DB)
	return q.ListLibraries(ctx)
}

func (a *App) GetLibrary(ctx context.Context, id int64) (sqlc.Library, error) {
	q := sqlc.New(a.DB)
	return q.GetLibraryByID(ctx, id)
}

func (a *App) UpdateLibrary(ctx context.Context, id int64, name string, paths []string) (sqlc.Library, error) {
	for _, p := range paths {
		if vfs.IsSMBPath(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return sqlc.Library{}, fmt.Errorf("path %q: %w", p, err)
		}
		if !info.IsDir() {
			return sqlc.Library{}, fmt.Errorf("path %q is not a directory", p)
		}
	}

	q := sqlc.New(a.DB)
	return q.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{
		ID:           id,
		Name:         name,
		Paths:        paths,
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
	})
}

func (a *App) DeleteLibrary(ctx context.Context, id int64) error {
	q := sqlc.New(a.DB)
	return q.DeleteLibrary(ctx, id)
}

func (a *App) ScanLibrary(ctx context.Context, id int64, opts scanner.ScanOptions) (scanner.ScanResult, error) {
	lib, err := a.GetLibrary(ctx, id)
	if err != nil {
		return scanner.ScanResult{}, fmt.Errorf("library %d: %w", id, err)
	}
	return a.Scanner.ScanLibrary(ctx, lib, opts)
}

func (a *App) MatchLibrary(ctx context.Context, id int64) (matcher.MatchResult, error) {
	lib, err := a.GetLibrary(ctx, id)
	if err != nil {
		return matcher.MatchResult{}, fmt.Errorf("library %d: %w", id, err)
	}
	return a.Matcher.MatchLibrary(ctx, id, lib.MediaType)
}

func (a *App) ResolveMatch(ctx context.Context, fileID, candidateID int64) error {
	return a.Matcher.ResolveMatch(ctx, fileID, candidateID)
}

func (a *App) ListLibraryFiles(ctx context.Context, libraryID int64, limit, offset int32) ([]sqlc.LibraryFile, error) {
	q := sqlc.New(a.DB)
	return q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (a *App) LibraryFileStats(ctx context.Context, libraryID int64) ([]sqlc.CountLibraryFilesByStatusRow, error) {
	q := sqlc.New(a.DB)
	return q.CountLibraryFilesByStatus(ctx, libraryID)
}

func (a *App) ListMatchCandidates(ctx context.Context, fileID int64) ([]sqlc.MatchCandidate, error) {
	q := sqlc.New(a.DB)
	return q.ListMatchCandidatesByFile(ctx, fileID)
}

func (a *App) EnqueueScanLibrary(ctx context.Context, id int64, force bool) error {
	_, err := a.River.Insert(ctx, worker.ScanLibraryArgs{
		LibraryID: id,
		Force:     force,
	}, nil)
	return err
}

func (a *App) ListDeletedFiles(ctx context.Context, libraryID int64, limit, offset int32) ([]sqlc.LibraryFile, error) {
	q := sqlc.New(a.DB)
	return q.ListDeletedLibraryFiles(ctx, sqlc.ListDeletedLibraryFilesParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (a *App) PurgeDeletedFiles(ctx context.Context, libraryID int64) error {
	q := sqlc.New(a.DB)
	return q.PurgeDeletedLibraryFiles(ctx, sqlc.PurgeDeletedLibraryFilesParams{
		LibraryID: libraryID,
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
}
