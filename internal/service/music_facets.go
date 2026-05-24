package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/pgvector/pgvector-go"
)

// FacetsView is the read shape of one track's analyzed facets. Maps
// the wire-friendly JSON over the raw sqlc.TrackFacet (which carries
// pgvector / pgtype wrappers).
//
// Loudness is intentionally absent — those values live on
// track_files (per-file) and albums (per-album-mode), populated by
// ScanTrackLoudnessWorker at probe time. The UI's LUFS chip reads
// from the track-detail / album-detail endpoints which already
// expose them. Don't reintroduce duplicate columns here.
type FacetsView struct {
	TrackID         int64                      `json:"track_id"`
	BPM             *float32                   `json:"bpm,omitempty"`
	BPMConfidence   *float32                   `json:"bpm_confidence,omitempty"`
	Key             *KeyView                   `json:"key,omitempty"`
	TopGenres       []sonicanalysis.GenreScore `json:"top_genres,omitempty"`
	MoodTags        sonicanalysis.MoodScores   `json:"mood_tags,omitempty"`
	AnalyzedAt      string                     `json:"analyzed_at,omitempty"`
	AnalyzerVersion int32                      `json:"analyzer_version"`
}

// KeyView pairs Camelot wheel notation with the major/minor display
// form so the UI can render either without recomputing.
type KeyView struct {
	Root    string  `json:"root"`    // "G", "F#", ...
	Mode    string  `json:"mode"`    // "major" or "minor"
	Display string  `json:"display"` // "G major"
	Camelot string  `json:"camelot"` // "9B" for G major
	Clarity float32 `json:"clarity"`
}

// TrackResult is one row of a similarity / search response.
type TrackResult struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	AlbumID  int64   `json:"album_id"`
	ArtistID int64   `json:"artist_id"`
	FilePath string  `json:"file_path"`
	Distance float32 `json:"distance"`
}

// ArtistResult / AlbumResult mirror their sqlc.Similar* rows.
type ArtistResult struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	MediaItemID int64   `json:"media_item_id"`
	MediaSlug   string  `json:"media_slug"`
	Distance    float32 `json:"distance"`
}

type AlbumResult struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	ArtistID int64   `json:"artist_id"`
	Slug     string  `json:"slug"`
	Distance float32 `json:"distance"`
}

// ErrNoFacets is returned when the requested track has no
// track_facets row yet (analysis pending).
var ErrNoFacets = errors.New("track has no analyzed facets yet")

// TrackFacets returns a track's FacetsView. ErrNoFacets if the track
// hasn't been analyzed yet.
func (a *App) TrackFacets(ctx context.Context, trackID int64) (*FacetsView, error) {
	row, err := sqlc.New(a.db).GetTrackFacets(ctx, trackID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoFacets
		}
		return nil, fmt.Errorf("get track facets: %w", err)
	}
	return facetsViewFromRow(row), nil
}

// TrackWaveform returns just the 2000-bucket waveform for a track,
// suitable for direct JSON serialization to the playbar.
func (a *App) TrackWaveform(ctx context.Context, trackID int64) ([]float32, error) {
	wf, err := sqlc.New(a.db).GetTrackWaveform(ctx, trackID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoFacets
		}
		return nil, fmt.Errorf("get waveform: %w", err)
	}
	return wf, nil
}

// SimilarMusicTracks returns the top-N most sonically similar tracks
// to the given seed track. Uses the seed's track_embedding for KNN.
func (a *App) SimilarMusicTracks(ctx context.Context, seedTrackID int64, limit int32) ([]TrackResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetTrackFacets(ctx, seedTrackID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoFacets
		}
		return nil, fmt.Errorf("seed facets: %w", err)
	}
	rows, err := q.SimilarTracksByTrack(ctx, sqlc.SimilarTracksByTrackParams{
		TrackEmbedding: seed.TrackEmbedding,
		TrackID:        seedTrackID,
		Limit:          limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar tracks: %w", err)
	}
	out := make([]TrackResult, len(rows))
	for i, r := range rows {
		out[i] = TrackResult{
			ID: r.ID, Title: r.Title, AlbumID: r.AlbumID, ArtistID: r.ArtistID,
			FilePath: r.FilePath, Distance: r.Distance,
		}
	}
	return out, nil
}

