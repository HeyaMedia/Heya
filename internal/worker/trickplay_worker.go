package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/trickplay"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// TrickplayFileArgs generates trickplay sprites for one library_file.
// One job per file so the fan-out from kickoff_trickplay is cancellable
// per item — clicking "Cancel" on the tasks page cancels every queued
// trickplay_file job at once.
//
// Skipped at insert time when:
//   - library_files.path lives on an SMB share (vfs.IsSMBPath)
//   - the file is already marked has_trickplay
//
// The worker re-validates these conditions before running, so a stale
// job that's been queued since the file was deleted is a safe no-op.
type TrickplayFileArgs struct {
	LibraryFileID   int64  `json:"library_file_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (TrickplayFileArgs) Kind() string { return "trickplay_file" }
func (TrickplayFileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "trickplay",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type TrickplayFileWorker struct {
	river.WorkerDefaults[TrickplayFileArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *TrickplayFileWorker) Work(ctx context.Context, job *river.Job[TrickplayFileArgs]) error {
	q := sqlc.New(w.DB)

	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return err
	}

	if file.HasTrickplay {
		return nil
	}
	if vfs.IsSMBPath(file.Path) {
		return nil
	}
	if len(file.MediaInfo) == 0 {
		return nil
	}

	w.Progress.SetCurrent(TrickplayFileArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(file.Path))

	var info struct {
		Duration float64 `json:"duration"`
		Streams  []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(file.MediaInfo, &info); err != nil {
		return nil
	}
	if info.Duration <= 0 {
		return nil
	}
	hasVideo := false
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			hasVideo = true
			break
		}
	}
	if !hasVideo {
		return nil
	}

	outDir := filepath.Join(filepath.Dir(file.Path), "trickplay")
	if _, err := trickplay.GenerateSprites(ctx, file.Path, info.Duration, outDir); err != nil {
		log.Warn().Err(err).Str("file", file.Path).Msg("trickplay_file: generation failed")
		return fmt.Errorf("generate sprites: %w", err)
	}

	return q.UpdateLibraryFileTrickplay(ctx, sqlc.UpdateLibraryFileTrickplayParams{
		ID:           file.ID,
		HasTrickplay: true,
	})
}

// ThumbnailExtraArgs extracts a thumbnail frame for one extra library-file link.
// One job per extra link so the kickoff_thumbnails fan-out is cancellable per item.
type ThumbnailExtraArgs struct {
	ExtraID         int64  `json:"extra_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (ThumbnailExtraArgs) Kind() string { return "thumbnail_extra" }
func (ThumbnailExtraArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "thumbnails",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type ThumbnailExtraWorker struct {
	river.WorkerDefaults[ThumbnailExtraArgs]
	DB       *pgxpool.Pool
	DataDir  string
	Progress *TaskProgressBroadcaster
}

func (w *ThumbnailExtraWorker) Work(ctx context.Context, job *river.Job[ThumbnailExtraArgs]) error {
	q := sqlc.New(w.DB)

	row, err := q.GetMediaExtraLinkByID(ctx, job.Args.ExtraID)
	if err != nil {
		return err
	}

	if row.ThumbnailPath != "" {
		return nil
	}
	if row.FilePath == "" {
		return nil
	}

	label := row.Title
	if label == "" {
		label = filepath.Base(row.FilePath)
	}
	w.Progress.SetCurrent(ThumbnailExtraArgs{}.Kind(), job.Args.ScheduledTaskID, label)

	dir := filepath.Join(w.DataDir, "images", "extras", fmt.Sprintf("%d", row.MediaItemID))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	outPath := filepath.Join(dir, fmt.Sprintf("extra_%d.jpg", row.ID))

	if err := trickplay.ExtractThumbnail(ctx, row.FilePath, row.DurationMs, outPath); err != nil {
		log.Warn().Err(err).Int64("extra_id", row.ID).Msg("thumbnail_extra: extraction failed")
		return fmt.Errorf("extract thumbnail: %w", err)
	}

	return q.UpdateMediaExtraLinkThumbnail(ctx, sqlc.UpdateMediaExtraLinkThumbnailParams{
		ID:            row.ID,
		ThumbnailPath: outPath,
	})
}
