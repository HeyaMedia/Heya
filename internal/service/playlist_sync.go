package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/playlistsync"
	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/rs/zerolog/log"
)

const lastFMPlaylistUnavailable = "Last.fm retired its playlist API; the remaining endpoints are deprecated and no longer supported"

type PlaylistSyncView struct {
	Service      string     `json:"service"`
	ExternalID   string     `json:"external_id"`
	ExternalURL  string     `json:"external_url,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

type ExternalPlaylistView struct {
	ExternalID    string     `json:"external_id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	URL           string     `json:"url,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	TrackCount    int        `json:"track_count"`
	LocalPlaylist *int64     `json:"local_playlist_id,omitempty"`
}

type PlaylistServiceCatalog struct {
	Service      string                    `json:"service"`
	Capabilities playlistsync.Capabilities `json:"capabilities"`
	Playlists    []ExternalPlaylistView    `json:"playlists"`
}

type playlistSyncLink struct {
	UserID       int64
	PlaylistID   int64
	Service      string
	ExternalID   string
	Snapshot     []string
	LastSyncedAt *time.Time
}

var playlistSyncLocks sync.Map

func playlistSyncCapabilities(service string) playlistsync.Capabilities {
	switch service {
	case "listenbrainz":
		return playlistsync.Capabilities{Available: true, Read: true, Write: true}
	case "lastfm":
		return playlistsync.Capabilities{Reason: lastFMPlaylistUnavailable}
	default:
		return playlistsync.Capabilities{Reason: "unknown playlist provider"}
	}
}

// playlistSyncProvider is the only credential-aware adapter registry. Adding
// Spotify/Tidal/Qobuz later means implementing playlistsync.Provider and
// registering its constructor here; merge, persistence, APIs and UI remain
// provider-neutral.
func (a *App) playlistSyncProvider(ctx context.Context, userID int64, service string) (playlistsync.Provider, error) {
	if service == "lastfm" {
		return playlistsync.Unsupported{Name: service, Reason: lastFMPlaylistUnavailable}, nil
	}
	if service != "listenbrainz" {
		return nil, fmt.Errorf("unknown playlist service %q", service)
	}
	var username, token string
	if err := a.db.QueryRow(ctx, `
		SELECT username, token FROM user_music_services
		WHERE user_id = $1 AND service = $2`, userID, service).Scan(&username, &token); err != nil || token == "" {
		return nil, fmt.Errorf("listenbrainz must be connected before syncing playlists")
	}
	return &playlistsync.ListenBrainz{Token: token, Username: username}, nil
}

