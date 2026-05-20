package worker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "ffprobe", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loading fixture %s: %v", name, err)
	}
	return data
}

// --- Unit tests using JSON fixtures ---

func TestParseFFProbeOutput_MoviePredator(t *testing.T) {
	data := loadFixture(t, "movie_predator_1987.json")
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if info.Container != "matroska,webm" {
		t.Errorf("container = %q, want matroska,webm", info.Container)
	}

	if info.Duration < 6394 || info.Duration > 6395 {
		t.Errorf("duration = %f, want ~6394.4", info.Duration)
	}
	if info.Size != 2003397901 {
		t.Errorf("size = %d, want 2003397901", info.Size)
	}
	if info.BitRate != 2506440 {
		t.Errorf("bitrate = %d, want 2506440", info.BitRate)
	}

	if len(info.Streams) != 21 {
		t.Errorf("stream count = %d, want 21", len(info.Streams))
	}

	video := findStream(info.Streams, "video", 0)
	if video == nil {
		t.Fatal("no video stream found")
	}
	if video.CodecName != "av1" {
		t.Errorf("video codec = %q, want av1", video.CodecName)
	}
	if video.Width != 1920 || video.Height != 1040 {
		t.Errorf("video resolution = %dx%d, want 1920x1040", video.Width, video.Height)
	}
	if video.PixFmt != "yuv420p10le" {
		t.Errorf("pix_fmt = %q, want yuv420p10le", video.PixFmt)
	}
	if video.ColorTransfer != "smpte2084" {
		t.Errorf("color_transfer = %q, want smpte2084 (HDR10)", video.ColorTransfer)
	}
	if video.Profile != "Main" {
		t.Errorf("profile = %q, want Main", video.Profile)
	}

	audioCount := countStreamType(info.Streams, "audio")
	if audioCount != 2 {
		t.Errorf("audio streams = %d, want 2", audioCount)
	}

	subCount := countStreamType(info.Streams, "subtitle")
	if subCount != 18 {
		t.Errorf("subtitle streams = %d, want 18", subCount)
	}

	defaultSub := findStreamWithDisposition(info.Streams, "subtitle", "default")
	if defaultSub == nil {
		t.Error("expected a default subtitle stream")
	}
}

func TestParseFFProbeOutput_AnimeHorimiya(t *testing.T) {
	data := loadFixture(t, "anime_horimiya_s01e03.json")
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	attachmentCount := 0
	for _, s := range info.Streams {
		if s.CodecType == "attachment" {
			attachmentCount++
		}
	}
	if attachmentCount != 0 {
		t.Errorf("attachments should be filtered out, found %d", attachmentCount)
	}

	if len(info.Streams) != 5 {
		t.Errorf("filtered stream count = %d, want 5 (1 video + 2 audio + 2 subtitle)", len(info.Streams))
	}

	video := findStream(info.Streams, "video", 0)
	if video == nil {
		t.Fatal("no video stream found")
	}
	if video.CodecName != "h264" {
		t.Errorf("video codec = %q, want h264", video.CodecName)
	}
	if video.Profile != "High 10" {
		t.Errorf("profile = %q, want High 10 (10-bit)", video.Profile)
	}
	if video.PixFmt != "yuv420p10le" {
		t.Errorf("pix_fmt = %q, want yuv420p10le", video.PixFmt)
	}

	audio1 := findStream(info.Streams, "audio", 0)
	audio2 := findStream(info.Streams, "audio", 1)
	if audio1 == nil || audio2 == nil {
		t.Fatal("expected 2 audio streams")
	}
	if audio1.CodecName != "aac" {
		t.Errorf("audio1 codec = %q, want aac", audio1.CodecName)
	}
	if audio2.CodecName != "flac" {
		t.Errorf("audio2 codec = %q, want flac", audio2.CodecName)
	}
	if audio1.Tags["language"] != "eng" {
		t.Errorf("audio1 language = %q, want eng", audio1.Tags["language"])
	}
	if audio2.Tags["language"] != "jpn" {
		t.Errorf("audio2 language = %q, want jpn", audio2.Tags["language"])
	}

	forcedSub := findStreamWithDisposition(info.Streams, "subtitle", "forced")
	if forcedSub == nil {
		t.Fatal("expected a forced subtitle stream")
	}
	if forcedSub.Tags["title"] != "Signs & Songs" {
		t.Errorf("forced sub title = %q, want 'Signs & Songs'", forcedSub.Tags["title"])
	}
	if forcedSub.Disposition.Default != 1 {
		t.Error("forced sub should also be the default")
	}

	dialogueSub := findStreamByTitle(info.Streams, "subtitle", "Dialogue")
	if dialogueSub == nil {
		t.Fatal("expected a dialogue subtitle stream")
	}
	if dialogueSub.Disposition != nil && dialogueSub.Disposition.Forced == 1 {
		t.Error("dialogue sub should not be forced")
	}

	if info.Duration < 1425 || info.Duration > 1426 {
		t.Errorf("duration = %f, want ~1425", info.Duration)
	}
}

