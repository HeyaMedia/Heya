package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/worker"
)

func loadWorkerFixture(t *testing.T, name string) worker.MediaInfo {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "ffprobe", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loading fixture %s: %v", name, err)
	}
	info, err := worker.ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", name, err)
	}
	return *info
}

func TestBuildStreamInfoResponse_Predator(t *testing.T) {
	info := loadWorkerFixture(t, "movie_predator_1987.json")
	resp := buildStreamInfoResponse(info, transcoder.DefaultClientCaps, "/test/file.mkv", 1)

	if resp.Container != "matroska,webm" {
		t.Errorf("container = %q, want matroska,webm", resp.Container)
	}
	if resp.Duration < 6394 {
		t.Errorf("duration = %f, want ~6394", resp.Duration)
	}

	if len(resp.Video) != 1 {
		t.Fatalf("video streams = %d, want 1", len(resp.Video))
	}
	v := resp.Video[0]
	if v.Codec != "av1" {
		t.Errorf("video codec = %q, want av1", v.Codec)
	}
	if !v.HDR {
		t.Error("expected HDR=true for smpte2084 content")
	}
	if v.Width != 1920 || v.Height != 1040 {
		t.Errorf("resolution = %dx%d, want 1920x1040", v.Width, v.Height)
	}

	if len(resp.Audio) != 2 {
		t.Fatalf("audio streams = %d, want 2", len(resp.Audio))
	}
	if resp.Audio[0].Codec != "ac3" {
		t.Errorf("audio[0] codec = %q, want ac3", resp.Audio[0].Codec)
	}
	if resp.Audio[0].Channels != 6 {
		t.Errorf("audio[0] channels = %d, want 6", resp.Audio[0].Channels)
	}
	if resp.Audio[0].Language != "eng" {
		t.Errorf("audio[0] language = %q, want eng", resp.Audio[0].Language)
	}

	if len(resp.Subtitle) != 18 {
		t.Errorf("subtitle streams = %d, want 18", len(resp.Subtitle))
	}
}

func TestBuildStreamInfoResponse_Horimiya(t *testing.T) {
	info := loadWorkerFixture(t, "anime_horimiya_s01e03.json")
	resp := buildStreamInfoResponse(info, transcoder.DefaultClientCaps, "/test/file.mkv", 1)

	if len(resp.Video) != 1 {
		t.Fatalf("video streams = %d, want 1", len(resp.Video))
	}
	if resp.Video[0].Profile != "High 10" {
		t.Errorf("video profile = %q, want 'High 10'", resp.Video[0].Profile)
	}
	if resp.Video[0].HDR {
		t.Error("bt709 content should not be HDR")
	}

	if len(resp.Audio) != 2 {
		t.Fatalf("audio streams = %d, want 2", len(resp.Audio))
	}
	if resp.Audio[0].Language != "eng" || resp.Audio[1].Language != "jpn" {
		t.Errorf("audio languages = [%s, %s], want [eng, jpn]", resp.Audio[0].Language, resp.Audio[1].Language)
	}
	if resp.Audio[0].Codec != "aac" || resp.Audio[1].Codec != "flac" {
		t.Errorf("audio codecs = [%s, %s], want [aac, flac]", resp.Audio[0].Codec, resp.Audio[1].Codec)
	}

	if len(resp.Subtitle) != 2 {
		t.Fatalf("subtitle streams = %d, want 2", len(resp.Subtitle))
	}
	signs := resp.Subtitle[0]
	if !signs.IsForced {
		t.Error("Signs & Songs subtitle should be forced")
	}
	if !signs.IsDefault {
		t.Error("Signs & Songs subtitle should be default")
	}
	dialogue := resp.Subtitle[1]
	if dialogue.IsForced {
		t.Error("Dialogue subtitle should not be forced")
	}
}

func TestBuildStreamInfoResponse_Extant(t *testing.T) {
	info := loadWorkerFixture(t, "tv_extant_s01e13.json")
	resp := buildStreamInfoResponse(info, transcoder.DefaultClientCaps, "/test/file.mkv", 1)

	if len(resp.Video) != 1 || len(resp.Audio) != 1 || len(resp.Subtitle) != 1 {
		t.Errorf("streams = %d/%d/%d, want 1/1/1", len(resp.Video), len(resp.Audio), len(resp.Subtitle))
	}
	if resp.Audio[0].Channels != 6 {
		t.Errorf("audio channels = %d, want 6 (5.1)", resp.Audio[0].Channels)
	}
}

func TestBuildStreamInfoResponse_EmptyMediaInfo(t *testing.T) {
	resp := buildStreamInfoResponse(worker.MediaInfo{}, transcoder.DefaultClientCaps, "/test/file.mkv", 1)

	out, _ := json.Marshal(resp)
	s := string(out)
	if !strings.Contains(s, `"video":[]`) || !strings.Contains(s, `"audio":[]`) || !strings.Contains(s, `"subtitle":[]`) {
		t.Error("empty arrays should serialize as [], not null")
	}
}