func (a *App) ListPlaylistSyncs(ctx context.Context, userID, playlistID int64) ([]PlaylistSyncView, error) {
	rows, err := a.db.Query(ctx, `
		SELECT service, external_id, last_synced_at, last_error
		FROM user_playlist_syncs
		WHERE user_id = $1 AND playlist_id = $2
		ORDER BY service`, userID, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlaylistSyncView{}
	for rows.Next() {
		var v PlaylistSyncView
		if err := rows.Scan(&v.Service, &v.ExternalID, &v.LastSyncedAt, &v.LastError); err != nil {
			return nil, err
		}
		if v.Service == "listenbrainz" {
			v.ExternalURL = "https://listenbrainz.org/playlist/" + v.ExternalID
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (a *App) ListExternalPlaylists(ctx context.Context, userID int64, service string) (PlaylistServiceCatalog, error) {
	capabilities := playlistSyncCapabilities(service)
	catalog := PlaylistServiceCatalog{Service: service, Capabilities: capabilities, Playlists: []ExternalPlaylistView{}}
	if !capabilities.Available {
		return catalog, nil
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return catalog, err
	}
	remote, err := provider.List(ctx)
	if err != nil {
		return catalog, err
	}
	links := map[string]int64{}
	rows, err := a.db.Query(ctx, `
		SELECT external_id, playlist_id FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2`, userID, service)
	if err != nil {
		return catalog, err
	}
	for rows.Next() {
		var externalID string
		var playlistID int64
		if rows.Scan(&externalID, &playlistID) == nil {
			links[externalID] = playlistID
		}
	}
	rows.Close()
	for _, p := range remote {
		v := ExternalPlaylistView{
			ExternalID: p.ExternalID, Name: p.Name, Description: p.Description,
			URL: p.URL, TrackCount: len(p.Tracks),
		}
		if !p.UpdatedAt.IsZero() {
			updated := p.UpdatedAt
			v.UpdatedAt = &updated
		}
		if id, ok := links[p.ExternalID]; ok {
			v.LocalPlaylist = &id
		}
		catalog.Playlists = append(catalog.Playlists, v)
	}
	return catalog, nil
}

// EnableLocalPlaylistSync creates a provider playlist from a local playlist.
// Disabling removes only the link: neither copy is destructively deleted.
func (a *App) EnableLocalPlaylistSync(ctx context.Context, userID, playlistID int64, service string, enabled bool) error {
	if !enabled {
		_, err := a.db.Exec(ctx, `DELETE FROM user_playlist_syncs WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service)
		return err
	}
	capabilities := playlistSyncCapabilities(service)
	if !capabilities.Write {
		return fmt.Errorf("%s", capabilities.Reason)
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return err
	}
	pl, _, err := a.localPlaylistForSync(ctx, userID, playlistID)
	if err != nil {
		return err
	}
	externalID, err := provider.Create(ctx, pl)
	if err != nil {
		return err
	}
	snapshot, _ := json.Marshal(trackIDs(pl.Tracks))
	_, err = a.db.Exec(ctx, `
		INSERT INTO user_playlist_syncs
			(user_id, playlist_id, service, external_id, snapshot_track_ids, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (playlist_id, service) DO NOTHING`, userID, playlistID, service, externalID, snapshot)
	return err
}

// EnableExternalPlaylistSync imports a provider playlist into Heya and links
// it. Re-selecting an already linked playlist is idempotent.
func (a *App) EnableExternalPlaylistSync(ctx context.Context, userID int64, service, externalID string, enabled bool) (int64, error) {
	var existing int64
	err := a.db.QueryRow(ctx, `
		SELECT playlist_id FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID).Scan(&existing)
	if err == nil {
		if !enabled {
			_, err = a.db.Exec(ctx, `DELETE FROM user_playlist_syncs WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID)
		}
		return existing, err
	}
	if !enabled {
		return 0, nil
	}
	capabilities := playlistSyncCapabilities(service)
	if !capabilities.Read || !capabilities.Write {
		return 0, fmt.Errorf("%s", capabilities.Reason)
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return 0, err
	}
	remote, err := provider.Get(ctx, externalID)
	if err != nil {
		return 0, err
	}
	q := sqlc.New(a.db)
	newSlug := slug.GenerateUnique(ctx, remote.Name, "", 0, userPlaylistSlugExists(q, userID))
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var playlistID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO user_playlists (user_id, name, description, cover_path, slug)
		VALUES ($1, $2, $3, '', $4) RETURNING id`, userID, remote.Name, remote.Description, newSlug).Scan(&playlistID); err != nil {
		return 0, err
	}
	trackIDs, _, err := a.resolveProviderTracks(ctx, remote.Tracks)
	if err != nil {
		return 0, err
	}
	if err := replaceLocalPlaylistTracks(ctx, tx, playlistID, trackIDs); err != nil {
		return 0, err
	}
	snapshot, _ := json.Marshal(trackIDsFromProvider(remote.Tracks))
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_playlist_syncs
			(user_id, playlist_id, service, external_id, snapshot_track_ids, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, now())`, userID, playlistID, service, externalID, snapshot); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return playlistID, nil
}

func (a *App) loadPlaylistSyncLink(ctx context.Context, userID, playlistID int64, service string) (playlistSyncLink, error) {
	var link playlistSyncLink
	var raw []byte
	err := a.db.QueryRow(ctx, `
		SELECT user_id, playlist_id, service, external_id, snapshot_track_ids, last_synced_at
		FROM user_playlist_syncs
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service).
		Scan(&link.UserID, &link.PlaylistID, &link.Service, &link.ExternalID, &raw, &link.LastSyncedAt)
	if err != nil {
		return link, fmt.Errorf("playlist is not synced to %s", service)
	}
	_ = json.Unmarshal(raw, &link.Snapshot)
	return link, nil
}

func (a *App) SyncPlaylist(ctx context.Context, userID, playlistID int64, service string) error {
	key := fmt.Sprintf("%d/%s", playlistID, service)
	value, _ := playlistSyncLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	link, err := a.loadPlaylistSyncLink(ctx, userID, playlistID, service)
	if err != nil {
		return err
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	remote, err := provider.Get(ctx, link.ExternalID)
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	local, localUpdated, err := a.localPlaylistForSync(ctx, userID, playlistID)
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}

	// Provider tracks that cannot exist locally are carried through the local
	// side of the merge so a later pass does not interpret them as deletions.
	localIDs := trackIDs(local.Tracks)
	_, matchableBase, err := a.resolveProviderTracks(ctx, providerTracks(link.Snapshot))
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	for _, id := range link.Snapshot {
		if !matchableBase[id] {
			localIDs = append(localIDs, id)
		}
	}
	remoteIDs := trackIDs(remote.Tracks)
	mergedIDs := playlistsync.MergeTrackIDs(link.Snapshot, localIDs, remoteIDs)

	localMetaChanged := link.LastSyncedAt == nil || localUpdated.After(*link.LastSyncedAt)
	remoteMetaChanged := link.LastSyncedAt == nil || (!remote.UpdatedAt.IsZero() && remote.UpdatedAt.After(*link.LastSyncedAt))
	mergedName, mergedDescription := local.Name, local.Description
	if remoteMetaChanged && !localMetaChanged {
		mergedName, mergedDescription = remote.Name, remote.Description
	}
	merged := playlistsync.Playlist{Name: mergedName, Description: mergedDescription, Tracks: providerTracks(mergedIDs)}

	if !sameStrings(mergedIDs, remoteIDs) || mergedName != remote.Name || mergedDescription != remote.Description {
		if err := provider.Replace(ctx, link.ExternalID, merged); err != nil {
			return a.recordPlaylistSyncError(ctx, link, err)
		}
	}
	resolved, _, err := a.resolveProviderTracks(ctx, merged.Tracks)
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	if err := a.applySyncedPlaylist(ctx, userID, playlistID, mergedName, mergedDescription, resolved); err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	snapshot, _ := json.Marshal(mergedIDs)
	_, err = a.db.Exec(ctx, `
		UPDATE user_playlist_syncs
		SET snapshot_track_ids = $4, last_synced_at = now(), last_error = '', updated_at = now()
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service, snapshot)
	return err
}

func (a *App) recordPlaylistSyncError(ctx context.Context, link playlistSyncLink, syncErr error) error {
	_, _ = a.db.Exec(ctx, `
		UPDATE user_playlist_syncs SET last_error = $4, updated_at = now()
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, link.UserID, link.PlaylistID, link.Service, syncErr.Error())
	return syncErr
}

func (a *App) localPlaylistForSync(ctx context.Context, userID, playlistID int64) (playlistsync.Playlist, time.Time, error) {
	var pl playlistsync.Playlist
	var updated time.Time
	if err := a.db.QueryRow(ctx, `
		SELECT name, description, updated_at FROM user_playlists
		WHERE id = $1 AND user_id = $2`, playlistID, userID).Scan(&pl.Name, &pl.Description, &updated); err != nil {
		return pl, updated, fmt.Errorf("playlist not found: %w", err)
	}
	rows, err := a.db.Query(ctx, `
		SELECT t.recording_mbid, t.title, ar.name
		FROM user_playlist_tracks upt
		JOIN tracks t ON t.id = upt.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE upt.playlist_id = $1 AND t.recording_mbid <> ''
		ORDER BY upt.position, t.id`, playlistID)
	if err != nil {
		return pl, updated, err
	}
	defer rows.Close()
	for rows.Next() {
		var track playlistsync.Track
		if err := rows.Scan(&track.ProviderID, &track.Title, &track.Artist); err != nil {
			return pl, updated, err
		}
		pl.Tracks = append(pl.Tracks, track)
	}
	return pl, updated, rows.Err()
}

func (a *App) resolveProviderTracks(ctx context.Context, tracks []playlistsync.Track) ([]int64, map[string]bool, error) {
	ids := make([]int64, 0, len(tracks))
	matched := map[string]bool{}
	seen := map[int64]bool{}
	for _, track := range tracks {
		id, _, ok := a.matchListen(ctx, scrobble.Listen{
			RecordingMBID: track.ProviderID, TrackName: track.Title, ArtistName: track.Artist,
		})
		if !ok {
			continue
		}
		matched[track.ProviderID] = true
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, matched, nil
}

// execTx is the subset both pgx.Tx and pgxpool.Pool expose. Kept local to
// avoid coupling the provider package to database implementation details.
type execTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func replaceLocalPlaylistTracks(ctx context.Context, tx execTx, playlistID int64, trackIDs []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_playlist_tracks WHERE playlist_id = $1`, playlistID); err != nil {
		return err
	}
	for i, trackID := range trackIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_playlist_tracks (playlist_id, track_id, position)
			VALUES ($1, $2, $3) ON CONFLICT (playlist_id, track_id) DO NOTHING`, playlistID, trackID, i+1); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) applySyncedPlaylist(ctx context.Context, userID, playlistID int64, name, description string, trackIDs []int64) error {
	q := sqlc.New(a.db)
	existing, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return err
	}
	newSlug := existing.Slug
	if name != existing.Name {
		newSlug = slug.GenerateUnique(ctx, name, "", playlistID, userPlaylistSlugExists(q, userID))
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE user_playlists SET name = $3, description = $4, slug = $5, updated_at = now()
		WHERE id = $1 AND user_id = $2`, playlistID, userID, name, description, newSlug); err != nil {
		return err
	}
	if err := replaceLocalPlaylistTracks(ctx, tx, playlistID, trackIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *App) TriggerPlaylistSync(userID, playlistID int64) {
	ctx := a.LifetimeContext()
	go func() {
		rows, err := a.db.Query(ctx, `SELECT service FROM user_playlist_syncs WHERE user_id = $1 AND playlist_id = $2`, userID, playlistID)
		if err != nil {
			return
		}
		var services []string
		for rows.Next() {
			var service string
			if rows.Scan(&service) == nil {
				services = append(services, service)
			}
		}
		rows.Close()
		for _, service := range services {
			if err := a.SyncPlaylist(ctx, userID, playlistID, service); err != nil {
				log.Warn().Err(err).Str("service", service).Int64("playlist", playlistID).Msg("playlist sync failed")
			}
		}
	}()
}

func (a *App) runPlaylistSyncLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.LifetimeContext().Done():
			return
		case <-ticker.C:
			rows, err := a.db.Query(a.LifetimeContext(), `SELECT user_id, playlist_id, service FROM user_playlist_syncs ORDER BY last_synced_at NULLS FIRST`)
			if err != nil {
				continue
			}
			type due struct {
				userID, playlistID int64
				service            string
			}
			var links []due
			for rows.Next() {
				var d due
				if rows.Scan(&d.userID, &d.playlistID, &d.service) == nil {
					links = append(links, d)
				}
			}
			rows.Close()
			for _, d := range links {
				if err := a.SyncPlaylist(a.LifetimeContext(), d.userID, d.playlistID, d.service); err != nil {
					log.Debug().Err(err).Str("service", d.service).Int64("playlist", d.playlistID).Msg("periodic playlist sync failed")
				}
			}
		}
	}
}

func trackIDs(tracks []playlistsync.Track) []string { return trackIDsFromProvider(tracks) }
func trackIDsFromProvider(tracks []playlistsync.Track) []string {
	out := make([]string, 0, len(tracks))
	for _, track := range tracks {
		if track.ProviderID != "" {
			out = append(out, track.ProviderID)
		}
	}
	return out
}
func providerTracks(ids []string) []playlistsync.Track {
	out := make([]playlistsync.Track, 0, len(ids))
	for _, id := range ids {
		out = append(out, playlistsync.Track{ProviderID: id})
	}
	return out
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
