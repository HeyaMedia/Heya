package jellyfin

import (
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"
)

// Favorites + played toggles. Both respond with the updated UserItemDataDto,
// which clients apply to their cached item without refetching.

// favoriteEntityType maps an id kind onto Heya's user_favorites entity_type.
// Video-level items and music artists both live on media_items, so they
// share "media_item" — the same rows Heya's own favorite toggles write.
func favoriteEntityType(kind Kind) (string, bool) {
	switch kind {
	case KindItem:
		return "media_item", true
	case KindTrack:
		return "track", true
	case KindAlbum:
		return "album", true
	case KindSeason:
		return "season", true
	case KindEpisode:
		return "episode", true
	case KindPerson:
		return "person", true
	default:
		return "", false
	}
}

// POST|DELETE /UserFavoriteItems/{itemId}
func (s *Server) handleSetFavorite(loved bool) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		u, _ := UserFrom(r.Context())
		kind, id, err := DecodeID(p["itemId"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		entityType, ok := favoriteEntityType(kind)
		if !ok {
			http.NotFound(w, r)
			return
		}
		ctx := r.Context()
		// Music favorites ARE hearts: they write the unified rating store
		// (heart = 10, unfavorite clears), the same signal the web app's
		// reactions and Subsonic stars feed. Jellyfin addresses music artists
		// as media_items, so those resolve to the artists row first. Video
		// keeps boolean user_favorites.
		rating := int16(0)
		if loved {
			rating = 10
		}
		switch {
		case kind == KindTrack:
			err = s.app.SetUserTrackRating(ctx, u.ID, id, rating)
		case kind == KindAlbum:
			err = s.app.SetUserAlbumRating(ctx, u.ID, id, rating)
		case kind == KindItem:
			if artistID, isArtist := s.app.ArtistIDForMediaItem(ctx, id); isArtist {
				err = s.app.SetUserArtistRating(ctx, u.ID, artistID, rating)
			} else {
				_, err = s.app.SetEntityLoved(ctx, u.ID, entityType, id, loved)
			}
		default:
			_, err = s.app.SetEntityLoved(ctx, u.ID, entityType, id, loved)
		}
		if err != nil {
			log.Warn().Err(err).Str("component", "jellyfin").Msg("favorite toggle failed")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, userDataDto{
			IsFavorite: loved,
			Key:        strconv.FormatInt(id, 10),
		})
	}
}

// POST|DELETE /UserPlayedItems/{itemId}
func (s *Server) handleSetPlayed(played bool) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		ctx := r.Context()
		u, _ := UserFrom(r.Context())
		kind, id, err := DecodeID(p["itemId"])
		if err != nil {
			http.NotFound(w, r)
			return
		}

		switch kind {
		case KindItem:
			// MarkMediaWatched dispatches movie vs series internally.
			err = s.app.MarkMediaWatched(ctx, u.ID, id, played)
		case KindSeason:
			if played {
				err = s.app.MarkSeasonWatched(ctx, u.ID, id)
			} else {
				err = s.app.UnmarkSeasonWatched(ctx, u.ID, id)
			}
		case KindEpisode:
			if played {
				err = s.app.MarkEpisodeWatched(ctx, u.ID, id)
			} else {
				err = s.app.UnmarkEpisodeWatched(ctx, u.ID, id)
			}
		case KindTrack, KindAlbum:
			// Music has scrobble history, not played flags; acknowledging
			// keeps clients' optimistic UI consistent.
			err = nil
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			log.Warn().Err(err).Str("component", "jellyfin").Msg("played toggle failed")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ud := userDataDto{
			Played: played,
			Key:    strconv.FormatInt(id, 10),
		}
		if played {
			ud.PlayCount = 1
		}
		writeJSON(w, http.StatusOK, ud)
	}
}
