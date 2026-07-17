package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/playlistsync"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// fakeSeriesProvider serves canned collection listings and playlist bodies so
// the series adoption flow can run without network access.
type fakeSeriesProvider struct {
	collection []playlistsync.Playlist
	byID       map[string]playlistsync.Playlist
}

func (f *fakeSeriesProvider) Service() string { return "listenbrainz" }
func (f *fakeSeriesProvider) IdentityKind() playlistsync.IdentityKind {
	return playlistsync.IdentityRecordingMBID
}
func (f *fakeSeriesProvider) Capabilities() playlistsync.Capabilities {
	return playlistsync.Capabilities{Available: true, Read: true, Write: true}
}
func (f *fakeSeriesProvider) List(context.Context) ([]playlistsync.Playlist, error) {
	return nil, nil
}
func (f *fakeSeriesProvider) Get(_ context.Context, externalID string) (playlistsync.Playlist, error) {
	playlist, ok := f.byID[externalID]
	if !ok {
		return playlistsync.Playlist{}, fmt.Errorf("playlist %q not found", externalID)
	}
	return playlist, nil
}
func (f *fakeSeriesProvider) Create(context.Context, playlistsync.Playlist) (string, error) {
	return "", fmt.Errorf("create not expected for pull-only series")
}
func (f *fakeSeriesProvider) Replace(context.Context, string, playlistsync.Playlist) error {
	return fmt.Errorf("replace not expected for pull-only series")
}
func (f *fakeSeriesProvider) Collections() []playlistsync.Collection {
	return []playlistsync.Collection{{Key: "created_for", Name: "Created for You"}}
}
func (f *fakeSeriesProvider) ListCollection(_ context.Context, key string) ([]playlistsync.Playlist, error) {
	if key != "created_for" {
		return nil, fmt.Errorf("unknown collection %q", key)
	}
	return f.collection, nil
}

func seriesEdition(id, series string, created time.Time) playlistsync.Playlist {
	return playlistsync.Playlist{
		ExternalID: id,
		Name:       playlistsync.SeriesDisplayName(series) + " for alice, week of " + created.Format("2006-01-02 Mon"),
		SeriesKey:  series,
		CreatedAt:  created,
	}
}

func TestSeriesPlaylistAdoption(t *testing.T) {
	pool := testutil.SetupDB(t)
	userID := testutil.TestUserID(t, pool)
	ctx := context.Background()

	provider := &fakeSeriesProvider{byID: map[string]playlistsync.Playlist{}}
	app := &App{db: pool, playlistProviderOverride: func(int64, string) playlistsync.Provider {
		return provider
	}}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM user_playlists WHERE user_id = $1 AND (name LIKE 'Weekly Jams%' OR name LIKE 'Weekly Exploration%')`, userID)
	})

	week1 := seriesEdition("wj-1", "weekly-jams", time.Date(2026, 7, 5, 22, 0, 0, 0, time.UTC))
	week2 := seriesEdition("wj-2", "weekly-jams", time.Date(2026, 7, 12, 22, 0, 0, 0, time.UTC))
	exploration := seriesEdition("we-1", "weekly-exploration", time.Date(2026, 7, 12, 22, 10, 0, 0, time.UTC))
	for _, p := range []playlistsync.Playlist{week1, week2, exploration} {
		provider.byID[p.ExternalID] = p
	}

	// Legacy state from before series tracking: week 1 imported as its own
	// playlist, plus an expired edition whose remote is no longer listed.
	var legacyID, expiredID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO user_playlists (user_id, name, description, cover_path, slug)
		VALUES ($1, 'Weekly Jams for alice, week of 2026-07-06 Mon', '', '', 'wj-legacy-series-test') RETURNING id`, userID).Scan(&legacyID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO user_playlists (user_id, name, description, cover_path, slug)
		VALUES ($1, 'Weekly Jams for alice, week of 2026-06-29 Mon', '', '', 'wj-expired-series-test') RETURNING id`, userID).Scan(&expiredID))
	for playlistID, externalID := range map[int64]string{legacyID: "wj-1", expiredID: "wj-expired"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO user_playlist_syncs (user_id, playlist_id, service, external_id, sync_mode)
			VALUES ($1, $2, 'listenbrainz', $3, 'pull_only')`, userID, playlistID, externalID)
		require.NoError(t, err)
	}

	// Week 2 listing: old and new weekly-jams editions plus a series never
	// imported before. The legacy playlist must be claimed, renamed, and
	// re-pointed; the expired duplicate deleted; exploration imported fresh.
	provider.collection = []playlistsync.Playlist{week1, week2, exploration}
	require.NoError(t, app.reconcilePlaylistCollection(ctx, userID, "listenbrainz", "created_for"))

	var name, externalID, series string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT p.name, s.external_id, s.series FROM user_playlist_syncs s
		JOIN user_playlists p ON p.id = s.playlist_id
		WHERE s.user_id = $1 AND s.playlist_id = $2`, userID, legacyID).Scan(&name, &externalID, &series))
	require.Equal(t, "Weekly Jams", name)
	require.Equal(t, "wj-2", externalID)
	require.Equal(t, "weekly-jams", series)

	var expiredCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM user_playlists WHERE id = $1`, expiredID).Scan(&expiredCount))
	require.Zero(t, expiredCount, "expired per-edition mirror should be deleted")

	var explorationCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM user_playlist_syncs s JOIN user_playlists p ON p.id = s.playlist_id
		WHERE s.user_id = $1 AND s.series = 'weekly-exploration' AND p.name = 'Weekly Exploration'`, userID).Scan(&explorationCount))
	require.Equal(t, 1, explorationCount)

	// Reconciling again must be a no-op: still exactly one playlist per series.
	require.NoError(t, app.reconcilePlaylistCollection(ctx, userID, "listenbrainz", "created_for"))

	// Week 3 arrives; the same local playlist re-points instead of duplicating.
	week3 := seriesEdition("wj-3", "weekly-jams", time.Date(2026, 7, 19, 22, 0, 0, 0, time.UTC))
	provider.byID["wj-3"] = week3
	provider.collection = []playlistsync.Playlist{week2, week3, exploration}
	require.NoError(t, app.reconcilePlaylistCollection(ctx, userID, "listenbrainz", "created_for"))

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT p.name, s.external_id, s.series FROM user_playlist_syncs s
		JOIN user_playlists p ON p.id = s.playlist_id
		WHERE s.user_id = $1 AND s.playlist_id = $2`, userID, legacyID).Scan(&name, &externalID, &series))
	require.Equal(t, "Weekly Jams", name)
	require.Equal(t, "wj-3", externalID)

	var jamsCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM user_playlists WHERE user_id = $1 AND name LIKE 'Weekly Jams%'`, userID).Scan(&jamsCount))
	require.Equal(t, 1, jamsCount, "exactly one Weekly Jams playlist should exist")
}
