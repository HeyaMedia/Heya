package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
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

// Playlist cover mutations are rare and a single Heya process owns writes.
// Serializing their file+database lifecycle prevents metadata updates from
// resurrecting a concurrently-cleared path and cross-format uploads from
// deleting one another's freshly-published files.
var playlistCoverMutationMu sync.Mutex

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

// CreateUserPlaylist creates a new playlist for the user. Cover paths are
// server-owned and can only be set through SetUserPlaylistCover.
func (a *App) CreateUserPlaylist(ctx context.Context, userID int64, name, description string) (sqlc.UserPlaylist, error) {
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
		CoverPath:   "",
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
	tracks, _ := q.ListPlaylistTracks(ctx, sqlc.ListPlaylistTracksParams{PlaylistID: pl.ID, UserID: pl.UserID})
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

// DeleteUserPlaylist drops a playlist and its tracks (cascade), then removes
// its server-managed custom cover on a best-effort basis.
func (a *App) DeleteUserPlaylist(ctx context.Context, userID, playlistID int64) error {
	playlistCoverMutationMu.Lock()
	defer playlistCoverMutationMu.Unlock()
	q := sqlc.New(a.db)
	playlist, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	if err := q.DeleteUserPlaylist(ctx, sqlc.DeleteUserPlaylistParams{ID: playlistID, UserID: userID}); err != nil {
		return err
	}
	if a.config != nil && a.config.DataDir.Value != "" {
		if path, ok := managedPlaylistCoverPath(a.config.DataDir.Value, userID, playlistID, playlist.CoverPath); ok {
			_ = os.Remove(path)
		}
	}
	return nil
}

// SetPlaylistPin toggles one of the two independent pin scopes: "page"
// floats the playlist on /music/playlists, "sidebar" adds it to the left
// sidebar's pinned set. Neither touches updated_at — pinning isn't a content
// change and must not reshuffle "recently updated" sorts.
func (a *App) SetPlaylistPin(ctx context.Context, userID, playlistID int64, scope string, pinned bool) error {
	q := sqlc.New(a.db)
	switch scope {
	case "page":
		return q.SetPlaylistPagePin(ctx, sqlc.SetPlaylistPagePinParams{ID: playlistID, UserID: userID, Pinned: pinned})
	case "sidebar":
		return q.SetPlaylistSidebarPin(ctx, sqlc.SetPlaylistSidebarPinParams{ID: playlistID, UserID: userID, SidebarPinned: pinned})
	default:
		return fmt.Errorf("unknown pin scope %q (want page or sidebar)", scope)
	}
}

// SetSidebarPlaylistOrder persists the manual drag order for the sidebar.
// The FE always sends the complete list, so every row gets a fresh 1-based
// position each save.
func (a *App) SetSidebarPlaylistOrder(ctx context.Context, userID int64, ids []int64) error {
	q := sqlc.New(a.db)
	for i, id := range ids {
		if err := q.SetPlaylistSidebarPosition(ctx, sqlc.SetPlaylistSidebarPositionParams{
			ID: id, UserID: userID, SidebarPosition: int32(i + 1),
		}); err != nil {
			return err
		}
	}
	return nil
}

// UpdateUserPlaylist renames / re-describes an existing playlist. Dedicated
// upload/clear methods exclusively own CoverPath.
// Renaming regenerates the slug (dev-phase convention: no legacy-URL shims —
// see CLAUDE.md) so the URL always reflects the current name; an unchanged
// name keeps the existing slug rather than paying a needless collision check.
func (a *App) UpdateUserPlaylist(ctx context.Context, userID, playlistID int64, name, description string, tags []string) error {
	playlistCoverMutationMu.Lock()
	defer playlistCoverMutationMu.Unlock()
	q := sqlc.New(a.db)
	existing, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	newSlug := existing.Slug
	if name != existing.Name {
		newSlug = slug.GenerateUnique(ctx, name, "", playlistID, userPlaylistSlugExists(q, userID))
	}
	if tags == nil {
		tags = existing.Tags // omitted in the request → keep stored tags
	}
	cleaned := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] || len(cleaned) >= 16 {
			continue
		}
		seen[strings.ToLower(t)] = true
		cleaned = append(cleaned, t)
	}
	if err := q.UpdateUserPlaylist(ctx, sqlc.UpdateUserPlaylistParams{
		ID:          playlistID,
		UserID:      userID,
		Name:        name,
		Description: description,
		CoverPath:   existing.CoverPath,
		Slug:        newSlug,
		Tags:        cleaned,
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
func (a *App) SetUserPlaylistCover(ctx context.Context, userID, playlistID int64, file io.Reader) error {
	playlistCoverMutationMu.Lock()
	defer playlistCoverMutationMu.Unlock()
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}

	dir := filepath.Join(a.config.DataDir.Value, "images", "playlists", strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create cover dir: %w", err)
	}
	staged, err := images.StageRasterContext(ctx, dir, file)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidImageUpload, err)
	}
	defer func() { _ = staged.Rollback() }()
	destFilename := fmt.Sprintf("%d%s", playlistID, staged.Info.Extension)
	destPath := filepath.Join(dir, destFilename)
	if err := staged.Publish(destPath); err != nil {
		return fmt.Errorf("publish uploaded cover: %w", err)
	}

	relPath := filepath.Join("images", "playlists", strconv.FormatInt(userID, 10), destFilename)
	if err := q.UpdateUserPlaylistCoverPath(ctx, sqlc.UpdateUserPlaylistCoverPathParams{
		ID:        playlistID,
		UserID:    userID,
		CoverPath: relPath,
	}); err != nil {
		return err
	}
	if err := staged.Commit(); err != nil {
		return fmt.Errorf("commit uploaded cover: %w", err)
	}

	// A prior cover saved under another extension is now unreferenced.
	if pl.CoverPath != "" {
		oldAbs, managed := managedPlaylistCoverPath(a.config.DataDir.Value, userID, playlistID, pl.CoverPath)
		if managed && filepath.Clean(oldAbs) != filepath.Clean(destPath) {
			_ = os.Remove(oldAbs)
		}
	}
	return nil
}

