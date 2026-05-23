package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
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

func (a *App) CreateLibrary(ctx context.Context, name string, mediaType sqlc.MediaType, paths []string, userID int64, settings *metadata.LibrarySettings) (sqlc.Library, error) {
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

	var settingsJSON []byte
	if settings != nil {
		settingsJSON, _ = json.Marshal(settings)
	} else {
		defaults := metadata.DefaultSettings(string(mediaType))
		settingsJSON, _ = json.Marshal(defaults)
	}

	q := sqlc.New(a.db)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         name,
		MediaType:    mediaType,
		Paths:        paths,
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     settingsJSON,
	})
	if err != nil {
		return sqlc.Library{}, fmt.Errorf("creating library: %w", err)
	}

	return lib, nil
}

func (a *App) ListLibraries(ctx context.Context) ([]sqlc.Library, error) {
	q := sqlc.New(a.db)
	return q.ListLibraries(ctx)
}

func (a *App) GetLibrary(ctx context.Context, id int64) (sqlc.Library, error) {
	q := sqlc.New(a.db)
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

	q := sqlc.New(a.db)
	return q.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{
		ID:           id,
		Name:         name,
		Paths:        paths,
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
	})
}

func (a *App) UpdateLibrarySettings(ctx context.Context, id int64, settings metadata.LibrarySettings) (sqlc.Library, error) {
	settingsJSON, _ := json.Marshal(settings)
	q := sqlc.New(a.db)
	return q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       id,
		Settings: settingsJSON,
	})
}

func (a *App) GetLibrarySettings(ctx context.Context, id int64) (metadata.LibrarySettings, error) {
	lib, err := a.GetLibrary(ctx, id)
	if err != nil {
		return metadata.LibrarySettings{}, err
	}
	return metadata.ParseSettings(lib.Settings), nil
}

func (a *App) DeleteLibrary(ctx context.Context, id int64) error {
	q := sqlc.New(a.db)
	return q.DeleteLibrary(ctx, id)
}

func (a *App) MatchLibrary(ctx context.Context, id int64) (matcher.MatchResult, error) {
	lib, err := a.GetLibrary(ctx, id)
	if err != nil {
		return matcher.MatchResult{}, fmt.Errorf("library %d: %w", id, err)
	}
	return a.matcher.MatchLibrary(ctx, id, lib.MediaType)
}

func (a *App) ResolveMatch(ctx context.Context, fileID, candidateID int64) error {
	return a.matcher.ResolveMatch(ctx, fileID, candidateID)
}

func (a *App) ListLibraryFiles(ctx context.Context, libraryID int64, limit, offset int32) ([]sqlc.LibraryFile, error) {
	q := sqlc.New(a.db)
	return q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (a *App) LibraryFileStats(ctx context.Context, libraryID int64) ([]sqlc.CountLibraryFilesByStatusRow, error) {
	q := sqlc.New(a.db)
	return q.CountLibraryFilesByStatus(ctx, libraryID)
}

func (a *App) ListMatchCandidates(ctx context.Context, fileID int64) ([]sqlc.MatchCandidate, error) {
	q := sqlc.New(a.db)
	return q.ListMatchCandidatesByFile(ctx, fileID)
}

func (a *App) EnqueueScanLibrary(id int64, force bool) {
	a.scanTask.Enqueue(id, force)
	a.scheduler.TriggerNow(scheduler.TaskScanLibraries)
}

func (a *App) EnqueueForceRefreshMetadata(ctx context.Context, libraryID int64) error {
	_, err := a.river.Insert(ctx, worker.ForceRefreshMetadataArgs{LibraryID: libraryID}, nil)
	return err
}

func (a *App) EnqueueForceRefreshImages(ctx context.Context, libraryID int64) error {
	_, err := a.river.Insert(ctx, worker.ForceRefreshImagesArgs{LibraryID: libraryID}, nil)
	return err
}

func (a *App) ListDeletedFiles(ctx context.Context, libraryID int64, limit, offset int32) ([]sqlc.LibraryFile, error) {
	q := sqlc.New(a.db)
	return q.ListDeletedLibraryFiles(ctx, sqlc.ListDeletedLibraryFilesParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (a *App) PurgeDeletedFiles(ctx context.Context, libraryID int64) error {
	q := sqlc.New(a.db)
	return q.PurgeDeletedLibraryFiles(ctx, sqlc.PurgeDeletedLibraryFilesParams{
		LibraryID: libraryID,
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
}
