package server

import (
	"testing"
)

func TestParseLyricsSyncedLRC(t *testing.T) {
	input := []byte(`[ti:I Feel So Close]
[ar:Calvin Harris]
[00:01.10] I feel so close to you right now, it's a force field
[00:08.24] I wear my heart upon my sleeve, like a big deal
[00:32.01]
[01:16.23] I feel so close to you right now, it's a force field
`)
	resp := parseLyrics(input)

	if !resp.Synced {
		t.Fatal("expected synced=true")
	}
	if len(resp.Lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(resp.Lines))
	}
	if resp.Lines[0].TimeMs != 1_100 {
		t.Errorf("line[0] time: got %d, want 1100", resp.Lines[0].TimeMs)
	}
	if resp.Lines[1].TimeMs != 8_240 {
		t.Errorf("line[1] time: got %d, want 8240", resp.Lines[1].TimeMs)
	}
	if resp.Lines[2].Text != "" {
		t.Errorf("line[2] should be empty interlude, got %q", resp.Lines[2].Text)
	}
	if resp.Lines[3].TimeMs != 76_230 {
		t.Errorf("line[3] time: got %d, want 76230", resp.Lines[3].TimeMs)
	}
}

func TestParseLyricsPlainText(t *testing.T) {
	input := []byte("Verse one\nVerse two\n\nChorus\n")
	resp := parseLyrics(input)

	if resp.Synced {
		t.Fatal("expected synced=false for plain text")
	}
	if len(resp.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(resp.Lines))
	}
	for i, l := range resp.Lines {
		if l.TimeMs != -1 {
			t.Errorf("line[%d] expected time=-1, got %d", i, l.TimeMs)
		}
	}
}

func TestParseLyricsMultiTagKaraoke(t *testing.T) {
	// A single LRC line with multiple time tags — common for repeated
	// choruses to share text. Each tag becomes its own entry.
	input := []byte("[00:10.00][01:30.00] Chorus line\n")
	resp := parseLyrics(input)

	if !resp.Synced {
		t.Fatal("expected synced=true")
	}
	if len(resp.Lines) != 2 {
		t.Fatalf("expected 2 lines (one per tag), got %d", len(resp.Lines))
	}
	if resp.Lines[0].TimeMs != 10_000 || resp.Lines[1].TimeMs != 90_000 {
		t.Errorf("multi-tag times: got %d / %d", resp.Lines[0].TimeMs, resp.Lines[1].TimeMs)
	}
	if resp.Lines[0].Text != "Chorus line" {
		t.Errorf("multi-tag text: got %q", resp.Lines[0].Text)
	}
}
