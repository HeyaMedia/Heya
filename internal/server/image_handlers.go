package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/imageserve"
	"github.com/karbowiak/heya/internal/metadata/studios"
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

		sortOrder := -1
		if s := r.URL.Query().Get("sort"); s != "" {
			sortOrder, _ = strconv.Atoi(s)
		}
		label := r.URL.Query().Get("label")

		path, ok := app.GetMediaImagePath(r.Context(), id, imageType, sortOrder, label)
		if !ok {
			http.NotFound(w, r)
			return
		}
		app.ImageResizer().Serve(w, r, path, imageserve.ParseQuery(r.URL.Query()))
	}
}

func handlePersonImage(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		path, ok := app.GetPersonImagePath(r.Context(), id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		app.ImageResizer().Serve(w, r, path, imageserve.ParseQuery(r.URL.Query()))
	}
}

func handleStudioImage(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		name, ok := app.GetStudioLogoName(r.Context(), id)
		if !ok {
			http.NotFound(w, r)
			return
		}

		resolver := studios.NewResolver(app.ConfigSnapshot().DataDir.Value)
		logoPath := resolver.LogoPath(name)
		if logoPath == "" {
			http.NotFound(w, r)
			return
		}
		// Studio logos are typically SVG/PNG — skip resize for SVG (imaging
		// can't decode them). The resizer's passthrough on no params keeps
		// this transparent.
		app.ImageResizer().Serve(w, r, logoPath, imageserve.ParseQuery(r.URL.Query()))
	}
}
