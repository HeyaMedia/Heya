package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

func handleGetUpNext(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		result, _ := app.GetUpNext(r.Context(), user.ID, mediaItemID)
		if !result.HasNext {
			writeJSON(w, http.StatusOK, map[string]any{"has_next": false})
			return
		}

		resp := map[string]any{
			"has_next":       true,
			"episode_id":     result.EpisodeID,
			"episode_number": result.EpisodeNumber,
			"episode_title":  result.EpisodeTitle,
			"season_number":  result.SeasonNumber,
			"season_id":      result.SeasonID,
			"media_item_id":  result.MediaItemID,
			"runtime":        result.Runtime,
		}
		if result.FileID > 0 {
			resp["file_id"] = result.FileID
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
