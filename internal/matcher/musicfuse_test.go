package matcher

import (
	"testing"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/musicconsensus"
)

func TestExtractMusicTags(t *testing.T) {
	// Mixed casing / naming across taggers, plus slash-form track/disc and an
	// ISO date — all must normalize to the same clean fields.
	raw := map[string]string{
		"ARTIST":               "3 Doors Down",
		"ALBUM ARTIST":         "3 Doors Down",
		"Album":                "The Better Life",
		"title":                "Kryptonite",
		"DATE":                 "2000-02-08",
		"track":                "1/12",
		"DISCNUMBER":           "1/2",
		"MUSICBRAINZ_ARTISTID": "abc1234a-1111-2222-3333-abcdefabcdef",
	}
	got := extractMusicTags(raw)
	if got.Artist != "3 Doors Down" || got.AlbumArtist != "3 Doors Down" {
		t.Errorf("artist/albumartist = %q/%q", got.Artist, got.AlbumArtist)
	}
	if got.Album != "The Better Life" || got.Title != "Kryptonite" {
		t.Errorf("album/title = %q/%q", got.Album, got.Title)
	}
	if got.Year != "2000" {
		t.Errorf("year = %q, want 2000", got.Year)
	}
	if got.TrackNumber != 1 || got.TrackTotal != 12 {
		t.Errorf("track = %d/%d, want 1/12", got.TrackNumber, got.TrackTotal)
	}
	if got.DiscNumber != 1 {
		t.Errorf("disc = %d, want 1", got.DiscNumber)
	}
	if got.ArtistMBID != "abc1234a-1111-2222-3333-abcdefabcdef" {
		t.Errorf("artist mbid = %q", got.ArtistMBID)
	}

	if empty := extractMusicTags(nil); empty != (musicTags{}) {
		t.Errorf("nil tags must yield zero musicTags, got %+v", empty)
	}
}

func TestApplyMatcherMusicConsensusQuarantinesLeadOutlierIDs(t *testing.T) {
	const (
		asacoMBID  = "bb4297af-90c2-4bb2-bd50-67951921c9c3"
		djPaulMBID = "43906e48-a7c0-4b80-a5dd-37d1fe6ccdb9"
		wrongAlbum = "97470000-0000-4000-8000-000000000000"
	)
	outlier := musicTags{
		Artist:          "DJ Paul",
		AlbumArtist:     "DJ Paul",
		Album:           "To Kill Again...The Mixtape",
		Year:            "2010",
		ArtistMBID:      djPaulMBID,
		AlbumArtistMBID: djPaulMBID,
		AlbumMBID:       wrongAlbum,
	}
	all := []musicTags{outlier}
	evidence := []musicconsensus.Evidence{matcherMusicConsensusEvidence(outlier)}
	for i := 0; i < 9; i++ {
		tags := musicTags{Artist: "Asaco", AlbumArtist: "Asaco", Album: "Nomake Story", Year: "2020"}
		if i == 0 {
			tags.AlbumArtistMBID = asacoMBID
		}
		all = append(all, tags)
		evidence = append(evidence, matcherMusicConsensusEvidence(tags))
	}

	got := applyMatcherMusicConsensus(outlier, all, musicconsensus.Build(evidence))
	if got.AlbumArtist != "Asaco" || got.Album != "Nomake Story" || got.Year != "2020" {
		t.Fatalf("consensus lead tags = %#v", got)
	}
	if got.AlbumArtistMBID != asacoMBID {
		t.Fatalf("majority artist MBID = %q, want %q", got.AlbumArtistMBID, asacoMBID)
	}
	if got.ArtistMBID == djPaulMBID || got.AlbumMBID == wrongAlbum {
		t.Fatalf("outlier IDs survived: %#v", got)
	}
}