func TestIsHDR(t *testing.T) {
	tests := []struct {
		transfer string
		want     bool
	}{
		{"smpte2084", true},
		{"arib-std-b67", true},
		{"bt709", false},
		{"", false},
	}
	for _, tt := range tests {
		s := worker.StreamInfo{ColorTransfer: tt.transfer}
		if got := isHDR(s); got != tt.want {
			t.Errorf("isHDR(%q) = %v, want %v", tt.transfer, got, tt.want)
		}
	}
}

func TestReasonStrings(t *testing.T) {
	cases := []struct {
		name string
		bits transcoder.TranscodeReason
		want []string
	}{
		{"none", 0, []string{}},
		{"container only", transcoder.ReasonContainerNotSupported, []string{"container"}},
		{
			"container + hdr",
			transcoder.ReasonContainerNotSupported | transcoder.ReasonHDRNotSupported,
			[]string{"container", "hdr"},
		},
		{
			"dolby vision + lossless audio",
			transcoder.ReasonDolbyVisionNotSupported | transcoder.ReasonAudioLosslessNotSupported,
			[]string{"lossless_audio", "dolby_vision"},
		},
		{
			"rotation only",
			transcoder.ReasonVideoRotationNotSupported,
			[]string{"rotation"},
		},
	}
	for _, tc := range cases {
		got := reasonStrings(tc.bits)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: index %d: got %q want %q", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}

func TestDeriveBitDepth(t *testing.T) {
	cases := []struct {
		bits, pix string
		want      int
	}{
		{"10", "yuv420p10le", 10},
		{"", "yuv420p10le", 10},
		{"", "yuv420p12be", 12},
		{"", "yuv420p", 8},
		{"", "yuvj420p", 8},
		{"", "", 0},
		{"bogus", "yuv420p", 8}, // unparseable falls back to pix_fmt
	}
	for _, tc := range cases {
		got := deriveBitDepth(tc.bits, tc.pix)
		if got != tc.want {
			t.Errorf("deriveBitDepth(%q,%q) = %d, want %d", tc.bits, tc.pix, got, tc.want)
		}
	}
}

func TestNormalizeRotation(t *testing.T) {
	// ffprobe Display Matrix: -90 means 90° CW. We normalise to positive CW.
	cases := map[int]int{
		0:    0,
		-90:  90,
		90:   270, // ffprobe "90" = 90° CCW = 270° CW
		-180: 180,
		180:  180,
		-270: 270,
		270:  90,
		360:  0, // wraps
		-360: 0,
		45:   0, // not a canonical step → ignored
	}
	for in, want := range cases {
		if got := normalizeRotation(in); got != want {
			t.Errorf("normalizeRotation(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDeriveSideDataFields(t *testing.T) {
	t.Run("dovi profile 8 + display matrix", func(t *testing.T) {
		side := []worker.SideData{
			{Type: "Display Matrix", Rotation: -90},
			{Type: "DOVI configuration record", DvProfile: 8, DvBlSignalCompatibilityID: 1},
		}
		dv, compat, rot := deriveSideDataFields(side)
		if dv != 8 || compat != 1 || rot != 90 {
			t.Errorf("got dv=%d compat=%d rot=%d", dv, compat, rot)
		}
	})
	t.Run("empty", func(t *testing.T) {
		dv, compat, rot := deriveSideDataFields(nil)
		if dv != 0 || compat != 0 || rot != 0 {
			t.Errorf("expected zeros, got dv=%d compat=%d rot=%d", dv, compat, rot)
		}
	})
	t.Run("unknown side data type ignored", func(t *testing.T) {
		side := []worker.SideData{
			{Type: "Mastering display metadata"},
			{Type: "Content light level metadata"},
		}
		dv, compat, rot := deriveSideDataFields(side)
		if dv != 0 || compat != 0 || rot != 0 {
			t.Errorf("expected zeros, got dv=%d compat=%d rot=%d", dv, compat, rot)
		}
	})
}

func TestWorkerToTranscoderInfo_FullDerivation(t *testing.T) {
	info := &worker.MediaInfo{
		Container: "mp4",
		Streams: []worker.StreamInfo{
			{
				CodecName:         "hevc",
				CodecType:         "video",
				CodecTagString:    "hev1",
				BitsPerRawSample:  "10",
				PixFmt:            "yuv420p10le",
				SampleAspectRatio: "32:27",
				FieldOrder:        "tt",
				SideDataList: []worker.SideData{
					{Type: "Display Matrix", Rotation: -90},
					{Type: "DOVI configuration record", DvProfile: 8, DvBlSignalCompatibilityID: 1},
				},
			},
			{CodecName: "truehd", CodecType: "audio", Channels: 8, ChannelLayout: "7.1"},
		},
	}
	t.Helper()
	out := workerToTranscoderInfo(info)
	if len(out.Streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(out.Streams))
	}
	v := out.Streams[0]
	if v.CodecTag != "hev1" || v.BitDepth != 10 || v.SampleAspectRatio != "32:27" ||
		v.FieldOrder != "tt" || v.Rotation != 90 || v.DvProfile != 8 || v.DvBlCompatID != 1 {
		t.Errorf("video derivation incomplete: %+v", v)
	}
	a := out.Streams[1]
	if a.Channels != 8 || a.ChannelLayout != "7.1" {
		t.Errorf("audio channel info missing: %+v", a)
	}
}
