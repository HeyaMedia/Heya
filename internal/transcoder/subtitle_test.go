package transcoder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSubtitlesAsPublishesCompleteWebVTT(t *testing.T) {
	if !IsFFmpegAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	input, err := filepath.Abs(filepath.Join("..", "..", "testdata", "library", "movies", "Mad Max Fury Road (2015)", "Mad Max Fury Road (2015).en.srt"))
	if err != nil {
		t.Fatalf("resolve subtitle fixture: %v", err)
	}
	output := filepath.Join(t.TempDir(), "subtitle.vtt")
	if err := ExtractSubtitlesAs(context.Background(), input, 0, output, "webvtt"); err != nil {
		t.Fatalf("extract subtitle: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read published subtitle: %v", err)
	}
	if !strings.HasPrefix(string(data), "WEBVTT") || !strings.Contains(string(data), "Witness me.") {
		t.Fatalf("unexpected WebVTT output: %q", data)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".subtitle.vtt.*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary outputs: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary outputs leaked: %v", matches)
	}
}
