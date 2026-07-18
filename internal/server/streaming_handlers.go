package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
)

func handleDirectStream(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := app.GetLibraryFileByRef(r.Context(), r.PathValue("file_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		ct := contentTypeFromExt(filepath.Ext(file.Path))
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Accept-Ranges", "bytes")

		serveLibraryFile(w, r, file.Path)
	}
}

// handleExtraStream range-serves a media extra's video file from library_file_links.
func handleExtraStream(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid extra id")
			return
		}

		extra, err := app.GetMediaExtra(r.Context(), id)
		if err != nil || extra.FilePath == "" {
			writeError(w, http.StatusNotFound, "extra not found")
			return
		}

		ct := contentTypeFromExt(filepath.Ext(extra.FilePath))
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Accept-Ranges", "bytes")

		serveLibraryFile(w, r, extra.FilePath)
	}
}

func handleHLSMaster(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, ok := app.ResolveLibraryFileID(r.Context(), r.PathValue("file_id"))
		if !ok {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		file, err := app.EnsureFileProbed(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info mediaprobe.MediaInfo
		if len(file.MediaInfo) > 0 {
			_ = json.Unmarshal(file.MediaInfo, &info)
		}

		caps := parseClientCaps(r)
		tInfo := mediaProbeToTranscoderInfo(&info)
		audioTrack := 0
		if a := r.URL.Query().Get("audio"); a != "" {
			if v, err := strconv.Atoi(a); err == nil && v >= 0 {
				audioTrack = v
			}
		}
		plan := transcoder.DecideForHLS(&tInfo, audioTrack, caps)

		bw := estimateBandwidth(&info)
		res := extractResolution(&info)

		srcVideo := extractVideoCodec(&info)
		srcAudio := extractAudioCodecAt(&info, audioTrack)
		var videoCodec, audioCodec string
		if plan.CopyVideo {
			videoCodec = srcVideo
		} else {
			videoCodec = "h264"
		}
		if plan.CopyAudio {
			audioCodec = srcAudio
		} else {
			audioCodec = "aac"
		}
		codecStr := transcoder.FormatCodecString(videoCodec, audioCodec)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")

		if _, err := fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:6\n"); err != nil {
			return
		}
		if codecStr != "" {
			if _, err := fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\",RESOLUTION=%s\n", bw, codecStr, res); err != nil {
				return
			}
		} else {
			if _, err := fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bw, res); err != nil {
				return
			}
		}
		_, _ = fmt.Fprintf(w, "%s/index.m3u8%s\n", hlsBasePath(r, file.PublicID.String()), queryPassthrough(r))
	}
}

func handleHLSPlaylist(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, ok := app.ResolveLibraryFileID(r.Context(), r.PathValue("file_id"))
		if !ok {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		if app.TranscoderSessions() == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		file, err := app.GetLibraryFile(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info mediaprobe.MediaInfo
		if len(file.MediaInfo) > 0 {
			_ = json.Unmarshal(file.MediaInfo, &info)
		}

		session := getOrCreateSession(app, r, fileID, info.Duration)
		if session == nil {
			writeError(w, http.StatusInternalServerError, "failed to start transcode")
			return
		}
		session.Touch()

		playlist := transcoder.GenerateDynamicPlaylistWithQuery(session, hlsChildQuery(r))

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(playlist))
	}
}

func handleHLSSegment(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, ok := app.ResolveLibraryFileID(r.Context(), r.PathValue("file_id"))
		if !ok {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		segmentName := r.PathValue("segment")

		if app.TranscoderSessions() == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		audioTrack := 0
		if a := r.URL.Query().Get("audio"); a != "" {
			if v, err := strconv.Atoi(a); err == nil && v >= 0 {
				audioTrack = v
			}
		}
		session := app.TranscoderSessions().GetExistingSession(fileID, audioTrack, r.URL.Query().Get("sid"))
		if session == nil {
			writeError(w, http.StatusNotFound, "no active transcode")
			return
		}

		session.Touch()

		if segmentName == "init.mp4" {
			if !session.IsFMP4() {
				writeError(w, http.StatusNotFound, "not an fMP4 session")
				return
			}
			if !session.HasInitSegment() {
				if err := session.EnsureSegment(r.Context(), 0); err != nil {
					writeHLSSegmentError(w, err)
					return
				}
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			// The session derives this path from Heya's cache root and a hashed
			// session key; no request-controlled path component reaches ServeFile.
			http.ServeFile(w, r, session.InitSegmentPath()) //nolint:gosec
			return
		}

		segIdx := parseSegmentIndex(segmentName)

		if err := session.EnsureSegment(r.Context(), segIdx); err != nil {
			writeHLSSegmentError(w, err)
			return
		}

		segPath := session.SegmentPath(segIdx)
		if session.IsFMP4() {
			w.Header().Set("Content-Type", "video/mp4")
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
		}
		w.Header().Set("Cache-Control", "no-cache")
		// SegmentPath accepts the parsed numeric index and joins it beneath the
		// session's server-created cache directory.
		http.ServeFile(w, r, segPath) //nolint:gosec
	}
}

func writeHLSSegmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The requester is already gone (or its deadline elapsed), so there is
		// no useful response left to write.
		return
	case errors.Is(err, transcoder.ErrInvalidSegment):
		writeError(w, http.StatusNotFound, "segment not found")
	case errors.Is(err, transcoder.ErrTranscodeSessionClosed):
		writeError(w, http.StatusGone, "transcode session closed")
	case errors.Is(err, transcoder.ErrTranscodeFailed):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "segment transcode failed")
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "segment not ready")
	}
}

