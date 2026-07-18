package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaanalysis"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type FFProbeWorker struct {
	river.WorkerDefaults[FFProbeArgs]
	DB       *pgxpool.Pool
	Analysis *mediaanalysis.Service
	Progress *TaskProgressBroadcaster
}

func (w *FFProbeWorker) Work(ctx context.Context, job *river.Job[FFProbeArgs]) error {
	w.Progress.SetCurrent(FFProbeArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(job.Args.FilePath))
	source := scheduledJobSource(job.Metadata)
	probeCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	info, err := mediaprobe.Probe(probeCtx, job.Args.FilePath)
	if err != nil {
		log.Warn().Err(vfs.RedactError(err)).Str("path", vfs.RedactPath(job.Args.FilePath)).Msg("ffprobe failed")
		return fmt.Errorf("ffprobe: %w", err)
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("ffprobe marshal: %w", err)
	}

	q := sqlc.New(w.DB)
	if err := q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        job.Args.LibraryFileID,
		MediaInfo: infoJSON,
	}); err != nil {
		return fmt.Errorf("ffprobe db write: %w", err)
	}

	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return fmt.Errorf("ffprobe reload library file: %w", err)
	}
	contentHash := mediafile.ComputeContentHash(file.Size, infoJSON)
	if contentHash != "" {
		if err := q.UpdateLibraryFileContentHash(ctx, sqlc.UpdateLibraryFileContentHashParams{
			ID:          job.Args.LibraryFileID,
			ContentHash: contentHash,
		}); err != nil {
			return fmt.Errorf("ffprobe content hash write: %w", err)
		}
	}

	log.Debug().
		Int64("file_id", job.Args.LibraryFileID).
		Str("container", info.Container).
		Int("streams", len(info.Streams)).
		Float64("duration", info.Duration).
		Msg("ffprobe complete")

	hasVideo := false
	hasAudio := false
	var primaryAudio *mediaprobe.StreamInfo
	for i := range info.Streams {
		s := &info.Streams[i]
		switch s.CodecType {
		case "video":
			hasVideo = true
		case "audio":
			hasAudio = true
			if primaryAudio == nil {
				primaryAudio = s
			}
		}
	}

	if hasAudio && primaryAudio != nil {
		w.Analysis.UpdateAudioTrackFileFromProbe(ctx, job.Args.LibraryFileID, info, primaryAudio)
		w.enqueueLoudnessIfMusic(ctx, q, job.Args.LibraryFileID, job.Args.ScheduledTaskID, source)
	}

	if hasVideo {
		if !hasCurrentHLSBoundaryArtifact(file.Keyframes) {
			args := ScanKeyframesArgs{
				LibraryFileID:   job.Args.LibraryFileID,
				FilePath:        job.Args.FilePath,
				ScheduledTaskID: job.Args.ScheduledTaskID,
			}
			opts := args.InsertOpts()
			if _, err := river.ClientFromContext[pgx.Tx](ctx).Insert(ctx, args, applyScheduledJobSource(opts, source)); err != nil {
				log.Warn().Err(err).Int64("file_id", job.Args.LibraryFileID).Msg("ffprobe: enqueue keyframes failed")
			}
		}
		w.enqueuePostProbeVideoWork(ctx, q, file, job.Args.ScheduledTaskID, source)
	}

	return nil
}

func hasCurrentHLSBoundaryArtifact(raw []byte) bool {
	var kf transcoder.Keyframes
	return json.Unmarshal(raw, &kf) == nil && transcoder.HasExactHLSBoundaries(&kf)
}

type ScanKeyframesWorker struct {
	river.WorkerDefaults[ScanKeyframesArgs]
	Analysis *mediaanalysis.Service
	Progress *TaskProgressBroadcaster
}

func (w *ScanKeyframesWorker) Work(ctx context.Context, job *river.Job[ScanKeyframesArgs]) error {
	w.Progress.SetCurrent(ScanKeyframesArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(job.Args.FilePath))
	_, err := w.Analysis.AnalyzeAndPersistKeyframes(ctx, job.Args.LibraryFileID)
	return err
}

func (w *FFProbeWorker) enqueuePostProbeVideoWork(ctx context.Context, q *sqlc.Queries, file sqlc.LibraryFile, scheduledTaskID string, source string) {
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, file.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", file.LibraryID).Msg("ffprobe: library lookup for post-probe fanout failed")
		return
	}
	settings := metadata.ParseSettings(lib.Settings)
	links, err := q.ListLibraryFileLinksByFile(ctx, file.ID)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: file link lookup for post-probe fanout failed")
		return
	}

	if settings.EnableTrickplay && !file.HasTrickplay {
		if _, err := rc.Insert(ctx, TrickplayFileArgs{LibraryFileID: file.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: enqueue trickplay failed")
		}
	}
	if scannerMediaTypeScansSegments(lib.MediaType) && !file.SegmentsAnalyzedAt.Valid && libraryFileHasPrimaryLink(links) {
		if _, err := rc.Insert(ctx, ScanMediaSegmentsFileArgs{LibraryFileID: file.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("ffprobe: enqueue media segments failed")
		}
	}
	for _, link := range links {
		if link.RelationType == "extra" && link.ThumbnailPath == "" {
			if _, err := rc.Insert(ctx, ThumbnailExtraArgs{ExtraID: link.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
				log.Warn().Err(err).Int64("extra_id", link.ID).Msg("ffprobe: enqueue extra thumbnail failed")
			}
		}
	}
}

// enqueueLoudnessIfMusic schedules an ebur128 pass for the file's track_files
// row when the file lives in a music library. Silently noops outside music
// libraries or when no track_files row exists yet (matcher hasn't run).
func (w *FFProbeWorker) enqueueLoudnessIfMusic(ctx context.Context, q *sqlc.Queries, libraryFileID int64, scheduledTaskID string, source string) {
	tf, err := q.GetTrackFileByLibraryFileID(ctx, libraryFileID)
	if err != nil {
		// Matcher hasn't created the track_files row yet. It'll re-trigger
		// loudness when the row eventually appears via the scheduled
		// backstop. Don't block the probe pipeline on it.
		return
	}

	lf, err := q.GetLibraryFileByID(ctx, libraryFileID)
	if err != nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, lf.LibraryID)
	if err != nil || lib.MediaType != sqlc.MediaTypeMusic {
		return
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	if client == nil {
		return
	}
	if _, _, err := enqueueTrackLoudnessIfNeeded(ctx, q, client, ScanTrackLoudnessArgs{TrackFileID: tf.ID, ScheduledTaskID: scheduledTaskID}, scheduledJobInsertOpts(source)); err != nil {
		log.Warn().Err(err).Int64("track_file_id", tf.ID).Msg("enqueue track loudness failed")
	}
}