// ClearUserPlaylistCover removes a playlist's custom cover, both the DB
// pointer and the file on disk (a missing file is not an error — the DB
// row is the source of truth for "has a cover").
func (a *App) ClearUserPlaylistCover(ctx context.Context, userID, playlistID int64) error {
	playlistCoverMutationMu.Lock()
	defer playlistCoverMutationMu.Unlock()
	q := sqlc.New(a.db)
	pl, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return fmt.Errorf("playlist not found: %w", err)
	}
	if err := q.UpdateUserPlaylistCoverPath(ctx, sqlc.UpdateUserPlaylistCoverPathParams{
		ID:        playlistID,
		UserID:    userID,
		CoverPath: "",
	}); err != nil {
		return err
	}
	if path, ok := managedPlaylistCoverPath(a.config.DataDir.Value, userID, playlistID, pl.CoverPath); ok {
		_ = os.Remove(path)
	}
	return nil
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
	resolved, ok := managedPlaylistCoverPath(a.config.DataDir.Value, userID, playlistID, coverPath)
	if !ok {
		return "", errors.New("playlist cover path is outside managed storage")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("playlist cover is not a regular file")
	}
	// SetUserPlaylistCover fully decodes before publication. Avoid repeating a
	// potentially expensive decode on this anonymous hot path; imageserve pins
	// the raster MIME from this allow-listed extension and sends nosniff.
	return resolved, nil
}

func managedPlaylistCoverPath(dataDir string, userID, playlistID int64, stored string) (string, bool) {
	if stored == "" || filepath.IsAbs(stored) || userID < 0 || playlistID <= 0 {
		return "", false
	}
	root, err := filepath.Abs(filepath.Join(dataDir, "images", "playlists"))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(dataDir, stored))
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	if len(parts) != 2 {
		return "", false
	}
	owner, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || owner <= 0 || (userID > 0 && owner != userID) {
		return "", false
	}
	wantStem := strconv.FormatInt(playlistID, 10)
	if strings.TrimSuffix(parts[1], filepath.Ext(parts[1])) != wantStem {
		return "", false
	}
	switch strings.ToLower(filepath.Ext(parts[1])) {
	case ".jpg", ".png", ".webp":
		realRoot, rootErr := filepath.EvalSymlinks(root)
		realCandidate, candidateErr := filepath.EvalSymlinks(candidate)
		if rootErr != nil {
			return "", false
		}
		if candidateErr == nil {
			realRelative, relErr := filepath.Rel(realRoot, realCandidate)
			if relErr != nil || realRelative == "." || realRelative == ".." || strings.HasPrefix(realRelative, ".."+string(filepath.Separator)) {
				return "", false
			}
			candidate = realCandidate
		} else if candidateErr != nil && !errors.Is(candidateErr, os.ErrNotExist) {
			return "", false
		}
		return candidate, true
	default:
		return "", false
	}
}