func getOrCreateSession(app *service.App, r *http.Request, fileID int64, duration float64) *transcoder.TranscodeSession {
	file, err := app.EnsureFileProbed(r.Context(), fileID)
	if err != nil {
		return nil
	}

	var info mediaprobe.MediaInfo
	if len(file.MediaInfo) > 0 {
		_ = json.Unmarshal(file.MediaInfo, &info)
	}

	var kf *transcoder.Keyframes
	if len(file.Keyframes) > 0 {
		var k transcoder.Keyframes
		if err := json.Unmarshal(file.Keyframes, &k); err == nil && len(k.IFrames) > 0 {
			kf = &k
		}
	}

	tInfo := mediaProbeToTranscoderInfo(&info)

	audioTrack := 0
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioTrack = v
		}
	}

	caps := parseClientCaps(r)
	plan := transcoder.DecideForHLS(&tInfo, audioTrack, caps)

	profile, ok := transcoder.GetProfile(plan.Profile)
	if !ok {
		profile = transcoder.Profile{Name: plan.Profile, VideoCodec: "libx264", AudioCodec: "aac", CRF: 22, Preset: "medium"}
	}
	if plan.CopyVideo {
		profile.VideoCodec = "copy"
		profile.CRF = 0
		profile.MaxHeight = 0
	}
	if plan.CopyAudio {
		profile.AudioCodec = "copy"
	}

	if q := r.URL.Query().Get("quality"); q != "" && q != "auto" {
		if qProfile, qOk := transcoder.GetProfile(q); qOk {
			profile = qProfile
			plan.NeedsFMP4 = false
		}
	}

	opts := transcoder.TranscodeOpts{
		Input:      file.Path,
		Profile:    profile,
		HWAccel:    app.TranscoderSessions().HWAccel(),
		AudioTrack: audioTrack,
		ToneMap:    plan.NeedsToneMap,
		UseFMP4:    plan.NeedsFMP4,
		Plan:       &plan,
	}

	if duration <= 0 {
		duration = info.Duration
	}
	if duration <= 0 {
		duration = 1
	}

	// Never make playback wait for a full-file scan. Missing or legacy
	// artifacts use the in-memory heuristic for this session while a shared
	// background pass persists keyframes and exact boundaries for the next.
	if opts.Profile.VideoCodec == "copy" && !transcoder.HasExactHLSBoundaries(kf) {
		app.EnsureKeyframesAnalyzed(file.ID)
	}

	sessionID := r.URL.Query().Get("sid")
	return app.TranscoderSessions().GetOrCreate(r.Context(), fileID, file.Path, opts, sessionID, duration, kf)
}

func parseSegmentIndex(name string) int {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(name, "_")
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n
		}
	}
	return -1
}

