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

// Sonic-similarity / search result row types are sourced directly from the
// sqlc rich-row generated types — see music_facets / music_radio service
// methods. We used to wrap them in service-layer mirror structs but the rich
// rows already carry slugs + album/artist context the FE needs, so the
// extra indirection just rotted with every shape change.

// ErrNoFacets is returned when the requested track has no
// track_facets row yet (analysis pending).
var ErrNoFacets = errors.New("track has no analyzed facets yet")

// noFacetsErr maps a missing seed row (pgx.ErrNoRows) to ErrNoFacets so
// handlers can 404; anything else is wrapped with the given context.
func noFacetsErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoFacets
	}
	return fmt.Errorf("%s: %w", what, err)
}

// TrackFacets returns a track's FacetsView. ErrNoFacets if the track
// hasn't been analyzed yet.
func (a *App) TrackFacets(ctx context.Context, trackID int64) (*FacetsView, error) {
	row, err := sqlc.New(a.db).GetTrackFacets(ctx, trackID)
	if err != nil {
		return nil, noFacetsErr(err, "get track facets")
	}
	return facetsViewFromRow(row), nil
}

// TrackWaveform returns just the 2000-bucket waveform for a track,
// suitable for direct JSON serialization to the playbar.
func (a *App) TrackWaveform(ctx context.Context, trackID int64) ([]float32, error) {
	return a.ensureTrackWaveform(ctx, trackID)
}

func (a *App) ensureTrackWaveform(ctx context.Context, trackID int64) ([]float32, error) {
	q := sqlc.New(a.db)
	if wf, err := q.GetTrackWaveform(ctx, trackID); err == nil && len(wf) > 0 {
		return wf, nil
	}

	value, err, _ := a.waveformScan.Do(fmt.Sprintf("%d", trackID), func() (any, error) {
		// Re-check after joining the singleflight; another request or the Sonic
		// worker may have persisted it while this caller was waiting.
		if wf, getErr := q.GetTrackWaveform(ctx, trackID); getErr == nil && len(wf) > 0 {
			return wf, nil
		}
		row, getErr := q.GetTrackForAnalysis(ctx, trackID)
		if getErr != nil {
			return nil, noFacetsErr(getErr, "resolve waveform source")
		}
		wf, computeErr := sonicanalysis.ComputeWaveform(ctx, row.FilePath)
		if computeErr != nil {
			return nil, fmt.Errorf("compute waveform: %w", computeErr)
		}
		if persistErr := q.UpsertTrackWaveform(ctx, sqlc.UpsertTrackWaveformParams{TrackID: trackID, Waveform: wf}); persistErr != nil {
			return nil, fmt.Errorf("persist waveform: %w", persistErr)
		}
		return wf, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]float32), nil
}

