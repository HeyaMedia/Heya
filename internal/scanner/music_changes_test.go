package scanner

import "testing"

func TestMarkChangedMusicReleasesOnlySignalsKnownArtistAdditions(t *testing.T) {
	index := newMusicLibraryCatalogIndex()
	index.artistMBIDs["artist-known"] = true
	index.artistNames["name only"] = true
	index.releaseIDs["release-existing"] = true
	index.groupIDs["group-existing"] = true
	index.tuples[musicLibraryAlbumTuple("artist known", "tuple existing", "2024")] = true
	index.titles[musicLibraryAlbumTitle("artist known", "yearless existing")] = true

	artists := []MusicArtistPlan{
		{
			Artist:      "Artist Known",
			ExternalIDs: map[string]string{"mbid": "ARTIST-KNOWN"},
			Albums: []MusicAlbumPlan{
				{Album: "Existing edition", ExternalIDs: map[string]string{"musicbrainz_album": "RELEASE-EXISTING"}},
				{Album: "Existing group", ExternalIDs: map[string]string{"musicbrainz_release_group": "GROUP-EXISTING"}},
				{Album: "Tuple Existing", Year: "2024"},
				{Album: "Yearless Existing"},
				{Album: "Actually New", Year: "2026"},
			},
		},
		{
			Artist: "Brand New Artist",
			Albums: []MusicAlbumPlan{{Album: "Debut", Year: "2026"}},
		},
		{
			Artist: "Name Only",
			Albums: []MusicAlbumPlan{{Album: "New Single", Year: "2026"}},
		},
	}

	markChangedMusicReleasesFromIndex(artists, index)

	for index, album := range artists[0].Albums {
		want := index == 4
		if album.Changed != want {
			t.Fatalf("known artist album %d changed=%v, want %v: %#v", index, album.Changed, want, album)
		}
	}
	if artists[1].Albums[0].Changed {
		t.Fatalf("first-time artist used the change-refresh path: %#v", artists[1])
	}
	if !artists[2].Albums[0].Changed {
		t.Fatalf("new release under known name-only artist was not signaled: %#v", artists[2])
	}
}
