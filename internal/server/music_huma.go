package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// registerMusicRoutes mounts the music browsing + per-track read surface.
// Streaming endpoints (range-served bytes) stay on the stdlib mux —
// see music_stream_handlers.go for those.
func registerMusicRoutes(api huma.API, app *service.App) {
	// --- Top-level listings (paginated, merged across libraries) ---
	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists", "list-music-artists", "All music artists", "Music")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListMusicArtistsRow]], error) {
			limit := defaultPositive(in.Limit, 100)
			page, err := app.ListMusicArtists(ctx, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/albums", "list-music-albums", "All music albums", "Music")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListMusicAlbumsRow]], error) {
			limit := defaultPositive(in.Limit, 100)
			page, err := app.ListMusicAlbums(ctx, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks", "list-music-tracks", "All music tracks", "Music")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListMusicTracksRow]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := app.ListMusicTracks(ctx, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/home", "music-home", "Music homepage feed", "Music")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"24"`
		}) (*JSONOutput[*service.MusicHomeData], error) {
			data, err := app.GetMusicHome(ctx, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(data, 120), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{artist_slug}/albums/{album_slug}", "get-music-album", "Album detail by artist+album slug", "Music")),
		func(ctx context.Context, in *struct {
			ArtistSlug string `path:"artist_slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
			AlbumSlug  string `path:"album_slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"kind-of-blue"`
		}) (*JSONOutput[*service.MusicAlbumDetail], error) {
			if in.ArtistSlug == "" || in.AlbumSlug == "" {
				return nil, huma.Error400BadRequest("artist_slug and album_slug are required")
			}
			detail, err := app.GetAlbumDetail(ctx, in.ArtistSlug, in.AlbumSlug)
			if err != nil {
				return nil, huma.Error404NotFound("album not found")
			}
			return cachedJSON(detail, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{id}/similar", "similar-artists", "Artists similar by metadata", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]service.SimilarArtistRow], error) {
			rows, err := app.GetSimilarArtists(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(rows, 30), nil
		})

	// --- Loved tracks/artists/albums (per-user) ---
	for _, e := range []string{"track", "artist", "album"} {
		entity := e
		plural := pluralOf(entity)

		huma.Register(api, secured(op(http.MethodPost, "/api/me/loved/"+plural+"/{id}", "love-"+entity, "Mark a "+entity+" as loved", "Me")),
			func(ctx context.Context, in *IDPath) (*JSONOutput[lovedBody], error) {
				state, err := app.SetEntityLoved(ctx, userFrom(ctx).ID, entity, in.ID, true)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				return &JSONOutput[lovedBody]{Body: lovedBody{Loved: state}}, nil
			})

		huma.Register(api, secured(op(http.MethodDelete, "/api/me/loved/"+plural+"/{id}", "unlove-"+entity, "Remove the loved mark from a "+entity, "Me")),
			func(ctx context.Context, in *IDPath) (*JSONOutput[lovedBody], error) {
				state, err := app.SetEntityLoved(ctx, userFrom(ctx).ID, entity, in.ID, false)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				return &JSONOutput[lovedBody]{Body: lovedBody{Loved: state}}, nil
			})

		huma.Register(api, secured(op(http.MethodGet, "/api/me/loved/"+plural+"/ids", "loved-"+entity+"-ids", "Loved "+entity+" IDs", "Me")),
			func(ctx context.Context, _ *struct{}) (*JSONOutput[idsBody], error) {
				ids, err := listLovedIDs(ctx, app, entity, userFrom(ctx).ID)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				if ids == nil {
					ids = []int64{}
				}
				return noStoreJSON(idsBody{IDs: ids}), nil
			})
	}

	huma.Register(api, secured(op(http.MethodGet, "/api/me/loved/tracks", "list-loved-tracks", "Paginated loved tracks", "Me")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListUserLovedTracksRow]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := app.ListUserLovedTracks(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(page), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/loved/artists", "list-loved-artists", "Paginated loved artists", "Me")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListUserLovedArtistsRow]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := app.ListUserLovedArtists(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(page), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/loved/albums", "list-loved-albums", "Paginated loved albums", "Me")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[sqlc.ListUserLovedAlbumsRow]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := app.ListUserLovedAlbums(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(page), nil
		})

	// --- Playlists ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/playlists", "list-playlists", "List user's playlists", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[playlistsListBody], error) {
			rows, err := app.ListUserPlaylists(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(playlistsListBody{Items: rows}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/playlists", "create-playlist", "Create a new playlist", "Me")),
		func(ctx context.Context, in *struct {
			Body playlistMutation
		}) (*JSONOutput[sqlc.UserPlaylist], error) {
			pl, err := app.CreateUserPlaylist(ctx, userFrom(ctx).ID, in.Body.Name, in.Body.Description, in.Body.Cover)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &JSONOutput[sqlc.UserPlaylist]{Body: pl}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/playlists/{id}", "get-playlist", "Playlist detail with tracks", "Me")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[*service.PlaylistDetail], error) {
			detail, err := app.GetUserPlaylistDetail(ctx, userFrom(ctx).ID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			return noStoreJSON(detail), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/playlists/{id}", "update-playlist", "Update playlist metadata", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body playlistMutation
		}) (*struct{}, error) {
			if err := app.UpdateUserPlaylist(ctx, userFrom(ctx).ID, in.ID, in.Body.Name, in.Body.Description, in.Body.Cover); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return nil, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/playlists/{id}", "delete-playlist", "Delete a playlist", "Me")),
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			if err := app.DeleteUserPlaylist(ctx, userFrom(ctx).ID, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return nil, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/playlists/{id}/tracks/{track_id}", "add-playlist-track", "Append a track to a playlist", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			TrackID int64 `path:"track_id" minimum:"1"`
		}) (*struct{}, error) {
			if err := app.AddTrackToPlaylist(ctx, userFrom(ctx).ID, in.ID, in.TrackID); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/playlists/{id}/tracks/{track_id}", "remove-playlist-track", "Remove a track from a playlist", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			TrackID int64 `path:"track_id" minimum:"1"`
		}) (*struct{}, error) {
			if err := app.RemoveTrackFromPlaylist(ctx, userFrom(ctx).ID, in.ID, in.TrackID); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, nil
		})

	// --- Per-track reads (lyrics + files; streaming stays on stdlib mux) ---
	huma.Register(api, secured(op(http.MethodGet, "/api/tracks/{id}/files", "list-track-files", "Available formats for a track", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]sqlc.TrackFile], error) {
			files, err := app.ListTrackFiles(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(files, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/tracks/{id}/lyrics", "get-track-lyrics", "Parsed lyrics (synced or plain)", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[LyricsResponse], error) {
			body, err := readTrackLyrics(ctx, app, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			return cachedJSON(parseLyrics(body), 60), nil
		})

	// --- Sonic analysis reads ---
	// These results are stable once analysis completes — cache aggressively so
	// the FE doesn't re-fetch huge embedding payloads on every nav.
	huma.Register(api, secured(op(http.MethodGet, "/api/tracks/{id}/facets", "get-track-facets", "Per-track ML/DSP facets", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[*service.FacetsView], error) {
			facets, err := app.TrackFacets(ctx, in.ID)
			if err != nil {
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("no facets for this track yet")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(facets, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/tracks/{id}/waveform", "get-track-waveform", "Decimated waveform peaks", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[waveformBody], error) {
			wf, err := app.TrackWaveform(ctx, in.ID)
			if err != nil {
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("no waveform for this track yet")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(waveformBody{Waveform: wf}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/tracks/{id}/sonic-similar", "sonic-similar-tracks", "Tracks similar to a seed by audio embedding", "Music")),
		func(ctx context.Context, in *struct {
			IDPath
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[itemsBody], error) {
			rows, err := app.SimilarMusicTracks(ctx, in.ID, in.Limit)
			if err != nil {
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("seed track has no facets")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(itemsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{id}/sonic-similar", "sonic-similar-artists", "Artists similar by sonic centroid", "Music")),
		func(ctx context.Context, in *struct {
			IDPath
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[itemsBody], error) {
			rows, err := app.SimilarMusicArtists(ctx, in.ID, in.Limit)
			if err != nil {
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("seed artist has no centroid")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(itemsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/albums/{id}/sonic-similar", "sonic-similar-albums", "Albums similar by sonic centroid", "Music")),
		func(ctx context.Context, in *struct {
			IDPath
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[itemsBody], error) {
			rows, err := app.SimilarMusicAlbums(ctx, in.ID, in.Limit)
			if err != nil {
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("seed album has no centroid")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(itemsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/search-sonic", "search-music-sonic", "Free-text music vibe search (CLAP)", "Music")),
		func(ctx context.Context, in *struct {
			Q     string `query:"q" minLength:"1" doc:"Free-form audio vibe prompt"`
			Limit int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[itemsBody], error) {
			if in.Q == "" {
				return nil, huma.Error400BadRequest("q is required")
			}
			rows, err := app.SearchMusicByText(ctx, in.Q, in.Limit)
			if err != nil {
				if errors.Is(err, sonicanalysis.ErrTextSearcherUnavailable) {
					return nil, huma.Error503ServiceUnavailable("sonic text search unavailable: model not loaded")
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(itemsBody{Items: rows}, 60), nil
		})
}

type lovedBody struct {
	Loved bool `json:"loved"`
}

type idsBody struct {
	IDs []int64 `json:"ids"`
}

type playlistsListBody struct {
	Items any `json:"items"`
}

type playlistMutation struct {
	Name        string `json:"name" minLength:"1" maxLength:"128" example:"Friday focus"`
	Description string `json:"description" maxLength:"2000" example:"Deep work soundtrack"`
	Cover       string `json:"cover_path" maxLength:"512" doc:"Optional path/URL to a custom cover image"`
}

type waveformBody struct {
	Waveform any `json:"waveform"`
}

type itemsBody struct {
	Items any `json:"items"`
}

func pluralOf(entity string) string {
	switch entity {
	case "track":
		return "tracks"
	case "artist":
		return "artists"
	case "album":
		return "albums"
	}
	return entity + "s"
}

func listLovedIDs(ctx context.Context, app *service.App, entity string, userID int64) ([]int64, error) {
	switch entity {
	case "track":
		return app.ListUserLovedTrackIDs(ctx, userID)
	case "artist":
		return app.ListUserLovedArtistIDs(ctx, userID)
	case "album":
		return app.ListUserLovedAlbumIDs(ctx, userID)
	}
	return nil, nil
}

func defaultPositive(v, def int32) int32 {
	if v <= 0 {
		return def
	}
	return v
}

// readTrackLyrics adapts the existing primaryLyricsPath + readLyricsFile path
// used by the legacy handler. The lyrics parser lives in lyrics_handlers.go.
func readTrackLyrics(ctx context.Context, app *service.App, trackID int64) ([]byte, error) {
	path, err := primaryLyricsPathCtx(ctx, app, trackID)
	if err != nil {
		return nil, err
	}
	return readLyricsFile(path)
}
