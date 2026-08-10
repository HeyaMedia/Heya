package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/mediaprobe"
)

func TestCastAccessPolicy(t *testing.T) {
	app := &App{}
	app.setCastAllowedUsers([]int64{22, 22, -1})

	if !app.CastAccessAllowed(11, true) {
		t.Fatal("admin should always retain casting access")
	}
	if !app.CastAccessAllowed(22, false) {
		t.Fatal("explicitly allowed regular user was denied")
	}
	if app.CastAccessAllowed(33, false) {
		t.Fatal("unlisted regular user was allowed")
	}
	ids := app.castAllowedUserIDs()
	if len(ids) != 1 || ids[0] != 22 {
		t.Fatalf("normalized allowlist = %v, want [22]", ids)
	}
}

func TestNormalizeCastBaseURL(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{" https://heya.lan/ ", "https://heya.lan", true},
		{"http://192.168.20.10:8080", "http://192.168.20.10:8080", true},
		{"ftp://heya.lan", "", false},
		{"https://heya.lan/subpath", "", false},
		{"https://user:pass@heya.lan", "", false},
	} {
		got, err := normalizeCastBaseURL(test.in)
		if (err == nil) != test.ok || got != test.want {
			t.Errorf("normalizeCastBaseURL(%q) = %q, %v; want %q, ok=%v", test.in, got, err, test.want, test.ok)
		}
	}
}

func TestCastVideoCanDirectUsesConservativeBaseline(t *testing.T) {
	base := mediaprobe.MediaInfo{
		Duration: 120,
		Streams: []mediaprobe.StreamInfo{
			{CodecType: "video", CodecName: "h264", PixFmt: "yuv420p", FieldOrder: "progressive"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	if !castVideoCanDirect(base, "/media/movie.mp4", 0) {
		t.Fatal("baseline H.264/AAC MP4 should direct play")
	}
	if castVideoCanDirect(base, "/media/movie.mkv", 0) {
		t.Fatal("MKV should use HLS")
	}
	if castVideoCanDirect(base, "/media/movie.mp4", 1) {
		t.Fatal("alternate audio selection should use HLS")
	}

	hevc := base
	hevc.Streams = append([]mediaprobe.StreamInfo(nil), base.Streams...)
	hevc.Streams[0].CodecName = "hevc"
	if castVideoCanDirect(hevc, "/media/movie.mp4", 0) {
		t.Fatal("HEVC should use the compatibility HLS path")
	}

	hdr := base
	hdr.Streams = append([]mediaprobe.StreamInfo(nil), base.Streams...)
	hdr.Streams[0].ColorTransfer = "smpte2084"
	if castVideoCanDirect(hdr, "/media/movie.mp4", 0) {
		t.Fatal("HDR should use HLS so Heya can tone-map for the baseline receiver")
	}

	dimensions := base
	dimensions.Streams = append([]mediaprobe.StreamInfo(nil), base.Streams...)
	dimensions.Streams[0].Width, dimensions.Streams[0].Height = 3840, 2160
	if castVideoCanDirect(dimensions, "/media/movie.mp4", 0) {
		t.Fatal("4K should not be sent as universal direct play")
	}

	highRate := base
	highRate.Streams = append([]mediaprobe.StreamInfo(nil), base.Streams...)
	highRate.Streams[0].AvgFrameRate = "60000/1001"
	if castVideoCanDirect(highRate, "/media/movie.mp4", 0) {
		t.Fatal("1080p60 should not be sent as universal direct play")
	}

	hevc.Streams[0].CodecTagString = "hvc1"
	if !castVideoCanOptimisticDirect(hevc, "/media/movie.mp4", 0) {
		t.Fatal("browser Cast should optimistically try HEVC MP4")
	}
	if castVideoCanOptimisticDirect(hevc, "/media/movie.mp4", 1) {
		t.Fatal("alternate audio selection must use HLS")
	}
	base.Streams[0].Profile, base.Streams[0].Level = "High", 41
	base.Container = "mov,mp4,m4a,3gp,3g2,mj2"
	if got := castVideoContentType(base, 0); got != `video/mp4; codecs="avc1.640029, mp4a.40.2"` {
		t.Fatalf("Cast content type = %q", got)
	}
}
