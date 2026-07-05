package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
)

// parseAudioCaps pulls the audio-capability flags off the request query. The
// frontend sets these via capsToQueryString() in useClientCaps. Missing
// params are treated as "not supported" — better to over-transcode than to
// send bytes the browser silently fails on.
func parseAudioCaps(r *http.Request) transcoder.AudioCaps {
	get := func(k string) bool { return queryFlag(r.URL.Query().Get(k)) }
	return transcoder.AudioCaps{
		FLAC:   get("supports_flac_native") || get("supports_flac"),
		ALAC:   get("supports_alac"),
		MP3:    get("supports_mp3"),
		AAC:    get("supports_aac_audio"),
		Vorbis: get("supports_ogg_vorbis"),
		Opus:   get("supports_opus_audio") || get("supports_opus"),
		WavPCM: get("supports_wav_pcm"),
	}
}

// pickBestPlayableFile walks the candidates best-quality-first and returns
// the first one the client can decode natively. Returns ok=false when none
// of the available files match the caps — caller falls back to transcode.
func pickBestPlayableFile(files []sqlc.TrackFile, caps transcoder.AudioCaps) (sqlc.TrackFile, bool) {
	for _, f := range files {
		if transcoder.CanPlayDirect(f.Format, caps) {
			return f, true
		}
	}
	return sqlc.TrackFile{}, false
}

// audioQualityTiers maps the "quality" query param's fixed enum onto the
// AAC bitrate handleStreamTrack asks the session manager to encode. This is
// the single source of truth for the enum — musicTrackStreamInput's doc tag
// in binary_huma.go lists the same keys for the generated OpenAPI spec.
var audioQualityTiers = map[string]int{
	"aac-320": 320,
	"aac-256": 256,
	"aac-192": 192,
	"aac-128": 128,
}

// parseAudioQualityTier reads the "quality" query param. ok=false covers
// both "absent" and "unrecognized" — per the API contract, both cases fall
// through to today's caps-based decision tree unchanged.
func parseAudioQualityTier(r *http.Request) (int, bool) {
	kbps, ok := audioQualityTiers[r.URL.Query().Get("quality")]
	return kbps, ok
}

// handleStreamTrack picks the best file the client can play. With no audio
// caps in the query string everything maps to "can't play", so callers that
// don't probe caps still get the primary file (the legacy code path).
//
//   - "quality" tier given                → see handleStreamTrackQualityTier
//   - At least one file matches caps      → range-serve it untouched
//   - No file matches but caps were sent  → on-the-fly AAC-256 transcode
//     of the primary (highest-quality) file, cached after first run
//   - No caps sent at all                 → range-serve the primary directly,
//     matching the pre-Phase B.5 behavior
func handleStreamTrack(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trackID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid track id")
			return
		}

		files, err := app.ListTrackFiles(r.Context(), trackID)
		if err != nil || len(files) == 0 {
			writeError(w, http.StatusNotFound, "no playable file for track")
			return
		}

		// Explicit quality-tier override takes priority over the caps-based
		// tree below. Absent/unrecognized "quality" leaves hasCaps/caps
		// computation — and thus the rest of this function — byte-identical
		// to before this param existed.
		if tierKbps, ok := parseAudioQualityTier(r); ok {
			handleStreamTrackQualityTier(w, r, app, files, parseAudioCaps(r), tierKbps)
			return
		}

		hasCaps := len(r.URL.Query()) > 0
		caps := parseAudioCaps(r)

		if !hasCaps {
			_, _ = app.EnsureFileProbed(r.Context(), files[0].LibraryFileID)
			serveTrackFileBytes(w, r, app, files[0].LibraryFileID)
			return
		}

		if tf, ok := pickBestPlayableFile(files, caps); ok {
			_, _ = app.EnsureFileProbed(r.Context(), tf.LibraryFileID)
			serveTrackFileBytes(w, r, app, tf.LibraryFileID)
			return
		}

		// Fall back to AAC-256 fragmented MP4. Transcoded from the primary
		// file (highest quality_score) so we don't bake a low-bitrate
		// fallback when a lossless source is available.
		transcodePrimaryAndServe(w, r, app, files[0], transcoder.DefaultAudioBitrateKbps)
	}
}

