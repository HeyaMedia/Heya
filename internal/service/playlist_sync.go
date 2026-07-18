package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/playlistsync"
	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/rs/zerolog/log"
)

const lastFMPlaylistUnavailable = "Last.fm retired its playlist API; the remaining endpoints are deprecated and no longer supported"
const (
	playlistSyncTwoWay   = "two_way"
	playlistSyncPullOnly = "pull_only"
)

type PlaylistSyncView struct {
	Service      string     `json:"service"`
	ExternalID   string     `json:"external_id"`
	ExternalURL  string     `json:"external_url,omitempty"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	SyncMode     string     `json:"sync_mode"`
}

type ExternalPlaylistView struct {
	ExternalID    string     `json:"external_id"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	URL           string     `json:"url,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	TrackCount    int        `json:"track_count"`
	LocalPlaylist *int64     `json:"local_playlist_id,omitempty"`
	SyncMode      string     `json:"sync_mode,omitempty"`
}

type PlaylistCollectionView struct {
	Key         string                 `json:"key"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	AutoSync    bool                   `json:"auto_sync"`
	Playlists   []ExternalPlaylistView `json:"playlists"`
}

type PlaylistServiceCatalog struct {
	Service      string                    `json:"service"`
	Capabilities playlistsync.Capabilities `json:"capabilities"`
	Playlists    []ExternalPlaylistView    `json:"playlists"`
	Collections  []PlaylistCollectionView  `json:"collections"`
}

