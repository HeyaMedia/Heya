package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
)

// Browse tiles for the Music > Browse surfaces. Each variant ({moods, genres,
// tempo}) returns a list of buckets you can drill into via the corresponding
// list-tracks endpoint.

// MoodBucket is one tile on Browse > Moods. Threshold is the score cutoff
// used to count + list tracks for this mood; we echo it so the FE can show
// "tracks scoring ≥ T".
type MoodBucket struct {
	Key        string  `json:"key"`       // e.g. "mood_happy"
	Label      string  `json:"label"`     // e.g. "Happy"
	Threshold  float32 `json:"threshold"` // score cutoff used for the count
	TrackCount int64   `json:"track_count"`
}

// GenreBucket is one tile on Browse > Genres. Genres are hierarchical labels
// from the Discogs-400 classifier ("Electronic---Techno") — we keep the raw
// name for filter pinning and surface a leaf label for display.
type GenreBucket struct {
	Name       string `json:"name"`   // raw hierarchical, e.g. "Electronic---Techno"
	Label      string `json:"label"`  // last segment for display, "Techno"
	Parent     string `json:"parent"` // leading segments joined, "Electronic"
	TrackCount int64  `json:"track_count"`
}

// TempoBucket is one tile on Browse > Tempo. Bands are half-open [Min, Max)
// so adjacent bands partition cleanly.
type TempoBucket struct {
	Key        string  `json:"key"`   // e.g. "120-140"
	Label      string  `json:"label"` // e.g. "Dance (120–140 BPM)"
	MinBPM     float32 `json:"min_bpm"`
	MaxBPM     float32 `json:"max_bpm"`
	TrackCount int64   `json:"track_count"`
}

// moodLabel translates the analyzer's tag name into a UI-friendly label.
// "mood_happy" → "Happy", "danceability" → "Danceable", etc.
var moodLabel = map[sonicanalysis.MoodTagName]string{
	sonicanalysis.MoodDanceability: "Danceable",
	sonicanalysis.MoodVoice:        "Vocal",
	sonicanalysis.MoodHappy:        "Happy",
	sonicanalysis.MoodSad:          "Melancholic",
	sonicanalysis.MoodAggressive:   "Aggressive",
	sonicanalysis.MoodRelaxed:      "Relaxed",
	sonicanalysis.MoodParty:        "Party",
	sonicanalysis.MoodElectronic:   "Electronic",
	sonicanalysis.MoodAcoustic:     "Acoustic",
}

// moodOrder fixes the display order for the Browse > Moods tiles. Picked so
// that the upbeat / high-energy moods are grouped at the start of the wall.
var moodOrder = []sonicanalysis.MoodTagName{
	sonicanalysis.MoodHappy,
	sonicanalysis.MoodParty,
	sonicanalysis.MoodDanceability,
	sonicanalysis.MoodAggressive,
	sonicanalysis.MoodElectronic,
	sonicanalysis.MoodAcoustic,
	sonicanalysis.MoodRelaxed,
	sonicanalysis.MoodSad,
	sonicanalysis.MoodVoice,
}

// tempoBands defines the BPM partition used by Browse > Tempo. Edges land
// on common DJ-style anchors so a user can recognize what each tile means.
var tempoBands = []TempoBucket{
	{Key: "0-90", Label: "Slow", MinBPM: 0, MaxBPM: 90},
	{Key: "90-110", Label: "Midtempo", MinBPM: 90, MaxBPM: 110},
	{Key: "110-130", Label: "House / Pop", MinBPM: 110, MaxBPM: 130},
	{Key: "130-150", Label: "Dance", MinBPM: 130, MaxBPM: 150},
	{Key: "150-300", Label: "Fast (Drum/Bass)", MinBPM: 150, MaxBPM: 300},
}

const (
	// moodThreshold is how confident the classifier needs to be before we count
	// a track for a mood. 0.6 picks tracks that *clearly* lean a given way
	// (we used to use 0.5 but that bled relaxed/happy into each other).
	moodThreshold = 0.6

	// genreScoreFloor is the minimum per-genre confidence for the bucket
	// counter. The classifier emits a few low-prob ghost labels per track;
	// 0.2 keeps the bucket counts honest without truncating real long-tail
	// genres.
	genreScoreFloor   = 0.2
	genreMinTrackHits = 3  // hide genres with fewer than this many tracks
	genreBucketLimit  = 60 // cap the wall length on Browse > Genres
)

// ListMoodBuckets returns one bucket per known mood tag, with the live track
// count for each. Counts use the same threshold the drilldown applies.
func (a *App) ListMoodBuckets(ctx context.Context) ([]MoodBucket, error) {
	q := sqlc.New(a.db)
	out := make([]MoodBucket, 0, len(moodOrder))
	for _, key := range moodOrder {
		count, err := q.CountTracksByMood(ctx, sqlc.CountTracksByMoodParams{
			MoodKey:   string(key),
			Threshold: moodThreshold,
		})
		if err != nil {
			return nil, fmt.Errorf("count mood %q: %w", key, err)
		}
		out = append(out, MoodBucket{
			Key:        string(key),
			Label:      moodLabel[key],
			Threshold:  moodThreshold,
			TrackCount: count,
		})
	}
	return out, nil
}

