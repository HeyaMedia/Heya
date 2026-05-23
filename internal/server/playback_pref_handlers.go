package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

type playbackPrefView struct {
	MediaItemID      int64  `json:"media_item_id"`
	AudioLanguage    string `json:"audio_language"`
	SubtitleLanguage string `json:"subtitle_language"`
	SubtitleMode     string `json:"subtitle_mode"`
}

type playbackPrefRequest struct {
	AudioLanguage    string `json:"audio_language"`
	SubtitleLanguage string `json:"subtitle_language"`
	SubtitleMode     string `json:"subtitle_mode"`
}

func handleGetPlaybackPreference(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("media_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		pref, found, err := app.GetPlaybackPreference(r.Context(), user.ID, mediaItemID)
		if err != nil || !found {
			writeJSON(w, http.StatusOK, playbackPrefView{MediaItemID: mediaItemID})
			return
		}

		writeJSON(w, http.StatusOK, playbackPrefView{
			MediaItemID:      pref.MediaItemID,
			AudioLanguage:    pref.AudioLanguage,
			SubtitleLanguage: pref.SubtitleLanguage,
			SubtitleMode:     pref.SubtitleMode,
		})
	}
}

func handleSetPlaybackPreference(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("media_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		var req playbackPrefRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.AudioLanguage == "" && req.SubtitleLanguage == "" && req.SubtitleMode == "" {
			app.DeletePlaybackPreference(r.Context(), user.ID, mediaItemID)
			writeJSON(w, http.StatusOK, playbackPrefView{MediaItemID: mediaItemID})
			return
		}

		pref, err := app.SetPlaybackPreference(r.Context(), user.ID, mediaItemID, req.AudioLanguage, req.SubtitleLanguage, req.SubtitleMode)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save preference")
			return
		}

		writeJSON(w, http.StatusOK, playbackPrefView{
			MediaItemID:      pref.MediaItemID,
			AudioLanguage:    pref.AudioLanguage,
			SubtitleLanguage: pref.SubtitleLanguage,
			SubtitleMode:     pref.SubtitleMode,
		})
	}
}

func handleGetMediaLanguages(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		langs, _ := app.GetMediaLanguages(r.Context(), mediaID)
		writeJSON(w, http.StatusOK, langs)
	}
}
