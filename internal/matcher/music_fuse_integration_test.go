package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/stretchr/testify/require"
)

// tagInfo builds a fake ffprobe result carrying the given embedded tags plus a
// minimal audio stream, standing in for a real probe of a track file.
func tagInfo(albumArtist, album, title, date string, track int) *mediaprobe.MediaInfo {
	tags := map[string]string{}
	if albumArtist != "" {
		tags["ALBUMARTIST"] = albumArtist
	}
	if album != "" {
		tags["ALBUM"] = album
	}
	if title != "" {
		tags["TITLE"] = title
	}
	if date != "" {
		tags["DATE"] = date
	}
	if track > 0 {
		tags["TRACK"] = strconv.Itoa(track)
	}
	return rawTagInfo(tags)
}

// TestMusicTagFusion_LeadProbeNotDoubled guards the cached-nil vs absent-key
// trap: a failed lead-track probe is cached as nil, and the per-track loop must
// reuse that (not re-probe), so a stalled file costs one timeout, not two.
func TestMusicTagFusion_LeadProbeNotDoubled(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)
	_, libID := seedUserAndMusicLib(t, ctx, qtx)

	var probeCalls int
	m := &Matcher{q: qtx, probe: func(_ context.Context, _ string) (*mediaprobe.MediaInfo, error) {
		probeCalls++
		return nil, context.DeadlineExceeded // simulate a stalled/failed probe
	}}

	// Curated path supplies artist+album; "01.flac" has no title so the track
	// isn't path-complete and would be probed — but it IS the lead track, whose
	// (failed) probe is already cached.
	p := "/x/Counter Artist - Album - 2020 - CAlbum/01.flac"
	pr, err := json.Marshal(parser.ParseStoragePath(p))
	require.NoError(t, err)
	f, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: libID, Path: p, ParseResult: pr, Status: sqlc.FileStatusPending,
	})
	require.NoError(t, err)

	matched, _, errored, _ := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f})
	require.Equal(t, 1, matched)
	require.Zero(t, errored)
	require.Equal(t, 1, probeCalls, "the lead track must be probed at most once even when the probe fails")
}

// rawTagInfo wraps an explicit tag map in a probe result with a minimal audio
// stream.
func rawTagInfo(tags map[string]string) *mediaprobe.MediaInfo {
	return &mediaprobe.MediaInfo{
		Format:  mediaprobe.FormatInfo{FormatName: "flac", Tags: tags},
		Streams: []mediaprobe.StreamInfo{{CodecType: "audio", CodecName: "flac", SampleRate: "44100", Channels: 2}},
	}
}