type playlistSyncLink struct {
	UserID       int64
	PlaylistID   int64
	Service      string
	ExternalID   string
	Series       string
	Snapshot     []string
	Unmatched    []string
	SyncMode     string
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
	if a.playlistProviderOverride != nil {
		return a.playlistProviderOverride(userID, service), nil
	}
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
		SELECT service, external_id, last_synced_at, last_error, sync_mode
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
		if err := rows.Scan(&v.Service, &v.ExternalID, &v.LastSyncedAt, &v.LastError, &v.SyncMode); err != nil {
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
	catalog := PlaylistServiceCatalog{Service: service, Capabilities: capabilities, Playlists: []ExternalPlaylistView{}, Collections: []PlaylistCollectionView{}}
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
	type linkedPlaylist struct {
		id   int64
		mode string
	}
	links := map[string]linkedPlaylist{}
	rows, err := a.db.Query(ctx, `
		SELECT external_id, playlist_id, sync_mode FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2`, userID, service)
	if err != nil {
		return catalog, err
	}
	for rows.Next() {
		var externalID string
		var playlistID int64
		var mode string
		if rows.Scan(&externalID, &playlistID, &mode) == nil {
			links[externalID] = linkedPlaylist{id: playlistID, mode: mode}
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
		if link, ok := links[p.ExternalID]; ok {
			id := link.id
			v.LocalPlaylist = &id
			v.SyncMode = link.mode
		}
		catalog.Playlists = append(catalog.Playlists, v)
	}
	if collectionProvider, ok := provider.(playlistsync.CollectionProvider); ok {
		for _, collection := range collectionProvider.Collections() {
			remote, err := collectionProvider.ListCollection(ctx, collection.Key)
			if err != nil {
				return catalog, err
			}
			view := PlaylistCollectionView{Key: collection.Key, Name: collection.Name, Description: collection.Description, Playlists: []ExternalPlaylistView{}}
			_ = a.db.QueryRow(ctx, `
				SELECT enabled FROM user_playlist_sync_policies
				WHERE user_id = $1 AND service = $2 AND collection = $3`, userID, service, collection.Key).Scan(&view.AutoSync)
			for _, p := range remote {
				item := ExternalPlaylistView{ExternalID: p.ExternalID, Name: p.Name, Description: p.Description, URL: p.URL, TrackCount: len(p.Tracks)}
				if !p.UpdatedAt.IsZero() {
					updated := p.UpdatedAt
					item.UpdatedAt = &updated
				}
				if link, ok := links[p.ExternalID]; ok {
					id := link.id
					item.LocalPlaylist = &id
					item.SyncMode = link.mode
				}
				view.Playlists = append(view.Playlists, item)
			}
			catalog.Collections = append(catalog.Collections, view)
		}
	}
	return catalog, nil
}

func (a *App) SetPlaylistCollectionPolicy(ctx context.Context, userID int64, service, collection string, enabled bool) error {
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return err
	}
	collectionProvider, ok := provider.(playlistsync.CollectionProvider)
	if !ok {
		return fmt.Errorf("%s does not expose generated playlist collections", service)
	}
	valid := false
	for _, candidate := range collectionProvider.Collections() {
		if candidate.Key == collection {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown %s playlist collection %q", service, collection)
	}
	_, err = a.db.Exec(ctx, `
		INSERT INTO user_playlist_sync_policies (user_id, service, collection, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, service, collection) DO UPDATE SET
			enabled = EXCLUDED.enabled, updated_at = now()`, userID, service, collection, enabled)
	if err != nil || !enabled {
		return err
	}
	return a.reconcilePlaylistCollection(ctx, userID, service, collection)
}

func (a *App) reconcilePlaylistCollection(ctx context.Context, userID int64, service, collection string) error {
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return err
	}
	collectionProvider, ok := provider.(playlistsync.CollectionProvider)
	if !ok {
		return fmt.Errorf("%s does not expose generated playlist collections", service)
	}
	remote, err := collectionProvider.ListCollection(ctx, collection)
	if err != nil {
		return err
	}
	editions := map[string][]playlistsync.Playlist{}
	for _, playlist := range remote {
		if playlist.SeriesKey != "" {
			editions[playlist.SeriesKey] = append(editions[playlist.SeriesKey], playlist)
			continue
		}
		if _, err := a.EnableExternalPlaylistSync(ctx, userID, service, playlist.ExternalID, true, playlistSyncPullOnly); err != nil {
			return err
		}
	}
	for seriesKey, series := range editions {
		if err := a.adoptSeriesEdition(ctx, userID, provider, service, seriesKey, series); err != nil {
			return err
		}
	}
	return nil
}

func seriesEditionTime(playlist playlistsync.Playlist) time.Time {
	if !playlist.CreatedAt.IsZero() {
		return playlist.CreatedAt
	}
	return playlist.UpdatedAt
}

// adoptSeriesEdition keeps exactly one local playlist per recurring provider
// series (Weekly Jams, Weekly Exploration, Daily Jams, …). Providers publish
// every edition as a brand-new remote playlist; instead of importing each one,
// the stable local playlist is re-pointed at the newest edition and refilled —
// Spotify Discover Weekly semantics.
func (a *App) adoptSeriesEdition(ctx context.Context, userID int64, provider playlistsync.Provider, service, seriesKey string, editions []playlistsync.Playlist) error {
	newest := editions[0]
	for _, candidate := range editions[1:] {
		if seriesEditionTime(candidate).After(seriesEditionTime(newest)) {
			newest = candidate
		}
	}
	var playlistID int64
	var linkedExternalID string
	err := a.db.QueryRow(ctx, `
		SELECT playlist_id, external_id FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2 AND series = $3`, userID, service, seriesKey).
		Scan(&playlistID, &linkedExternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		playlistID, linkedExternalID, err = a.claimLegacySeriesLink(ctx, userID, service, seriesKey, editions)
	}
	if err != nil {
		return err
	}
	a.cleanupSeriesDuplicates(ctx, userID, service, seriesKey, playlistID, editions)
	if playlistID == 0 {
		_, err := a.importExternalPlaylist(ctx, userID, service, provider, newest.ExternalID, playlistsync.SeriesDisplayName(seriesKey), seriesKey, playlistSyncPullOnly)
		return err
	}
	if linkedExternalID == newest.ExternalID {
		return nil
	}
	// A fresh edition replaced the linked one: re-point the link and pull the
	// new content into the same local playlist. The snapshot resets so the
	// merge treats the new edition as the authoritative track list.
	if _, err := a.db.Exec(ctx, `
		UPDATE user_playlist_syncs
		SET external_id = $4, snapshot_track_ids = '[]', unmatched_track_ids = '[]',
			last_error = '', updated_at = now()
		WHERE user_id = $1 AND service = $2 AND series = $3`, userID, service, seriesKey, newest.ExternalID); err != nil {
		return err
	}
	return a.SyncPlaylist(ctx, userID, playlistID, service)
}

// claimLegacySeriesLink upgrades per-edition links created before series
// tracking existed: the most recently linked edition becomes the series
// playlist and is renamed to the stable series name.
func (a *App) claimLegacySeriesLink(ctx context.Context, userID int64, service, seriesKey string, editions []playlistsync.Playlist) (int64, string, error) {
	ids := make([]string, 0, len(editions))
	for _, edition := range editions {
		ids = append(ids, edition.ExternalID)
	}
	var playlistID int64
	var externalID string
	err := a.db.QueryRow(ctx, `
		SELECT playlist_id, external_id FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2 AND series = '' AND external_id = ANY($3)
		ORDER BY created_at DESC LIMIT 1`, userID, service, ids).Scan(&playlistID, &externalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	name := playlistsync.SeriesDisplayName(seriesKey)
	newSlug := slug.GenerateUnique(ctx, name, "", playlistID, userPlaylistSlugExists(sqlc.New(a.db), userID))
	if _, err := a.db.Exec(ctx, `
		UPDATE user_playlists SET name = $3, slug = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2`, playlistID, userID, name, newSlug); err != nil {
		return 0, "", err
	}
	if _, err := a.db.Exec(ctx, `
		UPDATE user_playlist_syncs SET series = $4, updated_at = now()
		WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID, seriesKey); err != nil {
		return 0, "", err
	}
	return playlistID, externalID, nil
}

// cleanupSeriesDuplicates deletes the extra per-edition mirrors a series
// accumulated before series tracking (one local "Weekly Jams for …" playlist
// per week). Only auto-imported pull-only links are candidates — two-way
// links are user-created and never touched. Editions whose remote playlist
// already expired are caught by the per-edition title pattern.
func (a *App) cleanupSeriesDuplicates(ctx context.Context, userID int64, service, seriesKey string, keepPlaylistID int64, editions []playlistsync.Playlist) {
	ids := make([]string, 0, len(editions))
	for _, edition := range editions {
		ids = append(ids, edition.ExternalID)
	}
	namePattern := playlistsync.SeriesDisplayName(seriesKey) + " for %"
	rows, err := a.db.Query(ctx, `
		SELECT s.playlist_id FROM user_playlist_syncs s
		JOIN user_playlists p ON p.id = s.playlist_id
		WHERE s.user_id = $1 AND s.service = $2 AND s.series = '' AND s.sync_mode = 'pull_only'
			AND s.playlist_id <> $3
			AND (s.external_id = ANY($4) OR p.name LIKE $5)`,
		userID, service, keepPlaylistID, ids, namePattern)
	if err != nil {
		return
	}
	var stale []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if err := a.DeleteUserPlaylist(ctx, userID, id); err != nil {
			log.Warn().Err(err).Int64("playlist", id).Str("series", seriesKey).Msg("stale series playlist cleanup failed")
		}
	}
}

func (a *App) reconcileEnabledPlaylistCollections(ctx context.Context) {
	rows, err := a.db.Query(ctx, `
		SELECT user_id, service, collection
		FROM user_playlist_sync_policies WHERE enabled = true`)
	if err != nil {
		return
	}
	type policy struct {
		userID              int64
		service, collection string
	}
	var policies []policy
	for rows.Next() {
		var p policy
		if rows.Scan(&p.userID, &p.service, &p.collection) == nil {
			policies = append(policies, p)
		}
	}
	rows.Close()
	for _, p := range policies {
		if err := a.reconcilePlaylistCollection(ctx, p.userID, p.service, p.collection); err != nil {
			log.Debug().Err(err).Str("service", p.service).Str("collection", p.collection).Msg("playlist collection reconciliation failed")
		}
	}
}

// EnableLocalPlaylistSync creates a provider playlist from a local playlist.
// Disabling removes only the link: neither copy is destructively deleted.
func (a *App) EnableLocalPlaylistSync(ctx context.Context, userID, playlistID int64, service string, enabled bool) error {
	if !enabled {
		_, err := a.db.Exec(ctx, `DELETE FROM user_playlist_syncs WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service)
		return err
	}
	var alreadyLinked bool
	if err := a.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM user_playlist_syncs
			WHERE user_id = $1 AND playlist_id = $2 AND service = $3
		)`, userID, playlistID, service).Scan(&alreadyLinked); err != nil {
		return err
	}
	if alreadyLinked {
		return nil
	}
	capabilities := playlistSyncCapabilities(service)
	if !capabilities.Write {
		return fmt.Errorf("%s", capabilities.Reason)
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return err
	}
	pl, _, err := a.localPlaylistForSync(ctx, userID, playlistID, provider)
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
			(user_id, playlist_id, service, external_id, snapshot_track_ids, sync_mode, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, 'two_way', now())
		ON CONFLICT (playlist_id, service) DO NOTHING`, userID, playlistID, service, externalID, snapshot)
	return err
}

