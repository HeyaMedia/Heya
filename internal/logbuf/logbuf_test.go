package logbuf

import (
	"strings"
	"testing"
)

func TestRingBufferRedactsCredentialedURLs(t *testing.T) {
	t.Parallel()

	buffer := New(4)
	structured := []byte(`{"level":"error","message":"open https://user:password@example.test/share failed","path":"sftp://other:secret@storage.test/media","nested":{"source":"https://api:key@example.test/x"}}`)
	if _, err := buffer.Write(structured); err != nil {
		t.Fatalf("write structured log: %v", err)
	}
	if _, err := buffer.Write([]byte("retry ftp://plain:secret@storage.test/share")); err != nil {
		t.Fatalf("write plain log: %v", err)
	}

	entries := buffer.Recent(2)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if strings.Contains(entries[0].Message, "password") || entries[0].Fields["path"] != "sftp://xxxxx@storage.test/media" {
		t.Fatalf("structured log was not redacted: %#v", entries[0])
	}
	nested := entries[0].Fields["nested"].(map[string]any)
	if nested["source"] != "https://xxxxx@example.test/x" {
		t.Fatalf("nested log field was not redacted: %#v", nested)
	}
	if strings.Contains(entries[1].Message, "plain:secret") {
		t.Fatalf("plain log was not redacted: %#v", entries[1])
	}
}
