package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/worker"
)

type subtitleTrack struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
	Codec    string `json:"codec"`
	Title    string `json:"title"`
}

func handleListSubtitles(app *service.App) http.HandlerFunc {
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

		var tracks []subtitleTrack
		for _, s := range info.Streams {
			if s.CodecType != "subtitle" {
				continue
			}
			lang := s.Tags["language"]
			title := s.Tags["title"]
			tracks = append(tracks, subtitleTrack{
				Index:    s.Index,
				Language: lang,
				Codec:    s.CodecName,
				Title:    title,
			})
		}

		writeJSON(w, http.StatusOK, tracks)
	}
}

func handleGetSubtitle(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stream index")
			return
		}

		if app.TranscodeCache == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		cacheKey := fmt.Sprintf("sub_%d_%d", fileID, index)
		subPath := filepath.Join(app.TranscodeCache.SegmentDir(cacheKey), "subtitle.vtt")

		if _, err := os.Stat(subPath); err != nil {
			if err := transcoder.ExtractSubtitles(r.Context(), file.Path, index, subPath); err != nil {
				writeError(w, http.StatusInternalServerError, "subtitle extraction failed")
				return
			}
		}

		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		http.ServeFile(w, r, subPath)
	}
}
