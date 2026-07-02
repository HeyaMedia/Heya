package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
)

func handleDirectStream(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		file, err := app.GetLibraryFile(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		ct := contentTypeFromExt(filepath.Ext(file.Path))
		w.Header().Set("Content-Type", ct)
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
		defer f.Close()

		stat, _ := f.Stat()
		http.ServeContent(w, r, file.Path, stat.ModTime(), f)
	}
}

// handleExtraStream range-serves a media extra's video file (trailer,
// featurette, …). Same shape as handleDirectStream but resolved through
// media_extras — extras aren't library_files, they carry their own absolute
// file_path.
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

		if vfs.IsSMBPath(extra.FilePath) {
			serveVFSFile(w, r, extra.FilePath)
			return
		}

		f, err := os.Open(extra.FilePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot open file")
			return
		}
		defer func() { _ = f.Close() }()

		stat, _ := f.Stat()
		http.ServeContent(w, r, extra.FilePath, stat.ModTime(), f)
	}
}

func handleHLSMaster(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		file, err := app.EnsureFileProbed(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		caps := parseClientCaps(r)
		tInfo := workerToTranscoderInfo(&info)
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

		fmt.Fprintf(w, "#EXTM3U\n")
		fmt.Fprintf(w, "#EXT-X-VERSION:6\n")
		if codecStr != "" {
			fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\",RESOLUTION=%s\n", bw, codecStr, res)
		} else {
			fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bw, res)
		}
		fmt.Fprintf(w, "/api/stream/%d/hls/index.m3u8%s\n", fileID, queryPassthrough(r))
	}
}

func handleHLSPlaylist(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
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

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		session := getOrCreateSession(app, r, fileID, info.Duration)
		if session == nil {
			writeError(w, http.StatusInternalServerError, "failed to start transcode")
			return
		}
		session.Touch()

		tok := r.URL.Query().Get("token")
		playlist := transcoder.GenerateDynamicPlaylist(session, tok)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(playlist))
	}
}

func handleHLSSegment(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}
		segmentName := r.PathValue("segment")

		if app.TranscoderSessions() == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		session := app.TranscoderSessions().GetExisting(fileID)
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
				if !session.RequestSegment(r.Context(), 0) {
					writeError(w, http.StatusServiceUnavailable, "init segment not ready")
					return
				}
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			http.ServeFile(w, r, session.InitSegmentPath())
			return
		}

		segIdx := parseSegmentIndex(segmentName)

		if !session.RequestSegment(r.Context(), segIdx) {
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusServiceUnavailable, "segment not ready")
			return
		}

		segPath := session.SegmentPath(segIdx)
		if session.IsFMP4() {
			w.Header().Set("Content-Type", "video/mp4")
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, segPath)
	}
}

func getOrCreateSession(app *service.App, r *http.Request, fileID int64, duration float64) *transcoder.TranscodeSession {
	file, err := app.EnsureFileProbed(r.Context(), fileID)
	if err != nil {
		return nil
	}

	var info worker.MediaInfo
	if len(file.MediaInfo) > 0 {
		json.Unmarshal(file.MediaInfo, &info)
	}

	var kf *transcoder.Keyframes
	if len(file.Keyframes) > 0 {
		var k transcoder.Keyframes
		if err := json.Unmarshal(file.Keyframes, &k); err == nil && len(k.IFrames) > 0 {
			kf = &k
		}
	}

	tInfo := workerToTranscoderInfo(&info)

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

	input := file.Path
	if vfs.IsSMBPath(file.Path) {
		input = "pipe:0"
	}

	opts := transcoder.TranscodeOpts{
		Input:      input,
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

	// Best-effort live keyframe extraction for fMP4 copy-video when scan-time
	// keyframes are missing (typically SMB inputs, or freshly added files).
	if kf == nil && opts.UseFMP4 && opts.Profile.VideoCodec == "copy" && input != "pipe:0" {
		extractCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		k, err := transcoder.ExtractKeyframes(extractCtx, input)
		cancel()
		if err == nil && len(k.IFrames) > 0 {
			kf = k
		}
	}

	sessionID := r.URL.Query().Get("sid")
	return app.TranscoderSessions().GetOrCreate(fileID, file.Path, opts, sessionID, duration, kf)
}

func parseSegmentIndex(name string) int {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(name, "_")
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return n
		}
	}
	return 0
}

func queryPassthrough(r *http.Request) string {
	q := r.URL.Query()
	q.Del("token")
	if len(q) == 0 {
		return "?token=" + r.URL.Query().Get("token")
	}
	return "?" + q.Encode() + "&token=" + r.URL.Query().Get("token")
}

func workerToTranscoderInfo(info *worker.MediaInfo) transcoder.MediaInfo {
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
func deriveSideDataFields(side []worker.SideData) (dvProfile, dvCompat, rotation int) {
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

func extractVideoCodec(info *worker.MediaInfo) string {
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
func extractAudioCodecAt(info *worker.MediaInfo, audioIdx int) string {
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

func extractResolution(info *worker.MediaInfo) string {
	for _, s := range info.Streams {
		if s.CodecType == "video" && s.Width > 0 && s.Height > 0 {
			return fmt.Sprintf("%dx%d", s.Width, s.Height)
		}
	}
	return "1920x1080"
}

func extractSourceHeight(info *worker.MediaInfo) int {
	for _, s := range info.Streams {
		if s.CodecType == "video" && s.Height > 0 {
			return s.Height
		}
	}
	return 1080
}

func estimateBandwidth(info *worker.MediaInfo) int64 {
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

func serveVFSFile(w http.ResponseWriter, r *http.Request, smbPath string) {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		writeError(w, http.StatusInternalServerError, "invalid path")
		return
	}
	dirPath := smbPath[:lastSlash]
	fileName := smbPath[lastSlash+1:]

	source, err := vfs.Open(dirPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open remote path")
		return
	}
	defer source.Close()

	f, err := source.FS.Open(fileName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot open remote file")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stat remote file")
		return
	}

	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, fileName, stat.ModTime(), rs)
	} else {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
		io.Copy(w, f)
	}
}