// EnableExternalPlaylistSync imports a provider playlist into Heya and links
// it. Re-selecting an already linked playlist is idempotent.
func (a *App) EnableExternalPlaylistSync(ctx context.Context, userID int64, service, externalID string, enabled bool, mode string) (int64, error) {
	if mode == "" {
		mode = playlistSyncTwoWay
	}
	if mode != playlistSyncTwoWay && mode != playlistSyncPullOnly {
		return 0, fmt.Errorf("invalid playlist sync mode %q", mode)
	}
	var existing int64
	err := a.db.QueryRow(ctx, `
		SELECT playlist_id FROM user_playlist_syncs
		WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID).Scan(&existing)
	if err == nil {
		if !enabled {
			_, err = a.db.Exec(ctx, `DELETE FROM user_playlist_syncs WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID)
		} else {
			_, err = a.db.Exec(ctx, `UPDATE user_playlist_syncs SET sync_mode = $4, updated_at = now() WHERE user_id = $1 AND service = $2 AND external_id = $3`, userID, service, externalID, mode)
		}
		return existing, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if !enabled {
		return 0, nil
	}
	capabilities := playlistSyncCapabilities(service)
	if !capabilities.Read || (mode == playlistSyncTwoWay && !capabilities.Write) {
		return 0, fmt.Errorf("%s", capabilities.Reason)
	}
	provider, err := a.playlistSyncProvider(ctx, userID, service)
	if err != nil {
		return 0, err
	}
	return a.importExternalPlaylist(ctx, userID, service, provider, externalID, "", "", mode)
}

