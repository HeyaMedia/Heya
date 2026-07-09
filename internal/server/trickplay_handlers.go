package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/trickplay"
)

func handleTrickplayVTT(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := app.GetLibraryFileByRef(r.Context(), r.PathValue("file_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		duration, ok := trickplayDuration(file.MediaInfo)
		if !ok {
			writeError(w, http.StatusNotFound, "trickplay not available")
			return
		}
		gridDir := trickplay.GridDir(trickplay.SidecarDir(file.Path))
		vtt, err := trickplay.BuildVTT(duration, func(spriteIdx int) bool {
			_, err := os.Stat(filepath.Join(gridDir, trickplay.SpriteName(spriteIdx)))
			return err == nil
		})
		if err != nil || vtt == "" {
			writeError(w, http.StatusNotFound, "trickplay not available")
			return
		}

		w.Header().Set("Content-Type", "text/vtt")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = io.WriteString(w, vtt)
	}
}

func trickplayDuration(mediaInfo []byte) (float64, bool) {
	var info struct {
		Duration float64 `json:"duration"`
	}
	if len(mediaInfo) == 0 || json.Unmarshal(mediaInfo, &info) != nil || info.Duration <= 0 {
		return 0, false
	}
	return info.Duration, true
}

func handleTrickplaySprite(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		// Reject any path component (a %2f-decoded separator, a leading dir, or
		// "..") so the sprite name can't traverse out of the trickplay dir. The
		// route's `pattern` also blocks this, but this is the load-bearing check
		// since the sprite is served by a raw handler.
		if filepath.Ext(filename) != ".jpg" || filepath.Base(filename) != filename {
			writeError(w, http.StatusBadRequest, "invalid filename")
			return
		}

		file, err := app.GetLibraryFileByRef(r.Context(), r.PathValue("file_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		spritePath := filepath.Join(trickplay.GridDir(trickplay.SidecarDir(file.Path)), filename)
		if _, err := os.Stat(spritePath); err != nil {
			writeError(w, http.StatusNotFound, "sprite not found")
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, spritePath)
	}
}

func handleExtraThumbnail(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		extra, err := app.GetMediaExtra(r.Context(), id)
		if err != nil || extra.ThumbnailPath == "" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, extra.ThumbnailPath)
	}
}
