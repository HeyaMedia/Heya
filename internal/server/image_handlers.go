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

		sortOrder := -1
		if s := r.URL.Query().Get("sort"); s != "" {
			sortOrder, _ = strconv.Atoi(s)
		}

		if sortOrder >= 0 {
			assets, err := q.ListMediaAssets(r.Context(), id)
			if err == nil {
				for _, a := range assets {
					if string(a.AssetType) == imageType && int(a.SortOrder) == sortOrder && a.LocalPath != "" {
						serveFile(w, r, a.LocalPath)
						return
					}
				}
			}
		}

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
		for _, a := range assets {
			if string(a.AssetType) == imageType && a.LocalPath != "" {
				serveFile(w, r, a.LocalPath)
				return
			}
		}

		http.NotFound(w, r)
	}
}

func handlePersonImage(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		q := sqlc.New(app.DB)
		person, err := q.GetPersonByID(r.Context(), id)
		if err != nil || person.ProfilePath == "" || strings.HasPrefix(person.ProfilePath, "http") {
			http.NotFound(w, r)
			return
		}

		serveFile(w, r, person.ProfilePath)
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
