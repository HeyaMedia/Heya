package saver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteXMLDoesNotRewriteUnchangedNFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artist.nfo")
	first := ArtistNFO{Title: "Ado", MBID: "e53f1f1c-5b85-4f47-9a3d-7587d4c9ef98"}
	if err := writeXML(path, first); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := writeXML(path, first); err != nil {
		t.Fatal(err)
	}
	unchanged, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.ModTime().Equal(before.ModTime()) {
		t.Fatalf("identical NFO was rewritten: before=%s after=%s", before.ModTime(), unchanged.ModTime())
	}

	time.Sleep(20 * time.Millisecond)
	if err := writeXML(path, ArtistNFO{Title: "Ado", Biography: "Updated"}); err != nil {
		t.Fatal(err)
	}
	changed, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.ModTime().After(unchanged.ModTime()) {
		t.Fatalf("changed NFO was not rewritten: before=%s after=%s", unchanged.ModTime(), changed.ModTime())
	}
}
