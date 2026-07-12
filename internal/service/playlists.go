package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/slug"
)

// PlaylistDetail wraps a playlist row + its ordered tracks for the playlist
// page render. Wraps both so callers can render the hero + tracklist with
// one round-trip. HasCover is derived (cover_path != "") rather than a raw
// column so the FE never has to reason about path shapes — just render the
// cover endpoint when true.
type PlaylistDetail struct {
	Playlist sqlc.UserPlaylist            `json:"playlist"`
	Tracks   []sqlc.ListPlaylistTracksRow `json:"tracks"`
	HasCover bool                         `json:"has_cover"`
	Syncs    []PlaylistSyncView           `json:"syncs"`
}

// userPlaylistSlugExists adapts UserPlaylistSlugExists to slug.ExistsFunc —
// slug collisions are scoped per-user, not global (two users can each have
// a playlist named "Focus" and both get the plain "focus" slug).
func userPlaylistSlugExists(q *sqlc.Queries, userID int64) slug.ExistsFunc {
	return func(ctx context.Context, candidate string, excludeID int64) (bool, error) {
		return q.UserPlaylistSlugExists(ctx, sqlc.UserPlaylistSlugExistsParams{
			UserID: userID,
			Slug:   candidate,
			ID:     excludeID,
		})
	}
}

// CreateUserPlaylist creates a new playlist for the user. cover is optional.
func (a *App) CreateUserPlaylist(ctx context.Context, userID int64, name, description, cover string) (sqlc.UserPlaylist, error) {
	if name == "" {
		return sqlc.UserPlaylist{}, fmt.Errorf("playlist name required")
	}
	q := sqlc.New(a.db)
	// id=0 — no row exists yet, so nothing to exclude from the collision check.
	newSlug := slug.GenerateUnique(ctx, name, "", 0, userPlaylistSlugExists(q, userID))
	return q.CreateUserPlaylist(ctx, sqlc.CreateUserPlaylistParams{
		UserID:      userID,
		Name:        name,
		Description: description,
		CoverPath:   cover,
		Slug:        newSlug,
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
	detail, err := buildPlaylistDetail(ctx, q, pl)
	if err == nil {
		detail.Syncs, err = a.ListPlaylistSyncs(ctx, userID, pl.ID)
	}
	return detail, err
}

// GetUserPlaylistDetailByRef resolves the {id} path param on the playlist
// detail route, which — like the rest of the app's slug-addressed routes —
// accepts either the numeric ID or the URL slug. Numeric strings always
// resolve as an ID (playlist slugs never look like bare integers: the
// slugifier only emits [a-z0-9-], and a run of digits would need to collide
// with an actual all-digit name to be ambiguous, which GenerateUnique's
// dedup suffixing avoids in practice).
func (a *App) GetUserPlaylistDetailByRef(ctx context.Context, userID int64, ref string) (*PlaylistDetail, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return a.GetUserPlaylistDetail(ctx, userID, id)
	}
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylistBySlug(ctx, sqlc.GetUserPlaylistBySlugParams{Slug: ref, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("playlist not found: %w", err)
	}
	detail, err := buildPlaylistDetail(ctx, q, pl)
	if err == nil {
		detail.Syncs, err = a.ListPlaylistSyncs(ctx, userID, pl.ID)
	}
	return detail, err
}

// buildPlaylistDetail fetches the ordered tracklist for an already-resolved,
// already-ownership-checked playlist row and assembles the detail payload.
func buildPlaylistDetail(ctx context.Context, q *sqlc.Queries, pl sqlc.UserPlaylist) (*PlaylistDetail, error) {
	tracks, _ := q.ListPlaylistTracks(ctx, pl.ID)
	if tracks == nil {
		tracks = []sqlc.ListPlaylistTracksRow{}
	}
	return &PlaylistDetail{Playlist: pl, Tracks: tracks, HasCover: pl.CoverPath != "", Syncs: []PlaylistSyncView{}}, nil
}

// AddTrackToPlaylist appends a track to the end of the playlist (idempotent
// — already-present tracks are no-ops).
func (a *App) AddTrackToPlaylist(ctx context.Context, userID, playlistID, trackID int64) error {
	q := sqlc.New(a.db)
	// Ownership check — never trust the path parameter alone.
	if _, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID}); err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	if err := q.AddTrackToPlaylist(ctx, sqlc.AddTrackToPlaylistParams{PlaylistID: playlistID, TrackID: trackID}); err != nil {
		return err
	}
	a.TriggerPlaylistSync(userID, playlistID)
	return nil
}

// RemoveTrackFromPlaylist removes a track from a playlist.
func (a *App) RemoveTrackFromPlaylist(ctx context.Context, userID, playlistID, trackID int64) error {
	q := sqlc.New(a.db)
	if _, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID}); err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	if err := q.RemoveTrackFromPlaylist(ctx, sqlc.RemoveTrackFromPlaylistParams{PlaylistID: playlistID, TrackID: trackID}); err != nil {
		return err
	}
	a.TriggerPlaylistSync(userID, playlistID)
	return nil
}

