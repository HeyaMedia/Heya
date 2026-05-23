package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
)

func handleTrickplayVTT(app *service.App) http.HandlerFunc {
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

		vttPath := filepath.Join(filepath.Dir(file.Path), "trickplay", "index.vtt")
		if _, err := os.Stat(vttPath); err != nil {
			writeError(w, http.StatusNotFound, "trickplay not available")
			return
		}

		w.Header().Set("Content-Type", "text/vtt")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, vttPath)
	}
}

func handleTrickplaySprite(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		filename := r.PathValue("filename")
		if filepath.Ext(filename) != ".jpg" {
			writeError(w, http.StatusBadRequest, "invalid filename")
			return
		}

		file, err := app.GetLibraryFile(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		spritePath := filepath.Join(filepath.Dir(file.Path), "trickplay", filename)
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
