package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// RefreshMusicArtistsTask is the music-side analogue of RefreshMetadataTask.
// It finds artists whose enriched_at is older than the owning library's
// MetadataRefreshDays setting and enqueues a RefreshMusicArtist job for each
// (one at a time via the music_metadata queue, so cold heya.media lookups
// don't pile up).
type RefreshMusicArtistsTask struct {
	DB    *pgxpool.Pool
	River *river.Client[pgx.Tx]
}

func (t *RefreshMusicArtistsTask) ID() TaskID { return TaskRefreshMusicArtists }

type staleArtist struct {
	ArtistID   int64
	Name       string
	LibraryID  int64
	Refresh    int // MetadataRefreshDays from library settings
	EnrichedAt *time.Time
}

func (t *RefreshMusicArtistsTask) findStaleArtists(ctx context.Context) ([]staleArtist, error) {
	rows, err := t.DB.Query(ctx, `
		SELECT a.id, a.name, mi.library_id, l.settings, a.enriched_at
		FROM artists a
		JOIN media_items mi ON mi.id = a.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE l.media_type = 'music'
		ORDER BY a.enriched_at ASC NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var out []staleArtist
	for rows.Next() {
		var s staleArtist
		var settingsJSON []byte
		var enrichedAt *time.Time
		if err := rows.Scan(&s.ArtistID, &s.Name, &s.LibraryID, &settingsJSON, &enrichedAt); err != nil {
			continue
		}
		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			// Library opted out of periodic refresh.
			continue
		}
		s.Refresh = settings.MetadataRefreshDays
		s.EnrichedAt = enrichedAt

		// Never enriched OR enriched longer ago than the library's window.
		if enrichedAt == nil {
			out = append(out, s)
			continue
		}
		cutoff := now.AddDate(0, 0, -s.Refresh)
		if enrichedAt.Before(cutoff) {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

func (t *RefreshMusicArtistsTask) CountPending(ctx context.Context) (int, error) {
	items, err := t.findStaleArtists(ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (t *RefreshMusicArtistsTask) Run(ctx context.Context, progress *ProgressTracker) error {
	items, err := t.findStaleArtists(ctx)
	if err != nil {
		return err
	}

	progress.SetTotal(len(items))

	for _, it := range items {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := t.River.Insert(ctx, worker.RefreshMusicArtistArgs{ArtistID: it.ArtistID}, nil); err != nil {
			log.Warn().Err(err).Int64("artist_id", it.ArtistID).Msg("refresh_music_artists: enqueue failed")
			progress.Fail(it.Name)
			continue
		}
		progress.Advance(it.Name)
	}

	if progress.Snapshot().Completed > 0 {
		log.Info().Int("enqueued", progress.Snapshot().Completed).Msg("refresh_music_artists: artists enqueued")
	}
	return nil
}
