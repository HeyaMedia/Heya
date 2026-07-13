package server

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

type ratingBody struct {
	Rating int `json:"rating" minimum:"0" maximum:"10"`
}

type batchRatingsBody struct {
	Ratings map[string]int `json:"ratings" doc:"Map of track_id (as string) → rating 1..10. Tracks the user hasn't rated are omitted entirely."`
}

type lovedBody struct {
	Loved bool `json:"loved"`
}

type topTracksBody struct {
	Items []service.ArtistTopTrackRow `json:"items"`
}

type idsBody struct {
	IDs []int64 `json:"ids"`
}

type playlistsListBody struct {
	Items []sqlc.ListUserPlaylistsRow `json:"items"`
}

type playlistMutation struct {
	Name        string   `json:"name" minLength:"1" maxLength:"128" example:"Friday focus"`
	Description string   `json:"description" maxLength:"2000" example:"Deep work soundtrack"`
	Cover       string   `json:"cover_path" maxLength:"512" doc:"Optional path/URL to a custom cover image"`
	Tags        []string `json:"tags,omitempty" maxItems:"16" doc:"Free-form organization tags; omit to keep existing"`
}

// playlistCoverUploadForm declares the multipart/form-data schema for the
// playlist-cover upload endpoint — mirrors uploadAssetForm's `file` field
// (metadata_editor_huma.go). No extra fields: unlike media assets, a
// playlist has exactly one cover slot, so there's no asset_type to select.
type playlistCoverUploadForm struct {
	File huma.FormFile `form:"file" contentType:"image/*" required:"true"`
}

type waveformBody struct {
	Waveform any `json:"waveform"`
}

// Typed response envelopes for the sonic-similarity + sonic-search endpoints.
// Each one is just `{ items: [...] }` but with the row type spelled out so the
// generated TS client preserves the shape instead of falling back to `any`.
// Bodies use sqlc-generated row types directly — the rich queries already
// carry slugs + album/artist context, so an extra mirror struct would just
// duplicate the field list.
type trackResultsBody struct {
	Items []sqlc.SimilarTracksByTrackRichRow `json:"items"`
}

type trackTextSearchBody struct {
	Items []sqlc.SimilarTracksByTextRichRow `json:"items"`
}

type artistResultsBody struct {
	Items []sqlc.SimilarArtistsRow `json:"items"`
}

type albumResultsBody struct {
	Items []sqlc.SimilarAlbumsRow `json:"items"`
}

// mixToBody is the typed envelope for /api/music/tracks/{id}/mix-to. Shape
// matches the other similarity endpoints (`{items: [...]}`) so the FE row
// component can render any of them.
type mixToBody struct {
	Items []sqlc.MixToTracksRow `json:"items"`
}

// Browse-by-facet envelopes. Each tile-list endpoint returns one of these;
// the row types are owned by the service package so the FE sees the full
// shape via the generated TS client.
type moodBucketsBody struct {
	Items []service.MoodBucket `json:"items"`
}
type genreBucketsBody struct {
	Items []service.GenreBucket `json:"items"`
}
type tempoBucketsBody struct {
	Items []service.TempoBucket `json:"items"`
}

// Track drilldown bodies carry Total so the FE can size a full-length
// virtual scroll track and random-access any page.
type moodTracksBody struct {
	Items []sqlc.ListTracksByMoodRow `json:"items"`
	Total int64                      `json:"total"`
}
type genreTracksBody struct {
	Items []sqlc.ListTracksByGenreRow `json:"items"`
	Total int64                       `json:"total"`
}
type tempoTracksBody struct {
	Items []sqlc.ListTracksByTempoBandRow `json:"items"`
	Total int64                           `json:"total"`
}

type recentlyPlayedBody struct {
	Items []sqlc.ListRecentlyPlayedTracksRow `json:"items"`
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
