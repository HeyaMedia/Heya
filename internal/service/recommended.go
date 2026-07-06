package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// RecRailItem is one poster-rail tile on a Recommended landing page. It carries the
// minimum the FE needs to render a MediaCard and route to the detail page —
// id (poster lookup), slug + media_type (URL), title/year/sub (labels).
type RecRailItem struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Slug      string  `json:"slug"`
	Year      string  `json:"year,omitempty"`
	Sub       string  `json:"sub,omitempty"`
	MediaType string  `json:"media_type"`
	Rating    float64 `json:"rating,omitempty"`
	Available bool    `json:"available"`

	// libraryID is carried so the whole rail stack can be title-localized in one
	// batch (see localizeRails). Unexported → never serialized, off the schema.
	libraryID int64
}

// RecRail is a titled row of RecRailItems. Key is a stable id the FE uses for the
// v-for and for scoping any per-rail behaviour.
type RecRail struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle,omitempty"`
	Items    []RecRailItem `json:"items"`

	// Baseline is the entity this rail is "about" — the actor for a "Starring
	// X" rail, the genre for a "More X" rail, empty otherwise. Off the native
	// wire (the FE derives labels from Title); the Jellyfin compat layer maps it
	// to RecommendationDto.BaselineItemName for /Movies/Recommendations.
	Baseline string `json:"-"`
}

// RecommendedResult is the whole ordered rail stack for one section. The server
// owns the ordering and which rails exist so the FE only renders — and so the
// same logic backs a future `heya recommended <section>` CLI command.
type RecommendedResult struct {
	Rails []RecRail `json:"rails"`
}

// recRailLimit caps every discovery rail. 24 fills a wide row without a giant
// payload; the FE horizontal-scrolls whatever it gets.
const recRailLimit int32 = 24

// Recommended builds the personalized discovery rails for a section's landing
// page. The FE composes the activity rows (Continue Watching, Up Next, Recently
// Added/Watched) itself from their own endpoints; this owns the "what should I
// watch" half. Empty rails are omitted, so a brand-new account with no history
// simply gets the non-personalized rails (or nothing).
func (a *App) Recommended(ctx context.Context, userID int64, section string) (RecommendedResult, error) {
	switch section {
	case "movie", "movies":
		return a.movieRecommendations(ctx, userID)
	case "tv":
		return a.tvRecommendations(ctx, userID)
	default:
		return RecommendedResult{}, fmt.Errorf("unknown recommended section %q (want movie | tv)", section)
	}
}

func (a *App) movieRecommendations(ctx context.Context, userID int64) (RecommendedResult, error) {
	q := sqlc.New(a.db)
	var rails []RecRail

	if rows, err := q.ListRecentlyReleasedMovies(ctx, recRailLimit); err == nil {
		items := mapRecRows(rows, func(r sqlc.ListRecentlyReleasedMoviesRow) RecRailItem {
			return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
		})
		rails = appendRail(rails, "recently-released", "Recently Released", "New in theaters & digital", "", items, 3)
	}

	if rows, err := q.ListTopUnwatchedMovies(ctx, sqlc.ListTopUnwatchedMoviesParams{UserID: userID, Limit: recRailLimit}); err == nil {
		items := mapRecRows(rows, func(r sqlc.ListTopUnwatchedMoviesRow) RecRailItem {
			return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
		})
		rails = appendRail(rails, "top-unwatched", "Top Unwatched Movies", "Highly rated, not seen yet", "", items, 3)
	}

	// "Starring <actor>": the most-watched top-billed actor across finished
	// films who still has enough unseen owned titles to fill a rail.
	if actors, err := q.ListTopWatchedMovieActors(ctx, sqlc.ListTopWatchedMovieActorsParams{UserID: userID, Limit: 6}); err == nil {
		for _, ac := range actors {
			rows, err := q.ListPersonUnseenMovies(ctx, sqlc.ListPersonUnseenMoviesParams{PersonID: ac.PersonID, UserID: userID, Lim: recRailLimit})
			if err != nil || len(rows) < 4 {
				continue
			}
			items := mapRecRows(rows, func(r sqlc.ListPersonUnseenMoviesRow) RecRailItem {
				return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
			})
			rails = appendRail(rails, "by-actor", "Starring "+ac.Name, "Because you've watched their films", ac.Name, items, 4)
			break
		}
	}

	// "More <genre>": the genre the user finishes most, with enough unseen
	// owned titles to be worth a rail.
	if genres, err := q.ListTopWatchedMovieGenres(ctx, userID); err == nil {
		for _, g := range genres {
			rows, err := q.ListTopMoviesInGenreUnseen(ctx, sqlc.ListTopMoviesInGenreUnseenParams{Genre: g.Genre, UserID: userID, Lim: recRailLimit})
			if err != nil || len(rows) < 4 {
				continue
			}
			items := mapRecRows(rows, func(r sqlc.ListTopMoviesInGenreUnseenRow) RecRailItem {
				return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
			})
			rails = appendRail(rails, "more-genre", "More "+g.Genre, "Because you watch a lot of "+g.Genre, g.Genre, items, 4)
			break
		}
	}

	rails = a.appendLocalRecRail(ctx, q, rails, userID, "movie", "Recommended Movies")
	a.localizeRails(ctx, q, rails)
	return RecommendedResult{Rails: rails}, nil
}

