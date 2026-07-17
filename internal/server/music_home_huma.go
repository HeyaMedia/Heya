package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// registerMusicHomeRoutes wires the 10 smart-home shelves powering the
// /music landing page. Each shelf is its own endpoint so the FE can
// refresh any one in isolation (and the rotating shelves can
// poll-then-replace without churning the whole grid).
//
// Cache TTLs: 30s for the "fresh data" shelves, 300s (= 5 minutes) for the
// rotating ones so the FE's "refresh every 5 minutes" loop lines up with
// the seed rotation window.
func registerMusicHomeRoutes(api huma.API, app *service.App) {
	// Artist play queue — top-played-first ordering of the artist's catalog,
	// powering the "play this artist" button on home tiles. Lives under
	// /api/music to stay consistent with the other artist-scoped routes
	// (slug path param + no /home prefix).
	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/play-queue", "artist-play-queue", "Artist's tracks ordered by user play count desc (top hits first)", "Music")),
		func(ctx context.Context, in *struct {
			Slug  string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200"`
			Limit int32  `query:"limit" minimum:"1" maximum:"1000" default:"500"`
		}) (*JSONOutput[artistPlayQueueBody], error) {
			rows, err := app.ArtistPlayQueue(ctx, userFrom(ctx).ID, in.Slug, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(artistPlayQueueBody{Items: rows}), nil
		})

	// 1. Mixes for You — live-generated from the shared music recommender. A
	// 1h cache amortizes the candidate SQL while daily jitter keeps a stable
	// listening session and naturally rotates tomorrow's slate.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/mixes-for-you", "music-home-mixes", "Personal mixes from taste, sonic similarity, and provider metadata", "MusicHome")),
		func(ctx context.Context, in *struct {
			MaxMixes     int `query:"max"            minimum:"1" maximum:"10"  default:"10"`
			TracksPerMix int `query:"tracks_per_mix" minimum:"5" maximum:"100" default:"30"`
		}) (*JSONOutput[mixesBody], error) {
			mixes, err := app.GenerateMixesForUser(ctx, userFrom(ctx).ID, in.MaxMixes, in.TracksPerMix)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			if mixes == nil {
				mixes = []service.MusicMix{}
			}
			return cachedJSON(mixesBody{Items: mixes}, 3600), nil
		})

	// 2. Recently Added (albums + singles + EPs). Reuses the existing
	// ListRecentlyAddedAlbums query — exposed as its own endpoint here so
	// the smart-home page can fetch it without pulling the full /home
	// payload.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/recently-added", "music-home-recently-added", "Newest album/EP/single additions", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit  int32 `query:"limit" minimum:"1" maximum:"100" default:"24"`
			Offset int32 `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[recentAlbumsBody], error) {
			items, err := app.ListRecentlyAddedAlbumsPage(ctx, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(recentAlbumsBody{Items: items}, 60), nil
		})

	// 3. Recently Played Artists (artist-grain, deduped).
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/recently-played-artists", "music-home-recent-artists", "Distinct artists from recent plays", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[recentArtistsBody], error) {
			rows, err := app.RecentlyPlayedArtistsForUser(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(recentArtistsBody{Items: rows}), nil
		})

	// 4. On This Day — albums whose release_date hits today.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/on-this-day", "music-home-on-this-day", "Anniversary releases (release_date matches today)", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"20"`
		}) (*JSONOutput[onThisDayBody], error) {
			rows, err := app.OnThisDayAlbums(ctx, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(onThisDayBody{Items: rows}, 3600), nil
		})

	// 5. Recent Playlists — user playlists ordered by derived last-play.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/recent-playlists", "music-home-recent-playlists", "User playlists ordered by last play", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"12"`
		}) (*JSONOutput[recentPlaylistsBody], error) {
			rows, err := app.RecentPlaylistsForUser(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(recentPlaylistsBody{Items: rows}), nil
		})

	// 6. More By <Artist> — 2-3 randomly-picked-from-history artists with
	// their albums. Rotates every 5 minutes via the shelf seed.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/more-by-artists", "music-home-more-by-artists", "Random artists from user history with their albums", "MusicHome")),
		func(ctx context.Context, in *struct {
			Picks           int32 `query:"picks"             minimum:"1" maximum:"10" default:"3"`
			AlbumsPerArtist int32 `query:"albums_per_artist" minimum:"1" maximum:"20" default:"6"`
		}) (*JSONOutput[moreByArtistsBody], error) {
			rows, err := app.MoreByArtistsForUser(ctx, userFrom(ctx).ID, in.Picks, in.AlbumsPerArtist)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(moreByArtistsBody{Items: rows}, 300), nil
		})

	// 7. More in <Genre> — one genre from user's top-played, list of artists
	// in that genre (no images, names only — matches the hibiki UX).
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/more-in-genre", "music-home-more-in-genre", "Artists in a rotating user-genre pick", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"20"`
		}) (*JSONOutput[moreInGenreBody], error) {
			shelf, err := app.MoreInGenreForUser(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			body := moreInGenreBody{}
			if shelf != nil {
				body.Enabled = true
				body.Genre = shelf.Genre
				body.Artists = shelf.Artists
			}
			return cachedJSON(body, 300), nil
		})

	// 8. Most Played in <Month> — defaults to last calendar month, falls
	// back to this month for fresh installs with no previous-month plays.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/most-played-last-month", "music-home-most-played-month", "Top-played albums in the previous calendar month", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"12"`
		}) (*JSONOutput[mostPlayedBody], error) {
			shelf, err := app.MostPlayedAlbumsLastMonth(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			body := mostPlayedBody{}
			if shelf != nil {
				body.Enabled = true
				body.WindowLabel = shelf.WindowLabel
				body.Albums = shelf.Albums
			}
			return noStoreJSON(body), nil
		})

	// 9. Haven't Played in a While — surfaces 3 lapsed-favorite artists.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/lapsed-artists", "music-home-lapsed-artists", "Artists user used to play but hasn't in months", "MusicHome")),
		func(ctx context.Context, in *struct {
			Picks           int32 `query:"picks"             minimum:"1" maximum:"10" default:"3"`
			AlbumsPerArtist int32 `query:"albums_per_artist" minimum:"1" maximum:"20" default:"6"`
		}) (*JSONOutput[lapsedShelfBody], error) {
			shelf, err := app.LapsedArtistsForUser(ctx, userFrom(ctx).ID, in.Picks, in.AlbumsPerArtist)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			body := lapsedShelfBody{Artists: []service.LapsedArtistEntry{}}
			if shelf != nil {
				body.Enabled = true
				body.SinceLabel = shelf.SinceLabel
				body.Artists = shelf.Artists
			}
			return cachedJSON(body, 300), nil
		})

	// 10. More from <Label> — picks a label from the user's listening.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/home/more-from-label", "music-home-more-from-label", "Albums on a label user listens to", "MusicHome")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"40" default:"20"`
		}) (*JSONOutput[moreFromLabelBody], error) {
			shelf, err := app.MoreFromLabelForUser(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			body := moreFromLabelBody{Albums: []sqlc.ListAlbumsByLabelRow{}}
			if shelf != nil {
				body.Enabled = true
				body.Label = shelf.Label
				body.Albums = shelf.Albums
			}
			return cachedJSON(body, 300), nil
		})
}