func TestParseFFProbeOutput_TVExtant(t *testing.T) {
	data := loadFixture(t, "tv_extant_s01e13.json")
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(info.Streams) != 3 {
		t.Errorf("stream count = %d, want 3 (1 video + 1 audio + 1 subtitle)", len(info.Streams))
	}

	video := findStream(info.Streams, "video", 0)
	if video == nil {
		t.Fatal("no video stream found")
	}
	if video.Width != 1920 || video.Height != 1080 {
		t.Errorf("resolution = %dx%d, want 1920x1080", video.Width, video.Height)
	}

	audio := findStream(info.Streams, "audio", 0)
	if audio == nil {
		t.Fatal("no audio stream found")
	}
	if audio.Channels != 6 {
		t.Errorf("audio channels = %d, want 6 (5.1)", audio.Channels)
	}

	if info.Size != 497818777 {
		t.Errorf("size = %d, want 497818777", info.Size)
	}
}

func TestParseFFProbeOutput_InvalidJSON(t *testing.T) {
	_, err := ParseFFProbeOutput([]byte(`{garbage`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseFFProbeOutput_EmptyStreams(t *testing.T) {
	data := []byte(`{"format": {"format_name": "mp4", "duration": "10.5"}, "streams": []}`)
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(info.Streams) != 0 {
		t.Errorf("stream count = %d, want 0", len(info.Streams))
	}
	if info.Container != "mp4" {
		t.Errorf("container = %q, want mp4", info.Container)
	}
	if info.Duration != 10.5 {
		t.Errorf("duration = %f, want 10.5", info.Duration)
	}
}

func TestParseFFProbeOutput_OnlyAttachments(t *testing.T) {
	data := []byte(`{
		"format": {"format_name": "matroska,webm"},
		"streams": [
			{"index": 0, "codec_type": "attachment", "codec_name": "ttf"},
			{"index": 1, "codec_type": "attachment", "codec_name": "otf"}
		]
	}`)
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(info.Streams) != 0 {
		t.Errorf("stream count = %d, want 0 (all attachments filtered)", len(info.Streams))
	}
}

func TestPopulateNumericFields(t *testing.T) {
	tests := []struct {
		name     string
		format   FormatInfo
		wantDur  float64
		wantSize int64
		wantBR   int64
	}{
		{
			name:     "normal values",
			format:   FormatInfo{Duration: "6394.400000", Size: "2003397901", BitRate: "2506440"},
			wantDur:  6394.4,
			wantSize: 2003397901,
			wantBR:   2506440,
		},
		{
			name:     "empty strings",
			format:   FormatInfo{Duration: "", Size: "", BitRate: ""},
			wantDur:  0,
			wantSize: 0,
			wantBR:   0,
		},
		{
			name:     "malformed values",
			format:   FormatInfo{Duration: "not-a-number", Size: "abc", BitRate: "xyz"},
			wantDur:  0,
			wantSize: 0,
			wantBR:   0,
		},
		{
			name:     "partial values",
			format:   FormatInfo{Duration: "123.456", Size: "", BitRate: "1000"},
			wantDur:  123.456,
			wantSize: 0,
			wantBR:   1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &MediaInfo{Format: tt.format}
			populateNumericFields(info)
			if info.Duration != tt.wantDur {
				t.Errorf("duration = %f, want %f", info.Duration, tt.wantDur)
			}
			if info.Size != tt.wantSize {
				t.Errorf("size = %d, want %d", info.Size, tt.wantSize)
			}
			if info.BitRate != tt.wantBR {
				t.Errorf("bitrate = %d, want %d", info.BitRate, tt.wantBR)
			}
		})
	}
}

func TestMediaInfoJSONRoundTrip(t *testing.T) {
	data := loadFixture(t, "anime_horimiya_s01e03.json")
	info, err := ParseFFProbeOutput(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded MediaInfo
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Container != info.Container {
		t.Errorf("container round-trip: %q != %q", decoded.Container, info.Container)
	}
	if decoded.Duration != info.Duration {
		t.Errorf("duration round-trip: %f != %f", decoded.Duration, info.Duration)
	}
	if len(decoded.Streams) != len(info.Streams) {
		t.Errorf("stream count round-trip: %d != %d", len(decoded.Streams), len(info.Streams))
	}

	for i, s := range decoded.Streams {
		orig := info.Streams[i]
		if s.CodecName != orig.CodecName {
			t.Errorf("stream[%d] codec round-trip: %q != %q", i, s.CodecName, orig.CodecName)
		}
		if s.Disposition != nil && orig.Disposition != nil {
			if s.Disposition.Forced != orig.Disposition.Forced {
				t.Errorf("stream[%d] disposition.forced round-trip: %d != %d", i, s.Disposition.Forced, orig.Disposition.Forced)
			}
		}
	}
}

func TestBackwardCompatibility_OldMediaInfoJSON(t *testing.T) {
	oldJSON := []byte(`{
		"format": {"format_name": "mp4", "duration": "120.0", "size": "1000000", "bit_rate": "66666"},
		"streams": [
			{"index": 0, "codec_name": "h264", "codec_type": "video", "width": 1920, "height": 1080, "tags": {}},
			{"index": 1, "codec_name": "aac", "codec_type": "audio", "channels": 2, "tags": {"language": "eng"}}
		],
		"duration": 120.0,
		"size": 1000000,
		"bit_rate": 66666,
		"container": "mp4"
	}`)

	var info MediaInfo
	if err := json.Unmarshal(oldJSON, &info); err != nil {
		t.Fatalf("failed to unmarshal old-format JSON: %v", err)
	}

	if info.Container != "mp4" {
		t.Errorf("container = %q, want mp4", info.Container)
	}
	if len(info.Streams) != 2 {
		t.Errorf("streams = %d, want 2", len(info.Streams))
	}

	video := info.Streams[0]
	if video.Disposition != nil {
		t.Error("old data should have nil disposition")
	}
	if video.Profile != "" {
		t.Error("old data should have empty profile")
	}
	if video.PixFmt != "" {
		t.Error("old data should have empty pix_fmt")
	}
}

// --- Integration tests (require real files + ffprobe binary) ---

func TestFFProbeRealFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	files := discoverFulldataFiles(t)
	if len(files) == 0 {
		t.Skip("no files in fulldata/")
	}

	file := files[0]
	t.Logf("probing: %s", file)

	output, err := runFFProbe(file)
	if err != nil {
		t.Fatalf("ffprobe exec: %v", err)
	}

	info, err := ParseFFProbeOutput(output)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if info.Container == "" {
		t.Error("container should not be empty")
	}
	if info.Duration <= 0 {
		t.Error("duration should be positive")
	}
	if len(info.Streams) == 0 {
		t.Error("expected at least one stream")
	}

	videoFound := false
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			videoFound = true
			if s.Width == 0 || s.Height == 0 {
				t.Error("video stream should have resolution")
			}
		}
	}
	if !videoFound {
		t.Error("expected at least one video stream")
	}
}

