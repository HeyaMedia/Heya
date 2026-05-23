package server

import (
	"encoding/json"
	"net/http"

	"github.com/karbowiak/heya/internal/service"
)

func handleGetSystemSetting(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "" {
			writeError(w, http.StatusBadRequest, "key required")
			return
		}

		val, err := app.GetSystemSetting(r.Context(), key)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": nil})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": val})
	}
}

func handleUpdateSystemSetting(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "" {
			writeError(w, http.StatusBadRequest, "key required")
			return
		}

		var body struct {
			Value json.RawMessage `json:"value"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.SetSystemSetting(r.Context(), key, body.Value); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}
}
