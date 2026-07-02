package matcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercises upsertMusicArtist against a media_items row that already claims
// the incoming MBID in external_ids ("squatting" idx_media_items_mbid_unique).
// This is the knas Big Red Machine → "Taylor Swift" chimera: a bad upstream
// merge stamped Taylor Swift's external ids onto Big Red Machine's media_item,
// after which the real Taylor Swift folder could never match — every scan
// died on the unique index with no way to resolve.

func seedArtistWithItemIDs(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID int64, name, disambig, artistMBID string, itemExternalIDs map[string]string) (artistID, itemID int64) {
	t.Helper()
	extJSON, err := json.Marshal(itemExternalIDs)
	require.NoError(t, err)
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: libID, MediaType: sqlc.MediaTypeMusic, Title: name, SortTitle: name,
		ExternalIds: extJSON,
	})
	require.NoError(t, err)
	a, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: item.ID, Name: name, Disambiguation: disambig, MusicbrainzID: artistMBID,
	})
	require.NoError(t, err)
	return a.ID, item.ID
}

func itemExternalIDs(t *testing.T, ctx context.Context, qtx *sqlc.Queries, itemID int64) map[string]string {
	t.Helper()
	item, err := qtx.GetMediaItemByID(ctx, itemID)
	require.NoError(t, err)
	ids := map[string]string{}
	require.NoError(t, json.Unmarshal(item.ExternalIds, &ids))
	return ids
}

func TestUpsertMusicArtistMBIDSquatter(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx}

	_, libID := seedUserAndMusicLib(t, ctx, qtx)

	const taylorMBID = "20244d07-534f-4eff-b4d4-930878889970"
	const brmMBID = "6757d72a-3ac9-4ccb-b69b-39f691477180"

	t.Run("conflicting squatter gets a separate artist without the mbid key", func(t *testing.T) {
		// The chimera: an artist row carrying Big Red Machine's MBID whose
		// media_item was stamped with Taylor Swift's mbid by a bad enrich.
		squatterID, squatterItemID := seedArtistWithItemIDs(t, ctx, qtx, libID,
			"Taylor Swift", "Bon Iver's Justin Vernon and The National's Aaron Dessner", brmMBID,
			map[string]string{"mbid": taylorMBID, "musicbrainz_artist": brmMBID})

		got, err := m.upsertMusicArtist(ctx, libID, "Taylor Swift", "", taylorMBID, "", "")
		require.NoError(t, err)
		require.NotEqual(t, squatterID, got.ID, "must not fuse into the chimera artist")
		require.Equal(t, taylorMBID, got.MusicbrainzID)

		// The new media_item must NOT carry the mbid key (it's squatted), but
		// keeps the identity under musicbrainz_artist.
		ids := itemExternalIDs(t, ctx, qtx, got.MediaItemID)
		require.NotContains(t, ids, "mbid")
		require.Equal(t, taylorMBID, ids["musicbrainz_artist"])

		// The squatter is untouched.
		squatterIDs := itemExternalIDs(t, ctx, qtx, squatterItemID)
		require.Equal(t, taylorMBID, squatterIDs["mbid"])

		// Re-running resolves to the created artist (via artists.musicbrainz_id).
		again, err := m.upsertMusicArtist(ctx, libID, "Taylor Swift", "", taylorMBID, "", "")
		require.NoError(t, err)
		require.Equal(t, got.ID, again.ID)
	})

	t.Run("compatible squatter is adopted and backfilled", func(t *testing.T) {
		// Divergence without conflict: the media_item claims the MBID but the
		// artists row never got the backfill. Name is different so only the
		// media-item join can resolve it.
		const mbid = "c85cfd6b-b1e9-4a50-bd55-eb725f04f7d5"
		artistID, _ := seedArtistWithItemIDs(t, ctx, qtx, libID,
			"HANABIE", "metalcore band", "",
			map[string]string{"mbid": mbid})

		got, err := m.upsertMusicArtist(ctx, libID, "花冷え。", "", mbid, "", "")
		require.NoError(t, err)
		require.Equal(t, artistID, got.ID, "same act — adopt the existing row")
		require.Equal(t, mbid, got.MusicbrainzID, "musicbrainz_id backfilled from the media_item claim")
	})

	t.Run("conflicting squatter is not re-adopted via name match", func(t *testing.T) {
		// Same chimera, but this time the (name, disambiguation) tuple matches
		// exactly (both empty disambig) — the name lookup would return the very
		// row the squatter branch refused. The MBID contradiction must veto
		// name adoption, and the new row needs a disambiguated tuple or it
		// trips uq_artists_name_disambig.
		const ourMBID = "11111111-aaaa-bbbb-cccc-222222222222"
		const theirMBID = "33333333-dddd-eeee-ffff-444444444444"
		squatterID, _ := seedArtistWithItemIDs(t, ctx, qtx, libID,
			"Phoenix Chimera", "", theirMBID,
			map[string]string{"mbid": ourMBID})

		got, err := m.upsertMusicArtist(ctx, libID, "Phoenix Chimera", "", ourMBID, "", "")
		require.NoError(t, err)
		require.NotEqual(t, squatterID, got.ID, "name match must not overrule the MBID contradiction")
		require.Equal(t, ourMBID, got.MusicbrainzID)
		require.Contains(t, got.Disambiguation, "(mbid 11111111)", "tuple disambiguated to dodge uq_artists_name_disambig")
		ids := itemExternalIDs(t, ctx, qtx, got.MediaItemID)
		require.NotContains(t, ids, "mbid")
	})

	t.Run("same name with contradicting MBID and no squat stays separate", func(t *testing.T) {
		// The legit two-acts-one-name case (e.g. "666"): the existing row has a
		// different established MBID but nothing squats the media_items index,
		// so the new artist keeps its mbid key.
		const mbidA = "55555555-aaaa-bbbb-cccc-666666666666"
		const mbidB = "77777777-dddd-eeee-ffff-888888888888"
		existingID, _ := seedArtistWithItemIDs(t, ctx, qtx, libID,
			"666", "", mbidA, map[string]string{})

		got, err := m.upsertMusicArtist(ctx, libID, "666", "", mbidB, "", "")
		require.NoError(t, err)
		require.NotEqual(t, existingID, got.ID, "different act — must not fuse")
		require.Equal(t, mbidB, got.MusicbrainzID)
		ids := itemExternalIDs(t, ctx, qtx, got.MediaItemID)
		require.Equal(t, mbidB, ids["mbid"], "no squat — the mbid key is kept")
	})

	t.Run("no squatter creates normally with the mbid key", func(t *testing.T) {
		const mbid = "aaaaaaaa-1111-2222-3333-444444444444"
		got, err := m.upsertMusicArtist(ctx, libID, "Fresh Act", "", mbid, "", "")
		require.NoError(t, err)
		require.Equal(t, mbid, got.MusicbrainzID)
		ids := itemExternalIDs(t, ctx, qtx, got.MediaItemID)
		require.Equal(t, mbid, ids["mbid"])
		require.Equal(t, mbid, ids["musicbrainz_artist"])
	})
}
