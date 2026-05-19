package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
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

		f, err := os.Open(file.Path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot open file")
			return
		}
		defer f.Close()

		stat, _ := f.Stat()
		ct := contentTypeFromExt(filepath.Ext(file.Path))
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Accept-Ranges", "bytes")
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

		tInfo := workerToTranscoderInfo(&info)
		plan := transcoder.Decide(&tInfo, transcoder.DefaultClientCaps)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprintf(w, "#EXTM3U\n")
		fmt.Fprintf(w, "#EXT-X-VERSION:6\n")

		profiles := []string{"1080p", "720p"}
		if plan.Action == transcoder.ActionDirectPlay || plan.Action == transcoder.ActionRemux {
			profiles = []string{plan.Profile}
		}

		for _, p := range profiles {
			bw := "8000000"
			res := "1920x1080"
			if p == "720p" {
				bw = "4000000"
				res = "1280x720"
			}
			fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%s,RESOLUTION=%s\n", bw, res)
			fmt.Fprintf(w, "/api/stream/%d/hls/%s/index.m3u8\n", fileID, p)
		}
	}
}

func handleHLSPlaylist(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}
		quality := r.PathValue("quality")

		if app.Transcoder == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		profile, ok := transcoder.GetProfile(quality)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown quality profile")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		session, err := app.Transcoder.GetOrStart(r.Context(), fileID, file.Path, profile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		playlistPath := filepath.Join(session.OutputDir, "index.m3u8")
		if _, err := os.Stat(playlistPath); err != nil {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:0\n")
			return
		}

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		http.ServeFile(w, r, playlistPath)
	}
}

func handleHLSSegment(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}
		quality := r.PathValue("quality")
		segment := r.PathValue("segment")

		if app.TranscodeCache == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		key := fmt.Sprintf("%d:%s", fileID, quality)
		path := app.TranscodeCache.SegmentPath(key, segment)

		if _, err := os.Stat(path); err != nil {
			writeError(w, http.StatusNotFound, "segment not ready")
			return
		}

		w.Header().Set("Content-Type", "video/mp4")
		http.ServeFile(w, r, path)
	}
}

func workerToTranscoderInfo(info *worker.MediaInfo) transcoder.MediaInfo {
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
