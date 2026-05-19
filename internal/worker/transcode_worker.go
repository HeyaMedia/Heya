package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type TranscodeWorker struct {
	river.WorkerDefaults[TranscodeArgs]
	DB    *pgxpool.Pool
	Cache *transcoder.CacheManager
}

func (w *TranscodeWorker) Work(ctx context.Context, job *river.Job[TranscodeArgs]) error {
	if !transcoder.IsFFmpegAvailable() {
		log.Warn().Msg("ffmpeg not available, skipping transcode")
		return nil
	}

	q := sqlc.New(w.DB)
	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return nil
	}

	var info MediaInfo
	if len(file.MediaInfo) > 0 {
		json.Unmarshal(file.MediaInfo, &info)
	}

	profileName := job.Args.Profile
	if profileName == "" {
		tInfo := toTranscoderMediaInfo(&info)
		plan := transcoder.Decide(&tInfo, transcoder.DefaultClientCaps)
		if plan.Action == transcoder.ActionDirectPlay {
			log.Debug().Int64("file_id", file.ID).Msg("direct play, no transcode needed")
			return nil
		}
		profileName = plan.Profile
	}

	profile, ok := transcoder.GetProfile(profileName)
	if !ok {
		log.Warn().Str("profile", profileName).Msg("unknown transcode profile")
		return nil
	}

	key := transcoder.FormatKey(file.ID, profileName)
	outputDir := w.Cache.SegmentDir(key)

	log.Info().Int64("file_id", file.ID).Str("profile", profileName).Msg("starting background transcode")

	if err := transcoder.TranscodeToHLS(ctx, file.Path, outputDir, profile); err != nil {
		log.Error().Err(err).Int64("file_id", file.ID).Msg("transcode failed")
		return nil
	}

	log.Info().Int64("file_id", file.ID).Str("profile", profileName).Msg("transcode complete")
	return nil
}

func toTranscoderMediaInfo(info *MediaInfo) transcoder.MediaInfo {
	var streams []transcoder.StreamInfo
	for _, s := range info.Streams {
		streams = append(streams, transcoder.StreamInfo{
			CodecName: s.CodecName,
			CodecType: s.CodecType,
		})
	}
	return transcoder.MediaInfo{
		Container: info.Container,
		Streams:   streams,
	}
}
