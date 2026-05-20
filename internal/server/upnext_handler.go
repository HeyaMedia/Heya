package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

		q := sqlc.New(app.DB)
		ep, err := q.GetNextUnwatchedEpisode(r.Context(), sqlc.GetNextUnwatchedEpisodeParams{
			UserID:      user.ID,
			MediaItemID: mediaItemID,
		})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"has_next": false})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"has_next":       true,
			"episode_id":     ep.EpisodeID,
			"episode_number": ep.EpisodeNumber,
			"episode_title":  ep.Title,
			"season_number":  ep.SeasonNumber,
			"season_id":      ep.SeasonID,
			"media_item_id":  ep.MediaItemID,
			"runtime":        ep.RuntimeMinutes,
		})
	}
}