// importExternalPlaylist creates the local playlist plus its sync link for a
// freshly linked remote playlist. An empty name keeps the remote title; series
// is empty for ordinary one-off playlists.
func (a *App) importExternalPlaylist(ctx context.Context, userID int64, service string, provider playlistsync.Provider, externalID, name, series, mode string) (int64, error) {
	remote, err := provider.Get(ctx, externalID)
	if err != nil {
		return 0, err
	}
	if name == "" {
		name = remote.Name
	}
	q := sqlc.New(a.db)
	newSlug := slug.GenerateUnique(ctx, name, "", 0, userPlaylistSlugExists(q, userID))
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var playlistID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO user_playlists (user_id, name, description, cover_path, slug)
		VALUES ($1, $2, $3, '', $4) RETURNING id`, userID, name, remote.Description, newSlug).Scan(&playlistID); err != nil {
		return 0, err
	}
	trackIDs, matched, err := a.resolveProviderTracks(ctx, provider, remote.Tracks)
	if err != nil {
		return 0, err
	}
	if err := replaceLocalPlaylistTracks(ctx, tx, playlistID, trackIDs); err != nil {
		return 0, err
	}
	snapshot, _ := json.Marshal(trackIDsFromProvider(remote.Tracks))
	unmatched, _ := json.Marshal(unmatchedTrackIDs(remote.Tracks, matched))
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_playlist_syncs
			(user_id, playlist_id, service, external_id, series, snapshot_track_ids, unmatched_track_ids, sync_mode, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())`, userID, playlistID, service, externalID, series, snapshot, unmatched, mode); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return playlistID, nil
}

