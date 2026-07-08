package matcher

import (
	"context"
	"strconv"
	"testing"
	"time"

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
	name := func(s string) string {
		return "find-sibling-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + s
	}

	t.Run("shared real MBID merges", func(t *testing.T) {
		// Two rows that resolved to the same real MBID (the HANABIE / 花冷え。
		// case): distinct names so creation doesn't collide, same MBID.
		hanabie := name("HANABIE")
		hanabieJP := name("花冷え。")
		src := seedBareArtist(t, ctx, qtx, libID, hanabie, "metalcore band", realMBID)
		dst := seedBareArtist(t, ctx, qtx, libID, hanabieJP, "metalcore band", realMBID)
		got, contradicted := m.findCanonicalSibling(ctx, src, realMBID, hanabieJP, "metalcore band")
		require.NotNil(t, got)
		require.False(t, contradicted)
		require.Equal(t, dst, got.ID)
	})

	t.Run("empty MBID + empty disambig does NOT merge", func(t *testing.T) {
		aviciiFolder := name("Avicii folder")
		aliciaKeys := name("Alicia Keys")
		src := seedBareArtist(t, ctx, qtx, libID, aviciiFolder, "", "")
		seedBareArtist(t, ctx, qtx, libID, aliciaKeys, "", "")
		// post-enrich resolved name collides, but no MBID and no disambiguation.
		got, contradicted := m.findCanonicalSibling(ctx, src, "", aliciaKeys, "")
		require.Nil(t, got)
		require.False(t, contradicted)
	})

	t.Run("synthetic MBID does NOT merge", func(t *testing.T) {
		synthA := name("Synth A")
		synthB := name("Synth B")
		src := seedBareArtist(t, ctx, qtx, libID, synthA, "", synthMBID)
		seedBareArtist(t, ctx, qtx, libID, synthB, "", synthMBID)
		got, contradicted := m.findCanonicalSibling(ctx, src, synthMBID, synthB, "")
		require.Nil(t, got)
		require.False(t, contradicted)
	})

	t.Run("same name + matching non-empty disambig merges", func(t *testing.T) {
		dupLatin := name("Dup Latin")
		dup := name("Dup")
		src := seedBareArtist(t, ctx, qtx, libID, dupLatin, "metalcore band", "")
		dst := seedBareArtist(t, ctx, qtx, libID, dup, "metalcore band", "")
		got, contradicted := m.findCanonicalSibling(ctx, src, "", dup, "metalcore band")
		require.NotNil(t, got)
		require.False(t, contradicted)
		require.Equal(t, dst, got.ID)
	})

	t.Run("same name + empty disambig does NOT merge (the 666 case)", func(t *testing.T) {
		sixA := name("666 a")
		six := name("666")
		src := seedBareArtist(t, ctx, qtx, libID, sixA, "", "")
		seedBareArtist(t, ctx, qtx, libID, six, "", "")
		got, contradicted := m.findCanonicalSibling(ctx, src, "", six, "")
		require.Nil(t, got)
		require.False(t, contradicted)
	})

	t.Run("same name + different disambig does NOT merge (the Ado case)", func(t *testing.T) {
		ado := name("Ado")
		src := seedBareArtist(t, ctx, qtx, libID, ado, "techno", "")
		seedBareArtist(t, ctx, qtx, libID, ado, "Japanese vocalist", "")
		got, contradicted := m.findCanonicalSibling(ctx, src, "", ado, "techno")
		require.Nil(t, got)
		require.False(t, contradicted)
	})

	t.Run("name+disambig sibling with contradicting MBID does NOT merge", func(t *testing.T) {
		// The pair upsertMusicArtist deliberately split (chimera vs real act):
		// a no-MBID upstream record whose (name, disambig) lands on the
		// sibling's tuple must not re-fuse them — both rows hold real,
		// differing MBIDs. contradicted=true also tells RefreshMusicArtist to
		// skip instead of tripping uq_artists_name_disambig on the UPDATE.
		const srcMBID = "1a2b3c4d-5678-49ab-8cde-0f1234567890"
		const otherMBID = "9c7902b0-1234-4e0e-8a8d-abcdefabcdef"
		splitSrc := name("Split Src")
		splitTwin := name("Split Twin")
		src := seedBareArtist(t, ctx, qtx, libID, splitSrc, "shared disambig", srcMBID)
		seedBareArtist(t, ctx, qtx, libID, splitTwin, "shared disambig", otherMBID)
		got, contradicted := m.findCanonicalSibling(ctx, src, srcMBID, splitTwin, "shared disambig")
		require.Nil(t, got)
		require.True(t, contradicted)
	})
}