func (a *App) tvRecommendations(ctx context.Context, userID int64) (RecommendedResult, error) {
	q := sqlc.New(a.db)
	var rails []RecRail

	if rows, err := q.ListTopRatedTV(ctx, recRailLimit); err == nil {
		items := mapRecRows(rows, func(r sqlc.ListTopRatedTVRow) RecRailItem {
			return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
		})
		rails = appendRail(rails, "top-rated", "Top Rated TV", "The best-reviewed shows you own", "", items, 3)
	}

	if genres, err := q.ListTopWatchedTVGenres(ctx, userID); err == nil {
		for _, g := range genres {
			rows, err := q.ListTopTVInGenre(ctx, sqlc.ListTopTVInGenreParams{Genre: g.Genre, Lim: recRailLimit})
			if err != nil || len(rows) < 4 {
				continue
			}
			items := mapRecRows(rows, func(r sqlc.ListTopTVInGenreRow) RecRailItem {
				return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, ratingFloat(r.Rating))
			})
			rails = appendRail(rails, "more-genre", "More "+g.Genre, "Because you watch a lot of "+g.Genre, g.Genre, items, 4)
			break
		}
	}

	// "Rediscover": shows watched a while ago that have aired new episodes since.
	if rows, err := q.ListRediscoverTV(ctx, sqlc.ListRediscoverTVParams{UserID: userID, Limit: recRailLimit}); err == nil {
		items := mapRecRows(rows, func(r sqlc.ListRediscoverTVRow) RecRailItem {
			return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, 0)
		})
		rails = appendRail(rails, "rediscover", "Rediscover", "New episodes since you last watched", "", items, 1)
	}

	rails = a.appendLocalRecRail(ctx, q, rails, userID, "tv", "Recommended Shows")
	a.localizeRails(ctx, q, rails)
	return RecommendedResult{Rails: rails}, nil
}

// appendLocalRecRail adds the library-wide TMDB "recommended" rail for a
// section — owned titles TMDB most often recommends, minus finished movies.
func (a *App) appendLocalRecRail(ctx context.Context, q *sqlc.Queries, rails []RecRail, userID int64, itemType, title string) []RecRail {
	rows, err := q.ListLocalRecommendations(ctx, sqlc.ListLocalRecommendationsParams{
		ItemType: sqlc.MediaType(itemType),
		RecType:  itemType,
		UserID:   userID,
		Lim:      recRailLimit,
	})
	if err != nil {
		return rails
	}
	items := mapRecRows(rows, func(r sqlc.ListLocalRecommendationsRow) RecRailItem {
		return newRecRailItem(r.ID, r.LibraryID, r.Title, r.Slug, r.Year, r.MediaType, 0)
	})
	return appendRail(rails, "recommended", title, "Frequently recommended alongside what you own", "", items, 3)
}

// newRecRailItem stamps Available=true — every rail query already gates on a live,
// non-deleted library file, so anything returned is playable.
func newRecRailItem(id, libraryID int64, title, slug, year, mediaType string, rating float64) RecRailItem {
	return RecRailItem{ID: id, Title: title, Slug: slug, Year: year, MediaType: mediaType, Rating: rating, Available: true, libraryID: libraryID}
}

// localizeRails overlays each item's title with its library's PreferredLanguage
// variant (falling back to English, then the raw title) in a single batched
// pass across the whole rail stack — the same overlay the enriched list pages
// use, so a Japanese-preferred library shows the same localized titles here.
func (a *App) localizeRails(ctx context.Context, q *sqlc.Queries, rails []RecRail) {
	var targets []titleTarget
	for _, r := range rails {
		for _, it := range r.Items {
			targets = append(targets, titleTarget{ID: it.ID, LibraryID: it.libraryID})
		}
	}
	if len(targets) == 0 {
		return
	}
	overlay := a.preferredTitleOverlayFor(ctx, q, targets)
	for ri := range rails {
		for ii := range rails[ri].Items {
			if t, ok := overlay[rails[ri].Items[ii].ID]; ok && t != "" {
				rails[ri].Items[ii].Title = t
			}
		}
	}
}

// mapRecRows maps a typed sqlc row slice into RecRailItems.
func mapRecRows[T any](rows []T, f func(T) RecRailItem) []RecRailItem {
	out := make([]RecRailItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, f(r))
	}
	return out
}

// appendRail appends a rail only when it clears the minimum item count — a
// one-tile rail reads as broken, so sparse rails are dropped instead.
func appendRail(rails []RecRail, key, title, subtitle, baseline string, items []RecRailItem, minItems int) []RecRail {
	if len(items) < minItems {
		return rails
	}
	return append(rails, RecRail{Key: key, Title: title, Subtitle: subtitle, Baseline: baseline, Items: items})
}
