package scheduler

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
	"github.com/rs/zerolog/log"
)

type GenerateTrickplayTask struct {
	DB      *pgxpool.Pool
	DataDir string
}

func (t *GenerateTrickplayTask) ID() TaskID { return TaskGenerateTrickplay }

type trickplayCandidate struct {
	fileID   int64
	filePath string
	duration float64
}

func (t *GenerateTrickplayTask) findCandidates(ctx context.Context) ([]trickplayCandidate, error) {
	rows, err := t.DB.Query(ctx, `
		SELECT lf.id, lf.path, lf.media_info
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.deleted_at IS NULL
		  AND lf.status = 'matched'
		  AND lf.has_trickplay = false
		  AND lf.media_info IS NOT NULL
		  AND l.settings->>'enable_trickplay' = 'true'
	`)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var candidates []trickplayCandidate
	for rows.Next() {
		var fileID int64
		var filePath string
		var mediaInfoBytes []byte
		if err := rows.Scan(&fileID, &filePath, &mediaInfoBytes); err != nil {
			continue
		}

		if vfs.IsSMBPath(filePath) {
			continue
		}

		var info struct {
			Duration float64 `json:"duration"`
			Streams  []struct {
				CodecType string `json:"codec_type"`
			} `json:"streams"`
		}
		if err := json.Unmarshal(mediaInfoBytes, &info); err != nil || info.Duration <= 0 {
			continue
		}

		hasVideo := false
		for _, s := range info.Streams {
			if s.CodecType == "video" {
				hasVideo = true
				break
			}
		}
		if !hasVideo {
			continue
		}

		candidates = append(candidates, trickplayCandidate{
			fileID:   fileID,
			filePath: filePath,
			duration: info.Duration,
		})
	}
	return candidates, rows.Err()
}

func (t *GenerateTrickplayTask) CountPending(ctx context.Context) (int, error) {
	candidates, err := t.findCandidates(ctx)
	if err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func (t *GenerateTrickplayTask) Run(ctx context.Context, progress *ProgressTracker) error {
	candidates, err := t.findCandidates(ctx)
	if err != nil {
		return err
	}

	progress.SetTotal(len(candidates))
	q := sqlc.New(t.DB)

	for _, c := range candidates {
		if ctx.Err() != nil {
			return nil
		}

		outDir := filepath.Join(filepath.Dir(c.filePath), "trickplay")
		name := filepath.Base(c.filePath)
		_, err := trickplay.GenerateSprites(ctx, c.filePath, c.duration, outDir)
		if err != nil {
			log.Warn().Err(err).Str("file", c.filePath).Msg("scheduler: trickplay generation failed")
			progress.Fail(name)
		} else {
			q.UpdateLibraryFileTrickplay(ctx, sqlc.UpdateLibraryFileTrickplayParams{
				ID:           c.fileID,
				HasTrickplay: true,
			})
			progress.Advance(name)
		}
	}
	return nil
}

type GenerateThumbnailsTask struct {
	DB      *pgxpool.Pool
	DataDir string
}

func (t *GenerateThumbnailsTask) ID() TaskID { return TaskGenerateThumbnails }

type thumbnailCandidate struct {
	extraID     int64
	mediaItemID int64
	filePath    string
	durationMs  int32
	title       string
}

func (t *GenerateThumbnailsTask) findCandidates(ctx context.Context) ([]thumbnailCandidate, error) {
	rows, err := t.DB.Query(ctx, `
		SELECT me.id, me.media_item_id, me.file_path, me.duration_ms, me.title
		FROM media_extras me
		JOIN media_items mi ON mi.id = me.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE me.thumbnail_path = ''
		  AND me.file_path != ''
		  AND l.settings->>'generate_thumbnails' = 'true'
	`)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var candidates []thumbnailCandidate
	for rows.Next() {
		var c thumbnailCandidate
		if err := rows.Scan(&c.extraID, &c.mediaItemID, &c.filePath, &c.durationMs, &c.title); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (t *GenerateThumbnailsTask) CountPending(ctx context.Context) (int, error) {
	candidates, err := t.findCandidates(ctx)
	if err != nil {
		return 0, err
	}
	return len(candidates), nil
}

func (t *GenerateThumbnailsTask) Run(ctx context.Context, progress *ProgressTracker) error {
	candidates, err := t.findCandidates(ctx)
	if err != nil {
		return err
	}

	progress.SetTotal(len(candidates))
	q := sqlc.New(t.DB)

	for _, c := range candidates {
		if ctx.Err() != nil {
			return nil
		}

		dir := filepath.Join(t.DataDir, "images", "extras", fmt.Sprintf("%d", c.mediaItemID))
		os.MkdirAll(dir, 0755)
		outPath := filepath.Join(dir, fmt.Sprintf("extra_%d.jpg", c.extraID))

		if err := trickplay.ExtractThumbnail(ctx, c.filePath, c.durationMs, outPath); err != nil {
			log.Warn().Err(err).Int64("extra_id", c.extraID).Msg("scheduler: thumbnail generation failed")
			progress.Fail(c.title)
			continue
		}

		q.UpdateExtraThumbnail(ctx, sqlc.UpdateExtraThumbnailParams{
			ID:            c.extraID,
			ThumbnailPath: outPath,
		})

		progress.Advance(c.title)
	}
	return nil
}
