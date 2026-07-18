package service

import (
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestRedactMusicTrackDetailPaths(t *testing.T) {
	t.Parallel()

	row := sqlc.GetTrackDetailByIDRow{
		FilePath:   "https://user:password@storage.test/music/track.flac",
		LyricsPath: "sftp://lyrics:secret@storage.test/music/track.lrc",
	}
	got := redactMusicTrackDetailPaths(row)
	if got.FilePath != "https://xxxxx@storage.test/music/track.flac" || got.LyricsPath != "sftp://xxxxx@storage.test/music/track.lrc" {
		t.Fatalf("unexpected redacted row: %#v", got)
	}
	if strings.Contains(got.FilePath+got.LyricsPath, "password") || strings.Contains(got.FilePath+got.LyricsPath, "secret") {
		t.Fatal("credential remains in track response")
	}
	if row.FilePath == got.FilePath {
		t.Fatal("redaction unexpectedly mutated its value argument")
	}
}

func TestRedactTrackFilePathsDoesNotMutateExecutionValues(t *testing.T) {
	t.Parallel()

	files := []sqlc.TrackFile{{LyricsPath: "sftp://lyrics:secret@storage.test/music/track.lrc"}}
	got := redactTrackFilePaths(files)
	if got[0].LyricsPath != "sftp://xxxxx@storage.test/music/track.lrc" {
		t.Fatalf("unexpected redacted track file: %#v", got[0])
	}
	if files[0].LyricsPath != "sftp://lyrics:secret@storage.test/music/track.lrc" {
		t.Fatal("response redaction mutated the execution value")
	}
}

func TestRedactJobRowSanitizesJSONAndErrorsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	job := JobRow{
		Args:   `{"file_path":"https://user:password@storage.test/media/file.mkv","nested":{"paths":["/local","sftp://a:b@other.test/share"]}}`,
		Errors: `[{"error":"probe https://user:password@storage.test/media/file.mkv failed"}]`,
	}
	got := redactJobRow(job)
	if strings.Contains(got.Args+got.Errors, "password") || strings.Contains(got.Args+got.Errors, "sftp://a:b@") {
		t.Fatalf("credential remains in job response: %#v", got)
	}
	if !strings.Contains(got.Args, "https://xxxxx@storage.test/media/file.mkv") || !strings.Contains(got.Errors, "https://xxxxx@storage.test/media/file.mkv") {
		t.Fatalf("useful redacted path missing: %#v", got)
	}
	if !strings.Contains(job.Args, "password") || !strings.Contains(job.Errors, "password") {
		t.Fatal("response redaction mutated stored job values")
	}
}
