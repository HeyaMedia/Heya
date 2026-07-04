package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/podcastindex"
)

// SearchPodcasts is a thin wrapper around the cached PI client.
func (a *App) SearchPodcasts(ctx context.Context, query string, max int) ([]podcastindex.Podcast, error) {
	return a.podcastIndex.Search(ctx, query, max)
}

func (a *App) TrendingPodcasts(ctx context.Context, max int, category string) ([]podcastindex.Podcast, error) {
	return a.podcastIndex.Trending(ctx, max, category)
}

func (a *App) PodcastCategories(ctx context.Context) ([]podcastindex.Category, error) {
	return a.podcastIndex.Categories(ctx)
}

func (a *App) FetchPodcastFeed(ctx context.Context, feedURL string) (*podcastindex.PodcastDetail, error) {
	return podcastindex.FetchFeed(ctx, feedURL)
}

// ListPodcastSubscriptions returns the user's saved feeds.
func (a *App) ListPodcastSubscriptions(ctx context.Context, userID int64) ([]sqlc.UserPodcastSubscription, error) {
	return sqlc.New(a.db).ListPodcastSubscriptions(ctx, userID)
}

// SubscribePodcast upserts a feed into the user's subscriptions, snapshotting
// the title/author/artwork at subscribe time. The detail page fetches fresh
// feed data on each open so the snapshot is just for the subscriptions list.
func (a *App) SubscribePodcast(ctx context.Context, userID int64, feedURL, title, author, artwork string) (sqlc.UserPodcastSubscription, error) {
	if feedURL == "" {
		return sqlc.UserPodcastSubscription{}, fmt.Errorf("feed_url required")
	}
	return sqlc.New(a.db).AddPodcastSubscription(ctx, sqlc.AddPodcastSubscriptionParams{
		UserID:     userID,
		FeedUrl:    feedURL,
		Title:      title,
		Author:     author,
		ArtworkUrl: artwork,
	})
}

// UnsubscribePodcast drops the subscription. No-op when not subscribed.
func (a *App) UnsubscribePodcast(ctx context.Context, userID int64, feedURL string) error {
	return sqlc.New(a.db).RemovePodcastSubscription(ctx, sqlc.RemovePodcastSubscriptionParams{
		UserID:  userID,
		FeedUrl: feedURL,
	})
}

// RecordPodcastProgress upserts the episode's resume position. Caller picks
// `completed` based on FE rules (e.g. ≥95% heard, or user-clicked "mark as
// played"). Returns the persisted row so the FE can echo state immediately.
type PodcastProgressInput struct {
	FeedURL         string `json:"feed_url"`
	EpisodeGUID     string `json:"episode_guid"`
	Title           string `json:"title"`
	ArtworkURL      string `json:"artwork_url,omitempty"`
	AudioURL        string `json:"audio_url"`
	ProgressSeconds int32  `json:"progress_seconds"`
	TotalSeconds    int32  `json:"total_seconds"`
	Completed       bool   `json:"completed"`
}

func (a *App) RecordPodcastProgress(ctx context.Context, userID int64, in PodcastProgressInput) (sqlc.UserPodcastProgress, error) {
	if in.FeedURL == "" || in.EpisodeGUID == "" {
		return sqlc.UserPodcastProgress{}, fmt.Errorf("feed_url + episode_guid required")
	}
	return sqlc.New(a.db).UpsertPodcastProgress(ctx, sqlc.UpsertPodcastProgressParams{
		UserID:          userID,
		FeedUrl:         in.FeedURL,
		EpisodeGuid:     in.EpisodeGUID,
		Title:           in.Title,
		ArtworkUrl:      in.ArtworkURL,
		AudioUrl:        in.AudioURL,
		ProgressSeconds: in.ProgressSeconds,
		TotalSeconds:    in.TotalSeconds,
		Completed:       in.Completed,
	})
}

// ListPodcastContinue powers the "Continue Listening" rail. Bounded for
// payload sanity; the FE picks the top N.
func (a *App) ListPodcastContinue(ctx context.Context, userID int64, limit int32) ([]sqlc.UserPodcastProgress, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	return sqlc.New(a.db).ListPodcastContinue(ctx, sqlc.ListPodcastContinueParams{
		UserID:     userID,
		TrackLimit: limit,
	})
}
