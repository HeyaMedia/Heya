package service

import (
	"context"
)

// Heart-band helpers over the music rating store (rating ≥ 9 = heart). The
// compat layers (Subsonic star/getStarred, Jellyfin favorites) read and write
// THESE instead of the legacy boolean user_favorites rows, so every client
// feeds the same taste signal the web app's reactions write.

const heartRating = 9 // band floor: ratings ≥9 render/count as hearted

// HeartedTrackIDs / HeartedAlbumIDs / HeartedArtistIDs return the id sets the
// compat layers stamp favorite flags from.
func (a *App) HeartedTrackIDs(ctx context.Context, userID int64) ([]int64, error) {
	return a.heartedIDs(ctx, userID, `SELECT track_id FROM user_track_ratings WHERE user_id = $1 AND rating >= $2`)
}

func (a *App) HeartedAlbumIDs(ctx context.Context, userID int64) ([]int64, error) {
	return a.heartedIDs(ctx, userID, `SELECT album_id FROM user_album_ratings WHERE user_id = $1 AND rating >= $2`)
}

func (a *App) HeartedArtistIDs(ctx context.Context, userID int64) ([]int64, error) {
	return a.heartedIDs(ctx, userID, `SELECT artist_id FROM user_artist_ratings WHERE user_id = $1 AND rating >= $2`)
}

// HeartedArtistMediaItemIDs is the Jellyfin-shaped variant: music artists are
// media_items rows in Jellyfin's id scheme, so the favorite decoration for
// MusicArtist DTOs needs media_item ids.
func (a *App) HeartedArtistMediaItemIDs(ctx context.Context, userID int64) ([]int64, error) {
	return a.heartedIDs(ctx, userID, `
		SELECT ar.media_item_id FROM user_artist_ratings uar
		JOIN artists ar ON ar.id = uar.artist_id
		WHERE uar.user_id = $1 AND uar.rating >= $2`)
}

func (a *App) heartedIDs(ctx context.Context, userID int64, query string) ([]int64, error) {
	rows, err := a.db.Query(ctx, query, userID, heartRating)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ArtistIDForMediaItem resolves a music artist's media_item id back to the
// artists row — Jellyfin favorites arrive keyed by media_item.
func (a *App) ArtistIDForMediaItem(ctx context.Context, mediaItemID int64) (int64, bool) {
	var id int64
	err := a.db.QueryRow(ctx,
		`SELECT id FROM artists WHERE media_item_id = $1`, mediaItemID).Scan(&id)
	return id, err == nil
}
