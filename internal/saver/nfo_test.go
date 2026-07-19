package saver

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteXMLAttestsExactRetryAndPreservesMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artist.nfo")
	first := ArtistNFO{Title: "Ado", MBID: "e53f1f1c-5b85-4f47-9a3d-7587d4c9ef98"}
	firstOutput, err := writeXMLWithResult(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if !firstOutput.Attested || !firstOutput.Written {
		t.Fatalf("first output flags = attested:%v written:%v, want both", firstOutput.Attested, firstOutput.Written)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	retryOutput, err := writeXMLWithResult(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if !retryOutput.Attested || retryOutput.Written {
		t.Fatalf("retry output flags = attested:%v written:%v, want attested only", retryOutput.Attested, retryOutput.Written)
	}
	unchanged, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.ModTime().Equal(before.ModTime()) {
		t.Fatalf("identical NFO was rewritten: before=%s after=%s", before.ModTime(), unchanged.ModTime())
	}

	originalBody, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	userBody := append([]byte(nil), originalBody...)
	needle := []byte("Ado")
	index := bytes.Index(userBody, needle)
	if index < 0 {
		t.Fatalf("generated fixture does not contain %q", needle)
	}
	userBody[index] = 'X' // same size, different user-owned bytes
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, userBody, 0o644); err != nil {
		t.Fatal(err)
	}
	userOwned, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mismatchOutput, err := writeXMLWithResult(path, first)
	if err != nil {
		t.Fatal(err)
	}
	if mismatchOutput.Attested || mismatchOutput.Written {
		t.Fatalf("mismatch output flags = attested:%v written:%v, want neither", mismatchOutput.Attested, mismatchOutput.Written)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.ModTime().Equal(userOwned.ModTime()) {
		t.Fatalf("mismatched NFO was rewritten: before=%s after=%s", userOwned.ModTime(), changed.ModTime())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, userBody) {
		t.Fatalf("same-size user edit was overwritten: got %q want %q", body, userBody)
	}
}

func TestMediaDirOnlyCollapsesNumberedMediaContainers(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "tv season",
			filePath: filepath.Join("library", "Show", "Season 01", "episode.mkv"),
			want:     filepath.Join("library", "Show"),
		},
		{
			name:     "disc container",
			filePath: filepath.Join("library", "Movie", "Disc 2", "movie.mkv"),
			want:     filepath.Join("library", "Movie"),
		},
		{
			name:     "compact cd container",
			filePath: filepath.Join("library", "Concert", "CD3", "track.flac"),
			want:     filepath.Join("library", "Concert"),
		},
		{
			name:     "movie title beginning with disc",
			filePath: filepath.Join("library", "Disclosure (2013)", "Disclosure.mkv"),
			want:     filepath.Join("library", "Disclosure (2013)"),
		},
		{
			name:     "movie title beginning with season",
			filePath: filepath.Join("library", "Season of the Witch (2011)", "Season of the Witch.mkv"),
			want:     filepath.Join("library", "Season of the Witch (2011)"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MediaDir(test.filePath); got != test.want {
				t.Fatalf("MediaDir(%q) = %q, want %q", test.filePath, got, test.want)
			}
		})
	}
}
