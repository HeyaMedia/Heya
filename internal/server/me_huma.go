package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// semanticSearchResult carries the NL-search hits plus whether the ML engine was
// actually available (false → the FE prompts the user to enable it).
type semanticSearchResult struct {
	Items   []service.ForYouItem `json:"items"`
	MLReady bool                 `json:"ml_ready"`
}

// registerMeRoutes mounts the per-user state surface under /api/me/*. This
// covers everything that used to be scattered across /api/watch, /api/favorites,
// /api/lists, /api/user, plus the watched-marking endpoints under /api/me/watched.
//
// /api/me/loved + /api/me/playlists live in music_huma.go (also consolidated).
func registerMeRoutes(api huma.API, app *service.App) {
	// --- Unified playback emission ---
	// One endpoint for video AND music. The server dispatches based on
	// entity_type — movies/episodes upsert into user_watch_progress (resume
	// state) and tracks append to play_events (history log). The two stores
	// stay separate because their semantics genuinely differ (current state
	// vs. event log); only the wire shape is unified.
	huma.Register(api, secured(op(http.MethodPost, "/api/me/playback", "record-playback", "Record a playback event (video progress / music scrobble)", "Me")),
		func(ctx context.Context, in *struct {
			Body service.PlaybackEvent
		}) (*JSONOutput[okBody], error) {
			if err := app.RecordPlayback(ctx, userFrom(ctx).ID, in.Body); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/watch/continue", "continue-watching", "Items the user can resume", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ContinueWatchingEnrichedRow], error) {
			items, err := app.ListContinueWatching(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(items), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/watch/recent", "recently-watched", "Recently-watched activity", "Me")),
		func(ctx context.Context, in *struct {
			Limit  int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
			Offset int32 `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[[]sqlc.ListRecentlyWatchedRow], error) {
			items, err := app.ListRecentlyWatched(ctx, userFrom(ctx).ID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(items), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/watch/recent-episodes", "recently-watched-episodes", "Recently-watched TV episodes (one row per episode)", "Me")),
		func(ctx context.Context, in *struct {
			Limit  int32 `query:"limit" minimum:"1" maximum:"100" default:"24"`
			Offset int32 `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[[]sqlc.ListRecentlyWatchedEpisodesRow], error) {
			items, err := app.ListRecentlyWatchedEpisodes(ctx, userFrom(ctx).ID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(items), nil
		})

	// Personalized discovery rails for a section's Recommended landing page.
	// The server owns the ordering and which rails exist (watch-history genre /
	// actor affinity, top-unwatched, rediscover, local TMDB recs); the FE
	// composes the activity rows (continue / up-next / recently added) itself.
	huma.Register(api, secured(op(http.MethodGet, "/api/me/recommended/{section}", "recommended-rails", "Personalized discovery rails for a section", "Me")),
		func(ctx context.Context, in *struct {
			Section string `path:"section" enum:"movie,tv" doc:"Section to build rails for"`
		}) (*JSONOutput[service.RecommendedResult], error) {
			result, err := app.Recommended(ctx, userFrom(ctx).ID, in.Section)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(result), nil
		})

	// Offset-paged continuation of a single discovery rail — the infinite
	// horizontal scroll behind the bundle above. key/baseline/baseline_id come
	// back verbatim from the RecRail being extended.
	huma.Register(api, secured(op(http.MethodGet, "/api/me/recommended/{section}/rail", "recommended-rail-page", "One more page of a single discovery rail", "Me")),
		func(ctx context.Context, in *struct {
			Section    string `path:"section" enum:"movie,tv"`
			Key        string `query:"key" enum:"recently-released,top-unwatched,by-actor,more-genre,recommended,top-rated,rediscover" doc:"RecRail.key of the rail being paged"`
			Baseline   string `query:"baseline" maxLength:"128" doc:"RecRail.baseline (genre name) where the rail has one"`
			BaselineID int64  `query:"baseline_id" minimum:"0" doc:"RecRail.baseline_id (person id) where the rail has one"`
			Limit      int32  `query:"limit" minimum:"1" maximum:"100" default:"24"`
			Offset     int32  `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[railPageBody], error) {
			items, err := app.RecommendedRailPage(ctx, userFrom(ctx).ID, in.Section, in.Key, in.Baseline, in.BaselineID, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(railPageBody{Items: items, HasMore: len(items) == int(in.Limit)}), nil
		})

	// Personalized "For You" — the taste-vector + TMDB-graph engine (non-ML).
	// Facets hard-filter the candidate pool and the engine ranks by taste WITHIN
	// it (e.g. genre="Horror" for a horror binge). Distinct from
	// /api/me/recommended/{section} (the browse-landing rails) and the global
	// /api/recommendations aggregate feed. Off-the-shelf ML engine plugs in later
	// behind a config flag without changing this contract.
	huma.Register(api, secured(op(http.MethodGet, "/api/me/recommendations", "for-you-recommendations", "Personalized, steerable recommendations", "Me")),
		func(ctx context.Context, in *struct {
			Type      string  `query:"type" enum:"movie,tv,anime" doc:"Restrict to one media type"`
			Genre     string  `query:"genre" maxLength:"64" doc:"Only titles in this genre"`
			Keyword   string  `query:"keyword" maxLength:"128" doc:"Only titles carrying this keyword/tag"`
			MinRating float64 `query:"min_rating" minimum:"0" maximum:"10" doc:"Minimum external rating"`
			Limit     int32   `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"Number of results"`
			Offset    int32   `query:"offset" minimum:"0" maximum:"200" default:"0" doc:"Rank offset for paging (the engine re-ranks at most its top 200)"`
		}) (*JSONOutput[service.ForYouResult], error) {
			result, err := app.ForYou(ctx, userFrom(ctx).ID, service.ForYouFacets{
				Type: in.Type, Genre: in.Genre, Keyword: in.Keyword,
				MinRating: in.MinRating, Limit: in.Limit, Offset: in.Offset,
			})
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(result), nil
		})

	// Natural-language semantic search — embed the query with the ML engine and
	// KNN over media_item_facets. Returns ml_ready=false (not an error) when the
	// engine is disabled or the model is still downloading, so the FE can prompt
	// the user to enable it instead of showing a hard failure.
	huma.Register(api, secured(op(http.MethodGet, "/api/search/semantic", "semantic-search", "Natural-language 'find me something like…' search", "Discover")),
		func(ctx context.Context, in *struct {
			Q     string `query:"q" doc:"Natural-language query"`
			Type  string `query:"type" enum:"movie,tv,anime" doc:"Restrict to one media type"`
			Limit int32  `query:"limit" minimum:"1" maximum:"100" default:"40"`
		}) (*JSONOutput[semanticSearchResult], error) {
			items, err := app.SemanticSearch(ctx, in.Q, service.ForYouFacets{Type: in.Type, Limit: in.Limit})
			if err != nil {
				if errors.Is(err, service.ErrMLDisabled) {
					return noStoreJSON(semanticSearchResult{Items: []service.ForYouItem{}, MLReady: false}), nil
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(semanticSearchResult{Items: items, MLReady: true}), nil
		})

	// --- Favorites ---
	huma.Register(api, secured(op(http.MethodPost, "/api/me/favorites", "toggle-favorite", "Toggle a favorite flag", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				EntityType string `json:"entity_type" enum:"media_item,episode,season,track,artist,album" doc:"Entity kind"`
				EntityID   int64  `json:"entity_id" minimum:"1"`
			}
		}) (*JSONOutput[favoritedBody], error) {
			user := userFrom(ctx)
			fav, err := app.ToggleFavorite(ctx, user.ID, in.Body.EntityType, in.Body.EntityID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[favoritedBody]{Body: favoritedBody{Favorited: fav}}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/favorites/check", "check-favorite", "Check whether an entity is favorited", "Me")),
		func(ctx context.Context, in *struct {
			EntityType string `query:"entity_type" enum:"media_item,episode,season,track,artist,album"`
			EntityID   int64  `query:"entity_id" minimum:"1"`
		}) (*JSONOutput[favoritedBody], error) {
			fav, _ := app.IsFavorited(ctx, userFrom(ctx).ID, in.EntityType, in.EntityID)
			return noStoreJSON(favoritedBody{Favorited: fav}), nil
		})

	// --- User lists ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/lists", "list-user-lists", "User's saved lists", "Me")),
		func(ctx context.Context, in *struct {
			MediaItemID int64 `query:"media_item_id" doc:"When set, returns lists with a contains flag for this item"`
		}) (*JSONOutput[[]userListView], error) {
			user := userFrom(ctx)
			if in.MediaItemID > 0 {
				lists, containingIDs, err := app.ListUserListsWithContaining(ctx, user.ID, in.MediaItemID)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				containing := make(map[int64]bool, len(containingIDs))
				for _, id := range containingIDs {
					containing[id] = true
				}
				views := make([]userListView, len(lists))
				for i, l := range lists {
					views[i] = listRowToView(l)
					c := containing[views[i].ID]
					views[i].Contains = &c
				}
				return noStoreJSON(views), nil
			}
			lists, err := app.ListUserLists(ctx, user.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			views := make([]userListView, len(lists))
			for i, l := range lists {
				views[i] = listRowToView(l)
			}
			return noStoreJSON(views), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/lists", "create-user-list", "Create a new list", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				Name        string          `json:"name" minLength:"1" maxLength:"128" example:"Saturday night watchlist"`
				Description string          `json:"description" maxLength:"2000" example:"Slow burns + couch comfort"`
				ListType    string          `json:"list_type" enum:"manual,smart" example:"manual" doc:"manual (user-curated) or smart (filter-backed)"`
				FilterJSON  json.RawMessage `json:"filter_json" doc:"Smart-list filter spec, ignored for manual"`
				MediaType   string          `json:"media_type" enum:"movie,tv,music,book,comic,podcast,radio" example:"movie"`
			}
		}) (*JSONOutput[userListView], error) {
			user := userFrom(ctx)
			if in.Body.Name == "" {
				return nil, huma.Error400BadRequest("name is required")
			}
			list, err := app.CreateUserList(ctx, user.ID, in.Body.Name, in.Body.Description, in.Body.ListType, in.Body.MediaType, in.Body.FilterJSON)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[userListView]{Body: userListToView(list)}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/lists/{id}", "get-user-list", "List detail with items", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[userListDetailBody], error) {
			list, items, err := app.GetUserList(ctx, in.ID, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error404NotFound("list not found")
			}
			return noStoreJSON(userListDetailBody{List: list, Items: items}), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/lists/{id}", "update-user-list", "Update list metadata", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Name        string          `json:"name" maxLength:"128"`
				Description string          `json:"description" maxLength:"2000"`
				FilterJSON  json.RawMessage `json:"filter_json"`
				Icon        string          `json:"icon" maxLength:"64"`
			}
		}) (*JSONOutput[userListView], error) {
			list, err := app.UpdateUserList(ctx, in.ID, userFrom(ctx).ID, in.Body.Name, in.Body.Description, in.Body.Icon, in.Body.FilterJSON)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[userListView]{Body: userListToView(list)}, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/lists/{id}", "delete-user-list", "Delete a list", "Me")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			_ = app.DeleteUserList(ctx, in.ID, userFrom(ctx).ID)
			return statusOK("deleted"), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/lists/{id}/items", "add-list-item", "Append a media item to a list", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				MediaItemID int64 `json:"media_item_id" minimum:"1"`
			}
		}) (*JSONOutput[sqlc.UserListItem], error) {
			item, err := app.AddToList(ctx, in.ID, in.Body.MediaItemID, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[sqlc.UserListItem]{Body: item}, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/lists/{id}/items/{media_id}", "remove-list-item", "Remove a media item from a list", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			MediaID int64 `path:"media_id" minimum:"1"`
		}) (*StatusOutput, error) {
			_ = app.RemoveFromList(ctx, in.ID, in.MediaID, userFrom(ctx).ID)
			return statusOK("removed"), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/lists/{id}/reorder", "reorder-list", "Reorder items within a list", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Items []service.ReorderItem `json:"items"`
			}
		}) (*StatusOutput, error) {
			if err := app.ReorderList(ctx, in.ID, userFrom(ctx).ID, in.Body.Items); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("reordered"), nil
		})

	// --- Settings / state / playback prefs ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/settings", "get-user-settings", "User-level preferences", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.UserSettings], error) {
			settings, err := app.GetUserSettings(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to load settings")
			}
			return noStoreJSON(settings), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/settings", "update-user-settings", "Update user preferences", "Me")),
		func(ctx context.Context, in *struct {
			Body service.UserSettings
		}) (*JSONOutput[service.UserSettings], error) {
			if err := app.UpdateUserSettings(ctx, userFrom(ctx).ID, in.Body); err != nil {
				return nil, huma.Error500InternalServerError("failed to save settings")
			}
			return &JSONOutput[service.UserSettings]{Body: in.Body}, nil
		})

	// --- Password change ---
	huma.Register(api, secured(op(http.MethodPut, "/api/me/password", "change-password", "Change your password", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				CurrentPassword string `json:"current_password" minLength:"1" maxLength:"256" doc:"Current password (verified before swap)"`
				NewPassword     string `json:"new_password" minLength:"8" maxLength:"256" doc:"New password — minimum 8 chars"`
			}
		}) (*JSONOutput[okBody], error) {
			err := app.ChangePassword(ctx, userFrom(ctx).ID, in.Body.CurrentPassword, in.Body.NewPassword)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	// --- Auth sessions (devices you're signed in on) ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/auth-sessions", "list-auth-sessions", "List browser/CLI sessions for the current user", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.AuthSessionView], error) {
			sessions, err := app.ListAuthSessions(ctx, userFrom(ctx).ID, auth.TokenFromContext(ctx))
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to list sessions")
			}
			return noStoreJSON(sessions), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/auth-sessions/{id}", "revoke-auth-session", "Sign out a specific device", "Me")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1"`
		}) (*JSONOutput[okBody], error) {
			if err := app.RevokeAuthSession(ctx, userFrom(ctx).ID, in.ID); err != nil {
				return nil, huma.Error500InternalServerError("failed to revoke session")
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/auth-sessions/revoke-others", "revoke-other-auth-sessions", "Sign out every other device", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[okBody], error) {
			if err := app.RevokeOtherAuthSessions(ctx, userFrom(ctx).ID, auth.TokenFromContext(ctx)); err != nil {
				return nil, huma.Error500InternalServerError("failed to revoke other sessions")
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	// --- Personal API tokens ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/api-tokens", "list-api-tokens", "List your personal API tokens", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ApiTokenView], error) {
			tokens, err := app.ListApiTokens(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to list tokens")
			}
			return noStoreJSON(tokens), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/api-tokens", "create-api-token", "Mint a new API token", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				Name          string `json:"name" minLength:"1" maxLength:"64" example:"Backup script" doc:"Human label so you can recognise the token"`
				ExpiresInDays int    `json:"expires_in_days" minimum:"0" maximum:"3650" doc:"0 means never expires"`
			}
		}) (*JSONOutput[service.CreateApiTokenResult], error) {
			var dur time.Duration
			if in.Body.ExpiresInDays > 0 {
				dur = time.Duration(in.Body.ExpiresInDays) * 24 * time.Hour
			}
			result, err := app.CreateApiToken(ctx, userFrom(ctx).ID, in.Body.Name, dur)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to create token")
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/api-tokens/{id}", "revoke-api-token", "Revoke an API token", "Me")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1"`
		}) (*JSONOutput[okBody], error) {
			if err := app.RevokeApiToken(ctx, userFrom(ctx).ID, in.ID); err != nil {
				return nil, huma.Error500InternalServerError("failed to revoke token")
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/media-state", "get-media-state", "Watched + favorited IDs snapshot", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[mediaStateBody], error) {
			state, _ := app.GetUserMediaState(ctx, userFrom(ctx).ID)
			return noStoreJSON(mediaStateBody{
				Watched:   state.WatchedIDs,
				Favorited: state.FavoritedIDs,
			}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/state", "get-user-state", "Watched/favorited/etc. for a specific scope", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				Scope    string `json:"scope" enum:"movies,series,seasons,episodes"`
				SeriesID int64  `json:"series_id,omitempty"`
			}
		}) (*JSONOutput[map[string]any], error) {
			result, err := app.GetUserState(ctx, userFrom(ctx).ID, in.Body.Scope, in.Body.SeriesID)
			if err != nil {
				msg := err.Error()
				switch msg {
				case "scope must be one of: movies, series, seasons, episodes",
					"series_id required for scope=seasons",
					"series_id required for scope=episodes":
					return nil, huma.Error400BadRequest(msg)
				}
				return nil, huma.Error404NotFound(msg)
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/playback/{media_id}", "get-playback-preference", "Per-item playback preferences", "Me")),
		func(ctx context.Context, in *struct {
			MediaID int64 `path:"media_id" minimum:"1"`
		}) (*JSONOutput[playbackPrefBody], error) {
			pref, found, err := app.GetPlaybackPreference(ctx, userFrom(ctx).ID, in.MediaID)
			if err != nil || !found {
				return noStoreJSON(playbackPrefBody{MediaItemID: in.MediaID}), nil
			}
			return noStoreJSON(playbackPrefBody{
				MediaItemID:      pref.MediaItemID,
				AudioLanguage:    pref.AudioLanguage,
				SubtitleLanguage: pref.SubtitleLanguage,
				SubtitleMode:     pref.SubtitleMode,
			}), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/playback/{media_id}", "set-playback-preference", "Save per-item playback preferences", "Me")),
		func(ctx context.Context, in *struct {
			MediaID int64 `path:"media_id" minimum:"1"`
			Body    struct {
				AudioLanguage    string `json:"audio_language" maxLength:"16" doc:"ISO 639-1/-2/-3 code or empty to clear"`
				SubtitleLanguage string `json:"subtitle_language" maxLength:"16" doc:"ISO 639-1/-2/-3 code or empty to clear"`
				SubtitleMode     string `json:"subtitle_mode" maxLength:"16" doc:"'off' | 'forced' | 'full' | empty to clear"`
			}
		}) (*JSONOutput[playbackPrefBody], error) {
			user := userFrom(ctx)
			if in.Body.AudioLanguage == "" && in.Body.SubtitleLanguage == "" && in.Body.SubtitleMode == "" {
				_ = app.DeletePlaybackPreference(ctx, user.ID, in.MediaID)
				return &JSONOutput[playbackPrefBody]{Body: playbackPrefBody{MediaItemID: in.MediaID}}, nil
			}
			pref, err := app.SetPlaybackPreference(ctx, user.ID, in.MediaID, in.Body.AudioLanguage, in.Body.SubtitleLanguage, in.Body.SubtitleMode)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to save preference")
			}
			return &JSONOutput[playbackPrefBody]{Body: playbackPrefBody{
				MediaItemID:      pref.MediaItemID,
				AudioLanguage:    pref.AudioLanguage,
				SubtitleLanguage: pref.SubtitleLanguage,
				SubtitleMode:     pref.SubtitleMode,
			}}, nil
		})

	// --- Watched marking (consolidated) ---
	huma.Register(api, secured(op(http.MethodPost, "/api/me/watched/episode/{id}", "mark-episode-watched", "Mark an episode as watched", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[watchedBody], error) {
			_ = app.MarkEpisodeWatched(ctx, userFrom(ctx).ID, in.ID)
			return &JSONOutput[watchedBody]{Body: watchedBody{Watched: true}}, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/watched/episode/{id}", "unmark-episode-watched", "Remove the watched mark from an episode", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[watchedBody], error) {
			_ = app.UnmarkEpisodeWatched(ctx, userFrom(ctx).ID, in.ID)
			return &JSONOutput[watchedBody]{Body: watchedBody{Watched: false}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/watched/season/{id}", "mark-season-watched", "Mark all episodes in a season", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Watched bool `json:"watched"`
			}
		}) (*JSONOutput[watchedBody], error) {
			user := userFrom(ctx)
			if in.Body.Watched {
				_ = app.MarkSeasonWatched(ctx, user.ID, in.ID)
			} else {
				_ = app.UnmarkSeasonWatched(ctx, user.ID, in.ID)
			}
			return &JSONOutput[watchedBody]{Body: watchedBody{Watched: in.Body.Watched}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/watched/media/{id}", "mark-media-watched", "Mark a movie or TV show as watched (dispatches by type)", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Watched bool `json:"watched"`
			}
		}) (*JSONOutput[watchedBody], error) {
			if err := app.MarkMediaWatched(ctx, userFrom(ctx).ID, in.ID, in.Body.Watched); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[watchedBody]{Body: watchedBody{Watched: in.Body.Watched}}, nil
		})

	// --- Read-side accessors that hang off /api/media but are user-scoped ---
	huma.Register(api, secured(op(http.MethodGet, "/api/media/{id}/watched-episodes", "get-watched-episodes", "Per-season watched-episode counts and IDs", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]service.SeasonWatchInfo], error) {
			result, err := app.GetWatchedEpisodes(ctx, userFrom(ctx).ID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("series not found")
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/media/{id}/up-next", "get-up-next", "Next episode for a series", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[service.UpNextResult], error) {
			result, _ := app.GetUpNext(ctx, userFrom(ctx).ID, in.ID)
			return noStoreJSON(result), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/media/{id}/languages", "get-media-languages", "Available audio/subtitle languages", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[service.MediaLanguages], error) {
			langs, _ := app.GetMediaLanguages(ctx, in.ID)
			return cachedJSON(langs, 60), nil
		})
}

// railPageBody is one page of a discovery rail. HasMore is the len==limit
// heuristic — a short page means the rail ran dry.
type railPageBody struct {
	Items   []service.RecRailItem `json:"items"`
	HasMore bool                  `json:"has_more"`
}

type favoritedBody struct {
	Favorited bool `json:"favorited"`
}

type watchedBody struct {
	Watched bool `json:"watched"`
}

type userListDetailBody struct {
	List  sqlc.UserList        `json:"list"`
	Items []sqlc.MediaItemCard `json:"items"`
}

type mediaStateBody struct {
	Watched   []int64 `json:"watched"`
	Favorited []int64 `json:"favorited"`
}

type playbackPrefBody struct {
	MediaItemID      int64  `json:"media_item_id"`
	AudioLanguage    string `json:"audio_language"`
	SubtitleLanguage string `json:"subtitle_language"`
	SubtitleMode     string `json:"subtitle_mode"`
}

// okBody is a plain `{ok: true}` ack for fire-and-forget endpoints. Named
// so the generated TS / OpenAPI schema gets a sensible label instead of the
// anonymous-struct mangled name.
type okBody struct {
	Ok bool `json:"ok"`
}

// Reuse the existing helpers from list_handlers.go: userListView, listRowToView,
// userListToView.
