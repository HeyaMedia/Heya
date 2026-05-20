package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
)

type createLibraryRequest struct {
	Name      string                    `json:"name"`
	MediaType string                    `json:"media_type"`
	Paths     []string                  `json:"paths"`
	Settings  *metadata.LibrarySettings `json:"settings,omitempty"`
}

type libraryView struct {
	ID        int64                    `json:"id"`
	Name      string                   `json:"name"`
	MediaType string                   `json:"media_type"`
	Paths     []string                 `json:"paths"`
	CreatedBy int64                    `json:"created_by"`
	Settings  metadata.LibrarySettings `json:"settings"`
}

func handleCreateLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req createLibraryRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Name == "" || req.MediaType == "" || len(req.Paths) == 0 {
			writeError(w, http.StatusBadRequest, "name, media_type, and paths are required")
			return
		}

		mt, err := service.ParseMediaType(req.MediaType)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		lib, err := app.CreateLibrary(r.Context(), req.Name, mt, req.Paths, user.ID, req.Settings)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, toLibraryView(lib))
	}
}

func handleListLibraries(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		libs, err := app.ListLibraries(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list libraries")
			return
		}

		views := make([]libraryView, len(libs))
		for i, lib := range libs {
			views[i] = toLibraryView(lib)
		}

		writeJSON(w, http.StatusOK, views)
	}
}

func handleGetLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library ID")
			return
		}

		lib, err := app.GetLibrary(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "library not found")
			return
		}

		writeJSON(w, http.StatusOK, toLibraryView(lib))
	}
}

func handleUpdateLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library ID")
			return
		}

		var req createLibraryRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		lib, err := app.UpdateLibrary(r.Context(), id, req.Name, req.Paths)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, toLibraryView(lib))
	}
}

func handleDeleteLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library ID")
			return
		}

		if err := app.DeleteLibrary(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete library")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func handleUpdateLibrarySettings(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library ID")
			return
		}

		var settings metadata.LibrarySettings
		if err := readJSON(r, &settings); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		lib, err := app.UpdateLibrarySettings(r.Context(), id, settings)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, toLibraryView(lib))
	}
}

func handleListProviders(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infos := app.Registry.Available()
		writeJSON(w, http.StatusOK, infos)
	}
}

func toLibraryView(lib sqlc.Library) libraryView {
	settings := metadata.ParseSettings(lib.Settings)
	return libraryView{
		ID:        lib.ID,
		Name:      lib.Name,
		MediaType: string(lib.MediaType),
		Paths:     lib.Paths,
		CreatedBy: lib.CreatedBy,
		Settings:  settings,
	}
}

func handleGetLibrarySettings(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library ID")
			return
		}

		settings, err := app.GetLibrarySettings(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "library not found")
			return
		}

		defaults := metadata.DefaultSettings(r.URL.Query().Get("type"))
		writeJSON(w, http.StatusOK, map[string]any{
			"settings": settings,
			"defaults": defaults,
		})
	}
}
