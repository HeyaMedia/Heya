package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// PlaylistDetail wraps a playlist row + its ordered tracks for the playlist
// page render. Wraps both so callers can render the hero + tracklist with
// one round-trip.
type PlaylistDetail struct {
	Playlist sqlc.UserPlaylist            `json:"playlist"`
	Tracks   []sqlc.ListPlaylistTracksRow `json:"tracks"`
}

// CreateUserPlaylist creates a new playlist for the user. cover is optional.
func (a *App) CreateUserPlaylist(ctx context.Context, userID int64, name, description, cover string) (sqlc.UserPlaylist, error) {
	if name == "" {
		return sqlc.UserPlaylist{}, fmt.Errorf("playlist name required")
	}
	return sqlc.New(a.db).CreateUserPlaylist(ctx, sqlc.CreateUserPlaylistParams{
		UserID:      userID,
		Name:        name,
		Description: description,
		CoverPath:   cover,
	})
}

// ListUserPlaylists returns the sidebar payload — every playlist the user
// owns with cover + count.
func (a *App) ListUserPlaylists(ctx context.Context, userID int64) ([]sqlc.ListUserPlaylistsRow, error) {
	rows, err := sqlc.New(a.db).ListUserPlaylists(ctx, userID)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []sqlc.ListUserPlaylistsRow{}, nil
	}
	return rows, nil
}

// GetUserPlaylistDetail returns the playlist + ordered tracks for the
// detail page. Returns an error when the playlist doesn't belong to the
// caller — the playlist row itself isn't user-visible until ownership
// matches.
func (a *App) GetUserPlaylistDetail(ctx context.Context, userID, playlistID int64) (*PlaylistDetail, error) {
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("playlist not found: %w", err)
	}
	tracks, _ := q.ListPlaylistTracks(ctx, playlistID)
	if tracks == nil {
		tracks = []sqlc.ListPlaylistTracksRow{}
	}
	return &PlaylistDetail{Playlist: pl, Tracks: tracks}, nil
}

// AddTrackToPlaylist appends a track to the end of the playlist (idempotent
// — already-present tracks are no-ops).
func (a *App) AddTrackToPlaylist(ctx context.Context, userID, playlistID, trackID int64) error {
	q := sqlc.New(a.db)
	// Ownership check — never trust the path parameter alone.
	if _, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID}); err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	return q.AddTrackToPlaylist(ctx, sqlc.AddTrackToPlaylistParams{PlaylistID: playlistID, TrackID: trackID})
}

// RemoveTrackFromPlaylist removes a track from a playlist.
func (a *App) RemoveTrackFromPlaylist(ctx context.Context, userID, playlistID, trackID int64) error {
	q := sqlc.New(a.db)
	if _, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID}); err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	return q.RemoveTrackFromPlaylist(ctx, sqlc.RemoveTrackFromPlaylistParams{PlaylistID: playlistID, TrackID: trackID})
}

// DeleteUserPlaylist drops a playlist and its tracks (cascade).
func (a *App) DeleteUserPlaylist(ctx context.Context, userID, playlistID int64) error {
	return sqlc.New(a.db).DeleteUserPlaylist(ctx, sqlc.DeleteUserPlaylistParams{ID: playlistID, UserID: userID})
}

// UpdateUserPlaylist renames / re-describes / re-covers an existing playlist.
func (a *App) UpdateUserPlaylist(ctx context.Context, userID, playlistID int64, name, description, cover string) error {
	return sqlc.New(a.db).UpdateUserPlaylist(ctx, sqlc.UpdateUserPlaylistParams{
		ID:          playlistID,
		UserID:      userID,
		Name:        name,
		Description: description,
		CoverPath:   cover,
	})
}