func (a *App) loadPlaylistSyncLink(ctx context.Context, userID, playlistID int64, service string) (playlistSyncLink, error) {
	var link playlistSyncLink
	var raw, unmatchedRaw []byte
	err := a.db.QueryRow(ctx, `
		SELECT user_id, playlist_id, service, external_id, series, snapshot_track_ids, unmatched_track_ids, sync_mode, last_synced_at
		FROM user_playlist_syncs
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service).
		Scan(&link.UserID, &link.PlaylistID, &link.Service, &link.ExternalID, &link.Series, &raw, &unmatchedRaw, &link.SyncMode, &link.LastSyncedAt)
	if err != nil {
		return link, fmt.Errorf("playlist is not synced to %s", service)
	}
	_ = json.Unmarshal(raw, &link.Snapshot)
	_ = json.Unmarshal(unmatchedRaw, &link.Unmatched)
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
	remoteIDs := trackIDs(remote.Tracks)
	mergedIDs := remoteIDs
	mergedName, mergedDescription := remote.Name, remote.Description
	merged := remote
	if link.SyncMode != playlistSyncPullOnly {
		local, localUpdated, err := a.localPlaylistForSync(ctx, userID, playlistID, provider)
		if err != nil {
			return a.recordPlaylistSyncError(ctx, link, err)
		}

		// Provider tracks that could not exist locally at the last pass are carried
		// through the local side so absence isn't mistaken for a user deletion.
		localIDs := append(trackIDs(local.Tracks), link.Unmatched...)
		mergedIDs = playlistsync.MergeTrackIDs(link.Snapshot, localIDs, remoteIDs)
		localMetaChanged := link.LastSyncedAt == nil || localUpdated.After(*link.LastSyncedAt)
		remoteMetaChanged := link.LastSyncedAt == nil || (!remote.UpdatedAt.IsZero() && remote.UpdatedAt.After(*link.LastSyncedAt))
		mergedName, mergedDescription = local.Name, local.Description
		if remoteMetaChanged && !localMetaChanged {
			mergedName, mergedDescription = remote.Name, remote.Description
		}
		merged = playlistsync.Playlist{Name: mergedName, Description: mergedDescription, Tracks: providerTracks(mergedIDs)}

		if !sameStrings(mergedIDs, remoteIDs) || mergedName != remote.Name || mergedDescription != remote.Description {
			if err := provider.Replace(ctx, link.ExternalID, merged); err != nil {
				return a.recordPlaylistSyncError(ctx, link, err)
			}
		}
	}
	if link.Series != "" {
		// Series playlists keep their stable local identity ("Weekly Jams",
		// not "Weekly Jams for alice, week of …") — only the content follows
		// the provider's newest edition.
		if err := a.db.QueryRow(ctx, `
			SELECT name, description FROM user_playlists
			WHERE id = $1 AND user_id = $2`, playlistID, userID).Scan(&mergedName, &mergedDescription); err != nil {
			return a.recordPlaylistSyncError(ctx, link, err)
		}
	}
	resolved, matched, err := a.resolveProviderTracks(ctx, provider, merged.Tracks)
	if err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	if err := a.applySyncedPlaylist(ctx, userID, playlistID, provider, mergedName, mergedDescription, resolved); err != nil {
		return a.recordPlaylistSyncError(ctx, link, err)
	}
	snapshot, _ := json.Marshal(mergedIDs)
	unmatched, _ := json.Marshal(unmatchedTrackIDs(merged.Tracks, matched))
	_, err = a.db.Exec(ctx, `
		UPDATE user_playlist_syncs
		SET snapshot_track_ids = $4, unmatched_track_ids = $5,
			last_synced_at = now(), last_error = '', updated_at = now()
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, userID, playlistID, service, snapshot, unmatched)
	return err
}

func (a *App) recordPlaylistSyncError(ctx context.Context, link playlistSyncLink, syncErr error) error {
	_, _ = a.db.Exec(ctx, `
		UPDATE user_playlist_syncs SET last_error = $4, updated_at = now()
		WHERE user_id = $1 AND playlist_id = $2 AND service = $3`, link.UserID, link.PlaylistID, link.Service, syncErr.Error())
	return syncErr
}