// SimilarMusicTracks returns the top-N most sonically similar tracks to the
// given seed track. Uses the seed's track_embedding for KNN. Returns the
// rich row shape (with album+artist context) so the FE doesn't need to
// resolve slugs separately to play / link the result.
func (a *App) SimilarMusicTracks(ctx context.Context, seedTrackID int64, limit int32) ([]sqlc.SimilarTracksByTrackRichRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetTrackFacets(ctx, seedTrackID)
	if err != nil {
		return nil, noFacetsErr(err, "seed facets")
	}
	rows, err := q.SimilarTracksByTrackRich(ctx, sqlc.SimilarTracksByTrackRichParams{
		TrackEmbedding: seed.TrackEmbedding,
		ExcludeIds:     []int64{seedTrackID},
		TrackLimit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar tracks: %w", err)
	}
	return rows, nil
}

// BPM tolerance (± seconds-per-beat) for the DJ-mix endpoint. ±5 is a
// common comfortable mix range for DJs — within this a typical 2-deck setup
// can pitch-match without obvious audio artifacts. Constant rather than a
// per-request parameter because exposing it lets callers ask for absurdly
// wide windows that defeat the "harmonically compatible" framing.
const djMixBPMTolerance = 5.0

// BuildDJMix returns harmonically-compatible tracks for the seed: same
// Camelot wheel position, the relative key (A↔B), or ±1 wheel positions,
// all within ±djMixBPMTolerance BPM, ordered by embedding distance.
//
// Different from Instant Radio: radio expands by sonic similarity alone,
// mix-to constrains to keys that will sound good back-to-back in a DJ set.
func (a *App) BuildDJMix(ctx context.Context, seedTrackID int64, limit int32) ([]sqlc.MixToTracksRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := sqlc.New(a.db)
	seed, err := q.GetTrackFacets(ctx, seedTrackID)
	if err != nil {
		return nil, noFacetsErr(err, "seed facets")
	}
	if !seed.Bpm.Valid || !seed.KeyRoot.Valid || !seed.KeyMode.Valid {
		return nil, fmt.Errorf("seed track is missing bpm or key — cannot mix")
	}

	seedKey := sonicanalysis.Key{
		Root: sonicanalysis.PitchClass(seed.KeyRoot.Int16),
		Mode: sonicanalysis.KeyMode(seed.KeyMode.Int16),
	}
	compatible := seedKey.CompatibleKeys()
	if len(compatible) == 0 {
		return nil, fmt.Errorf("seed key out of range")
	}
	codes := make([]int32, 0, len(compatible))
	for _, k := range compatible {
		codes = append(codes, int32(k.Root)*2+int32(k.Mode))
	}

	rows, err := q.MixToTracks(ctx, sqlc.MixToTracksParams{
		TrackEmbedding: seed.TrackEmbedding,
		BpmMin:         seed.Bpm.Float32 - djMixBPMTolerance,
		BpmMax:         seed.Bpm.Float32 + djMixBPMTolerance,
		KeyCodes:       codes,
		ExcludeIds:     []int64{seedTrackID},
		TrackLimit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("mix-to: %w", err)
	}
	return rows, nil
}

// SimilarMusicArtists returns the top-N most sonically similar artists to the
// given seed artist. Row already carries `media_slug` for the FE.
func (a *App) SimilarMusicArtists(ctx context.Context, seedArtistID int64, limit int32) ([]sqlc.SimilarArtistsRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetArtistCentroid(ctx, seedArtistID)
	if err != nil {
		return nil, noFacetsErr(err, "seed centroid")
	}
	rows, err := q.SimilarArtists(ctx, sqlc.SimilarArtistsParams{
		SonicCentroid: seed.SonicCentroid,
		ArtistID:      seedArtistID,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar artists: %w", err)
	}
	return rows, nil
}

// SimilarMusicAlbums returns the top-N most sonically similar albums to the
// given seed album. Row carries artist_slug + album_slug so the FE can link
// to the album detail / cover endpoints directly.
func (a *App) SimilarMusicAlbums(ctx context.Context, seedAlbumID int64, limit int32) ([]sqlc.SimilarAlbumsRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := sqlc.New(a.db)
	seed, err := q.GetAlbumCentroid(ctx, seedAlbumID)
	if err != nil {
		return nil, noFacetsErr(err, "seed centroid")
	}
	rows, err := q.SimilarAlbums(ctx, sqlc.SimilarAlbumsParams{
		SonicCentroid: seed.SonicCentroid,
		AlbumID:       seedAlbumID,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("similar albums: %w", err)
	}
	return rows, nil
}

// SearchMusicByText runs a CLAP text→audio KNN over all analyzed
// tracks. Returns the rich row shape (album+artist context with slugs)
// so search results can link / play without follow-up lookups.
func (a *App) SearchMusicByText(ctx context.Context, text string, limit int32) ([]sqlc.SimilarTracksByTextRichRow, error) {
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
	rows, err := sqlc.New(a.db).SimilarTracksByTextRich(ctx, sqlc.SimilarTracksByTextRichParams{
		TextEmbedding: pgvector.NewVector(embed),
		TrackLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("text search: %w", err)
	}
	return rows, nil
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
