package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// onDemandProbeTimeout bounds a synchronous, user-facing probe. Local files
// finish well under a second; SMB sources stream through a pipe and can be
// slower, but a play request must never hang indefinitely on a flaky mount.
const onDemandProbeTimeout = 60 * time.Second

// EnsureFileProbed guarantees a library file carries ffprobe metadata before it
// is played. The async FFProbeWorker is the normal path (driven by scans); this
// is the on-demand backstop for files a user tries to play before enrichment
// has caught up. Without media_info the transcode decision is a blind guess
// ("transcode 1080p, no media info") and playback usually fails — so we probe
// inline and only then let the stream handlers decide.
//
// It is idempotent and cheap on the hot path: a file that already carries
// media_info (or isn't probeable in the first place) returns immediately.
// Otherwise it probes synchronously, persists media_info + content hash, and
// refreshes the backing track_files row for audio, returning the updated file.
//
// A probe failure is logged and swallowed — the original (unprobed) file is
// returned with a nil error. Blocking playback on a failed probe can't make an
// unplayable file play, and the stream handlers still have their existing
// fallbacks. The only error returned is a genuine "file not found".
func (a *App) EnsureFileProbed(ctx context.Context, fileID int64) (sqlc.LibraryFile, error) {
	q := sqlc.New(a.db)
	file, err := q.GetLibraryFileByID(ctx, fileID)
	if err != nil {
		return sqlc.LibraryFile{}, err
	}
	if !mediaInfoEmpty(file.MediaInfo) || !mediafile.IsProbeable(file.Path) {
		return file, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, onDemandProbeTimeout)
	defer cancel()

	info, err := worker.ProbeFile(probeCtx, file.Path)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", fileID).Msg("on-demand ffprobe failed; playing with no media info")
		return file, nil
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", fileID).Msg("on-demand ffprobe marshal failed")
		return file, nil
	}
	if err := q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        fileID,
		MediaInfo: infoJSON,
	}); err != nil {
		log.Warn().Err(err).Int64("file_id", fileID).Msg("on-demand ffprobe db write failed")
		return file, nil
	}
	file.MediaInfo = infoJSON

	if hash := scanner.ComputeContentHash(file.Size, infoJSON); hash != "" {
		_ = q.UpdateLibraryFileContentHash(ctx, sqlc.UpdateLibraryFileContentHashParams{
			ID:          fileID,
			ContentHash: hash,
		})
	}

	// Mirror the worker's audio side effect so a music track played before its
	// scan-time probe lands still gets a real bitrate / quality_score. Loudness
	// (ebur128) is deliberately left to the scheduled backstop — it only affects
	// ReplayGain normalisation, never playability.
	if audio := mediaprobe.PrimaryAudio(info); audio != nil {
		worker.UpdateAudioTrackFileFromProbe(ctx, q, fileID, info, audio)
	}

	log.Info().
		Int64("file_id", fileID).
		Str("container", info.Container).
		Int("streams", len(info.Streams)).
		Float64("duration", info.Duration).
		Msg("on-demand probe complete")
	return file, nil
}

// mediaInfoEmpty reports whether a library_files.media_info blob has never been
// populated by a probe. The column defaults to '{}' on insert, so an empty
// object counts as "not yet probed" alongside NULL / zero-length.
func mediaInfoEmpty(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "{}" || s == "null"
}
