package watcher

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

func SetupPeriodicScans(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) error {
	q := sqlc.New(db)
	libs, err := q.ListLibraries(ctx)
	if err != nil {
		return err
	}

	for _, lib := range libs {
		settings := metadata.ParseSettings(lib.Settings)
		if !settings.Watch {
			continue
		}

		hasSMB := false
		for _, p := range lib.Paths {
			if vfs.IsSMBPath(p) {
				hasSMB = true
				break
			}
		}

		if !hasSMB {
			continue
		}

		interval := time.Hour
		if lib.ScanInterval.Valid {
			interval = time.Duration(lib.ScanInterval.Microseconds) * time.Microsecond
		}

		libID := lib.ID
		riverClient.PeriodicJobs().Add(
			river.NewPeriodicJob(
				river.PeriodicInterval(interval),
				func() (river.JobArgs, *river.InsertOpts) {
					return worker.ScanLibraryArgs{LibraryID: libID}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		)

		log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Dur("interval", interval).Msg("scheduled periodic re-scan for SMB library")
	}

	for _, lib := range libs {
		s := metadata.ParseSettings(lib.Settings)
		if s.MetadataRefreshDays <= 0 {
			continue
		}

		refreshInterval := time.Duration(s.MetadataRefreshDays) * 24 * time.Hour
		libID := lib.ID
		riverClient.PeriodicJobs().Add(
			river.NewPeriodicJob(
				river.PeriodicInterval(refreshInterval),
				func() (river.JobArgs, *river.InsertOpts) {
					return worker.MetadataRefreshArgs{LibraryID: libID}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: false},
			),
		)

		log.Info().Int64("library_id", lib.ID).Int("days", s.MetadataRefreshDays).Msg("scheduled periodic metadata refresh")
	}

	return nil
}
