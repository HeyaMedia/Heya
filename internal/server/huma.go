package server

import (
	"fmt"
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
		Name: "Heya",
		URL:  "https://heya.media",
	}

	config.DocsPath = ""
	config.OpenAPIPath = "/api/openapi"

	api := humago.New(mux, config)

	registerHumaRoutes(api, app)

	return api
}

const scalarHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Heya API Reference</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <style>
    body { margin: 0; padding: 0; height: 100vh; }
  </style>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@latest/dist/browser/standalone.js"></script>
  <script>
    Scalar.createApiReference('#app', {
      url: '%s',
      theme: 'kepler',
      darkMode: true,
      hideModels: false,
      hideDownloadButton: false,
      authentication: {
        preferredSecurityScheme: 'bearer',
        http: {
          bearer: { token: '' }
        }
      }
    })
  </script>
</body>
</html>`

func scalarHandler(specURL string) http.HandlerFunc {
	page := fmt.Sprintf(scalarHTML, specURL)

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}
}