func TestFFProbeRealFile_AllFulldata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	files := discoverFulldataFiles(t)
	if len(files) == 0 {
		t.Skip("no files in fulldata/")
	}

	var stats struct {
		total, video, audio, subtitle int
		codecs                        map[string]int
	}
	stats.codecs = make(map[string]int)

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			output, err := runFFProbe(file)
			if err != nil {
				t.Fatalf("ffprobe exec: %v", err)
			}

			info, err := ParseFFProbeOutput(output)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			stats.total++
			for _, s := range info.Streams {
				switch s.CodecType {
				case "video":
					stats.video++
					stats.codecs[s.CodecName]++
				case "audio":
					stats.audio++
				case "subtitle":
					stats.subtitle++
				}
			}

			for _, s := range info.Streams {
				if s.CodecType == "attachment" {
					t.Error("attachment stream leaked through filter")
				}
			}
		})
	}

	t.Logf("probed %d files: %d video, %d audio, %d subtitle streams", stats.total, stats.video, stats.audio, stats.subtitle)
	t.Logf("video codecs: %v", stats.codecs)
}

func TestFFProbeWorker_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	files := discoverFulldataFiles(t)
	if len(files) == 0 {
		t.Skip("no files in fulldata/")
	}

	pool := testutil.SetupDB(t)
	userID := testutil.TestUserID(t, pool)
	ctx := context.Background()

	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "ffprobe-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{filepath.Dir(files[0])},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("creating library: %v", err)
	}
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   lib.ID,
		Path:        files[0],
		Size:        1000,
		Mtime:       pgtype.Timestamptz{Valid: false},
		ParseResult: []byte("{}"),
		Status:      sqlc.FileStatusPending,
	})
	if err != nil {
		t.Fatalf("creating library file: %v", err)
	}

	worker := &FFProbeWorker{DB: pool}

	// Simulate what Work() does without the River job wrapper
	output, err := runFFProbe(files[0])
	if err != nil {
		t.Fatalf("ffprobe exec: %v", err)
	}

	info, err := ParseFFProbeOutput(output)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	infoJSON, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{
		ID:        file.ID,
		MediaInfo: infoJSON,
	}); err != nil {
		t.Fatalf("db write: %v", err)
	}

	got, err := q.GetLibraryFileByID(ctx, file.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	var stored MediaInfo
	if err := json.Unmarshal(got.MediaInfo, &stored); err != nil {
		t.Fatalf("unmarshal stored: %v", err)
	}

	if stored.Container != info.Container {
		t.Errorf("stored container = %q, want %q", stored.Container, info.Container)
	}
	if stored.Duration != info.Duration {
		t.Errorf("stored duration = %f, want %f", stored.Duration, info.Duration)
	}
	if len(stored.Streams) != len(info.Streams) {
		t.Errorf("stored streams = %d, want %d", len(stored.Streams), len(info.Streams))
	}

	_ = worker
}

