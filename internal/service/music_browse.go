package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"golang.org/x/sync/errgroup"
)

// Browse tiles for the Music > Browse surfaces. Each variant ({moods, genres,
// tempo}) returns a list of buckets you can drill into via the corresponding
// list-tracks endpoint.

// BrowseBucketArtist is one entry in a bucket's per-tile artist strip — just
// enough identity to build an image URL and a link. Shape matches the
// MediaImageRef the FE's usePosterUrl/useImageUrl composables expect ({id,
// public_id}), so a bucket's Artists entries plug straight into those
// without a mapping step.
type BrowseBucketArtist struct {
	ID       int64  `json:"id"`
	PublicID string `json:"public_id"`
}

// browseTopArtistLimit caps how many per-bucket artist avatars the browse
// tiles carry — enough for a small stacked/collage strip, not a full list.
const browseTopArtistLimit = 6

// MoodBucket is one tile on Browse > Moods. Threshold is the score cutoff
// used to count + list tracks for this mood; we echo it so the FE can show
// "tracks scoring ≥ T".
type MoodBucket struct {
	Key        string               `json:"key"`       // e.g. "mood_happy"
	Label      string               `json:"label"`     // e.g. "Happy"
	Threshold  float32              `json:"threshold"` // score cutoff used for the count
	TrackCount int64                `json:"track_count"`
	Artists    []BrowseBucketArtist `json:"artists"`
}

// GenreBucket is one tile on Browse > Genres. Genres are hierarchical labels
// from the Discogs-400 classifier ("Electronic---Techno") — we keep the raw
// name for filter pinning and surface a leaf label for display.
type GenreBucket struct {
	Name       string               `json:"name"`   // raw hierarchical, e.g. "Electronic---Techno"
	Label      string               `json:"label"`  // last segment for display, "Techno"
	Parent     string               `json:"parent"` // leading segments joined, "Electronic"
	TrackCount int64                `json:"track_count"`
	Artists    []BrowseBucketArtist `json:"artists"`
}

