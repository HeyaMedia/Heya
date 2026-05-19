package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/karbowiak/heya/internal/service"
)

func NewHumaAPI(mux *http.ServeMux, app *service.App) huma.API {
	config := huma.DefaultConfig("Heya Media Server API", "1.0.0")
	config.Info.Description = "Self-hosted media server for movies, TV, music, and books. " +
		"Supports TMDB, MusicBrainz, and OpenLibrary metadata providers."
	config.Info.Contact = &huma.Contact{
		Name: "Kura",
		URL:  "https://github.com/karbowiak/heya",
	}

	config.DocsPath = "/api/docs"
	config.OpenAPIPath = "/api/openapi"

	api := humago.New(mux, config)

	registerHumaRoutes(api, app)

	return api
}
