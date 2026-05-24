package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

// EnabledSource resolves the master enable flag at runtime. Lets the
// task respect the system_settings toggle without importing the
// service layer (avoids an import cycle).
type EnabledSource func(ctx context.Context) bool

// AnalyzeMusicTask runs the sonic-analysis pipeline over the music
// library during the scheduled off-hours window. Loads models on
// start, processes one track at a time, refreshes centroids for
// touched artists/albums at end-of-batch, then unloads models.
//
// Wired into the scheduler with id `analyze_music_facets`. The
// Runner enforces the time window + max-runtime; this Task is just
// the loop body. analyzer_version comes from
// sonicanalysis.AnalyzerVersion — a code constant bumped when the
// pipeline schema changes.
type AnalyzeMusicTask struct {
	DB       *pgxpool.Pool
	Analyzer *sonicanalysis.Analyzer
	Fetcher  *sonicanalysis.ModelFetcher
	Enabled  EnabledSource
}

func (t *AnalyzeMusicTask) ID() TaskID { return TaskAnalyzeMusicFacets }

func (t *AnalyzeMusicTask) isEnabled(ctx context.Context) bool {
	if t.Enabled == nil {
		return true
	}
	return t.Enabled(ctx)
}

// CountPending returns the number of tracks with a usable primary
// file that either have no facets row yet, or whose facets row is
// older than sonicanalysis.AnalyzerVersion. Returns 0 when the
// sonicanalysis master switch is off (hides the task from the UI's
// "X pending" counter).
func (t *AnalyzeMusicTask) CountPending(ctx context.Context) (int, error) {
	if t.Analyzer == nil || !t.isEnabled(ctx) {
		return 0, nil
	}
	n, err := sqlc.New(t.DB).CountPendingAnalysis(ctx, sonicanalysis.AnalyzerVersion)
	if err != nil {
		return 0, fmt.Errorf("count pending: %w", err)
	}
	return int(n), nil
}

// Run loads the analyzer, processes tracks one at a time until the
// context is cancelled (window closed or shutdown), refreshes
// centroids for touched artists/albums, then unloads. The Runner's
// ctx already enforces the per-task max_runtime_minutes, so we don't
// need our own timeout.
func (t *AnalyzeMusicTask) Run(ctx context.Context, progress *ProgressTracker) error {
	if t.Analyzer == nil {
		return errors.New("sonicanalysis analyzer not configured")
	}
	if !t.isEnabled(ctx) {
		log.Info().Msg("scheduler: analyze_music_facets skipped — disabled in settings")
		return nil
	}
	if t.Fetcher != nil && !t.Fetcher.Ready() {
		log.Warn().
			Str("fetcher_state", t.Fetcher.State().String()).
			Msg("scheduler: analyze_music_facets skipped — models not ready yet")
		return nil
	}

	if err := t.Analyzer.Load(ctx); err != nil {
		return fmt.Errorf("analyzer load: %w", err)
	}
	defer t.Analyzer.Unload()

	q := sqlc.New(t.DB)
	currentVersion := sonicanalysis.AnalyzerVersion
	affectedArtists := make(map[int64]struct{})
	affectedAlbums := make(map[int64]struct{})

	for ctx.Err() == nil {
		next, err := q.NextTrackForAnalysis(ctx, currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return fmt.Errorf("next track: %w", err)
		}

		progress.SetCurrentItem(next.Title)

		facets, analyzeErr := t.Analyzer.Analyze(ctx, next.FilePath)
		if analyzeErr != nil {
			log.Warn().Err(analyzeErr).
				Int64("track_id", next.ID).
				Str("file", next.FilePath).
				Msg("scheduler: analyze failed")
			progress.Fail(next.Title)
			// Persist a stub row so we don't re-pick this track every
			// iteration. The next run can retry by bumping
			// analyzer_version.
			t.markFailed(ctx, next.ID, currentVersion)
			continue
		}

		if err := t.persist(ctx, next.ID, facets, currentVersion); err != nil {
			log.Warn().Err(err).Int64("track_id", next.ID).Msg("scheduler: persist failed")
			progress.Fail(next.Title)
			continue
		}

		affectedArtists[next.ArtistID] = struct{}{}
		affectedAlbums[next.AlbumID] = struct{}{}
		progress.Advance(next.Title)
	}

	// End-of-window centroid refresh. One UPSERT per touched artist/
	// album. Skip if the parent context is already cancelled — the
	// caller will pick this up on the next window anyway.
	if ctx.Err() == nil {
		for artistID := range affectedArtists {
			if err := q.RefreshArtistCentroid(ctx, artistID); err != nil {
				log.Warn().Err(err).Int64("artist_id", artistID).Msg("scheduler: artist centroid refresh failed")
			}
		}
		for albumID := range affectedAlbums {
			if err := q.RefreshAlbumCentroid(ctx, albumID); err != nil {
				log.Warn().Err(err).Int64("album_id", albumID).Msg("scheduler: album centroid refresh failed")
			}
		}
	}

	log.Info().
		Int("artists_touched", len(affectedArtists)).
		Int("albums_touched", len(affectedAlbums)).
		Msg("scheduler: analyze_music_facets window complete")
	return nil
}

func (t *AnalyzeMusicTask) persist(ctx context.Context, trackID int64, f *sonicanalysis.Facets, currentVersion int32) error {
	topGenresJSON, err := json.Marshal(f.TopGenres)
	if err != nil {
		return fmt.Errorf("marshal top_genres: %w", err)
	}
	moodTagsJSON, err := json.Marshal(f.MoodTags)
	if err != nil {
		return fmt.Errorf("marshal mood_tags: %w", err)
	}
	params := sqlc.UpsertTrackFacetsParams{
		TrackID:          trackID,
		TrackEmbedding:   pgvector.NewVector(f.TrackEmbed),
		ArtistEmbedding:  pgvector.NewVector(f.ArtistEmbed),
		ReleaseEmbedding: pgvector.NewVector(f.ReleaseEmbed),
		TextEmbedding:    pgvector.NewVector(f.TextEmbed),
		Bpm:              pgFloat4(float32(f.BPM)),
		BpmConfidence:    pgFloat4(float32(f.BPMConfidence)),
		KeyRoot:          pgInt2(int16(f.Key.Root)),
		KeyMode:          pgInt2(int16(f.Key.Mode)),
		KeyClarity:       pgFloat4(float32(f.KeyClarity)),
		TopGenres:        topGenresJSON,
		MoodTags:         moodTagsJSON,
		Waveform:         f.Waveform,
		AnalyzerVersion:  currentVersion,
	}
	return sqlc.New(t.DB).UpsertTrackFacets(ctx, params)
}

// markFailed writes a stub facets row so a permanently-broken track
// (decode error, unreadable file) doesn't get retried every minute.
// Bumping analyzer_version forces a retry across the library.
func (t *AnalyzeMusicTask) markFailed(ctx context.Context, trackID int64, currentVersion int32) {
	params := sqlc.UpsertTrackFacetsParams{
		TrackID:         trackID,
		AnalyzerVersion: currentVersion,
	}
	if err := sqlc.New(t.DB).UpsertTrackFacets(ctx, params); err != nil {
		log.Warn().Err(err).Int64("track_id", trackID).Msg("scheduler: failed to write stub facets row")
	}
}

func pgFloat4(v float32) pgtype.Float4 {
	return pgtype.Float4{Float32: v, Valid: true}
}
func pgInt2(v int16) pgtype.Int2 {
	return pgtype.Int2{Int16: v, Valid: true}
}