func TestParseSlashInt(t *testing.T) {
	cases := []struct {
		in         string
		num, total int
	}{
		{"3", 3, 0},
		{"03", 3, 0},
		{"3/12", 3, 12},
		{" 1 / 2 ", 1, 2},
		{"", 0, 0},
		{"A", 0, 0},
	}
	for _, c := range cases {
		n, tot := parseSlashInt(c.in)
		if n != c.num || tot != c.total {
			t.Errorf("parseSlashInt(%q) = %d/%d, want %d/%d", c.in, n, tot, c.num, c.total)
		}
	}
}

func TestPathTrust(t *testing.T) {
	clean := pathTrust("3 Doors Down - Album - 2000 - The Better Life")
	if clean < 0.5 {
		t.Errorf("curated name should keep near-full trust, got %.2f", clean)
	}
	// The user's motivating example: scene cruft, unspaced joins, underscores.
	garbage := pathTrust("someartist-somealbum-2015-releasegroupqualityblabla_asdf_fdsa_mp3_256k")
	if garbage >= baseTagTrust {
		t.Errorf("scene-garbage path (%.2f) must decay below tag trust %.2f", garbage, baseTagTrust)
	}
	if garbage < minSourceTrust {
		t.Errorf("path trust must not fall below the floor, got %.2f", garbage)
	}
	if garbage >= clean {
		t.Errorf("garbage trust %.2f should be well under clean %.2f", garbage, clean)
	}

	// Curated titles that merely contain a word that used to look like scene
	// noise (Tidal / CD / Bit / Proper / Reissue) must keep full trust.
	for _, name := range []string{
		"Fiona Apple - Album - 1996 - Tidal",
		"Proper Dose",
		"A Little Bit",
		"Some Album (Deluxe CD Edition)",
		"Greatest Hits Reissue",
	} {
		if got := pathTrust(name); got < 0.5 {
			t.Errorf("curated %q wrongly decayed to %.2f", name, got)
		}
	}
}

func TestExtractMusicTags_MBIDs(t *testing.T) {
	got := extractMusicTags(map[string]string{
		"MUSICBRAINZ_ARTISTID":       "aaaa1111-2222-3333-4444-555566667777",
		"MUSICBRAINZ_ALBUMARTISTID":  "bbbb1111-2222-3333-4444-555566667777",
		"MUSICBRAINZ_ALBUMID":        "cccc1111-2222-3333-4444-555566667777",
		"MUSICBRAINZ_RELEASEGROUPID": "dddd1111-2222-3333-4444-555566667777",
	})
	if got.ArtistMBID != "aaaa1111-2222-3333-4444-555566667777" {
		t.Errorf("artist mbid = %q", got.ArtistMBID)
	}
	if got.AlbumArtistMBID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("albumartist mbid = %q", got.AlbumArtistMBID)
	}
	if got.AlbumMBID != "cccc1111-2222-3333-4444-555566667777" {
		t.Errorf("album mbid must be the RELEASE id, got %q", got.AlbumMBID)
	}

	// A release-GROUP id alone must NOT become the album MBID — it is shared by
	// every edition and would collapse distinct editions in the global dedup.
	rgOnly := extractMusicTags(map[string]string{
		"MUSICBRAINZ_RELEASEGROUPID": "dddd1111-2222-3333-4444-555566667777",
	})
	if rgOnly.AlbumMBID != "" {
		t.Errorf("release-group id must not populate AlbumMBID, got %q", rgOnly.AlbumMBID)
	}
}

func TestIsPlaceholderValue(t *testing.T) {
	placeholders := []string{"", "Unknown Artist", "unknown", "Various Artists", "VA", "Untitled", "Track 07", "track", "01", "Ripped by dBpoweramp", "www.example.com release"}
	for _, p := range placeholders {
		if !isPlaceholderValue(p) {
			t.Errorf("%q should be a placeholder", p)
		}
	}
	real := []string{"3 Doors Down", "Ado", "The Better Life", "Kryptonite", "M83"}
	for _, r := range real {
		if isPlaceholderValue(r) {
			t.Errorf("%q should NOT be a placeholder", r)
		}
	}
}

