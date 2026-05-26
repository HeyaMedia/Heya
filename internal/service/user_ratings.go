package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Track ratings are stored as integers 1..10 (half-star precision on a
// 5-star UI). 0 ratings are stored as DELETE — there's no "I rated this
// zero" affordance.

// SetUserTrackRating writes a rating; rating==0 clears it.
func (a *App) SetUserTrackRating(ctx context.Context, userID, trackID int64, rating int16) error {
	if rating < 0 || rating > 10 {
		return fmt.Errorf("rating must be 0..10")
	}
	q := sqlc.New(a.db)
	if rating == 0 {
		return q.DeleteUserTrackRating(ctx, sqlc.DeleteUserTrackRatingParams{
			UserID: userID, TrackID: trackID,
		})
	}
	return q.SetUserTrackRating(ctx, sqlc.SetUserTrackRatingParams{
		UserID: userID, TrackID: trackID, Rating: rating,
	})
}

// GetUserTrackRating returns 0 when unrated (the FE treats 0 as empty).
func (a *App) GetUserTrackRating(ctx context.Context, userID, trackID int64) (int16, error) {
	r, err := sqlc.New(a.db).GetUserTrackRating(ctx, sqlc.GetUserTrackRatingParams{
		UserID: userID, TrackID: trackID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return r, nil
}

// RatingsForTracks bulk-fetches user ratings for a list of track IDs.
// Returns a map[trackID]rating — caller treats missing keys as unrated.
func (a *App) RatingsForTracks(ctx context.Context, userID int64, trackIDs []int64) (map[int64]int16, error) {
	if len(trackIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserTrackRatingsForTrackIDs(ctx, sqlc.GetUserTrackRatingsForTrackIDsParams{
		UserID:  userID,
		Column2: trackIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		out[r.TrackID] = r.Rating
	}
	return out, nil
}

// UserRatedTracksPage powers the Favorites view. minRating filters to
// "favorites only" when set to the user's favorites_threshold; pass 1
// to get every rated track regardless of score.
type UserRatedTracksPage struct {
	Items  []sqlc.ListUserRatedTracksRow `json:"items"`
	Total  int64                         `json:"total"`
	Limit  int32                         `json:"limit"`
	Offset int32                         `json:"offset"`
}

func (a *App) ListUserRatedTracks(ctx context.Context, userID int64, minRating int16, limit, offset int32) (*UserRatedTracksPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if minRating < 1 {
		minRating = 1
	}
	q := sqlc.New(a.db)
	items, err := q.ListUserRatedTracks(ctx, sqlc.ListUserRatedTracksParams{
		UserID:     userID,
		MinRating:  minRating,
		TrackLimit: limit,
		Offset:     offset,
	})
	if err != nil {
		return nil, err
	}
	total, err := q.CountUserRatedTracks(ctx, sqlc.CountUserRatedTracksParams{
		UserID: userID, Rating: minRating,
	})
	if err != nil {
		return nil, err
	}
	return &UserRatedTracksPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

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

// ============== Albums ==============

func (a *App) SetUserAlbumRating(ctx context.Context, userID, albumID int64, rating int16) error {
	if rating < 0 || rating > 10 {
		return fmt.Errorf("rating must be 0..10")
	}
	q := sqlc.New(a.db)
	if rating == 0 {
		return q.DeleteUserAlbumRating(ctx, sqlc.DeleteUserAlbumRatingParams{UserID: userID, AlbumID: albumID})
	}
	return q.SetUserAlbumRating(ctx, sqlc.SetUserAlbumRatingParams{
		UserID: userID, AlbumID: albumID, Rating: rating,
	})
}

func (a *App) GetUserAlbumRating(ctx context.Context, userID, albumID int64) (int16, error) {
	r, err := sqlc.New(a.db).GetUserAlbumRating(ctx, sqlc.GetUserAlbumRatingParams{UserID: userID, AlbumID: albumID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return r, nil
}

func (a *App) RatingsForAlbums(ctx context.Context, userID int64, albumIDs []int64) (map[int64]int16, error) {
	if len(albumIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserAlbumRatingsForIDs(ctx, sqlc.GetUserAlbumRatingsForIDsParams{
		UserID: userID, Column2: albumIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		out[r.AlbumID] = r.Rating
	}
	return out, nil
}

type UserRatedAlbumsPage struct {
	Items  []sqlc.ListUserRatedAlbumsRow `json:"items"`
	Total  int64                         `json:"total"`
	Limit  int32                         `json:"limit"`
	Offset int32                         `json:"offset"`
}

func (a *App) ListUserRatedAlbums(ctx context.Context, userID int64, minRating int16, limit, offset int32) (*UserRatedAlbumsPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if minRating < 1 {
		minRating = 1
	}
	q := sqlc.New(a.db)
	items, err := q.ListUserRatedAlbums(ctx, sqlc.ListUserRatedAlbumsParams{
		UserID: userID, MinRating: minRating, AlbumLimit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	total, err := q.CountUserRatedAlbums(ctx, sqlc.CountUserRatedAlbumsParams{
		UserID: userID, Rating: minRating,
	})
	if err != nil {
		return nil, err
	}
	return &UserRatedAlbumsPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ============== Artists ==============

func (a *App) SetUserArtistRating(ctx context.Context, userID, artistID int64, rating int16) error {
	if rating < 0 || rating > 10 {
		return fmt.Errorf("rating must be 0..10")
	}
	q := sqlc.New(a.db)
	if rating == 0 {
		return q.DeleteUserArtistRating(ctx, sqlc.DeleteUserArtistRatingParams{UserID: userID, ArtistID: artistID})
	}
	return q.SetUserArtistRating(ctx, sqlc.SetUserArtistRatingParams{
		UserID: userID, ArtistID: artistID, Rating: rating,
	})
}

func (a *App) GetUserArtistRating(ctx context.Context, userID, artistID int64) (int16, error) {
	r, err := sqlc.New(a.db).GetUserArtistRating(ctx, sqlc.GetUserArtistRatingParams{UserID: userID, ArtistID: artistID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return r, nil
}

func (a *App) RatingsForArtists(ctx context.Context, userID int64, artistIDs []int64) (map[int64]int16, error) {
	if len(artistIDs) == 0 {
		return map[int64]int16{}, nil
	}
	rows, err := sqlc.New(a.db).GetUserArtistRatingsForIDs(ctx, sqlc.GetUserArtistRatingsForIDsParams{
		UserID: userID, Column2: artistIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		out[r.ArtistID] = r.Rating
	}
	return out, nil
}

type UserRatedArtistsPage struct {
	Items  []sqlc.ListUserRatedArtistsRow `json:"items"`
	Total  int64                          `json:"total"`
	Limit  int32                          `json:"limit"`
	Offset int32                          `json:"offset"`
}

func (a *App) ListUserRatedArtists(ctx context.Context, userID int64, minRating int16, limit, offset int32) (*UserRatedArtistsPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if minRating < 1 {
		minRating = 1
	}
	q := sqlc.New(a.db)
	items, err := q.ListUserRatedArtists(ctx, sqlc.ListUserRatedArtistsParams{
		UserID: userID, MinRating: minRating, ArtistLimit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	total, err := q.CountUserRatedArtists(ctx, sqlc.CountUserRatedArtistsParams{
		UserID: userID, Rating: minRating,
	})
	if err != nil {
		return nil, err
	}
	return &UserRatedArtistsPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}
