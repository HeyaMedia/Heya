package formats

import "testing"

func TestQualityKeyVideo(t *testing.T) {
	cases := []struct {
		title string
		tv    bool
		want  string
	}{
		{"Movie.Name.2024.2160p.UHD.BluRay.x265-GROUP", false, "bluray-2160p"},
		{"Movie.Name.2024.1080p.BluRay.REMUX.AVC-GROUP", false, "remux-1080p"},
		{"Movie.Name.2024.2160p.WEB-DL.DDP5.1-GROUP", false, "webdl-2160p"},
		{"Movie.Name.2024.1080p.WEBRip.x264-GROUP", false, "webrip-1080p"},
		{"Show.S01E01.720p.HDTV.x264-GROUP", true, "hdtv-720p"},
		{"Show.S01E01.480p.HDTV.x264-GROUP", true, "sdtv"},
		{"Movie.Name.2024.DVDRip.x264-GROUP", false, "dvd"},
		{"Movie.Name.2024.720p.BluRay.REMUX-GROUP", false, "bluray-720p"},
		// Bare resolution: the upstream parser infers WEB-DL, arr-style.
		{"Movie Name (2024) 1080p", false, "webdl-1080p"},
		{"Movie.Name.2024.TELESYNC.x264-GROUP", false, ""},
	}
	for _, tc := range cases {
		attrs := ParseVideoRelease(tc.title, 0, tc.tv)
		if got := QualityKey(attrs); got != tc.want {
			t.Errorf("QualityKey(%q) = %q, want %q (attrs: res=%d sources=%v mod=%s)",
				tc.title, got, tc.want, attrs.Resolution, attrs.Sources, attrs.Modifier)
		}
	}
}

func TestMusicQualityKey(t *testing.T) {
	cases := []struct {
		title, want string
	}{
		{"Artist - Album (2024) [FLAC 24bit-96kHz]", "flac-24"},
		{"Artist - Album (2024) FLAC", "flac"},
		{"Artist-Album-WEB-2024-GROUP MP3 320", "mp3-320"},
		{"Artist - Album [V0]", "mp3-v0"},
		{"Artist - Album AAC 320", "aac-320"},
		{"Artist - Album (2024) [AAC]", ""},
		{"Artist.Album.2024.320kbps... 320", "mp3-320"},
		{"Artist - Album WavPack", "wavpack"},
		{"Artist - Album", ""},
	}
	for _, tc := range cases {
		if got := MusicQualityKey(tc.title); got != tc.want {
			t.Errorf("MusicQualityKey(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestBookQualityKey(t *testing.T) {
	if got := BookQualityKey("Author - Book Title (2024) EPUB"); got != "epub" {
		t.Errorf("epub: got %q", got)
	}
	if got := BookQualityKey("Author - Book Title (2024) M4B"); got != "" {
		t.Errorf("audiobook must be unmapped in v1, got %q", got)
	}
}