func TestIsUsableArtist(t *testing.T) {
	for _, bad := range []string{"", "Unknown Artist", "unknown", "VA", "Various Artists", "Untitled"} {
		if isUsableArtist(bad) {
			t.Errorf("%q must not be usable (would fuse globally)", bad)
		}
	}
	// Real bands, including all-numeric names, must be usable.
	for _, ok := range []string{"3 Doors Down", "311", "112", "1349", "M83"} {
		if !isUsableArtist(ok) {
			t.Errorf("%q must be usable (real band)", ok)
		}
	}
}

func TestFuseText(t *testing.T) {
	pClean := pathTrust("3 Doors Down - Album - 2000 - The Better Life")
	pGarbage := pathTrust("va-2015-somealbum_mp3_320")

	t.Run("agreement short-circuits to high confidence", func(t *testing.T) {
		got := fuseText("The Better Life", "The Better Life", pClean, tagTrust("The Better Life"))
		if got.Source != sourceBoth {
			t.Errorf("agreement source = %v, want both", got.Source)
		}
		if got.Value != "The Better Life" {
			t.Errorf("value = %q", got.Value)
		}
		if got.Confidence <= pClean {
			t.Errorf("agreement should boost confidence above path-alone (%.2f), got %.2f", pClean, got.Confidence)
		}
	})

	t.Run("empty path falls back to tag", func(t *testing.T) {
		got := fuseText("", "Kryptonite", basePathTrust, tagTrust("Kryptonite"))
		if got.Source != sourceTag || got.Value != "Kryptonite" {
			t.Errorf("got %+v, want tag/Kryptonite", got)
		}
	})

	t.Run("empty tag falls back to path", func(t *testing.T) {
		got := fuseText("The Better Life", "", pClean, tagTrust(""))
		if got.Source != sourcePath || got.Value != "The Better Life" {
			t.Errorf("got %+v, want path", got)
		}
	})

	t.Run("clean conflict: path wins the 55 lean", func(t *testing.T) {
		got := fuseText("Real Album", "Different Album", basePathTrust, baseTagTrust)
		if got.Source != sourcePath || got.Value != "Real Album" {
			t.Errorf("clean tie should lean path, got %+v", got)
		}
	})

	t.Run("garbage path loses to clean tag", func(t *testing.T) {
		got := fuseText("somealbum blabla", "The Real Album", pGarbage, tagTrust("The Real Album"))
		if got.Source != sourceTag || got.Value != "The Real Album" {
			t.Errorf("garbage path should lose to clean tag, got %+v (pathTrust=%.2f)", got, pGarbage)
		}
	})

	t.Run("placeholder tag loses to real path", func(t *testing.T) {
		got := fuseText("3 Doors Down", "Unknown Artist", basePathTrust, tagTrust("Unknown Artist"))
		if got.Source != sourcePath || got.Value != "3 Doors Down" {
			t.Errorf("placeholder tag should lose, got %+v", got)
		}
	})

	t.Run("both empty", func(t *testing.T) {
		if got := fuseText("", "", basePathTrust, baseTagTrust); got.Source != sourceNone || got.Value != "" {
			t.Errorf("both empty should yield none, got %+v", got)
		}
	})
}

func TestSameRelease(t *testing.T) {
	// Same release — only case / whitespace differences.
	for _, p := range [][2]string{
		{"The Better Life", "the better life"},
		{"Haru Ni Mau", "Haru Ni Mau"},
		{"  Album   X ", "Album X"},
	} {
		if !sameRelease(p[0], p[1]) {
			t.Errorf("sameRelease(%q,%q) = false, want true", p[0], p[1])
		}
	}
	// Different editions / scripts must NOT be treated as the same release —
	// each carries its own release MBID (the variant-title mislink guard).
	for _, p := range [][2]string{
		{"The Better Life", "The Better Life (Deluxe Edition)"},
		{"Album", "Album (Remastered)"},
		{"Kura Kura", "くらくら"},
		{"", "Anything"},
	} {
		if sameRelease(p[0], p[1]) {
			t.Errorf("sameRelease(%q,%q) = true, want false", p[0], p[1])
		}
	}
}

