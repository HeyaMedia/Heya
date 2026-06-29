package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
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
	// Capture identity before the row (and its ON DELETE CASCADE) is gone, so
	// the WS event can carry the media_type. A missing row is fine — the
	// DELETE is a no-op and we still broadcast the (harmless) invalidation.
	lib, _ := q.GetLibraryByID(ctx, id)
	if err := q.DeleteLibrary(ctx, id); err != nil {
		return err
	}
	// Tell connected browsers to drop their cached catalog data. The delete
	// cascades across an entire media type (items, files, the music
	// artist→album→track chain, home rails, mixes, recommendations), which no
	// page-local FE invalidation can cover.
	//
	// We go via Postgres NOTIFY, not a direct hub.Emit, because the WebSocket
	// clients live in the `heya serve` process — and DeleteLibrary also runs
	// from a `heya library remove` CLI call, which is a separate, cacheless,
	// one-shot process whose own hub has no subscribers. NOTIFY pokes the
	// running server, whose relay (StartCrossProcessRelay) re-emits onto the
	// live hub. Best-effort: if nothing is serving, there are no browsers to
	// update anyway.
	if err := eventhub.Notify(ctx, a.db, eventhub.EventLibraryDeleted, eventhub.LibraryPayload{
		LibraryID: id,
		Name:      lib.Name,
		MediaType: string(lib.MediaType),
	}); err != nil {
		log.Warn().Err(err).Int64("library_id", id).Msg("DeleteLibrary: cache-invalidation notify failed")
	}
	return nil
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
	if a.scheduler == nil {
		return
	}
	if err := a.scheduler.EnqueueLibraryScan(a.lifetimeCtx, id, force); err != nil {
		log.Warn().Err(err).Int64("library_id", id).Msg("EnqueueScanLibrary: insert kickoff failed")
	}
}

func (a *App) EnqueueForceRefreshMetadata(ctx context.Context, libraryID int64) error {
	_, err := a.river.Insert(ctx, worker.ForceRefreshMetadataArgs{LibraryID: libraryID}, nil)
	return err
}

func (a *App) EnqueueForceRefreshImages(ctx context.Context, libraryID int64) error {
	_, err := a.river.Insert(ctx, worker.ForceRefreshImagesArgs{LibraryID: libraryID}, nil)
	return err
}

// EnqueueScanLibraryDisk fans out one ScanLibraryDisk job per library, or
// targets a single library when libraryID > 0. UniqueByArgs in the job
// definition means a duplicate insert while one is queued/running is a no-op
// — admins can hammer the button without piling on work.
func (a *App) EnqueueScanLibraryDisk(ctx context.Context, libraryID int64) error {
	if libraryID > 0 {
		_, err := a.river.Insert(ctx, worker.ScanLibraryDiskArgs{LibraryID: libraryID}, nil)
		return err
	}
	libs, err := a.ListLibraries(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if _, err := a.river.Insert(ctx, worker.ScanLibraryDiskArgs{LibraryID: l.ID}, nil); err != nil {
			return err
		}
	}
	return nil
}

// LibraryDiskUsage is the typed view returned to handlers. Mirrors the sqlc
// row but exposes the timestamp as a regular time.Time.
type LibraryDiskUsage struct {
	LibraryID int64     `json:"library_id"`
	Path      string    `json:"path"`
	Bytes     int64     `json:"bytes"`
	FileCount int64     `json:"file_count"`
	ScannedAt time.Time `json:"scanned_at"`
}

func (a *App) ListLibraryDiskUsage(ctx context.Context) ([]LibraryDiskUsage, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListLibraryDiskUsage(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LibraryDiskUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, LibraryDiskUsage{
			LibraryID: r.LibraryID,
			Path:      r.Path,
			Bytes:     r.Bytes,
			FileCount: r.FileCount,
			ScannedAt: r.ScannedAt.Time,
		})
	}
	return out, nil
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
