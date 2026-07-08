package audiotags

import (
	"testing"

	"github.com/karbowiak/heya/internal/mediaprobe"
)

func TestFromMediaInfoNormalizesMusicTags(t *testing.T) {
	tags := FromMediaInfo(&mediaprobe.MediaInfo{
		Format: mediaprobe.FormatInfo{Tags: map[string]string{
			"ALBUMARTIST":                "ano",
			"DATE":                       "2022-11-23",
			"MUSICBRAINZ_ALBUMID":        "a212268d-ea6f-4387-b09e-c20353130bb4",
			"MUSICBRAINZ_RELEASEGROUPID": "9b19bfab-7916-4ec2-b5ff-9bfa13056630",
			"MUSICBRAINZ_ALBUMARTISTID":  "ebb4513e-4aab-4ac9-a949-14e77bb7b836",
			"TRACK":                      "1/1",
			"DISC":                       "1/1",
		}},
		Streams: []mediaprobe.StreamInfo{{
			CodecType: "audio",
			Tags: map[string]string{
				"ARTIST": "ano",
				"ALBUM":  "ちゅ、多様性。",
				"TITLE":  "ちゅ、多様性。",
			},
		}},
	})

	if tags.Artist != "ano" || tags.AlbumArtist != "ano" {
		t.Fatalf("artist tags: got artist=%q album_artist=%q", tags.Artist, tags.AlbumArtist)
	}
	if tags.Album != "ちゅ、多様性。" || tags.Title != "ちゅ、多様性。" {
		t.Fatalf("title tags: got album=%q title=%q", tags.Album, tags.Title)
	}
	if tags.Year != "2022" || tags.TrackNumber != 1 || tags.TrackTotal != 1 || tags.DiscNumber != 1 || tags.DiscTotal != 1 {
		t.Fatalf("numeric/date tags: got %#v", tags)
	}
	if tags.ReleaseGroupMBID != "9b19bfab-7916-4ec2-b5ff-9bfa13056630" {
		t.Fatalf("release group MBID: got %#v", tags)
	}
}
