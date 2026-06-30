package matcher

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// Exercises findCanonicalSibling's merge-trigger guards against a real Postgres
// inside a rolled-back transaction. These are the guards that stop the artist
// fusions seen on knas (Avicii→Alicia Keys via wrong MBID, Ado×2 via name).

func seedBareArtist(t *testing.T, ctx context.Context, qtx *sqlc.Queries, libID int64, name, disambig, mbid string) int64 {
	t.Helper()
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: libID, MediaType: sqlc.MediaTypeMusic, Title: name, SortTitle: name,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	a, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID: item.ID, Name: name, Disambiguation: disambig, MusicbrainzID: mbid,
	})
	require.NoError(t, err)
	return a.ID
}

func TestFindCanonicalSibling(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	m := &Matcher{q: qtx}

	_, libID := seedUserAndMusicLib(t, ctx, qtx)

	const realMBID = "c85cfd6b-b1e9-4a50-bd55-eb725f04f7d5"
	const synthMBID = "dddddddd-dddd-dddd-dddd-ddd513923292"

	t.Run("shared real MBID merges", func(t *testing.T) {
		// Two rows that resolved to the same real MBID (the HANABIE / 花冷え。
		// case): distinct names so creation doesn't collide, same MBID.
		src := seedBareArtist(t, ctx, qtx, libID, "HANABIE", "metalcore band", realMBID)
		dst := seedBareArtist(t, ctx, qtx, libID, "花冷え。", "metalcore band", realMBID)
		got := m.findCanonicalSibling(ctx, src, realMBID, "花冷え。", "metalcore band")
		require.NotNil(t, got)
		require.Equal(t, dst, got.ID)
	})

	t.Run("empty MBID + empty disambig does NOT merge", func(t *testing.T) {
		src := seedBareArtist(t, ctx, qtx, libID, "Avicii folder", "", "")
		seedBareArtist(t, ctx, qtx, libID, "Alicia Keys", "", "")
		// post-enrich resolved name collides, but no MBID and no disambiguation.
		require.Nil(t, m.findCanonicalSibling(ctx, src, "", "Alicia Keys", ""))
	})

	t.Run("synthetic MBID does NOT merge", func(t *testing.T) {
		src := seedBareArtist(t, ctx, qtx, libID, "Synth A", "", synthMBID)
		seedBareArtist(t, ctx, qtx, libID, "Synth B", "", synthMBID)
		require.Nil(t, m.findCanonicalSibling(ctx, src, synthMBID, "Synth B", ""))
	})

	t.Run("same name + matching non-empty disambig merges", func(t *testing.T) {
		src := seedBareArtist(t, ctx, qtx, libID, "Dup Latin", "metalcore band", "")
		dst := seedBareArtist(t, ctx, qtx, libID, "Dup", "metalcore band", "")
		got := m.findCanonicalSibling(ctx, src, "", "Dup", "metalcore band")
		require.NotNil(t, got)
		require.Equal(t, dst, got.ID)
	})

	t.Run("same name + empty disambig does NOT merge (the 666 case)", func(t *testing.T) {
		src := seedBareArtist(t, ctx, qtx, libID, "666 a", "", "")
		seedBareArtist(t, ctx, qtx, libID, "666", "", "")
		require.Nil(t, m.findCanonicalSibling(ctx, src, "", "666", ""))
	})

	t.Run("same name + different disambig does NOT merge (the Ado case)", func(t *testing.T) {
		src := seedBareArtist(t, ctx, qtx, libID, "Ado", "techno", "")
		seedBareArtist(t, ctx, qtx, libID, "Ado", "Japanese vocalist", "")
		require.Nil(t, m.findCanonicalSibling(ctx, src, "", "Ado", "techno"))
	})
}
