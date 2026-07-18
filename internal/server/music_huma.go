package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/secrettext"
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
			page, err := app.ListMusicTracks(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/counts", "music-counts", "Artist/album/track totals", "Music")),
		simpleGet(app.GetMusicCounts, 30))

	huma.Register(api, secured(op(http.MethodGet, "/api/music/home", "music-home", "Music homepage feed", "Music")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"24"`
		}) (*JSONOutput[*service.MusicHomeData], error) {
			data, err := app.GetMusicHome(ctx, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(data, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}", "get-music-artist", "Artist detail by slug", "Music")),
		func(ctx context.Context, in *struct {
			Slug string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
		}) (*JSONOutput[*sqlc.GetMusicArtistBySlugRow], error) {
			row, err := app.GetMusicArtistBySlug(ctx, in.Slug)
			if err != nil {
				return nil, huma.Error404NotFound("artist not found")
			}
			return cachedJSON(row, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/albums", "list-artist-albums", "Albums by artist (paginated)", "Music")),
		func(ctx context.Context, in *struct {
			Slug string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
			Pagination
		}) (*JSONOutput[*service.MusicListPage[sqlc.ListAlbumsByArtistSlugRow]], error) {
			limit := defaultPositive(in.Limit, 100)
			page, err := app.ListAlbumsByArtistSlug(ctx, in.Slug, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/tracks", "list-artist-tracks", "Tracks by artist (paginated)", "Music")),
		func(ctx context.Context, in *struct {
			Slug string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
			Pagination
		}) (*JSONOutput[*service.MusicListPage[sqlc.ListTracksByArtistSlugRow]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := app.ListTracksByArtistSlug(ctx, in.Slug, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(page, 30), nil
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

	// --- Album metadata editor (admin) ---
	huma.Register(api, adminSecured(op(http.MethodPut, "/api/music/albums/{id}", "update-album-metadata", "Edit album metadata fields", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.UpdateAlbumReq
		}) (*JSONOutput[sqlc.Album], error) {
			updated, err := app.UpdateAlbumMetadata(ctx, in.ID, in.Body)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[sqlc.Album]{Body: updated}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/music/albums/{id}/identify", "album-identify-search", "heya.media album search for re-identification", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Q string `query:"q" maxLength:"200" doc:"Title query (defaults to the album title)"`
		}) (*JSONOutput[identifyBody], error) {
			result, err := app.AlbumIdentifySearch(ctx, in.ID, in.Q)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[identifyBody]{Body: identifyBody{Results: result.Results}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/music/albums/{id}/identify", "apply-album-identify", "Pin the album to a chosen MusicBrainz release group and refresh the artist", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				ProviderName string `json:"provider_name" minLength:"1" maxLength:"32"`
				ProviderID   string `json:"provider_id" minLength:"1" maxLength:"256"`
			}
		}) (*StatusOutput, error) {
			if err := app.ApplyAlbumIdentify(ctx, in.ID, in.Body.ProviderName, in.Body.ProviderID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("identified"), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/similar", "similar-artists", "Artists similar by metadata (Last.fm / ListenBrainz)", "Music")),
		func(ctx context.Context, in *struct {
			Slug string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
		}) (*JSONOutput[[]service.SimilarArtistRow], error) {
			rows, err := app.GetSimilarArtistsBySlug(ctx, in.Slug)
			if err != nil {
				return nil, huma.Error404NotFound("artist not found")
			}
			return cachedJSON(rows, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/top-tracks", "artist-top-tracks", "Artist's Last.fm top tracks rail, with local linkage when owned", "Music")),
		func(ctx context.Context, in *struct {
			Slug  string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"ado"`
			Limit int32  `query:"limit" minimum:"1" maximum:"200" default:"25"`
		}) (*JSONOutput[topTracksBody], error) {
			rows, err := app.ListArtistTopTracksBySlug(ctx, in.Slug, in.Limit)
			if err != nil {
				return nil, huma.Error404NotFound("artist not found")
			}
			return cachedJSON(topTracksBody{Items: rows}, 60), nil
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

	registerLovedList(api, "tracks", app.ListUserLovedTracks)
	registerLovedList(api, "artists", app.ListUserLovedArtists)
	registerLovedList(api, "albums", app.ListUserLovedAlbums)

	// --- Playlists ---
	// --- Per-user listening history + taste profile ---
	// Scrobble events themselves flow through the unified /api/me/playback
	// endpoint (registered in me_huma.go); the reads below are music-specific
	// views of that history.
	huma.Register(api, secured(op(http.MethodGet, "/api/me/recently-played", "list-recently-played", "User's recently-played tracks (deduped)", "Me")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[recentlyPlayedBody], error) {
			limit := defaultPositive(in.Limit, 50)
			rows, err := app.ListRecentlyPlayed(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(recentlyPlayedBody{Items: rows}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/listening-stats", "get-listening-stats", "User's taste profile: top genres, mood averages, tempo histogram", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[*service.ListeningStats], error) {
			stats, err := app.ListeningStatsForUser(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			// Stats drift slowly relative to a single play; a short cache lets
			// the FE flick between panels without re-aggregating.
			return cachedJSON(stats, 60), nil
		})

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
			pl, err := app.CreateUserPlaylist(ctx, userFrom(ctx).ID, in.Body.Name, in.Body.Description)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &JSONOutput[sqlc.UserPlaylist]{Body: pl}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/playlists/{id}", "get-playlist", "Playlist detail with tracks (numeric ID or slug)", "Me")),
		func(ctx context.Context, in *SlugOrIDPath) (*JSONOutput[*service.PlaylistDetail], error) {
			detail, err := app.GetUserPlaylistDetailByRef(ctx, userFrom(ctx).ID, in.ID)
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
			if err := app.UpdateUserPlaylist(ctx, userFrom(ctx).ID, in.ID, in.Body.Name, in.Body.Description, in.Body.Tags); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return nil, nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/playlists/{id}/pin", "set-playlist-pin", "Pin/unpin a playlist (page or sidebar scope)", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				Scope  string `json:"scope" enum:"page,sidebar" doc:"Which pin set to toggle"`
				Pinned bool   `json:"pinned"`
			}
		}) (*struct{}, error) {
			if err := app.SetPlaylistPin(ctx, userFrom(ctx).ID, in.ID, in.Body.Scope, in.Body.Pinned); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/playlists/sidebar-order", "set-playlist-sidebar-order", "Persist manual sidebar playlist order", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				IDs []int64 `json:"ids" doc:"Playlist IDs in the desired top-to-bottom order (full list)"`
			}
		}) (*struct{}, error) {
			if err := app.SetSidebarPlaylistOrder(ctx, userFrom(ctx).ID, in.Body.IDs); err != nil {
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

	// Cover upload/clear are mutations, so they stay owner-scoped under the
	// normal /api/me bearer wrapper. The matching GET (raw image bytes) is
	// registered unauthenticated in binary_huma.go instead — see
	// GetUserPlaylistCoverPath's doc comment for why.
	uploadPlaylistCoverOp := secured(op(http.MethodPost, "/api/me/playlists/{id}/cover", "upload-playlist-cover", "Upload a custom playlist cover", "Me"))
	uploadPlaylistCoverOp.BodyReadTimeout = 30 * time.Second
	huma.Register(api, uploadPlaylistCoverOp,
		func(ctx context.Context, in *struct {
			IDPath
			RawBody huma.MultipartFormFiles[playlistCoverUploadForm]
		}) (*StatusOutput, error) {
			data := in.RawBody.Data()
			if !data.File.IsSet {
				return nil, huma.Error400BadRequest("file field required")
			}
			defer func() { _ = data.File.Close() }()

			if err := app.SetUserPlaylistCover(ctx, userFrom(ctx).ID, in.ID, data.File); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("uploaded"), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/playlists/{id}/cover", "clear-playlist-cover", "Remove the custom playlist cover", "Me")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.ClearUserPlaylistCover(ctx, userFrom(ctx).ID, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("cleared"), nil
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

	// --- Per-track reads (detail + lyrics + files; streaming lives in binary_huma.go) ---
	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}", "get-music-track", "Track detail with files + album/artist context", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[*service.MusicTrackDetail], error) {
			detail, err := app.GetMusicTrackDetail(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("track not found")
			}
			// Loudness/boundaries can be populated on demand; don't let a stale
			// pre-analysis response hide newly persisted playback data.
			return noStoreJSON(detail), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/files", "list-track-files", "Available formats for a track", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]sqlc.TrackFile], error) {
			files, err := app.ListTrackFiles(ctx, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			for i := range files {
				files[i].LyricsPath = secrettext.Redact(files[i].LyricsPath)
			}
			return cachedJSON(files, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/lyrics", "get-track-lyrics", "Parsed lyrics (synced or plain)", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[LyricsResponse], error) {
			body, err := readTrackLyrics(ctx, app, in.ID)
			if err != nil {
				if errors.Is(err, service.ErrTrackLyricsUnavailable) {
					return nil, huma.Error404NotFound(err.Error())
				}
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(parseLyrics(body), 300), nil
		})

	// --- Sonic analysis reads ---
	// These results are stable once analysis completes — cache aggressively so
	// the FE doesn't re-fetch huge embedding payloads on every nav.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/facets", "get-track-facets", "Per-track ML/DSP facets", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[*service.FacetsView], error) {
			facets, err := app.TrackFacets(ctx, in.ID)
			if err != nil {
				return nil, facetsErr(err, "no facets for this track yet", http.StatusInternalServerError)
			}
			return cachedJSON(facets, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/waveform", "get-track-waveform", "Decimated waveform peaks", "Music")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[waveformBody], error) {
			wf, err := app.TrackWaveform(ctx, in.ID)
			if err != nil {
				return nil, facetsErr(err, "no waveform for this track yet", http.StatusInternalServerError)
			}
			return cachedJSON(waveformBody{Waveform: wf}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/mix-to", "mix-to-tracks", "DJ-style harmonically-compatible tracks (Camelot ±1, BPM ±5)", "Music")),
		func(ctx context.Context, in *struct {
			IDPath
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[mixToBody], error) {
			rows, err := app.BuildDJMix(ctx, in.ID, in.Limit)
			if err != nil {
				return nil, facetsErr(err, "seed track has no facets", http.StatusBadRequest)
			}
			return cachedJSON(mixToBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/tracks/{id}/sonic-similar", "sonic-similar-tracks", "Tracks similar to a seed by audio embedding", "Music")),
		func(ctx context.Context, in *struct {
			IDPath
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[trackResultsBody], error) {
			rows, err := app.SimilarMusicTracks(ctx, in.ID, in.Limit)
			if err != nil {
				return nil, facetsErr(err, "seed track has no facets", http.StatusInternalServerError)
			}
			return cachedJSON(trackResultsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{slug}/sonic-similar", "sonic-similar-artists", "Artists similar by sonic centroid", "Music")),
		func(ctx context.Context, in *struct {
			Slug  string `path:"slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
			Limit int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[artistResultsBody], error) {
			rows, err := app.SimilarMusicArtistsBySlug(ctx, in.Slug, in.Limit)
			if err != nil {
				return nil, facetsErr(err, "seed artist has no centroid", http.StatusNotFound)
			}
			return cachedJSON(artistResultsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/artists/{artist_slug}/albums/{album_slug}/sonic-similar", "sonic-similar-albums", "Albums similar by sonic centroid", "Music")),
		func(ctx context.Context, in *struct {
			ArtistSlug string `path:"artist_slug" pattern:"^[a-z0-9-]+$" maxLength:"200" example:"miles-davis"`
			AlbumSlug  string `path:"album_slug"  pattern:"^[a-z0-9-]+$" maxLength:"200" example:"kind-of-blue"`
			Limit      int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[albumResultsBody], error) {
			rows, err := app.SimilarMusicAlbumsBySlugs(ctx, in.ArtistSlug, in.AlbumSlug, in.Limit)
			if err != nil {
				return nil, facetsErr(err, "seed album has no centroid", http.StatusNotFound)
			}
			return cachedJSON(albumResultsBody{Items: rows}, 300), nil
		})

	// --- Browse-by-facet tiles + drilldown listings ---
	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/moods", "browse-music-moods", "Mood-tile buckets (Happy, Party, …)", "Music")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[moodBucketsBody], error) {
			rows, err := app.ListMoodBuckets(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(moodBucketsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/moods/{mood}/tracks", "list-tracks-by-mood", "Tracks scoring high on a mood", "Music")),
		func(ctx context.Context, in *struct {
			Mood string `path:"mood" pattern:"^[a-z_]+$" maxLength:"40" example:"mood_happy"`
			Pagination
		}) (*JSONOutput[moodTracksBody], error) {
			rows, err := app.ListTracksByMood(ctx, in.Mood, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			total, err := app.CountTracksForMood(ctx, in.Mood)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return cachedJSON(moodTracksBody{Items: rows, Total: total}, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/genres", "browse-music-genres", "Genre-tile buckets ranked by track count", "Music")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[genreBucketsBody], error) {
			rows, err := app.ListGenreBuckets(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(genreBucketsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/genres/{name}/tracks", "list-tracks-by-genre", "Tracks tagged with a genre", "Music")),
		func(ctx context.Context, in *struct {
			Name string `path:"name" maxLength:"160" example:"Electronic---Techno"`
			Pagination
		}) (*JSONOutput[genreTracksBody], error) {
			rows, err := app.ListTracksByGenre(ctx, in.Name, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			total, err := app.CountTracksForGenre(ctx, in.Name)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return cachedJSON(genreTracksBody{Items: rows, Total: total}, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/tempo", "browse-music-tempo", "BPM-band tile buckets", "Music")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[tempoBucketsBody], error) {
			rows, err := app.ListTempoBuckets(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(tempoBucketsBody{Items: rows}, 300), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/browse/tempo/{band}/tracks", "list-tracks-by-tempo", "Tracks in a BPM band", "Music")),
		func(ctx context.Context, in *struct {
			Band string `path:"band" pattern:"^[0-9]+-[0-9]+$" maxLength:"20" example:"110-130"`
			Pagination
		}) (*JSONOutput[tempoTracksBody], error) {
			minBPM, maxBPM, ok := app.LookupTempoBand(in.Band)
			if !ok {
				return nil, huma.Error404NotFound("unknown tempo band")
			}
			rows, err := app.ListTracksByTempoBand(ctx, minBPM, maxBPM, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			total, err := app.CountTracksForTempoBand(ctx, minBPM, maxBPM)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(tempoTracksBody{Items: rows, Total: total}, 60), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/music/radio", "build-music-radio", "Build seed radio with sonic and metadata fallbacks", "Music")),
		func(ctx context.Context, in *struct {
			Body service.RadioRequest
		}) (*JSONOutput[*service.RadioResponse], error) {
			resp, err := app.BuildRadio(ctx, userFrom(ctx).ID, in.Body)
			if err != nil {
				if errors.Is(err, service.ErrNoRadioSeed) {
					return nil, huma.Error404NotFound("no playable recommendation candidates are available for that seed")
				}
				if errors.Is(err, service.ErrNoFacets) {
					return nil, huma.Error404NotFound("seed track has no facets")
				}
				if errors.Is(err, sonicanalysis.ErrTextSearcherUnavailable) {
					return nil, huma.Error503ServiceUnavailable("text-seeded radio unavailable: CLAP model not loaded")
				}
				return nil, huma.Error400BadRequest(err.Error())
			}
			// Radio results are personal + ephemeral — each press of "play radio"
			// should resolve fresh against the current library state.
			return noStoreJSON(resp), nil
		})

	// AI-directed Mix Builder: narrative brief → LLM acoustic plan → several
	// CLAP searches → grounded LLM selection and sequencing. It deliberately
	// sits beside Instant Radio because both return playable music queues, but
	// this route is explicit/slow and therefore never used for live typing.
	huma.Register(api, secured(op(http.MethodPost, "/api/ai/music-mix", "post-ai-music-mix", "Build an AI-directed music mix from a narrative brief", "Music")),
		func(ctx context.Context, in *struct {
			Body service.AIMusicMixRequest
		}) (*JSONOutput[service.AIMusicMixResult], error) {
			resp, err := app.AIMusicMix(ctx, userFrom(ctx).ID, in.Body)
			if err != nil {
				if errors.Is(err, sonicanalysis.ErrTextSearcherUnavailable) {
					return nil, huma.Error503ServiceUnavailable("AI Mix Builder needs the CLAP text model — enable Sonic Analysis and let its models finish downloading")
				}
				return nil, aiError(err)
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/search-sonic", "search-music-sonic", "Free-text music vibe search (CLAP)", "Music")),
		func(ctx context.Context, in *struct {
			Q     string `query:"q" minLength:"1" doc:"Free-form audio vibe prompt"`
			Limit int32  `query:"limit" minimum:"1" maximum:"100" default:"20"`
		}) (*JSONOutput[trackTextSearchBody], error) {
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
			return cachedJSON(trackTextSearchBody{Items: rows}, 60), nil
		})

	// --- Quick Stations ---
	// Library Radio / Deep Cuts / Time Travel / Random Album all return the
	// same StationResponse shape so the FE renders them with one component.
	// Every station is no-store: each tap should reroll fresh.
	huma.Register(api, secured(op(http.MethodGet, "/api/music/stations/library-radio", "stations-library-radio", "Personalized radio from across the library", "Music")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[*service.StationResponse], error) {
			resp, err := app.LibraryRadio(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/stations/deep-cuts", "stations-deep-cuts", "Tracks the user has barely played", "Music")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[*service.StationResponse], error) {
			resp, err := app.DeepCuts(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/stations/time-travel", "stations-time-travel", "Random tracks from a year range", "Music")),
		func(ctx context.Context, in *struct {
			MinYear int32 `query:"min_year" minimum:"1900" maximum:"2100" default:"1990"`
			MaxYear int32 `query:"max_year" minimum:"1900" maximum:"2100" default:"1999"`
			Limit   int32 `query:"limit"    minimum:"1"    maximum:"100"  default:"30"`
		}) (*JSONOutput[*service.StationResponse], error) {
			resp, err := app.TimeTravel(ctx, in.MinYear, in.MaxYear, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/music/stations/random-album", "stations-random-album", "One random album, end-to-end", "Music")),
		simpleGet(app.RandomAlbum, 0))

	// --- Ratings ---
	// 5-star UI with half-step precision = 1..10 integer underneath. Setting
	// rating=0 deletes the row (clears the rating). Tracks, albums, and
	// artists mirror one another so the FE reuses one rating widget +
	// composable across all three kinds.
	registerRatingRoutes[trackIDsBody, sqlc.ListUserRatedTracksRow](api, "tracks", "track",
		app.GetUserTrackRating, app.SetUserTrackRating, app.RatingsForTracks, app.ListUserRatedTracks,
		func(b trackIDsBody) []int64 { return b.TrackIDs })
	registerRatingRoutes[albumIDsBody, sqlc.ListUserRatedAlbumsRow](api, "albums", "album",
		app.GetUserAlbumRating, app.SetUserAlbumRating, app.RatingsForAlbums, app.ListUserRatedAlbums,
		func(b albumIDsBody) []int64 { return b.AlbumIDs })
	registerRatingRoutes[artistIDsBody, sqlc.ListUserRatedArtistsRow](api, "artists", "artist",
		app.GetUserArtistRating, app.SetUserArtistRating, app.RatingsForArtists, app.ListUserRatedArtists,
		func(b artistIDsBody) []int64 { return b.ArtistIDs })

	// Aggregate stats for one rating band (Loved Songs hero ledger). Its own
	// path segment ("track-stats", not "tracks/stats") so it can't collide
	// with the /ratings/tracks/{id} param route.
	huma.Register(api, secured(op(http.MethodGet, "/api/me/ratings/track-stats", "rated-track-stats", "Aggregates for a rating band (count, runtime, artists, last rated)", "Me")),
		func(ctx context.Context, in *struct {
			MinRating int16 `query:"min_rating" minimum:"1" maximum:"10" default:"1"`
			MaxRating int16 `query:"max_rating" minimum:"1" maximum:"10" default:"10"`
		}) (*JSONOutput[*sqlc.GetUserRatedTracksStatsRow], error) {
			stats, err := app.GetUserRatedTracksStats(ctx, userFrom(ctx).ID, in.MinRating, in.MaxRating)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(stats), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/ratings/threshold", "get-favorites-threshold", "Where the favorites bar sits on the 1..10 scale", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[ratingBody], error) {
			t, err := app.GetFavoritesThreshold(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(ratingBody{Rating: int(t)}), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/ratings/threshold", "set-favorites-threshold", "Move the favorites threshold (1..10)", "Me")),
		func(ctx context.Context, in *struct {
			Body ratingBody
		}) (*JSONOutput[ratingBody], error) {
			if err := app.SetFavoritesThreshold(ctx, userFrom(ctx).ID, int16(in.Body.Rating)); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(ratingBody{Rating: in.Body.Rating}), nil
		})

}

// Per-entity batch-lookup request bodies. Named types (rather than inline
// structs) so registerRatingRoutes can stay generic over the body shape while
// each entity keeps its own JSON field name.
type trackIDsBody struct {
	TrackIDs []int64 `json:"track_ids" doc:"List of track IDs to look up"`
}

type albumIDsBody struct {
	AlbumIDs []int64 `json:"album_ids" doc:"List of album IDs to look up"`
}

type artistIDsBody struct {
	ArtistIDs []int64 `json:"artist_ids" doc:"List of artist IDs to look up"`
}

// registerLovedList mounts one paginated loved-<entity> listing route.
func registerLovedList[T any](api huma.API, plural string,
	list func(context.Context, int64, int32, int32) (*service.MusicListPage[T], error),
) {
	huma.Register(api, secured(op(http.MethodGet, "/api/me/loved/"+plural, "list-loved-"+plural, "Paginated loved "+plural, "Me")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[*service.MusicListPage[T]], error) {
			limit := defaultPositive(in.Limit, 200)
			page, err := list(ctx, userFrom(ctx).ID, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(page), nil
		})
}

// registerRatingRoutes mounts the identical get/set/batch/list rating quartet
// for one rateable entity (tracks/albums/artists).
func registerRatingRoutes[B any, T any](api huma.API, plural, singular string,
	get func(context.Context, int64, int64) (int16, error),
	set func(context.Context, int64, int64, int16) error,
	batch func(context.Context, int64, []int64) (map[int64]int16, error),
	list func(context.Context, int64, int16, int16, int32, int32) (*service.MusicListPage[T], error),
	idsOf func(B) []int64,
) {
	huma.Register(api, secured(op(http.MethodGet, "/api/me/ratings/"+plural+"/{id}", "get-"+singular+"-rating", "Get user's rating for a "+singular+" (0 when unrated)", "Me")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1"`
		}) (*JSONOutput[ratingBody], error) {
			r, err := get(ctx, userFrom(ctx).ID, in.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(ratingBody{Rating: int(r)}), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/ratings/"+plural+"/{id}", "set-"+singular+"-rating", "Rate a "+singular+" (1..10; 0 clears)", "Me")),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id" minimum:"1"`
			Body ratingBody
		}) (*JSONOutput[ratingBody], error) {
			if err := set(ctx, userFrom(ctx).ID, in.ID, int16(in.Body.Rating)); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(ratingBody{Rating: in.Body.Rating}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/ratings/"+plural+"/batch", "batch-"+singular+"-ratings", "Bulk lookup of "+singular+" ratings", "Me")),
		func(ctx context.Context, in *struct{ Body B }) (*JSONOutput[batchRatingsBody], error) {
			m, err := batch(ctx, userFrom(ctx).ID, idsOf(in.Body))
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			out := make(map[string]int, len(m))
			for id, r := range m {
				out[strconv.FormatInt(id, 10)] = int(r)
			}
			return noStoreJSON(batchRatingsBody{Ratings: out}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/ratings/"+plural, "list-rated-"+plural, "Paginated rated "+plural, "Me")),
		func(ctx context.Context, in *struct {
			MinRating int16 `query:"min_rating" minimum:"1" maximum:"10" default:"1" doc:"Filter to ratings at or above N (1..10)"`
			MaxRating int16 `query:"max_rating" minimum:"1" maximum:"10" default:"10" doc:"Filter to ratings at or below N — [min,max] bands back the Favorites reaction tabs"`
			Pagination
		}) (*JSONOutput[*service.MusicListPage[T]], error) {
			limit := defaultPositive(in.Limit, 100)
			page, err := list(ctx, userFrom(ctx).ID, in.MinRating, in.MaxRating, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(page), nil
		})
}