func TestFuseMBID(t *testing.T) {
	real := "abc1234a-1111-2222-3333-abcdefabcdef"
	if got := fuseMBID("", real); got.Source != sourceTag || got.Value != real {
		t.Errorf("tag mbid should be adopted, got %+v", got)
	}
	if got := fuseMBID("nfo-"+real, real); got.Source != sourceNFO {
		t.Errorf("nfo mbid must win, got %+v", got)
	}
	if got := fuseMBID("", "not-a-uuid"); got.Value != "" {
		t.Errorf("malformed mbid must be rejected, got %+v", got)
	}
	if got := fuseMBID("", ""); got.Value != "" {
		t.Errorf("no mbid → empty, got %+v", got)
	}
}

func TestFuseTrackNumber(t *testing.T) {
	if n := fuseTrackNumber(4, 4); n != 4 {
		t.Errorf("agree → 4, got %d", n)
	}
	if n := fuseTrackNumber(4, 9); n != 4 {
		t.Errorf("conflict → path (4), got %d", n)
	}
	if n := fuseTrackNumber(0, 7); n != 7 {
		t.Errorf("path missing → tag (7), got %d", n)
	}
	if n := fuseTrackNumber(0, 0); n != 0 {
		t.Errorf("neither → 0, got %d", n)
	}
}

func TestTrackNumberAssigner(t *testing.T) {
	a := newTrackNumberAssigner()

	// Known numbers are reserved. Reserving the same number twice is fine —
	// quality-alternate files (FLAC + MP3) share a number and merge downstream,
	// so the assigner must NOT bump the second one.
	a.reserve(1, 1)
	a.reserve(1, 2)
	a.reserve(1, 1) // duplicate: still track 1, no synthesis

	// Unnumbered files fill above the reserved max, never colliding with 1 or 2.
	if n := a.fill(1); n != 3 {
		t.Errorf("fill disc1 = %d, want 3", n)
	}
	if n := a.fill(1); n != 4 {
		t.Errorf("second fill disc1 = %d, want 4", n)
	}

	// Order-independence: reserving a high number first still keeps fills above
	// everything reserved (an earlier-sorted unnumbered file can't steal a slot).
	a.reserve(2, 5)
	if n := a.fill(2); n != 6 {
		t.Errorf("fill disc2 after reserve(5) = %d, want 6", n)
	}

	// A second disc numbers independently.
	if n := a.fill(3); n != 1 {
		t.Errorf("fill disc3 = %d, want 1", n)
	}
}

func TestCollectAudioTags(t *testing.T) {
	info := &mediaprobe.MediaInfo{
		Format: mediaprobe.FormatInfo{Tags: map[string]string{"ARTIST": "Format Artist", "ALBUM": "Fmt Album"}},
		Streams: []mediaprobe.StreamInfo{
			{CodecType: "audio", Tags: map[string]string{"ARTIST": "Stream Artist", "TITLE": "Stream Title"}},
		},
	}
	merged := collectAudioTags(info)
	if merged["ARTIST"] != "Format Artist" {
		t.Errorf("format tags must win on conflict, got %q", merged["ARTIST"])
	}
	if merged["TITLE"] != "Stream Title" {
		t.Errorf("stream-only tag must survive, got %q", merged["TITLE"])
	}
	if collectAudioTags(nil) != nil {
		t.Error("nil info → nil tags")
	}
	if collectAudioTags(&mediaprobe.MediaInfo{}) != nil {
		t.Error("no tags → nil")
	}
}