// SimilarMusicArtists returns the top-N most sonically similar
// artists to the given seed artist. Uses the artist centroid for KNN.
func (a *App) SimilarMusicArtists(ctx context.Context, seedArtistID int64, limit int32) ([]ArtistResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetArtistCentroid(ctx, seedArtistID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoFacets
		}
		return nil, fmt.Errorf("seed centroid: %w", err)
	}
	rows, err := q.SimilarArtists(ctx, sqlc.SimilarArtistsParams{
		SonicCentroid: seed.SonicCentroid,
		ArtistID:      seedArtistID,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar artists: %w", err)
	}
	out := make([]ArtistResult, len(rows))
	for i, r := range rows {
		out[i] = ArtistResult{
			ID: r.ID, Name: r.Name, MediaItemID: r.MediaItemID,
			MediaSlug: r.MediaSlug, Distance: r.Distance,
		}
	}
	return out, nil
}

// SimilarMusicAlbums returns the top-N most sonically similar albums
// to the given seed album. Uses the album centroid for KNN.
func (a *App) SimilarMusicAlbums(ctx context.Context, seedAlbumID int64, limit int32) ([]AlbumResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetAlbumCentroid(ctx, seedAlbumID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoFacets
		}
		return nil, fmt.Errorf("seed centroid: %w", err)
	}
	rows, err := q.SimilarAlbums(ctx, sqlc.SimilarAlbumsParams{
		SonicCentroid: seed.SonicCentroid,
		AlbumID:       seedAlbumID,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar albums: %w", err)
	}
	out := make([]AlbumResult, len(rows))
	for i, r := range rows {
		out[i] = AlbumResult{
			ID: r.ID, Title: r.Title, ArtistID: r.ArtistID, Slug: r.Slug, Distance: r.Distance,
		}
	}
	return out, nil
}

// SearchMusicByText runs a CLAP text→audio KNN over all analyzed
// tracks. Returns up to `limit` tracks ordered by cosine ascending
// (lower distance = better match).
func (a *App) SearchMusicByText(ctx context.Context, text string, limit int32) ([]TrackResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("search text is empty")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if a.textSearcher == nil {
		return nil, sonicanalysis.ErrTextSearcherUnavailable
	}
	embed, err := a.textSearcher.Embed(text)
	if err != nil {
		return nil, fmt.Errorf("clap text embed: %w", err)
	}
	rows, err := sqlc.New(a.db).SimilarTracksByText(ctx, sqlc.SimilarTracksByTextParams{
		TextEmbedding: pgvector.NewVector(embed),
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("text search: %w", err)
	}
	out := make([]TrackResult, len(rows))
	for i, r := range rows {
		out[i] = TrackResult{
			ID: r.ID, Title: r.Title, AlbumID: r.AlbumID, ArtistID: r.ArtistID,
			FilePath: r.FilePath, Distance: r.Distance,
		}
	}
	return out, nil
}

// facetsViewFromRow maps the raw sqlc row to a JSON-friendly view,
// unpacking the pgtype.Null* wrappers and parsing the JSONB blobs.
func facetsViewFromRow(r sqlc.TrackFacet) *FacetsView {
	v := &FacetsView{
		TrackID:         r.TrackID,
		AnalyzerVersion: r.AnalyzerVersion,
	}
	if r.AnalyzedAt.Valid {
		v.AnalyzedAt = r.AnalyzedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	if r.Bpm.Valid {
		bpm := r.Bpm.Float32
		v.BPM = &bpm
	}
	if r.BpmConfidence.Valid {
		c := r.BpmConfidence.Float32
		v.BPMConfidence = &c
	}
	if r.KeyRoot.Valid && r.KeyMode.Valid {
		key := sonicanalysis.Key{
			Root: sonicanalysis.PitchClass(r.KeyRoot.Int16),
			Mode: sonicanalysis.KeyMode(r.KeyMode.Int16),
		}
		view := &KeyView{
			Root:    key.Root.String(),
			Mode:    key.Mode.String(),
			Display: key.String(),
			Camelot: key.CamelotCode(),
		}
		if r.KeyClarity.Valid {
			view.Clarity = r.KeyClarity.Float32
		}
		v.Key = view
	}
	if len(r.TopGenres) > 0 {
		var gs []sonicanalysis.GenreScore
		if err := json.Unmarshal(r.TopGenres, &gs); err == nil {
			v.TopGenres = gs
		}
	}
	if len(r.MoodTags) > 0 {
		mood := make(sonicanalysis.MoodScores)
		if err := json.Unmarshal(r.MoodTags, &mood); err == nil {
			v.MoodTags = mood
		}
	}
	return v
}