func (a *App) localPlaylistForSync(ctx context.Context, userID, playlistID int64, provider playlistsync.Provider) (playlistsync.Playlist, time.Time, error) {
	var pl playlistsync.Playlist
	var updated time.Time
	if err := a.db.QueryRow(ctx, `
		SELECT name, description, updated_at FROM user_playlists
		WHERE id = $1 AND user_id = $2`, playlistID, userID).Scan(&pl.Name, &pl.Description, &updated); err != nil {
		return pl, updated, fmt.Errorf("playlist not found: %w", err)
	}
	identityExpr := "t.recording_mbid"
	identityArgs := []any{playlistID}
	switch provider.IdentityKind() {
	case playlistsync.IdentityISRC:
		identityExpr = "t.isrc"
	case playlistsync.IdentityServiceID:
		identityExpr = "COALESCE(t.external_ids::jsonb ->> $2, '')"
		identityArgs = append(identityArgs, provider.Service())
	}
	query := fmt.Sprintf(`
		SELECT %s, t.title, ar.name
		FROM user_playlist_tracks upt
		JOIN tracks t ON t.id = upt.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE upt.playlist_id = $1 AND %s <> ''
		ORDER BY upt.position, t.id`, identityExpr, identityExpr)
	rows, err := a.db.Query(ctx, query, identityArgs...)
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

func (a *App) resolveProviderTracks(ctx context.Context, provider playlistsync.Provider, tracks []playlistsync.Track) ([]int64, map[string]bool, error) {
	ids := make([]int64, 0, len(tracks))
	matched := map[string]bool{}
	seen := map[int64]bool{}
	for _, track := range tracks {
		var id int64
		var ok bool
		switch provider.IdentityKind() {
		case playlistsync.IdentityRecordingMBID:
			id, _, ok = a.matchListen(ctx, scrobble.Listen{
				RecordingMBID: track.ProviderID, TrackName: track.Title, ArtistName: track.Artist,
			})
		case playlistsync.IdentityISRC:
			ok = a.db.QueryRow(ctx, `SELECT id FROM tracks WHERE isrc = $1 ORDER BY id LIMIT 1`, track.ProviderID).Scan(&id) == nil
		case playlistsync.IdentityServiceID:
			ok = a.db.QueryRow(ctx, `
				SELECT id FROM tracks
				WHERE external_ids::jsonb ->> $1 = $2
				ORDER BY id LIMIT 1`, provider.Service(), track.ProviderID).Scan(&id) == nil
		}
		if !ok && track.Title != "" && track.Artist != "" {
			id, _, ok = a.matchListen(ctx, scrobble.Listen{TrackName: track.Title, ArtistName: track.Artist})
		}
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

func (a *App) applySyncedPlaylist(ctx context.Context, userID, playlistID int64, provider playlistsync.Provider, name, description string, trackIDs []int64) error {
	q := sqlc.New(a.db)
	existing, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: playlistID, UserID: userID})
	if err != nil {
		return err
	}
	newSlug := existing.Slug
	if name != existing.Name {
		newSlug = slug.GenerateUnique(ctx, name, "", playlistID, userPlaylistSlugExists(q, userID))
	}
	// ListenBrainz can only represent recordings with an MBID. Keep local-only
	// tracks in Heya (appended after the synchronized sequence) rather than
	// silently deleting them on the first remote pull.
	identityExpr := "t.recording_mbid"
	identityArgs := []any{playlistID}
	switch provider.IdentityKind() {
	case playlistsync.IdentityISRC:
		identityExpr = "t.isrc"
	case playlistsync.IdentityServiceID:
		identityExpr = "COALESCE(t.external_ids::jsonb ->> $2, '')"
		identityArgs = append(identityArgs, provider.Service())
	}
	rows, err := a.db.Query(ctx, fmt.Sprintf(`
		SELECT t.id
		FROM user_playlist_tracks upt
		JOIN tracks t ON t.id = upt.track_id
		WHERE upt.playlist_id = $1 AND %s = ''
		ORDER BY upt.position, t.id`, identityExpr), identityArgs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var trackID int64
		if rows.Scan(&trackID) == nil {
			trackIDs = append(trackIDs, trackID)
		}
	}
	rows.Close()
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
	a.startBackground(func() {
		ctx := a.LifetimeContext()
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
	})
}

func (a *App) runPlaylistSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reconcileEnabledPlaylistCollections(ctx)
			rows, err := a.db.Query(ctx, `SELECT user_id, playlist_id, service FROM user_playlist_syncs ORDER BY last_synced_at NULLS FIRST`)
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
				if err := a.SyncPlaylist(ctx, d.userID, d.playlistID, d.service); err != nil {
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
func unmatchedTrackIDs(tracks []playlistsync.Track, matched map[string]bool) []string {
	out := make([]string, 0)
	for _, track := range tracks {
		if track.ProviderID != "" && !matched[track.ProviderID] {
			out = append(out, track.ProviderID)
		}
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
