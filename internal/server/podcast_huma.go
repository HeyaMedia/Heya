package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/podcastindex"
	"github.com/karbowiak/heya/internal/service"
)

// registerPodcastRoutes mounts the podcast surface:
//
//   - /api/podcasts/* — proxy + cache for podcastindex.org (trending,
//     search, categories) plus an RSS feed parser endpoint
//   - /api/me/podcasts/* — per-user subscriptions + episode progress
//   - /api/podcasts/episode/stream — audio enclosure proxy (in binary_huma.go)
func registerPodcastRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/podcasts/trending", "podcasts-trending", "Trending podcasts (optionally per-category)", "Podcasts")),
		func(ctx context.Context, in *struct {
			Max      int    `query:"max"      minimum:"1" maximum:"100" default:"15"`
			Category string `query:"category" maxLength:"100"`
		}) (*JSONOutput[podcastsBody], error) {
			rows, err := app.TrendingPodcasts(ctx, in.Max, in.Category)
			if err != nil {
				if errors.Is(err, podcastindex.ErrUnconfigured) {
					return nil, huma.Error503ServiceUnavailable("podcast-index API key not configured (set HEYA_PODCAST_INDEX_KEY/_SECRET)")
				}
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(podcastsBody{Items: rows}, 1800), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/podcasts/search", "podcasts-search", "Search podcasts by name / author / description", "Podcasts")),
		func(ctx context.Context, in *struct {
			Q   string `query:"q"   minLength:"1" maxLength:"200"`
			Max int    `query:"max" minimum:"1"   maximum:"100" default:"20"`
		}) (*JSONOutput[podcastsBody], error) {
			rows, err := app.SearchPodcasts(ctx, in.Q, in.Max)
			if err != nil {
				if errors.Is(err, podcastindex.ErrUnconfigured) {
					return nil, huma.Error503ServiceUnavailable("podcast-index API key not configured")
				}
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(podcastsBody{Items: rows}, 900), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/podcasts/categories", "podcasts-categories", "Podcast-Index category list", "Podcasts")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[podcastCategoriesBody], error) {
			rows, err := app.PodcastCategories(ctx)
			if err != nil {
				if errors.Is(err, podcastindex.ErrUnconfigured) {
					return nil, huma.Error503ServiceUnavailable("podcast-index API key not configured")
				}
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(podcastCategoriesBody{Items: rows}, 86400), nil
		})

	// Feed parse — accepts the RSS URL the FE got from a Podcast row's
	// feed_url field. Doesn't require PI auth; just fetches the feed.
	huma.Register(api, secured(op(http.MethodGet, "/api/podcasts/feed", "podcasts-feed", "Parse a podcast RSS feed by URL", "Podcasts")),
		func(ctx context.Context, in *struct {
			URL string `query:"url" minLength:"1" maxLength:"2000"`
		}) (*JSONOutput[*podcastindex.PodcastDetail], error) {
			detail, err := app.FetchPodcastFeed(ctx, in.URL)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return cachedJSON(detail, 600), nil
		})

	// --- Per-user subscriptions + progress ---
	huma.Register(api, secured(op(http.MethodGet, "/api/me/podcasts/subscriptions", "list-podcast-subscriptions", "User's podcast subscriptions", "Podcasts")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[podcastSubsBody], error) {
			rows, err := app.ListPodcastSubscriptions(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(podcastSubsBody{Items: rows}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/podcasts/subscriptions", "subscribe-podcast", "Subscribe to a podcast feed", "Podcasts")),
		func(ctx context.Context, in *struct {
			Body struct {
				FeedURL    string `json:"feed_url"    minLength:"1" maxLength:"2000"`
				Title      string `json:"title"       maxLength:"500"`
				Author     string `json:"author"      maxLength:"300"`
				ArtworkURL string `json:"artwork_url" maxLength:"2000"`
			}
		}) (*JSONOutput[sqlc.UserPodcastSubscription], error) {
			row, err := app.SubscribePodcast(ctx, userFrom(ctx).ID, in.Body.FeedURL, in.Body.Title, in.Body.Author, in.Body.ArtworkURL)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(row), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/podcasts/subscriptions", "unsubscribe-podcast", "Unsubscribe from a podcast feed", "Podcasts")),
		func(ctx context.Context, in *struct {
			URL string `query:"url" minLength:"1" maxLength:"2000"`
		}) (*JSONOutput[okBody], error) {
			if err := app.UnsubscribePodcast(ctx, userFrom(ctx).ID, in.URL); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(okBody{Ok: true}), nil
		})

	// Progress upsert — the player calls this in lock-step with the unified
	// /api/me/playback for music/video. Kept separate because podcast
	// episodes don't have stable media_item IDs (RSS GUIDs only).
	huma.Register(api, secured(op(http.MethodPost, "/api/me/podcasts/progress", "record-podcast-progress", "Update episode resume position", "Podcasts")),
		func(ctx context.Context, in *struct {
			Body service.PodcastProgressInput
		}) (*JSONOutput[sqlc.UserPodcastProgress], error) {
			row, err := app.RecordPodcastProgress(ctx, userFrom(ctx).ID, in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(row), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/podcasts/continue", "podcasts-continue", "Episodes the user can resume", "Podcasts")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"12"`
		}) (*JSONOutput[podcastContinueBody], error) {
			rows, err := app.ListPodcastContinue(ctx, userFrom(ctx).ID, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(podcastContinueBody{Items: rows}), nil
		})
}

type podcastsBody struct {
	Items []podcastindex.Podcast `json:"items"`
}

type podcastCategoriesBody struct {
	Items []podcastindex.Category `json:"items"`
}

type podcastSubsBody struct {
	Items []sqlc.UserPodcastSubscription `json:"items"`
}

type podcastContinueBody struct {
	Items []sqlc.UserPodcastProgress `json:"items"`
}