func queryPassthrough(r *http.Request) string {
	q := r.URL.Query()
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

func hlsBasePath(r *http.Request, fileRef string) string {
	if strings.HasPrefix(r.URL.Path, "/api/cast/media/video/") {
		return "/api/cast/media/video/" + fileRef + "/hls"
	}
	if strings.HasPrefix(r.URL.Path, "/api/playback/native/media/") {
		return "/api/playback/native/media/" + fileRef + "/hls"
	}
	return "/api/stream/" + fileRef + "/hls"
}

// hlsChildQuery keeps only authentication and exact transcode-session routing
// on segment URLs. Capability/quality flags have already been consumed while
// creating the variant session and need not be repeated dozens of times.
func hlsChildQuery(r *http.Request) string {
	in := r.URL.Query()
	out := url.Values{}
	for _, key := range []string{"token", "cast_token", "sid", "audio"} {
		if value := in.Get(key); value != "" {
			out.Set(key, value)
		}
	}
	return out.Encode()
}

func mediaProbeToTranscoderInfo(info *mediaprobe.MediaInfo) transcoder.MediaInfo {
	var streams []transcoder.StreamInfo
	for _, s := range info.Streams {
		dvProfile, dvCompat, rotation := deriveSideDataFields(s.SideDataList)
		streams = append(streams, transcoder.StreamInfo{
			CodecName:         s.CodecName,
			CodecType:         s.CodecType,
			Profile:           s.Profile,
			PixFmt:            s.PixFmt,
			Width:             s.Width,
			Height:            s.Height,
			ColorTransfer:     s.ColorTransfer,
			ColorPrimaries:    s.ColorPrimaries,
			ColorSpace:        s.ColorSpace,
			CodecTag:          s.CodecTagString,
			BitDepth:          deriveBitDepth(s.BitsPerRawSample, s.PixFmt),
			SampleAspectRatio: s.SampleAspectRatio,
			FieldOrder:        s.FieldOrder,
			Rotation:          rotation,
			DvProfile:         dvProfile,
			DvBlCompatID:      dvCompat,
			Channels:          s.Channels,
			ChannelLayout:     s.ChannelLayout,
		})
	}
	return transcoder.MediaInfo{
		Container: info.Container,
		Streams:   streams,
	}
}

// deriveSideDataFields walks ffprobe side_data_list entries and pulls out
// DV profile/compat and rotation. ffprobe reports Display Matrix rotation as
// a signed value where -90 means 90° clockwise; we normalise to 0/90/180/270
// (positive CW) so downstream consumers don't have to worry about sign.
func deriveSideDataFields(side []mediaprobe.SideData) (dvProfile, dvCompat, rotation int) {
	for _, sd := range side {
		switch sd.Type {
		case "DOVI configuration record", "Dolby Vision configuration record", "Dolby Vision Configuration":
			if sd.DvProfile > 0 {
				dvProfile = sd.DvProfile
				dvCompat = sd.DvBlSignalCompatibilityID
			}
		case "Display Matrix":
			rotation = normalizeRotation(sd.Rotation)
		}
	}
	return
}

func normalizeRotation(raw int) int {
	// ffprobe Display Matrix rotation: signed value, negative = CW.
	// Map to canonical 0/90/180/270 CW.
	r := -raw % 360
	if r < 0 {
		r += 360
	}
	switch r {
	case 0, 90, 180, 270:
		return r
	}
	return 0
}

// deriveBitDepth returns the sample bit depth from ffprobe's
// bits_per_raw_sample (preferred) or from the pix_fmt string as a fallback.
func deriveBitDepth(bitsStr, pixFmt string) int {
	if bitsStr != "" {
		if n, err := strconv.Atoi(bitsStr); err == nil && n > 0 {
			return n
		}
	}
	pix := strings.ToLower(pixFmt)
	switch {
	case strings.Contains(pix, "12le"), strings.Contains(pix, "12be"):
		return 12
	case strings.Contains(pix, "10le"), strings.Contains(pix, "10be"):
		return 10
	case pix == "":
		return 0
	default:
		return 8
	}
}

func extractVideoCodec(info *mediaprobe.MediaInfo) string {
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			return s.CodecName
		}
	}
	return ""
}

// extractAudioCodecAt returns the codec of the Nth audio stream (0-indexed
// across audio streams only, not file stream indices). Falls back to the first
// audio codec if the requested index is out of range.
func extractAudioCodecAt(info *mediaprobe.MediaInfo, audioIdx int) string {
	if audioIdx < 0 {
		audioIdx = 0
	}
	n := 0
	first := ""
	for _, s := range info.Streams {
		if s.CodecType != "audio" {
			continue
		}
		if first == "" {
			first = s.CodecName
		}
		if n == audioIdx {
			return s.CodecName
		}
		n++
	}
	return first
}

func extractResolution(info *mediaprobe.MediaInfo) string {
	for _, s := range info.Streams {
		if s.CodecType == "video" && s.Width > 0 && s.Height > 0 {
			return fmt.Sprintf("%dx%d", s.Width, s.Height)
		}
	}
	return "1920x1080"
}

func estimateBandwidth(info *mediaprobe.MediaInfo) int64 {
	if info.BitRate > 0 {
		return info.BitRate
	}
	return 8_000_000
}

func contentTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".flac":
		return "audio/flac"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/ogg; codecs=opus"
	case ".wav":
		return "audio/wav"
	case ".wma":
		return "audio/x-ms-wma"
	case ".alac":
		return "audio/mp4; codecs=alac"
	case ".epub":
		return "application/epub+zip"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func serveLibraryFile(w http.ResponseWriter, r *http.Request, path string) {
	file, err := vfs.OpenFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open file")
		return
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stat file")
		return
	}

	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}
