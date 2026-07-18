package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/publichttp"
)

// handleTMDBImageProxy serves images from image.tmdb.org through Heya so
// browsers don't talk to TMDB directly. Used for recommendation posters where
// we only have the upstream TMDB poster_path (no local asset yet).
//
// Path:  /api/tmdb/image/{path...}              (path is the TMDB poster path)
// Query: ?size=w92|w154|w185|w342|w500|w780|original   (default w342)
func handleTMDBImageProxy(fetcher *publichttp.Fetcher) http.HandlerFunc {
	allowedSizes := map[string]bool{
		"w92": true, "w154": true, "w185": true, "w342": true,
		"w500": true, "w780": true, "original": true,
	}
	const upstream = "https://image.tmdb.org/t/p/"

	return func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		// TMDB paths look like abcDEF.jpg — reject anything that escapes.
		if strings.Contains(path, "..") {
			http.NotFound(w, r)
			return
		}
		path = "/" + strings.TrimPrefix(path, "/")

		size := r.URL.Query().Get("size")
		if size == "" {
			size = "w342"
		}
		if !allowedSizes[size] {
			http.Error(w, "invalid size", http.StatusBadRequest)
			return
		}

		target := upstream + size + path

		var image *publichttp.Image
		var err error
		if fetcher != nil {
			image, err = fetcher.FetchImage(r.Context(), target, publichttp.MaxImageBytes)
		} else {
			image, err = publichttp.FetchImage(r.Context(), target)
		}
		if err != nil {
			var statusErr *publichttp.StatusError
			if errors.As(err, &statusErr) && statusErr.Code >= 400 && statusErr.Code <= 599 {
				http.Error(w, "upstream "+http.StatusText(statusErr.Code), statusErr.Code)
			} else {
				http.Error(w, "upstream image unavailable", http.StatusBadGateway)
			}
			return
		}

		publichttp.ServeImage(w, r, image, "public, max-age=604800, immutable")
	}
}