// TestMusicTagFusion exercises the wired matchMusicGroup fusion path against a
// real Postgres (rolled back): a scene-garbage release whose PATH yields no
// artist/album must still materialize a proper artist → album → track chain
// from embedded tags, with the collapse and Unknown-Artist guards enforced.
func TestMusicTagFusion(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	_, libID := seedUserAndMusicLib(t, ctx, qtx)

	// A prober keyed by path; files absent from the map probe to "no tags".
	infoByPath := map[string]*mediaprobe.MediaInfo{}
	m := &Matcher{q: qtx, probe: func(_ context.Context, path string) (*mediaprobe.MediaInfo, error) {
		return infoByPath[path], nil
	}}

	// Empty parse_result → no path-derived artist/album/track, forcing tag reliance.
	mkFile := func(path string) sqlc.LibraryFile {
		f, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: libID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		return f
	}
	tracksOf := func(albumID int64) []string {
		rows, err := tx.Query(ctx, `SELECT title FROM tracks WHERE album_id=$1 ORDER BY disc_number, track_number`, albumID)
		require.NoError(t, err)
		defer rows.Close()
		var titles []string
		for rows.Next() {
			var s string
			require.NoError(t, rows.Scan(&s))
			titles = append(titles, s)
		}
		return titles
	}

	t.Run("scene-garbage path resolves from tags", func(t *testing.T) {
		dir := "/x/unsorted/someartist-somealbum-2015-releasegroupqualityblabla_asdf_fdsa_mp3_256k"
		f1 := mkFile(dir + "/01.flac")
		f2 := mkFile(dir + "/02.flac")
		infoByPath[f1.Path] = tagInfo("Scene Artist", "Scene Album", "First Song", "2015", 1)
		infoByPath[f2.Path] = tagInfo("Scene Artist", "Scene Album", "Second Song", "2015", 2)

		matched, unmatched, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f1, f2})
		require.Equal(t, 2, matched)
		require.Zero(t, unmatched)
		require.Zero(t, errored)
		require.NotZero(t, artistID)

		artist, err := qtx.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{Lower: "Scene Artist", Lower_2: ""})
		require.NoError(t, err, "artist must be created from the ALBUMARTIST tag")
		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artist.ID, Lower: "Scene Album", Year: "2015"})
		require.NoError(t, err, "album must be created from ALBUM + DATE tags")
		require.Equal(t, []string{"First Song", "Second Song"}, tracksOf(album.ID))

		lf1, err := qtx.GetLibraryFileByID(ctx, f1.ID)
		require.NoError(t, err)
		require.Equal(t, sqlc.FileStatusMatched, lf1.Status)
	})

	t.Run("folder consensus rejects a poisoned NFO and lead-track outlier", func(t *testing.T) {
		const (
			asacoMBID  = "bb4297af-90c2-4bb2-bd50-67951921c9c4"
			djPaulMBID = "43906e48-a7c0-4b80-a5dd-37d1fe6ccdb9"
			wrongAlbum = "97470000-0000-4000-8000-000000000000"
		)
		artistName := fmt.Sprintf("Asaco Consensus %d", libID)
		artistDir := filepath.Join(t.TempDir(), artistName)
		releaseDir := filepath.Join(artistDir, artistName+" - Nomake Story")
		require.NoError(t, os.MkdirAll(releaseDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(artistDir, "artist.nfo"), []byte(fmt.Sprintf(`
<artist>
  <name>DJ Paul</name>
  <musicbrainzartistid>%s</musicbrainzartistid>
</artist>`, djPaulMBID)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(releaseDir, "album.nfo"), []byte(fmt.Sprintf(`
<album>
  <title>Nomake Story</title>
  <albumartist>DJ Paul</albumartist>
  <year>2010</year>
  <musicbrainzalbumid>%s</musicbrainzalbumid>
  <musicbrainzalbumartistid>%s</musicbrainzalbumartistid>
</album>`, wrongAlbum, djPaulMBID)), 0o644))

		group := make([]sqlc.LibraryFile, 0, 10)
		for i := 1; i <= 10; i++ {
			path := filepath.Join(releaseDir, fmt.Sprintf("%02d - Track %02d.flac", i, i))
			parsed, err := json.Marshal(parser.ParseStoragePath(path))
			require.NoError(t, err)
			file, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
				LibraryID: libID, Path: path, ParseResult: parsed, Status: sqlc.FileStatusPending,
			})
			require.NoError(t, err)
			group = append(group, file)
			if i == 1 {
				// Put the poisoned file first to prove scan order cannot make it
				// representative of the entire release.
				infoByPath[path] = rawTagInfo(map[string]string{
					"ALBUMARTIST":               "DJ Paul",
					"MUSICBRAINZ_ALBUMARTISTID": djPaulMBID,
					"ALBUM":                     "To Kill Again...The Mixtape",
					"MUSICBRAINZ_ALBUMID":       wrongAlbum,
					"DATE":                      "2010",
					"TITLE":                     fmt.Sprintf("Track %02d", i),
					"TRACK":                     strconv.Itoa(i),
				})
				continue
			}
			infoByPath[path] = rawTagInfo(map[string]string{
				"ALBUMARTIST":               artistName,
				"MUSICBRAINZ_ALBUMARTISTID": asacoMBID,
				"ALBUM":                     "Nomake Story",
				"DATE":                      "2020",
				"TITLE":                     fmt.Sprintf("Track %02d", i),
				"TRACK":                     strconv.Itoa(i),
			})
		}

		matched, unmatched, errored, artistID := m.matchMusicGroup(ctx, libID, group)
		require.Equal(t, 10, matched)
		require.Zero(t, unmatched)
		require.Zero(t, errored)

		var gotName, gotMBID string
		require.NoError(t, tx.QueryRow(ctx, `SELECT name, musicbrainz_id FROM artists WHERE id=$1`, artistID).Scan(&gotName, &gotMBID))
		require.Equal(t, artistName, gotName)
		require.Equal(t, asacoMBID, gotMBID)
		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artistID, Lower: "Nomake Story", Year: "2020"})
		require.NoError(t, err)
		require.NotEqual(t, wrongAlbum, album.MusicbrainzID, "outlier/NFO release MBID must be quarantined")
	})

	t.Run("collapse guard: untagged, unnumbered files stay distinct", func(t *testing.T) {
		dir := "/x/unsorted/collapse-dump"
		f1 := mkFile(dir + "/track_a.flac")
		f2 := mkFile(dir + "/track_b.flac")
		// Titles present, but NO track numbers on either file — both fuse to 0.
		infoByPath[f1.Path] = tagInfo("Collapse Artist", "Collapse Album", "Alpha", "2019", 0)
		infoByPath[f2.Path] = tagInfo("Collapse Artist", "Collapse Album", "Beta", "2019", 0)

		matched, _, errored, _ := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f1, f2})
		require.Equal(t, 2, matched)
		require.Zero(t, errored)

		artist, err := qtx.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{Lower: "Collapse Artist", Lower_2: ""})
		require.NoError(t, err)
		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artist.ID, Lower: "Collapse Album", Year: "2019"})
		require.NoError(t, err)
		// Two distinct tracks — NOT collapsed onto a single (album, disc, 0) row.
		require.Len(t, tracksOf(album.ID), 2, "the collapse guard must keep unnumbered files as separate tracks")
	})

	t.Run("path-foldered Various Artists is kept (not treated as junk)", func(t *testing.T) {
		// A human deliberately foldered "Various Artists" — a legitimate shared
		// bucket. The path supplies the name (no tags), so it must be created,
		// exactly as before tag fusion. Only tag-ONLY placeholders are rejected.
		p := "/x/Various Artists - Album - 2020 - Summer Hits/01 - Opener.flac"
		parsed := parser.ParseStoragePath(p)
		require.NotNil(t, parsed.Release)
		require.Equal(t, "Various Artists", parsed.Release.Artist)
		pr, err := json.Marshal(parsed)
		require.NoError(t, err)
		f, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: libID, Path: p, ParseResult: pr, Status: sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		// No tag mapping for this path → prober returns nil → path-only.

		matched, unmatched, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f})
		require.Equal(t, 1, matched)
		require.Zero(t, unmatched)
		require.Zero(t, errored)
		require.NotZero(t, artistID)
		_, err = qtx.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{Lower: "Various Artists", Lower_2: ""})
		require.NoError(t, err, "a path-foldered Various Artists must still be created")
	})

	t.Run("Unknown-Artist tags do not create a global poison row", func(t *testing.T) {
		dir := "/x/unsorted/junk-rip"
		f1 := mkFile(dir + "/01.flac")
		infoByPath[f1.Path] = tagInfo("Unknown Artist", "Some Album", "Whatever", "2020", 1)

		matched, unmatched, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f1})
		require.Zero(t, matched)
		require.Equal(t, 1, unmatched, "a placeholder artist must leave the file retryable-unmatched")
		require.Zero(t, errored)
		require.Zero(t, artistID)

		_, err := qtx.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{Lower: "Unknown Artist", Lower_2: ""})
		require.Error(t, err, "no global 'Unknown Artist' row may be created")
	})

	// mkParsed inserts a file whose parse_result is the parser's real output for
	// its path (curated folders → path-complete track info, no tags needed).
	mkParsed := func(p string) sqlc.LibraryFile {
		pr, err := json.Marshal(parser.ParseStoragePath(p))
		require.NoError(t, err)
		f, err := qtx.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: libID, Path: p, ParseResult: pr, Status: sqlc.FileStatusPending,
		})
		require.NoError(t, err)
		return f
	}
	countRows := func(q string, arg int64) int {
		var n int
		require.NoError(t, tx.QueryRow(ctx, q, arg).Scan(&n))
		return n
	}

	t.Run("same-number quality alternates merge onto one track", func(t *testing.T) {
		dir := "/x/Quality Artist - Album - 2020 - QAlbum"
		fFlac := mkParsed(dir + "/01 - Song.flac")
		fMp3 := mkParsed(dir + "/01 - Song.mp3")

		matched, _, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{fFlac, fMp3})
		require.Equal(t, 2, matched)
		require.Zero(t, errored)

		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artistID, Lower: "QAlbum", Year: "2020"})
		require.NoError(t, err)
		require.Equal(t, 1, countRows(`SELECT count(*) FROM tracks WHERE album_id=$1`, album.ID), "FLAC+MP3 of one track must be ONE track, not two")
		require.Equal(t, 2, countRows(`SELECT count(*) FROM track_files tf JOIN tracks t ON t.id=tf.track_id WHERE t.album_id=$1`, album.ID), "both files attach as quality-alternates")
	})

	t.Run("pregap/unnumbered file does not steal a numbered track's slot", func(t *testing.T) {
		dir := "/x/Order Artist - Album - 2021 - OAlbum"
		// '00 - Hidden' sorts before '01 - Song' and parses to track number 0.
		hidden := mkParsed(dir + "/00 - Hidden.flac")
		song := mkParsed(dir + "/01 - Song.flac")

		// Pass Hidden first to mimic path-sorted order; reserve-before-fill must
		// still leave Song at track 1.
		matched, _, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{hidden, song})
		require.Equal(t, 2, matched)
		require.Zero(t, errored)

		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artistID, Lower: "OAlbum", Year: "2021"})
		require.NoError(t, err)
		title := func(disc, num int) string {
			var s string
			require.NoError(t, tx.QueryRow(ctx, `SELECT title FROM tracks WHERE album_id=$1 AND disc_number=$2 AND track_number=$3`, album.ID, disc, num).Scan(&s))
			return s
		}
		require.Equal(t, "Song", title(1, 1), "the numbered track keeps track 1")
		require.Equal(t, "Hidden", title(1, 2), "the unnumbered pregap fills above it, not onto track 1")
	})

	t.Run("compilation performer MBID is not stamped on the album artist", func(t *testing.T) {
		// Path deliberately folders "Various Artists"; the lead track carries a
		// per-performer musicbrainz_artistid but NO album-artist MBID. That
		// performer id must NOT become the Various Artists row's MBID.
		p := "/x/Various Artists - Album - 2020 - Now Thats Music/01.flac"
		f := mkParsed(p)
		infoByPath[f.Path] = rawTagInfo(map[string]string{
			"ARTIST":               "Taylor Swift",
			"MUSICBRAINZ_ARTISTID": "12345678-1111-2222-3333-444455556666",
			"ALBUM":                "Now Thats Music",
			"TITLE":                "Some Song",
		})

		matched, _, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f})
		require.Equal(t, 1, matched)
		require.Zero(t, errored)
		require.NotZero(t, artistID)

		var name, mbid string
		require.NoError(t, tx.QueryRow(ctx, `SELECT name, musicbrainz_id FROM artists WHERE id=$1`, artistID).Scan(&name, &mbid))
		require.Equal(t, "Various Artists", name)
		require.Empty(t, mbid, "the lead performer's MBID must NOT be stamped on Various Artists")
	})

	t.Run("path-won name/album do not adopt a disagreeing tag's MBID", func(t *testing.T) {
		// Curated path names the artist "Alpha Band" and album "Gamma Record";
		// the tags name a DIFFERENT act/album and carry their MBIDs. The path
		// wins the name fusion, so those MBIDs belong to other entities and must
		// NOT be stamped (they would fuse globally via the MBID dedup).
		p := "/x/Alpha Band - Album - 2020 - Gamma Record/01 - Song.flac"
		f := mkParsed(p)
		infoByPath[f.Path] = rawTagInfo(map[string]string{
			"ALBUMARTIST":               "Beta Crew",
			"MUSICBRAINZ_ALBUMARTISTID": "11111111-aaaa-bbbb-cccc-222222222222",
			"ALBUM":                     "Delta Disc",
			"MUSICBRAINZ_ALBUMID":       "33333333-aaaa-bbbb-cccc-444444444444",
			"TITLE":                     "Song",
		})

		matched, _, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f})
		require.Equal(t, 1, matched)
		require.Zero(t, errored)

		var aName, aMBID string
		require.NoError(t, tx.QueryRow(ctx, `SELECT name, musicbrainz_id FROM artists WHERE id=$1`, artistID).Scan(&aName, &aMBID))
		require.Equal(t, "Alpha Band", aName, "path artist wins the name")
		require.Empty(t, aMBID, "a disagreeing tag album-artist MBID must NOT be stamped")

		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artistID, Lower: "Gamma Record", Year: "2020"})
		require.NoError(t, err, "path album title wins")
		require.Empty(t, album.MusicbrainzID, "a disagreeing tag album MBID must NOT be stamped")
	})

	t.Run("edition-variant tag album MBID is not stamped on the base title", func(t *testing.T) {
		// Path names the standard edition; the tag names the Deluxe edition
		// (fuzzy-equal title) with the DELUXE release MBID. The base-titled album
		// must NOT inherit the deluxe MBID — they are distinct releases.
		p := "/x/Edition Artist - Album - 2020 - Standard Album/01 - Song.flac"
		f := mkParsed(p)
		infoByPath[f.Path] = rawTagInfo(map[string]string{
			"ALBUMARTIST":         "Edition Artist",
			"ALBUM":               "Standard Album (Deluxe Edition)",
			"MUSICBRAINZ_ALBUMID": "55555555-aaaa-bbbb-cccc-666666666666",
			"TITLE":               "Song",
		})

		matched, _, errored, artistID := m.matchMusicGroup(ctx, libID, []sqlc.LibraryFile{f})
		require.Equal(t, 1, matched)
		require.Zero(t, errored)

		album, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{ArtistID: artistID, Lower: "Standard Album", Year: "2020"})
		require.NoError(t, err, "the standard-edition title from the path is kept")
		require.Empty(t, album.MusicbrainzID, "the Deluxe edition's release MBID must NOT land on the standard album")
	})
}