// handleStreamTrackQualityTier implements the explicit "quality" override: a
// client asked for a specific AAC bitrate (e.g. a bandwidth-capped mobile
// session), so we transcode the primary (highest quality_score) file to
// that tier even when a natively-playable file exists — UNLESS the file the
// client would otherwise get direct is lossy and already at/under the
// requested bitrate (plus a small margin), per
// transcoder.ShouldTranscodeForTier. Lossless sources always transcode when
// a tier is requested.
func handleStreamTrackQualityTier(w http.ResponseWriter, r *http.Request, app *service.App, files []sqlc.TrackFile, caps transcoder.AudioCaps, tierKbps int) {
	if tf, ok := pickBestPlayableFile(files, caps); ok && !transcoder.ShouldTranscodeForTier(tf.Format, int(tf.BitrateKbps), tierKbps) {
		_, _ = app.EnsureFileProbed(r.Context(), tf.LibraryFileID)
		serveTrackFileBytes(w, r, app, tf.LibraryFileID)
		return
	}
	transcodePrimaryAndServe(w, r, app, files[0], tierKbps)
}

// transcodePrimaryAndServe runs (or reuses the cached output of) an AAC
// transcode of the track's primary (highest quality_score) file at the
// given bitrate, then range-serves the result. Shared by the legacy
// caps-fallback path and the explicit quality-tier override.
func transcodePrimaryAndServe(w http.ResponseWriter, r *http.Request, app *service.App, primary sqlc.TrackFile, bitrateKbps int) {
	audio := app.AudioSessions()
	if audio == nil {
		writeError(w, http.StatusServiceUnavailable, "no compatible format and ffmpeg unavailable for transcode")
		return
	}
	lf, err := app.EnsureFileProbed(r.Context(), primary.LibraryFileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "library file not found")
		return
	}
	if vfs.IsSMBPath(lf.Path) {
		// AAC transcode from SMB would need a streaming source — defer.
		writeError(w, http.StatusServiceUnavailable, "transcode from remote source not supported")
		return
	}
	cached, err := audio.EnsureAAC(r.Context(), primary.ID, lf.Path, bitrateKbps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "transcode failed: "+err.Error())
		return
	}
	f, err := os.Open(cached) //nolint:gosec // cached path is a tempfile we just wrote
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open transcode output")
		return
	}
	defer func() { _ = f.Close() }()
	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "audio/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, cached, stat.ModTime(), f)
}

// handleStreamTrackFile range-serves an explicitly chosen format of a track.
// Untouched bytes — caller picks the format (FLAC vs MP3, 24/96 vs 16/44).
// Bit-perfect path: no transcoding, no remux, no resampling. Future native
// clients hit this for exclusive-mode audio output.
func handleStreamTrackFile(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trackID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid track id")
			return
		}
		fileID, err := strconv.ParseInt(r.PathValue("track_file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid track_file id")
			return
		}

		tf, err := app.GetTrackFile(r.Context(), fileID)
		if err != nil || tf.TrackID != trackID {
			writeError(w, http.StatusNotFound, "track file not found")
			return
		}
		_, _ = app.EnsureFileProbed(r.Context(), tf.LibraryFileID)
		serveTrackFileBytes(w, r, app, tf.LibraryFileID)
	}
}

func serveTrackFileBytes(w http.ResponseWriter, r *http.Request, app *service.App, libraryFileID int64) {
	file, err := app.GetLibraryFile(r.Context(), libraryFileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "library file not found")
		return
	}

	w.Header().Set("Content-Type", contentTypeFromExt(filepath.Ext(file.Path)))
	w.Header().Set("Accept-Ranges", "bytes")

	if vfs.IsSMBPath(file.Path) {
		serveVFSFile(w, r, file.Path)
		return
	}

	f, err := os.Open(file.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open file")
		return
	}
	defer func() { _ = f.Close() }()
	stat, _ := f.Stat()
	http.ServeContent(w, r, file.Path, stat.ModTime(), f)
}
