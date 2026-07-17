package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// RecordPlayEventInput is one permanent listen-history row. Live
// now-playing notifications never land here; callers create a row only after
// a track naturally completes.
type RecordPlayEventInput struct {
	TrackID         int64  `json:"track_id"`
	ListenedSeconds int32  `json:"listened_seconds"`
	Completed       bool   `json:"completed"`
	Source          string `json:"source,omitempty" doc:"Origin label: queue | radio | album | playlist | search | browse | similar"`
}

// ListeningStats packages the three derived views the FE needs for the
// "Your sound" pane — top genres, average mood scores, and tempo histogram.
// Returned in a single envelope so the FE doesn't fan out three calls.
type ListeningStats struct {
	TotalPlays int64                        `json:"total_plays"`
	TopGenres  []sqlc.TopUserGenresRow      `json:"top_genres"`
	MoodAvg    []sqlc.TopUserMoodsRow       `json:"mood_avg"`
	TempoHist  []sqlc.UserTempoHistogramRow `json:"tempo_histogram"`
}

// RecordPlayEvent persists one play event for the given user.
func (a *App) RecordPlayEvent(ctx context.Context, userID int64, in RecordPlayEventInput) (*sqlc.PlayEvent, error) {
	if in.TrackID <= 0 {
		return nil, fmt.Errorf("track_id required")
	}
	if in.ListenedSeconds < 0 {
		in.ListenedSeconds = 0
	}
	q := sqlc.New(a.db)
	row, err := q.RecordPlayEvent(ctx, sqlc.RecordPlayEventParams{
		UserID:          userID,
		TrackID:         in.TrackID,
		ListenedSeconds: in.ListenedSeconds,
		Completed:       in.Completed,
		Source:          in.Source,
	})
	if err != nil {
		return nil, fmt.Errorf("record play event: %w", err)
	}
	return &row, nil
}

// ListRecentlyPlayed returns the user's recently-played rail. Deduped by
// track so a looped track shows up once with the most-recent played_at.
func (a *App) ListRecentlyPlayed(ctx context.Context, userID int64, limit, offset int32) ([]sqlc.ListRecentlyPlayedTracksRow, error) {
	limit, offset = clampMusicPage(limit, offset)
	rows, err := sqlc.New(a.db).ListRecentlyPlayedTracks(ctx, sqlc.ListRecentlyPlayedTracksParams{
		UserID:      userID,
		TrackLimit:  limit,
		TrackOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list recently played: %w", err)
	}
	return rows, nil
}

// ListeningStatsForUser returns the user's aggregated taste profile. Three
// queries run sequentially against the same connection; total cost is small
// (an hour of listening = maybe 30 tracks = 30 fact joins per query).
func (a *App) ListeningStatsForUser(ctx context.Context, userID int64) (*ListeningStats, error) {
	q := sqlc.New(a.db)
	total, _ := q.CountUserPlayEvents(ctx, userID)
	genres, err := q.TopUserGenres(ctx, sqlc.TopUserGenresParams{
		UserID:      userID,
		MinScore:    genreScoreFloor,
		BucketLimit: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("top genres: %w", err)
	}
	moods, err := q.TopUserMoods(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("top moods: %w", err)
	}
	tempo, err := q.UserTempoHistogram(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tempo histogram: %w", err)
	}
	return &ListeningStats{
		TotalPlays: total,
		TopGenres:  genres,
		MoodAvg:    moods,
		TempoHist:  tempo,
	}, nil
}