// ListTracksByMood returns tracks scoring above moodThreshold for the given
// mood key, paginated by score (highest first).
func (a *App) ListTracksByMood(ctx context.Context, moodKey string, limit, offset int32) ([]sqlc.ListTracksByMoodRow, error) {
	if _, ok := moodLabel[sonicanalysis.MoodTagName(moodKey)]; !ok {
		return nil, fmt.Errorf("unknown mood %q", moodKey)
	}
	limit, offset = clampMusicPage(limit, offset)
	rows, err := sqlc.New(a.db).ListTracksByMood(ctx, sqlc.ListTracksByMoodParams{
		MoodKey:     moodKey,
		Threshold:   moodThreshold,
		TrackLimit:  limit,
		TrackOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list tracks for mood %q: %w", moodKey, err)
	}
	return rows, nil
}

// ListGenreBuckets returns the top-N hierarchical genres present in the
// library, sorted by track count.
func (a *App) ListGenreBuckets(ctx context.Context) ([]GenreBucket, error) {
	rows, err := sqlc.New(a.db).ListGenreBuckets(ctx, sqlc.ListGenreBucketsParams{
		MinScore:    genreScoreFloor,
		MinTracks:   genreMinTrackHits,
		BucketLimit: genreBucketLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list genre buckets: %w", err)
	}
	out := make([]GenreBucket, 0, len(rows))
	for _, r := range rows {
		label, parent := splitGenreLabel(r.GenreName)
		out = append(out, GenreBucket{
			Name:       r.GenreName,
			Label:      label,
			Parent:     parent,
			TrackCount: r.TrackCount,
		})
	}
	return out, nil
}

// ListTracksByGenre returns tracks scoring above genreScoreFloor for the
// given hierarchical genre name, paginated by score (highest first).
func (a *App) ListTracksByGenre(ctx context.Context, genre string, limit, offset int32) ([]sqlc.ListTracksByGenreRow, error) {
	if genre == "" {
		return nil, fmt.Errorf("genre is required")
	}
	limit, offset = clampMusicPage(limit, offset)
	rows, err := sqlc.New(a.db).ListTracksByGenre(ctx, sqlc.ListTracksByGenreParams{
		GenreName:   genre,
		MinScore:    genreScoreFloor,
		TrackLimit:  limit,
		TrackOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list tracks for genre %q: %w", genre, err)
	}
	return rows, nil
}

// ListTempoBuckets returns the fixed BPM partition with a live count per band.
func (a *App) ListTempoBuckets(ctx context.Context) ([]TempoBucket, error) {
	q := sqlc.New(a.db)
	out := make([]TempoBucket, 0, len(tempoBands))
	for _, b := range tempoBands {
		count, err := q.CountTracksByTempoBand(ctx, sqlc.CountTracksByTempoBandParams{
			MinBpm: b.MinBPM,
			MaxBpm: b.MaxBPM,
		})
		if err != nil {
			return nil, fmt.Errorf("count tempo band %s: %w", b.Key, err)
		}
		b.TrackCount = count
		out = append(out, b)
	}
	return out, nil
}

// LookupTempoBand resolves a band key (e.g. "110-130") to its half-open
// [min, max) BPM bounds. Returns false for unknown keys so the handler can
// 404 cleanly instead of returning a silently-empty list.
func (a *App) LookupTempoBand(key string) (min, max float32, ok bool) {
	for _, b := range tempoBands {
		if b.Key == key {
			return b.MinBPM, b.MaxBPM, true
		}
	}
	return 0, 0, false
}

// ListTracksByTempoBand returns tracks whose BPM falls in [min, max),
// ordered by ascending BPM.
func (a *App) ListTracksByTempoBand(ctx context.Context, min, max float32, limit, offset int32) ([]sqlc.ListTracksByTempoBandRow, error) {
	limit, offset = clampMusicPage(limit, offset)
	rows, err := sqlc.New(a.db).ListTracksByTempoBand(ctx, sqlc.ListTracksByTempoBandParams{
		MinBpm:      min,
		MaxBpm:      max,
		TrackLimit:  limit,
		TrackOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list tracks for tempo band [%g,%g): %w", min, max, err)
	}
	return rows, nil
}

// splitGenreLabel turns "Electronic---Techno" into ("Techno", "Electronic").
// Single-segment names get an empty parent.
func splitGenreLabel(name string) (label, parent string) {
	const sep = "---"
	// Find the last separator; everything after is the leaf label, everything
	// before becomes the parent path (still hierarchical, just for display).
	idx := -1
	for i := len(name) - len(sep); i >= 0; i-- {
		if name[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return name, ""
	}
	return name[idx+len(sep):], name[:idx]
}
