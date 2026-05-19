package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

func handleMediaImage(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		imageType := r.PathValue("type")
		q := sqlc.New(app.DB)

		if imageType == "poster" || imageType == "backdrop" {
			item, err := q.GetMediaItemByID(r.Context(), id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			var imgPath string
			if imageType == "poster" {
				imgPath = item.PosterPath
			} else {
				imgPath = item.BackdropPath
			}
			if imgPath == "" || strings.HasPrefix(imgPath, "http") {
				http.NotFound(w, r)
				return
			}
			serveFile(w, r, imgPath)
			return
		}

		assets, err := q.ListMediaAssets(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		sortOrder := 0
		if s := r.URL.Query().Get("sort"); s != "" {
			sortOrder, _ = strconv.Atoi(s)
		}

		for _, a := range assets {
			if string(a.AssetType) == imageType && int(a.SortOrder) == sortOrder {
				if a.LocalPath != "" {
					serveFile(w, r, a.LocalPath)
					return
				}
			}
		}

		http.NotFound(w, r)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), f)
}