// DeleteUserPlaylist drops a playlist and its tracks (cascade).
func (a *App) DeleteUserPlaylist(ctx context.Context, userID, playlistID int64) error {
	return sqlc.New(a.db).DeleteUserPlaylist(ctx, sqlc.DeleteUserPlaylistParams{ID: playlistID, UserID: userID})
}

// UpdateUserPlaylist renames / re-describes / re-covers an existing playlist.
// Renaming regenerates the slug (dev-phase convention: no legacy-URL shims —
// see CLAUDE.md) so the URL always reflects the current name; an unchanged
// name keeps the existing slug rather than paying a needless collision check.
func (a *App) UpdateUserPlaylist(ctx context.Context, userID, playlistID int64, name, description, cover string) error {
	q := sqlc.New(a.db)
	existing, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	newSlug := existing.Slug
	if name != existing.Name {
		newSlug = slug.GenerateUnique(ctx, name, "", playlistID, userPlaylistSlugExists(q, userID))
	}
	if err := q.UpdateUserPlaylist(ctx, sqlc.UpdateUserPlaylistParams{
		ID:          playlistID,
		UserID:      userID,
		Name:        name,
		Description: description,
		CoverPath:   cover,
		Slug:        newSlug,
	}); err != nil {
		return err
	}
	a.TriggerPlaylistSync(userID, playlistID)
	return nil
}

// SetUserPlaylistCover saves an uploaded cover image for a playlist, replacing
// any prior custom cover. Storage mirrors UploadMediaAsset's convention
// (internal/service/metadata_editor.go): files live under a per-owner
// directory keyed by ID rather than name, since playlist names/slugs churn
// on rename but the file must keep being found. cover_path is stored
// relative to DataDir so it composes the same way as every other on-disk
// asset path in the DB.
func (a *App) SetUserPlaylistCover(ctx context.Context, userID, playlistID int64, file io.Reader, filename string) error {
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = ".jpg"
	}
	destFilename := fmt.Sprintf("%d%s", playlistID, ext)

	dir := filepath.Join(a.config.DataDir.Value, "images", "playlists", strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cover dir: %w", err)
	}
	destPath := filepath.Join(dir, destFilename)

	// A prior cover saved under a different extension (re-upload as a
	// different format) would otherwise linger as an orphaned file.
	if pl.CoverPath != "" {
		if oldAbs := filepath.Join(a.config.DataDir.Value, pl.CoverPath); filepath.Base(oldAbs) != destFilename {
			_ = os.Remove(oldAbs)
		}
	}

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to save cover: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		return fmt.Errorf("failed to write cover: %w", err)
	}

	relPath := filepath.Join("images", "playlists", strconv.FormatInt(userID, 10), destFilename)
	return q.UpdateUserPlaylistCoverPath(ctx, sqlc.UpdateUserPlaylistCoverPathParams{
		ID:        playlistID,
		UserID:    userID,
		CoverPath: relPath,
	})
}

// ClearUserPlaylistCover removes a playlist's custom cover, both the DB
// pointer and the file on disk (a missing file is not an error — the DB
// row is the source of truth for "has a cover").
func (a *App) ClearUserPlaylistCover(ctx context.Context, userID, playlistID int64) error {
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	if pl.CoverPath != "" {
		_ = os.Remove(filepath.Join(a.config.DataDir.Value, pl.CoverPath))
	}
	return q.UpdateUserPlaylistCoverPath(ctx, sqlc.UpdateUserPlaylistCoverPathParams{
		ID:        playlistID,
		UserID:    userID,
		CoverPath: "",
	})
}

// GetUserPlaylistCoverPath resolves the absolute path to a playlist's stored
// custom cover, for the HTTP layer to stream. userID > 0 enforces ownership
// (used by any authenticated caller); userID == 0 skips the ownership
// filter, which is what the public <img>-facing GET route passes — like
// every other image endpoint in the app, that route is registered without
// auth (browsers can't attach a bearer token to an <img src>), so the cover
// bytes are served unconditionally by ID while playlist metadata itself
// stays ownership-gated. Returns "" (no error) when no custom cover is set.
func (a *App) GetUserPlaylistCoverPath(ctx context.Context, userID, playlistID int64) (string, error) {
	q := sqlc.New(a.db)
	var coverPath string
	var err error
	if userID > 0 {
		var pl sqlc.UserPlaylist
		pl, err = q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
		coverPath = pl.CoverPath
	} else {
		coverPath, err = q.GetUserPlaylistCoverPathByID(ctx, playlistID)
	}
	if err != nil {
		return "", fmt.Errorf("playlist not found: %w", err)
	}
	if coverPath == "" {
		return "", nil
	}
	return filepath.Join(a.config.DataDir.Value, coverPath), nil
}
