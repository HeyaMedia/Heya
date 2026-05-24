package scheduler

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// ScanMusicLoudnessTask is the backstop for the ebur128 hand-off in
// FFProbeWorker. It catches:
//   - files probed before the loudness pipeline existed
//   - files where the matcher's track_files row was created *after* probe,
//     so the hand-off saw "no row yet" and silently dropped the enqueue
//   - albums whose tracks have all been measured but never got their
//     album-level pass (e.g. the cascade worker crashed mid-run)
//
// Enqueues respect the unique-by-args guard on both worker types, so a
// concurrent inline enqueue from the probe pipeline can't cause duplicate
// work.
type ScanMusicLoudnessTask struct {
	DB    *pgxpool.Pool
	River *river.Client[pgx.Tx]
}

func (t *ScanMusicLoudnessTask) ID() TaskID { return TaskScanMusicLoudness }

// Per-tick caps. The loudness queue is MaxWorkers=1 so it'll chew through
// the backlog at ~30s/track regardless. Bounding the enqueue keeps the
// River job table from ballooning on a fresh import of a 100k-track library.
const (
	loudnessTrackBatch = 500
	loudnessAlbumBatch = 200
)

func (t *ScanMusicLoudnessTask) CountPending(ctx context.Context) (int, error) {
	q := sqlc.New(t.DB)
	tracks, err := q.ListTrackFilesPendingLoudness(ctx, loudnessTrackBatch)
	if err != nil {
		return 0, err
	}
	albums, err := q.ListAlbumsPendingLoudness(ctx, loudnessAlbumBatch)
	if err != nil {
		return 0, err
	}
	return len(tracks) + len(albums), nil
}

func (t *ScanMusicLoudnessTask) Run(ctx context.Context, progress *ProgressTracker) error {
	q := sqlc.New(t.DB)

	tracks, err := q.ListTrackFilesPendingLoudness(ctx, loudnessTrackBatch)
	if err != nil {
		return err
	}
	albums, err := q.ListAlbumsPendingLoudness(ctx, loudnessAlbumBatch)
	if err != nil {
		return err
	}

	progress.SetTotal(len(tracks) + len(albums))

	for _, row := range tracks {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := t.River.Insert(ctx, worker.ScanTrackLoudnessArgs{TrackFileID: row.ID}, nil); err != nil {
			log.Warn().Err(err).Int64("track_file_id", row.ID).Msg("scan_music_loudness: enqueue track failed")
			progress.Fail(row.Path)
			continue
		}
		progress.Advance(row.Path)
	}
	for _, row := range albums {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := t.River.Insert(ctx, worker.ScanAlbumLoudnessArgs{AlbumID: row.ID}, nil); err != nil {
			log.Warn().Err(err).Int64("album_id", row.ID).Msg("scan_music_loudness: enqueue album failed")
			progress.Fail(row.Title)
			continue
		}
		progress.Advance(row.Title)
	}

	if progress.Snapshot().Completed > 0 {
		log.Info().Int("enqueued", progress.Snapshot().Completed).Msg("scan_music_loudness: jobs enqueued")
	}
	return nil
}