// Typed body envelopes — same {items: T[]} pattern the rest of the music
// surface uses so the generated TS client doesn't lose shape.

type mixesBody struct {
	Items []service.MusicMix `json:"items"`
}

type artistPlayQueueBody struct {
	Items []sqlc.ListArtistTracksTopPlayedFirstRow `json:"items"`
}

type recentAlbumsBody struct {
	Items []sqlc.ListRecentlyAddedAlbumsRow `json:"items"`
}

type recentArtistsBody struct {
	Items []sqlc.ListRecentlyPlayedArtistsRow `json:"items"`
}

type onThisDayBody struct {
	Items []sqlc.ListOnThisDayAlbumsRow `json:"items"`
}

type recentPlaylistsBody struct {
	Items []sqlc.ListRecentUserPlaylistsRow `json:"items"`
}

type moreByArtistsBody struct {
	Items []service.MoreByArtist `json:"items"`
}

// Singleton shelves: `enabled` is the FE signal to hide the rail when the
// underlying data isn't there yet (user has no plays / no genre picks).
type moreInGenreBody struct {
	Enabled bool                         `json:"enabled"`
	Genre   string                       `json:"genre"`
	Artists []sqlc.ListArtistsByGenreRow `json:"artists"`
}

type mostPlayedBody struct {
	Enabled     bool                              `json:"enabled"`
	WindowLabel string                            `json:"window_label"`
	Albums      []sqlc.MostPlayedAlbumsInRangeRow `json:"albums"`
}

type lapsedShelfBody struct {
	Enabled    bool                        `json:"enabled"`
	SinceLabel string                      `json:"since_label"`
	Artists    []service.LapsedArtistEntry `json:"artists"`
}

type moreFromLabelBody struct {
	Enabled bool                        `json:"enabled"`
	Label   string                      `json:"label"`
	Albums  []sqlc.ListAlbumsByLabelRow `json:"albums"`
}
