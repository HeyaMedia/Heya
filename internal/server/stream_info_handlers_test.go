package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	resp := buildStreamInfoResponse(info)

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
	resp := buildStreamInfoResponse(info)

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
	resp := buildStreamInfoResponse(info)

	if len(resp.Video) != 1 || len(resp.Audio) != 1 || len(resp.Subtitle) != 1 {
		t.Errorf("streams = %d/%d/%d, want 1/1/1", len(resp.Video), len(resp.Audio), len(resp.Subtitle))
	}
	if resp.Audio[0].Channels != 6 {
		t.Errorf("audio channels = %d, want 6 (5.1)", resp.Audio[0].Channels)
	}
}

func TestBuildStreamInfoResponse_EmptyMediaInfo(t *testing.T) {
	resp := buildStreamInfoResponse(worker.MediaInfo{})

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
