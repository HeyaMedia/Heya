package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMusicQueueMatchRequiresArtistBoundary(t *testing.T) {
	idx := &musicQueueIndex{artists: []*musicQueueArtist{
		{name: "Meg", compact: []string{"meg"}, targets: []musicQueueTarget{{title: "Estate", compact: "estate"}}},
	}}

	artist, target := idx.match("Megadeth - Super Collider (2013) (FLAC)")
	require.Nil(t, artist)
	require.Nil(t, target)
}

func TestMusicQueueMatchUsesWholeAlbumPhrase(t *testing.T) {
	meg := &musicQueueArtist{
		name: "Meg", compact: []string{"meg"},
		targets: []musicQueueTarget{{title: "Estate", compact: "estate"}},
	}
	idx := &musicQueueIndex{artists: []*musicQueueArtist{meg}}

	artist, target := idx.match("Meg - Estate - SINGLE - WEB - FLAC - 2026")
	require.Same(t, meg, artist)
	require.NotNil(t, target)
	require.Equal(t, "Estate", target.title)

	artist, target = idx.match("Meg - Greatestates - SINGLE - WEB - FLAC - 2026")
	require.Same(t, meg, artist)
	require.Nil(t, target)
}

func TestMusicQueueMatchPreservesSceneSeparators(t *testing.T) {
	catscan := &musicQueueArtist{
		name: "Catscan", compact: []string{"catscan"},
		targets: []musicQueueTarget{{title: "Gravedigger", compact: "gravedigger"}},
	}
	artist, target := (&musicQueueIndex{artists: []*musicQueueArtist{catscan}}).match(
		"Catscan_-_Gravedigger-(TDM037)-SINGLE-WEB-2025-SRG",
	)
	require.Same(t, catscan, artist)
	require.NotNil(t, target)
}
