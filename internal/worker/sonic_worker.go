package worker

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
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// AnalyzeTrackFacetsArgs runs the CLAP/Discogs analysis pipeline over
// a single track's primary audio file and writes the resulting
// embeddings / BPM / key / mood-tags into track_facets.
//
// One job per track so the kickoff_sonic_analysis fan-out is
// cancellable per item. The CLAP model is held resident across jobs
// by sonicanalysis.Holder (lazy-load on first Borrow, idle-unload
// after 5 min) so the per-job overhead is just one Borrow + Analyze,
// not a fresh ~10s model open.
//
// AnalyzerVersion is stamped on every write so a future code bump
// invalidates existing rows and the scheduler re-picks them.
type AnalyzeTrackFacetsArgs struct {
	TrackID         int64  `json:"track_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (AnalyzeTrackFacetsArgs) Kind() string { return "analyze_track_facets" }
func (AnalyzeTrackFacetsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "sonic_analysis",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type AnalyzeTrackFacetsWorker struct {
	river.WorkerDefaults[AnalyzeTrackFacetsArgs]
	DB       *pgxpool.Pool
	Holder   *sonicanalysis.Holder
	Progress *TaskProgressBroadcaster
}

func (w *AnalyzeTrackFacetsWorker) Work(ctx context.Context, job *river.Job[AnalyzeTrackFacetsArgs]) error {
	if w.Holder == nil {
		return errors.New("sonic analyzer not configured")
	}

	q := sqlc.New(w.DB)
	row, err := q.GetTrackForAnalysis(ctx, job.Args.TrackID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Track row vanished (library deletion, db reset). Treat as
		// done — no point retrying.
		return nil
	}
	if err != nil {
		return err
	}
	if row.FilePath == "" {
		return nil
	}

	// Label is "Artist — Track"; the stage hook below carries the
	// current pipeline step (CLAP audio embed, Discogs heads, …) as
	// a separate field so the UI can show item + stage on two lines.
	label := row.Title
	if row.ArtistName != "" {
		label = row.ArtistName + " — " + row.Title
	}
	w.Progress.SetCurrent(AnalyzeTrackFacetsArgs{}.Kind(), job.Args.ScheduledTaskID, label)
	currentVersion := sonicanalysis.AnalyzerVersion

	lease, err := w.Holder.Borrow(ctx)
	if err != nil {
		return fmt.Errorf("borrow analyzer: %w", err)
	}
	defer lease.Close()

	stageHook := func(stage sonicanalysis.AnalyzeStage) {
		w.Progress.SetStage(AnalyzeTrackFacetsArgs{}.Kind(), job.Args.ScheduledTaskID, label, string(stage))
	}
	facets, analyzeErr := lease.Analyzer.AnalyzeWithProgress(ctx, row.FilePath, stageHook)
	if analyzeErr != nil {
		// Persist a stub row so a permanently-broken track (decode
		// error, unreadable file) doesn't get re-picked on every
		// kickoff. Bumping AnalyzerVersion forces a retry.
		log.Warn().Err(analyzeErr).
			Int64("track_id", row.ID).
			Str("file", row.FilePath).
			Msg("analyze_track_facets: analyze failed")
		_ = q.UpsertTrackFacets(ctx, sqlc.UpsertTrackFacetsParams{
			TrackID:         row.ID,
			AnalyzerVersion: currentVersion,
		})
		return analyzeErr
	}

	if err := persistTrackFacets(ctx, q, row.ID, facets, currentVersion); err != nil {
		return fmt.Errorf("persist facets: %w", err)
	}

	// Debounced centroid refresh. UniqueByArgs on the centroid jobs
	// means rapid back-to-back track completions for the same artist/
	// album collapse to a single refresh.
	client := river.ClientFromContext[pgx.Tx](ctx)
	if client != nil {
		if _, err := client.Insert(ctx, RefreshArtistCentroidArgs{ArtistID: row.ArtistID, ScheduledTaskID: job.Args.ScheduledTaskID}, nil); err != nil {
			log.Warn().Err(err).Int64("artist_id", row.ArtistID).Msg("analyze_track_facets: enqueue artist centroid refresh failed")
		}
		if _, err := client.Insert(ctx, RefreshAlbumCentroidArgs{AlbumID: row.AlbumID, ScheduledTaskID: job.Args.ScheduledTaskID}, nil); err != nil {
			log.Warn().Err(err).Int64("album_id", row.AlbumID).Msg("analyze_track_facets: enqueue album centroid refresh failed")
		}
	}

	return nil
}

func persistTrackFacets(ctx context.Context, q *sqlc.Queries, trackID int64, f *sonicanalysis.Facets, currentVersion int32) error {
	topGenresJSON, err := json.Marshal(f.TopGenres)
	if err != nil {
		return fmt.Errorf("marshal top_genres: %w", err)
	}
	moodTagsJSON, err := json.Marshal(f.MoodTags)
	if err != nil {
		return fmt.Errorf("marshal mood_tags: %w", err)
	}
	return q.UpsertTrackFacets(ctx, sqlc.UpsertTrackFacetsParams{
		TrackID:          trackID,
		TrackEmbedding:   pgvector.NewVector(f.TrackEmbed),
		ArtistEmbedding:  pgvector.NewVector(f.ArtistEmbed),
		ReleaseEmbedding: pgvector.NewVector(f.ReleaseEmbed),
		TextEmbedding:    pgvector.NewVector(f.TextEmbed),
		Bpm:              pgtype.Float4{Float32: float32(f.BPM), Valid: true},
		BpmConfidence:    pgtype.Float4{Float32: float32(f.BPMConfidence), Valid: true},
		KeyRoot:          pgtype.Int2{Int16: int16(f.Key.Root), Valid: true},
		KeyMode:          pgtype.Int2{Int16: int16(f.Key.Mode), Valid: true},
		KeyClarity:       pgtype.Float4{Float32: float32(f.KeyClarity), Valid: true},
		TopGenres:        topGenresJSON,
		MoodTags:         moodTagsJSON,
		Waveform:         f.Waveform,
		AnalyzerVersion:  currentVersion,
	})
}

// RefreshArtistCentroidArgs recomputes one artist's sonic + text
// centroid as the mean of its tracks' embeddings. UniqueByArgs debounces
// rapid bursts (e.g. when finishing a 200-track album, only one
// refresh per artist actually runs).
type RefreshArtistCentroidArgs struct {
	ArtistID        int64  `json:"artist_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (RefreshArtistCentroidArgs) Kind() string { return "refresh_artist_centroids" }
func (RefreshArtistCentroidArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "artist_centroid",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type RefreshArtistCentroidWorker struct {
	river.WorkerDefaults[RefreshArtistCentroidArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *RefreshArtistCentroidWorker) Work(ctx context.Context, job *river.Job[RefreshArtistCentroidArgs]) error {
	q := sqlc.New(w.DB)
	if artist, err := q.GetArtistByID(ctx, job.Args.ArtistID); err == nil {
		w.Progress.SetStage(RefreshArtistCentroidArgs{}.Kind(), job.Args.ScheduledTaskID, artist.Name, "artist centroid")
	}
	return q.RefreshArtistCentroid(ctx, job.Args.ArtistID)
}

// RefreshAlbumCentroidArgs mirrors RefreshArtistCentroidArgs for albums.
type RefreshAlbumCentroidArgs struct {
	AlbumID         int64  `json:"album_id" river:"unique"`
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (RefreshAlbumCentroidArgs) Kind() string { return "refresh_album_centroids" }
func (RefreshAlbumCentroidArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "album_centroid",
		MaxAttempts: 2,
		Priority:    PriorityAnalysis,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type RefreshAlbumCentroidWorker struct {
	river.WorkerDefaults[RefreshAlbumCentroidArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *RefreshAlbumCentroidWorker) Work(ctx context.Context, job *river.Job[RefreshAlbumCentroidArgs]) error {
	q := sqlc.New(w.DB)
	if album, err := q.GetAlbumByID(ctx, job.Args.AlbumID); err == nil {
		w.Progress.SetStage(RefreshAlbumCentroidArgs{}.Kind(), job.Args.ScheduledTaskID, album.Title, "album centroid")
	}
	return q.RefreshAlbumCentroid(ctx, job.Args.AlbumID)
}
