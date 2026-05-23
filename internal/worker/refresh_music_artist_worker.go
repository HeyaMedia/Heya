package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// RefreshMusicArtistWorker is the music-side analogue of MetadataFetchWorker
// for movies/TV: it pulls the canonical artist + discography payload from
// heya.media and writes the upgraded data back to the artist, album, and
// track rows that the matcher created from NFO / path data. Idempotent and
// safe to run repeatedly.
type RefreshMusicArtistWorker struct {
	river.WorkerDefaults[RefreshMusicArtistArgs]
	DB      *pgxpool.Pool
	Matcher MatchService
	Hub     EventPublisher
}

func (w *RefreshMusicArtistWorker) Work(ctx context.Context, job *river.Job[RefreshMusicArtistArgs]) error {
	q := sqlc.New(w.DB)

	artist, err := q.GetArtistByID(ctx, job.Args.ArtistID)
	if err != nil {
		return fmt.Errorf("get artist %d: %w", job.Args.ArtistID, err)
	}

	res, err := w.Matcher.RefreshMusicArtist(ctx, job.Args.ArtistID)
	if err != nil {
		log.Warn().Err(err).Int64("artist_id", job.Args.ArtistID).Str("name", artist.Name).Msg("RefreshMusicArtist failed")
		// Don't retry forever on transport / parse errors — River will retry
		// per InsertOpts.MaxAttempts.
		return err
	}

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: artist.MediaItemID,
			Title:       artist.Name,
			MediaType:   string(sqlc.MediaTypeMusic),
		})

		// Per-artist progress event for the post-scan fan-out (only when the
		// job was enqueued with batch context — periodic / inline jobs
		// from the scheduled task or MetadataMatchWorker carry no batch).
		if job.Args.BatchTotal > 0 {
			w.Hub.Emit(eventhub.EventScanProgress, eventhub.ScanPayload{
				LibraryID:  job.Args.BatchLibraryID,
				Phase:      "refresh",
				Total:      job.Args.BatchTotal,
				Done:       job.Args.BatchPosition,
				CurrentRef: artist.Name,
			})
		}
	}

	// If the library has SaveNFO on, queue an NFO write. We do this even when
	// the heya.media lookup was skipped, so the matcher's NFO+path baseline
	// gets persisted to disk for future scans.
	if mi, err := q.GetMediaItemByID(ctx, artist.MediaItemID); err == nil {
		if lib, err := q.GetLibraryByID(ctx, mi.LibraryID); err == nil {
			settings := metadata.ParseSettings(lib.Settings)
			if settings.SaveNFO {
				client := river.ClientFromContext[pgx.Tx](ctx)
				if _, err := client.Insert(ctx, SaveMusicNFOArgs{ArtistID: artist.ID}, nil); err != nil {
					log.Warn().Err(err).Int64("artist_id", artist.ID).Msg("enqueue SaveMusicNFO failed")
				}
			}
		}
	}

	if res.Skipped {
		log.Info().
			Int64("artist_id", artist.ID).
			Str("name", artist.Name).
			Msg("RefreshMusicArtist: heya.media has no record yet")
		return nil
	}

	log.Info().
		Int64("artist_id", artist.ID).
		Str("name", artist.Name).
		Int("albums_matched", res.AlbumsMatched).
		Int("albums_updated", res.AlbumsUpdated).
		Int("tracks_updated", res.TracksUpdated).
		Msg("RefreshMusicArtist complete")

	return nil
}

// ensure we use errors package somewhere
var _ = errors.Is
