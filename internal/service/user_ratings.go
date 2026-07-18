package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Ratings are stored as integers 1..10 (half-star precision on a 5-star UI).
// 0 ratings are stored as DELETE — there's no "I rated this zero" affordance.
// Tracks, albums, and artists share the exact same surface; the helpers below
// hold the shared validation/paging logic and each entity contributes only
// its sqlc calls.

// writeRating validates and routes a rating write; rating==0 clears.
func writeRating(rating int16, del, set func() error) error {
	if rating < 0 || rating > 10 {
		return fmt.Errorf("rating must be 0..10")
	}
	if rating == 0 {
		return del()
	}
	return set()
}

// readRating maps "no row" to 0 (the FE treats 0 as unrated).
func readRating(r int16, err error) (int16, error) {
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return r, nil
}

// ratingsByID shapes bulk-rating rows into map[entityID]rating — callers
// treat missing keys as unrated.
func ratingsByID[R any](rows []R, err error, key func(R) (int64, int16)) (map[int64]int16, error) {
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		id, rating := key(r)
		out[id] = rating
	}
	return out, nil
}

// ratedPage clamps paging + minRating and assembles the shared envelope.
// minRating filters to "favorites only" when set to the user's
// favorites_threshold; pass 1 to get every rated item regardless of score.
func ratedPage[T any](minRating, maxRating int16, limit, offset int32,
	list func(minR, maxR int16, limit, offset int32) ([]T, error),
	count func(minR, maxR int16) (int64, error),
) (*MusicListPage[T], error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if minRating < 1 {
		minRating = 1
	}
	if maxRating < minRating || maxRating > 10 {
		maxRating = 10
	}
	items, err := list(minRating, maxRating, limit, offset)
	if err != nil {
		return nil, err
	}
	total, err := count(minRating, maxRating)
	if err != nil {
		return nil, err
	}
	return &MusicListPage[T]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ============== Tracks ==============

func (a *App) SetUserTrackRating(ctx context.Context, userID, trackID int64, rating int16) error {
	q := sqlc.New(a.db)
	old, _ := a.GetUserTrackRating(ctx, userID, trackID)
	err := writeRating(rating,
		func() error {
			return q.DeleteUserTrackRating(ctx, sqlc.DeleteUserTrackRatingParams{UserID: userID, TrackID: trackID})
		},
		func() error {
			return q.SetUserTrackRating(ctx, sqlc.SetUserTrackRatingParams{UserID: userID, TrackID: trackID, Rating: rating})
		})
	if err == nil {
		// Reactions sync outbound to linked services (fire-and-forget) when
		// the rating crossed a band boundary — hearts become loves,
		// thumbs-downs become ListenBrainz hates, clears clear.
		a.ReactionOutbound(userID, trackID, old, rating)
	}
	return err
}

func (a *App) GetUserTrackRating(ctx context.Context, userID, trackID int64) (int16, error) {
	return readRating(sqlc.New(a.db).GetUserTrackRating(ctx, sqlc.GetUserTrackRatingParams{
		UserID: userID, TrackID: trackID,
	}))
}

func (a *App) RatingsForTracks(ctx context.Context, userID int64, trackIDs []int64) (map[int64]int16, error) {
	if len(trackIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserTrackRatingsForTrackIDs(ctx, sqlc.GetUserTrackRatingsForTrackIDsParams{
		UserID: userID, Column2: trackIDs,
	})
	return ratingsByID(rows, err, func(r sqlc.GetUserTrackRatingsForTrackIDsRow) (int64, int16) {
		return r.TrackID, r.Rating
	})
}

func (a *App) ListUserRatedTracks(ctx context.Context, userID int64, minRating, maxRating int16, limit, offset int32) (*MusicListPage[sqlc.ListUserRatedTracksRow], error) {
	q := sqlc.New(a.db)
	return ratedPage(minRating, maxRating, limit, offset,
		func(minR, maxR int16, limit, offset int32) ([]sqlc.ListUserRatedTracksRow, error) {
			return q.ListUserRatedTracks(ctx, sqlc.ListUserRatedTracksParams{
				UserID: userID, MinRating: minR, MaxRating: maxR, TrackLimit: limit, Offset: offset,
			})
		},
		func(minR, maxR int16) (int64, error) {
			return q.CountUserRatedTracks(ctx, sqlc.CountUserRatedTracksParams{UserID: userID, MinRating: minR, MaxRating: maxR})
		})
}

// GetUserRatedTracksStats aggregates a rating band for the Loved Songs
// hero ledger: track count, total runtime, distinct artists, most recent
// rating touch.
func (a *App) GetUserRatedTracksStats(ctx context.Context, userID int64, minRating, maxRating int16) (*sqlc.GetUserRatedTracksStatsRow, error) {
	row, err := sqlc.New(a.db).GetUserRatedTracksStats(ctx, sqlc.GetUserRatedTracksStatsParams{
		UserID: userID, MinRating: minRating, MaxRating: maxRating,
	})
	if err != nil {
		return nil, fmt.Errorf("rated track stats: %w", err)
	}
	return &row, nil
}

// ============== Albums ==============

func (a *App) SetUserAlbumRating(ctx context.Context, userID, albumID int64, rating int16) error {
	q := sqlc.New(a.db)
	return writeRating(rating,
		func() error {
			return q.DeleteUserAlbumRating(ctx, sqlc.DeleteUserAlbumRatingParams{UserID: userID, AlbumID: albumID})
		},
		func() error {
			return q.SetUserAlbumRating(ctx, sqlc.SetUserAlbumRatingParams{UserID: userID, AlbumID: albumID, Rating: rating})
		})
}

func (a *App) GetUserAlbumRating(ctx context.Context, userID, albumID int64) (int16, error) {
	return readRating(sqlc.New(a.db).GetUserAlbumRating(ctx, sqlc.GetUserAlbumRatingParams{
		UserID: userID, AlbumID: albumID,
	}))
}

func (a *App) RatingsForAlbums(ctx context.Context, userID int64, albumIDs []int64) (map[int64]int16, error) {
	if len(albumIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserAlbumRatingsForIDs(ctx, sqlc.GetUserAlbumRatingsForIDsParams{
		UserID: userID, Column2: albumIDs,
	})
	return ratingsByID(rows, err, func(r sqlc.GetUserAlbumRatingsForIDsRow) (int64, int16) {
		return r.AlbumID, r.Rating
	})
}

func (a *App) ListUserRatedAlbums(ctx context.Context, userID int64, minRating, maxRating int16, limit, offset int32) (*MusicListPage[sqlc.ListUserRatedAlbumsRow], error) {
	q := sqlc.New(a.db)
	return ratedPage(minRating, maxRating, limit, offset,
		func(minR, maxR int16, limit, offset int32) ([]sqlc.ListUserRatedAlbumsRow, error) {
			return q.ListUserRatedAlbums(ctx, sqlc.ListUserRatedAlbumsParams{
				UserID: userID, MinRating: minR, MaxRating: maxR, AlbumLimit: limit, Offset: offset,
			})
		},
		func(minR, maxR int16) (int64, error) {
			return q.CountUserRatedAlbums(ctx, sqlc.CountUserRatedAlbumsParams{UserID: userID, MinRating: minR, MaxRating: maxR})
		})
}

// ============== Artists ==============

func (a *App) SetUserArtistRating(ctx context.Context, userID, artistID int64, rating int16) error {
	q := sqlc.New(a.db)
	return writeRating(rating,
		func() error {
			return q.DeleteUserArtistRating(ctx, sqlc.DeleteUserArtistRatingParams{UserID: userID, ArtistID: artistID})
		},
		func() error {
			return q.SetUserArtistRating(ctx, sqlc.SetUserArtistRatingParams{UserID: userID, ArtistID: artistID, Rating: rating})
		})
}

func (a *App) GetUserArtistRating(ctx context.Context, userID, artistID int64) (int16, error) {
	return readRating(sqlc.New(a.db).GetUserArtistRating(ctx, sqlc.GetUserArtistRatingParams{
		UserID: userID, ArtistID: artistID,
	}))
}

func (a *App) RatingsForArtists(ctx context.Context, userID int64, artistIDs []int64) (map[int64]int16, error) {
	if len(artistIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserArtistRatingsForIDs(ctx, sqlc.GetUserArtistRatingsForIDsParams{
		UserID: userID, Column2: artistIDs,
	})
	return ratingsByID(rows, err, func(r sqlc.GetUserArtistRatingsForIDsRow) (int64, int16) {
		return r.ArtistID, r.Rating
	})
}

func (a *App) ListUserRatedArtists(ctx context.Context, userID int64, minRating, maxRating int16, limit, offset int32) (*MusicListPage[sqlc.ListUserRatedArtistsRow], error) {
	q := sqlc.New(a.db)
	return ratedPage(minRating, maxRating, limit, offset,
		func(minR, maxR int16, limit, offset int32) ([]sqlc.ListUserRatedArtistsRow, error) {
			return q.ListUserRatedArtists(ctx, sqlc.ListUserRatedArtistsParams{
				UserID: userID, MinRating: minR, MaxRating: maxR, ArtistLimit: limit, Offset: offset,
			})
		},
		func(minR, maxR int16) (int64, error) {
			return q.CountUserRatedArtists(ctx, sqlc.CountUserRatedArtistsParams{UserID: userID, MinRating: minR, MaxRating: maxR})
		})
}

// ============== Favorites threshold ==============

// FavoritesThresholdGet/Set let the UI move the favorites bar without a
// schema change. Default 7 (=3.5★) feels like "I really like this".
func (a *App) GetFavoritesThreshold(ctx context.Context, userID int64) (int16, error) {
	t, err := sqlc.New(a.db).GetUserFavoritesThreshold(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 7, nil
		}
		return 0, err
	}
	return t, nil
}

func (a *App) SetFavoritesThreshold(ctx context.Context, userID int64, threshold int16) error {
	if threshold < 1 || threshold > 10 {
		return fmt.Errorf("threshold must be 1..10")
	}
	return sqlc.New(a.db).UpdateUserFavoritesThreshold(ctx, sqlc.UpdateUserFavoritesThresholdParams{
		ID:                 userID,
		FavoritesThreshold: threshold,
	})
}
