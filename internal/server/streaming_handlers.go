package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
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

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
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

func handleHLSMaster(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
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
		_ = transcoder.Decide(&tInfo, caps)

		bw := estimateBandwidth(&info)
		res := extractResolution(&info)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")

		fmt.Fprintf(w, "#EXTM3U\n")
		fmt.Fprintf(w, "#EXT-X-VERSION:6\n")
		fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bw, res)
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

		if app.Transcoder == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		session := getOrCreateSession(app, r, fileID)
		if session == nil {
			writeError(w, http.StatusInternalServerError, "failed to start transcode")
			return
		}
		session.Touch()

		tok := r.URL.Query().Get("token")
		playlist := transcoder.GeneratePlaylist(info.Duration, "seg_%04d.ts", tok)

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

		if app.Transcoder == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		session := app.Transcoder.GetExisting(fileID)
		if session == nil {
			session = getOrCreateSession(app, r, fileID)
			if session == nil {
				writeError(w, http.StatusNotFound, "no active transcode")
				return
			}
		}
		session.Touch()
		session.Resume()

		segIdx := parseSegmentIndex(segmentName)

		if !session.WaitForSegment(r.Context(), segIdx) {
			writeError(w, http.StatusNotFound, "segment not available")
			return
		}

		segPath := session.SegmentPath(segIdx)
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, segPath)
	}
}

func getOrCreateSession(app *service.App, r *http.Request, fileID int64) *transcoder.TranscodeSession {
	q := sqlc.New(app.DB)
	file, err := q.GetLibraryFileByID(r.Context(), fileID)
	if err != nil {
		return nil
	}

	var info worker.MediaInfo
	if len(file.MediaInfo) > 0 {
		json.Unmarshal(file.MediaInfo, &info)
	}

	tInfo := workerToTranscoderInfo(&info)
	plan := transcoder.Decide(&tInfo, transcoder.DefaultClientCaps)

	profile, ok := transcoder.GetProfile(plan.Profile)
	if !ok {
		profile = transcoder.Profile{Name: plan.Profile, VideoCodec: "libx264", AudioCodec: "aac", CRF: 22, Preset: "medium"}
	}
	if plan.CopyVideo {
		profile.VideoCodec = "copy"
	}
	if plan.CopyAudio {
		profile.AudioCodec = "copy"
	}

	isPiped := vfs.IsSMBPath(file.Path)

	startTime := 0.0
	if !isPiped {
		if s := r.URL.Query().Get("start"); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
				startTime = v
			}
		}
	}

	audioTrack := 0
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioTrack = v
		}
	}

	input := file.Path
	if vfs.IsSMBPath(file.Path) {
		input = "pipe:0"
	}

	opts := transcoder.TranscodeOpts{
		Input:      input,
		Profile:    profile,
		HWAccel:    app.Transcoder.HWAccel(),
		StartTime:  startTime,
		AudioTrack: audioTrack,
	}

	sessionID := r.URL.Query().Get("sid")
	return app.Transcoder.GetOrCreate(fileID, file.Path, opts, sessionID)
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

func waitMs(ms int) <-chan time.Time {
	return time.After(time.Duration(ms) * time.Millisecond)
}

func workerToTranscoderInfo(info *worker.MediaInfo) transcoder.MediaInfo {
	var streams []transcoder.StreamInfo
	for _, s := range info.Streams {
		streams = append(streams, transcoder.StreamInfo{
			CodecName: s.CodecName,
			CodecType: s.CodecType,
			Width:     s.Width,
			Height:    s.Height,
		})
	}
	return transcoder.MediaInfo{
		Container: info.Container,
		Streams:   streams,
	}
}

func extractCodecs(info *worker.MediaInfo) (video, audio string) {
	for _, s := range info.Streams {
		if s.CodecType == "video" && video == "" {
			video = s.CodecName
		}
		if s.CodecType == "audio" && audio == "" {
			audio = s.CodecName
		}
	}
	return
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