// --- Helpers ---

func findStream(streams []StreamInfo, codecType string, nth int) *StreamInfo {
	count := 0
	for i := range streams {
		if streams[i].CodecType == codecType {
			if count == nth {
				return &streams[i]
			}
			count++
		}
	}
	return nil
}

func findStreamWithDisposition(streams []StreamInfo, codecType, flag string) *StreamInfo {
	for i := range streams {
		s := &streams[i]
		if s.CodecType != codecType || s.Disposition == nil {
			continue
		}
		switch flag {
		case "default":
			if s.Disposition.Default == 1 {
				return s
			}
		case "forced":
			if s.Disposition.Forced == 1 {
				return s
			}
		case "hearing_impaired":
			if s.Disposition.HearingImpaired == 1 {
				return s
			}
		}
	}
	return nil
}

func findStreamByTitle(streams []StreamInfo, codecType, title string) *StreamInfo {
	for i := range streams {
		if streams[i].CodecType == codecType && streams[i].Tags["title"] == title {
			return &streams[i]
		}
	}
	return nil
}

func countStreamType(streams []StreamInfo, codecType string) int {
	count := 0
	for _, s := range streams {
		if s.CodecType == codecType {
			count++
		}
	}
	return count
}

func runFFProbe(filePath string) ([]byte, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)
	return cmd.Output()
}

func discoverFulldataFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "fulldata")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}

	mediaExts := map[string]bool{
		".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
		".ts": true, ".flv": true, ".webm": true, ".mov": true,
	}

	var files []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if mediaExts[ext] {
			files = append(files, path)
		}
		return nil
	})
	return files
}