// TempoBucket is one tile on Browse > Tempo. Bands are half-open [Min, Max)
// so adjacent bands partition cleanly.
type TempoBucket struct {
	Key        string               `json:"key"`   // e.g. "120-140"
	Label      string               `json:"label"` // e.g. "Dance (120–140 BPM)"
	MinBPM     float32              `json:"min_bpm"`
	MaxBPM     float32              `json:"max_bpm"`
	TrackCount int64                `json:"track_count"`
	Artists    []BrowseBucketArtist `json:"artists"`
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
// count and top artists for each. Counts use the same threshold the
// drilldown applies. The bucket counts and the per-bucket top-artist rankings
// are independent queries over the same rows, so they run concurrently —
// exactly 2 queries total, not one pair per mood.
func (a *App) ListMoodBuckets(ctx context.Context) ([]MoodBucket, error) {
	q := sqlc.New(a.db)
	keys := make([]string, len(moodOrder))
	for i, key := range moodOrder {
		keys[i] = string(key)
	}

	var countRows []sqlc.CountTracksByMoodsRow
	var artistRows []sqlc.TopArtistsByMoodRow
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		rows, err := q.CountTracksByMoods(gctx, sqlc.CountTracksByMoodsParams{
			MoodKeys:  keys,
			Threshold: moodThreshold,
		})
		if err != nil {
			return fmt.Errorf("count mood buckets: %w", err)
		}
		countRows = rows
		return nil
	})
	g.Go(func() error {
		rows, err := q.TopArtistsByMood(gctx, sqlc.TopArtistsByMoodParams{
			MoodKeys:  keys,
			Threshold: moodThreshold,
			TopN:      browseTopArtistLimit,
		})
		if err != nil {
			return fmt.Errorf("top artists by mood: %w", err)
		}
		artistRows = rows
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	counts := make(map[string]int64, len(countRows))
	for _, r := range countRows {
		counts[r.BucketKey] = r.TrackCount
	}
	artists := make(map[string][]BrowseBucketArtist, len(moodOrder))
	for _, r := range artistRows {
		artists[r.BucketKey] = append(artists[r.BucketKey], BrowseBucketArtist{
			ID:       r.MediaItemID,
			PublicID: r.MediaItemPublicID.String(),
		})
	}

	out := make([]MoodBucket, 0, len(moodOrder))
	for _, key := range moodOrder {
		out = append(out, MoodBucket{
			Key:        string(key),
			Label:      moodLabel[key],
			Threshold:  moodThreshold,
			TrackCount: counts[string(key)],
			Artists:    orEmptyBrowseArtists(artists[string(key)]),
		})
	}
	return out, nil
}

// orEmptyBrowseArtists normalizes a nil artist slice to an empty one so the
// JSON field is always `[]`, never `null`.
func orEmptyBrowseArtists(artists []BrowseBucketArtist) []BrowseBucketArtist {
	if artists == nil {
		return []BrowseBucketArtist{}
	}
	return artists
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

// CountTracksForMood returns the total distinct recordings above the mood
// threshold — sizes the drilldown's virtual scroll track.
func (a *App) CountTracksForMood(ctx context.Context, moodKey string) (int64, error) {
	if _, ok := moodLabel[sonicanalysis.MoodTagName(moodKey)]; !ok {
		return 0, fmt.Errorf("unknown mood %q", moodKey)
	}
	return sqlc.New(a.db).CountTracksByMood(ctx, sqlc.CountTracksByMoodParams{
		MoodKey:   moodKey,
		Threshold: moodThreshold,
	})
}

// ListMoodTracksPage is the paginated-envelope flavor of ListTracksByMood for
// the /api/music/browse/moods/{mood}/tracks handler — list and count run
// concurrently via musicPage instead of the handler awaiting them in series.
func (a *App) ListMoodTracksPage(ctx context.Context, moodKey string, limit, offset int32) (*MusicListPage[sqlc.ListTracksByMoodRow], error) {
	return musicPage(limit, offset, fmt.Sprintf("listing tracks for mood %q", moodKey),
		func(limit, offset int32) ([]sqlc.ListTracksByMoodRow, error) {
			return a.ListTracksByMood(ctx, moodKey, limit, offset)
		},
		func() (int64, error) { return a.CountTracksForMood(ctx, moodKey) })
}

// CountTracksForGenre is the total-count counterpart of ListTracksByGenre.
func (a *App) CountTracksForGenre(ctx context.Context, genre string) (int64, error) {
	if genre == "" {
		return 0, fmt.Errorf("genre is required")
	}
	q := sqlc.New(a.db)
	if canonical, ok := sonicanalysis.CanonicalGenreName(genre); ok {
		return q.CountTracksByGenre(ctx, sqlc.CountTracksByGenreParams{
			GenreName: canonical,
			MinScore:  genreScoreFloor,
		})
	}
	albumIDs, err := q.ListMetadataGenreAlbumIDs(ctx, genre)
	if err != nil {
		return 0, fmt.Errorf("resolve albums for metadata genre %q: %w", genre, err)
	}
	if len(albumIDs) == 0 {
		return 0, nil
	}
	return q.CountTracksByMetadataGenre(ctx, albumIDs)
}

// ListGenreTracksPage is the paginated-envelope flavor of ListTracksByGenre
// for the /api/music/browse/genres/{name}/tracks handler — list and count
// run concurrently via musicPage instead of the handler awaiting them in
// series.
func (a *App) ListGenreTracksPage(ctx context.Context, genre string, limit, offset int32) (*MusicListPage[sqlc.ListTracksByGenreRow], error) {
	return musicPage(limit, offset, fmt.Sprintf("listing tracks for genre %q", genre),
		func(limit, offset int32) ([]sqlc.ListTracksByGenreRow, error) {
			return a.ListTracksByGenre(ctx, genre, limit, offset)
		},
		func() (int64, error) { return a.CountTracksForGenre(ctx, genre) })
}

// CountTracksForTempoBand is the total-count counterpart of ListTracksByTempoBand.
func (a *App) CountTracksForTempoBand(ctx context.Context, min, max float32) (int64, error) {
	return sqlc.New(a.db).CountTracksByTempoBand(ctx, sqlc.CountTracksByTempoBandParams{
		MinBpm: min,
		MaxBpm: max,
	})
}

// ListTempoTracksPage is the paginated-envelope flavor of
// ListTracksByTempoBand for the /api/music/browse/tempo/{band}/tracks
// handler — list and count run concurrently via musicPage instead of the
// handler awaiting them in series. Takes resolved [min, max) bounds rather
// than a band key so the handler keeps owning the 404-on-unknown-band check.
func (a *App) ListTempoTracksPage(ctx context.Context, min, max float32, limit, offset int32) (*MusicListPage[sqlc.ListTracksByTempoBandRow], error) {
	return musicPage(limit, offset, fmt.Sprintf("listing tracks for tempo band [%g,%g)", min, max),
		func(limit, offset int32) ([]sqlc.ListTracksByTempoBandRow, error) {
			return a.ListTracksByTempoBand(ctx, min, max, limit, offset)
		},
		func() (int64, error) { return a.CountTracksForTempoBand(ctx, min, max) })
}

// ListGenreBuckets returns the top-N hierarchical genres present in the
// library, sorted by track count, each with its top artists. The bucket list
// and the top-artist ranking are independent queries (the artist query isn't
// pre-filtered to the bucket list's top-N names — that selection isn't known
// until the bucket query returns), so they run concurrently and are joined
// afterward — exactly 2 queries total.
func (a *App) ListGenreBuckets(ctx context.Context) ([]GenreBucket, error) {
	q := sqlc.New(a.db)
	var bucketRows []sqlc.ListGenreBucketsRow
	var artistRows []sqlc.TopArtistsByGenresRow
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		rows, err := q.ListGenreBuckets(gctx, sqlc.ListGenreBucketsParams{
			MinScore:    genreScoreFloor,
			MinTracks:   genreMinTrackHits,
			BucketLimit: genreBucketLimit,
		})
		if err != nil {
			return fmt.Errorf("list genre buckets: %w", err)
		}
		bucketRows = rows
		return nil
	})
	g.Go(func() error {
		rows, err := q.TopArtistsByGenres(gctx, sqlc.TopArtistsByGenresParams{
			MinScore: genreScoreFloor,
			TopN:     browseTopArtistLimit,
		})
		if err != nil {
			return fmt.Errorf("top artists by genre: %w", err)
		}
		artistRows = rows
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	artists := make(map[string][]BrowseBucketArtist, len(bucketRows))
	for _, r := range artistRows {
		artists[r.GenreName] = append(artists[r.GenreName], BrowseBucketArtist{
			ID:       r.MediaItemID,
			PublicID: r.MediaItemPublicID.String(),
		})
	}

	out := make([]GenreBucket, 0, len(bucketRows))
	for _, r := range bucketRows {
		label, parent := splitGenreLabel(r.GenreName)
		out = append(out, GenreBucket{
			Name:       r.GenreName,
			Label:      label,
			Parent:     parent,
			TrackCount: r.TrackCount,
			Artists:    orEmptyBrowseArtists(artists[r.GenreName]),
		})
	}
	return out, nil
}

// ListTracksByGenre returns the tracks for one genre drilldown, paginated.
// Two disjoint genre vocabularies feed this page: names in the Discogs-400
// classifier vocabulary (the Browse > Genres tiles, "Electronic---Techno")
// query track_facets.top_genres by score; anything else is treated as a
// metadata genre/tag (the artist-hero chips, "melodic metalcore") and
// matches the artists/albums tag arrays instead — those chips used to land
// on a permanently-empty page because only the sonic bucket was consulted.
func (a *App) ListTracksByGenre(ctx context.Context, genre string, limit, offset int32) ([]sqlc.ListTracksByGenreRow, error) {
	if genre == "" {
		return nil, fmt.Errorf("genre is required")
	}
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)
	if canonical, ok := sonicanalysis.CanonicalGenreName(genre); ok {
		rows, err := q.ListTracksByGenre(ctx, sqlc.ListTracksByGenreParams{
			GenreName:   canonical,
			MinScore:    genreScoreFloor,
			TrackLimit:  limit,
			TrackOffset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list tracks for genre %q: %w", genre, err)
		}
		return rows, nil
	}
	albumIDs, err := q.ListMetadataGenreAlbumIDs(ctx, genre)
	if err != nil {
		return nil, fmt.Errorf("resolve albums for metadata genre %q: %w", genre, err)
	}
	if len(albumIDs) == 0 {
		return []sqlc.ListTracksByGenreRow{}, nil
	}
	rows, err := q.ListTracksByMetadataGenre(ctx, sqlc.ListTracksByMetadataGenreParams{
		AlbumIds:    albumIDs,
		TrackLimit:  limit,
		TrackOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list tracks for metadata genre %q: %w", genre, err)
	}
	out := make([]sqlc.ListTracksByGenreRow, len(rows))
	for i, r := range rows {
		out[i] = sqlc.ListTracksByGenreRow{
			TrackID:        r.TrackID,
			TrackTitle:     r.TrackTitle,
			Duration:       r.Duration,
			DiscNumber:     r.DiscNumber,
			TrackNumber:    r.TrackNumber,
			AlbumID:        r.AlbumID,
			AlbumTitle:     r.AlbumTitle,
			AlbumSlug:      r.AlbumSlug,
			AlbumCoverPath: r.AlbumCoverPath,
			AlbumYear:      r.AlbumYear,
			ArtistID:       r.ArtistID,
			ArtistName:     r.ArtistName,
			ArtistSlug:     r.ArtistSlug,
			Score:          1,
		}
	}
	return out, nil
}

// tempoBandEdges reads the 3 interior boundaries out of tempoBands (the
// exterior min/max come along for free) so the collapsed
// CountTracksByTempoBands / TopArtistsByTempoBands queries stay in lockstep
// with the Go-side band definitions instead of duplicating the literals.
// tempoBands is a fixed 5-band partition (see its declaration above); this
// panics loudly if that ever changes shape instead of silently miscounting.
func tempoBandEdges() (edge1, edge2, edge3, edge4, minBPM, maxBPM float32) {
	if len(tempoBands) != 5 {
		panic(fmt.Sprintf("tempoBandEdges: expected 5 tempo bands, got %d", len(tempoBands)))
	}
	return tempoBands[1].MinBPM, tempoBands[2].MinBPM, tempoBands[3].MinBPM, tempoBands[4].MinBPM,
		tempoBands[0].MinBPM, tempoBands[4].MaxBPM
}

// ListTempoBuckets returns the fixed BPM partition with a live count and top
// artists per band. The band counts and the per-band top-artist ranking are
// independent queries, so they run concurrently — exactly 2 queries total,
// not one pair per band.
func (a *App) ListTempoBuckets(ctx context.Context) ([]TempoBucket, error) {
	q := sqlc.New(a.db)
	edge1, edge2, edge3, edge4, minBPM, maxBPM := tempoBandEdges()

	var countRows []sqlc.CountTracksByTempoBandsRow
	var artistRows []sqlc.TopArtistsByTempoBandsRow
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		rows, err := q.CountTracksByTempoBands(gctx, sqlc.CountTracksByTempoBandsParams{
			Edge1: edge1, Edge2: edge2, Edge3: edge3, Edge4: edge4,
			MinBpm: minBPM, MaxBpm: maxBPM,
		})
		if err != nil {
			return fmt.Errorf("count tempo bands: %w", err)
		}
		countRows = rows
		return nil
	})
	g.Go(func() error {
		rows, err := q.TopArtistsByTempoBands(gctx, sqlc.TopArtistsByTempoBandsParams{
			Edge1: edge1, Edge2: edge2, Edge3: edge3, Edge4: edge4,
			MinBpm: minBPM, MaxBpm: maxBPM,
			TopN: browseTopArtistLimit,
		})
		if err != nil {
			return fmt.Errorf("top artists by tempo band: %w", err)
		}
		artistRows = rows
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	counts := make(map[int32]int64, len(countRows))
	for _, r := range countRows {
		counts[r.BandIndex] = r.TrackCount
	}
	artists := make(map[int32][]BrowseBucketArtist, len(tempoBands))
	for _, r := range artistRows {
		artists[r.BandIndex] = append(artists[r.BandIndex], BrowseBucketArtist{
			ID:       r.MediaItemID,
			PublicID: r.MediaItemPublicID.String(),
		})
	}

	out := make([]TempoBucket, 0, len(tempoBands))
	for i, b := range tempoBands {
		idx := int32(i)
		b.TrackCount = counts[idx]
		b.Artists = orEmptyBrowseArtists(artists[idx])
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
