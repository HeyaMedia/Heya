package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

var validMediaTypes = map[string]sqlc.MediaType{
	"movie":   sqlc.MediaTypeMovie,
	"tv":      sqlc.MediaTypeTv,
	"anime":   sqlc.MediaTypeAnime,
	"music":   sqlc.MediaTypeMusic,
	"book":    sqlc.MediaTypeBook,
	"comic":   sqlc.MediaTypeComic,
	"podcast": sqlc.MediaTypePodcast,
	"radio":   sqlc.MediaTypeRadio,
}

var libraryWatcherReconcileInterval = time.Minute

func ParseMediaType(s string) (sqlc.MediaType, error) {
	mt, ok := validMediaTypes[s]
	if !ok {
		return "", fmt.Errorf("invalid media type %q (valid: movie, tv, anime, music, book, comic, podcast, radio)", s)
	}
	return mt, nil
}

func (a *App) CreateLibrary(ctx context.Context, name string, mediaType sqlc.MediaType, paths []string, userID int64, settings *metadata.LibrarySettings) (sqlc.Library, error) {
	if err := validateLibraryPaths(paths); err != nil {
		return sqlc.Library{}, err
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
	a.notifyLibraryChanged(ctx, lib)
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
	if err := validateLibraryPaths(paths); err != nil {
		return sqlc.Library{}, err
	}

	q := sqlc.New(a.db)
	lib, err := q.UpdateLibrary(ctx, sqlc.UpdateLibraryParams{
		ID:           id,
		Name:         name,
		Paths:        paths,
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
	})
	if err != nil {
		return sqlc.Library{}, err
	}
	a.notifyLibraryChanged(ctx, lib)
	return lib, nil
}

func validateLibraryPaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("at least one filesystem path is required")
	}
	for _, p := range paths {
		if err := vfs.ValidateLocalPath(p); err != nil {
			return fmt.Errorf("path %q: %w", vfs.RedactPath(p), err)
		}
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("path %q: %w", vfs.RedactPath(p), err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path %q is not a directory", vfs.RedactPath(p))
		}
	}
	return nil
}

// ReportUnsupportedLibraryPaths makes legacy database rows visible at boot
// without preventing the API from starting—the admin still needs access to
// edit those rows. Actual filesystem entry points reject the same paths, so a
// legacy URL can never fall through to os.Stat as a misleading local name.
func (a *App) ReportUnsupportedLibraryPaths(ctx context.Context) {
	libs, err := a.ListLibraries(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("check configured library paths failed")
		return
	}
	for _, lib := range libs {
		for _, path := range lib.Paths {
			if err := vfs.ValidateLocalPath(path); err != nil {
				log.Error().Err(err).
					Int64("library_id", lib.ID).
					Str("library", lib.Name).
					Str("path", vfs.RedactPath(path)).
					Msg("library path requires migration before it can be scanned or played")
			}
		}
	}
}

func (a *App) UpdateLibrarySettings(ctx context.Context, id int64, settings metadata.LibrarySettings) (sqlc.Library, error) {
	settingsJSON, _ := json.Marshal(settings)
	q := sqlc.New(a.db)
	lib, err := q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       id,
		Settings: settingsJSON,
	})
	if err != nil {
		return sqlc.Library{}, err
	}
	a.notifyLibraryChanged(ctx, lib)
	return lib, nil
}

func (a *App) notifyLibraryChanged(ctx context.Context, lib sqlc.Library) {
	if err := eventhub.Notify(ctx, a.db, eventhub.EventLibraryChanged, eventhub.LibraryPayload{
		LibraryID: lib.ID,
		Name:      lib.Name,
		MediaType: string(lib.MediaType),
	}); err != nil {
		log.Warn().Err(err).Int64("library_id", lib.ID).Msg("library watcher reconciliation notify failed")
	}
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

// StartLibraryWatcherReconciler keeps the dedicated worker process's
// filesystem watchers aligned with library mutations performed by the API or
// a CLI process. Postgres LISTEN/NOTIFY is the low-latency path; a periodic DB
// reconciliation repairs notifications lost during listener reconnects.
func (a *App) StartLibraryWatcherReconciler(ctx context.Context) {
	if a.hub == nil || a.watcher == nil {
		return
	}
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		ticker := time.NewTicker(libraryWatcherReconcileInterval)
		defer ticker.Stop()
		ch := a.hub.Subscribe()
		defer a.hub.Unsubscribe(ch)
		reconcile := func() {
			if err := a.watcher.Reconcile(workCtx); err != nil && workCtx.Err() == nil {
				log.Warn().Err(err).Msg("periodic library watcher reconciliation failed")
			}
		}
		reconcile()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				reconcile()
			case ev, ok := <-ch:
				if !ok {
					return
				}
				id, ok := libraryIDFromEventPayload(ev.Payload)
				if !ok {
					continue
				}
				switch ev.Type {
				case eventhub.EventLibraryDeleted:
					a.watcher.Unwatch(id)
				case eventhub.EventLibraryChanged:
					lib, err := a.GetLibrary(workCtx, id)
					if err != nil {
						log.Warn().Err(err).Int64("library_id", id).Msg("reload changed library for watcher failed")
						continue
					}
					a.watcher.SyncLibrary(workCtx, lib)
				}
			}
		}
	})
}

// libraryIDFromEventPayload pulls library_id out of a hub event. Events that
// rode in through the relay carry a map[string]any (Event.Payload is `any`,
// so the NOTIFY JSON round-trip decodes the body as a generic map with
// float64 numbers) rather than a typed eventhub.LibraryPayload.
func libraryIDFromEventPayload(payload any) (int64, bool) {
	if typed, ok := payload.(eventhub.LibraryPayload); ok {
		return typed.LibraryID, typed.LibraryID > 0
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return 0, false
	}
	f, ok := m["library_id"].(float64)
	if !ok {
		return 0, false
	}
	return int64(f), true
}

func (a *App) MatchLibrary(ctx context.Context, id int64) (matcher.MatchResult, error) {
	lib, err := a.GetLibrary(ctx, id)
	if err != nil {
		return matcher.MatchResult{}, fmt.Errorf("library %d: %w", id, err)
	}
	// anime libraries match through the TV pipeline; see internal/mediatype.
	return a.matcher.MatchLibrary(ctx, id, mediatype.Runtime(lib.MediaType))
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
