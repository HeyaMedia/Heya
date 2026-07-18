package safelog

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

type recordingLevelWriter struct {
	bytes.Buffer
	level zerolog.Level
	limit int
	err   error
}

func (w *recordingLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	w.level = level
	if w.err != nil {
		return 0, w.err
	}
	if w.limit > 0 && len(p) > w.limit {
		_, _ = w.Write(p[:w.limit])
		return w.limit, nil
	}
	return w.Write(p)
}

func TestRedactSanitizesCompleteZerologEvent(t *testing.T) {
	sink := &recordingLevelWriter{}
	writer := Redact(sink)
	input := []byte(`{"level":"error","error":"open https://alice:secret@example.test/share","message":"failed"}` + "\n")

	written, err := writer.WriteLevel(zerolog.ErrorLevel, input)
	if err != nil {
		t.Fatalf("write level: %v", err)
	}
	if written != len(input) {
		t.Fatalf("written = %d, want source length %d", written, len(input))
	}
	if sink.level != zerolog.ErrorLevel {
		t.Fatalf("level = %s, want error", sink.level)
	}
	output := sink.String()
	if strings.Contains(output, "alice") || strings.Contains(output, "secret") {
		t.Fatalf("credentials remain in log output: %s", output)
	}
	if !strings.Contains(output, "https://xxxxx@example.test/share") {
		t.Fatalf("redacted path missing from log output: %s", output)
	}
}

func TestRedactImplementsOrdinaryWriter(t *testing.T) {
	var sink bytes.Buffer
	writer := Redact(&sink)
	input := []byte("open sftp://u:p@host.test/share")
	written, err := writer.Write(input)
	if err != nil || written != len(input) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", written, err, len(input))
	}
	if got := sink.String(); got != "open sftp://xxxxx@host.test/share" {
		t.Fatalf("output = %q", got)
	}
}

func TestRedactPropagatesWriterFailureAndShortWrite(t *testing.T) {
	wantErr := errors.New("sink failed")
	failed := Redact(&recordingLevelWriter{err: wantErr})
	if _, err := failed.Write([]byte("event")); !errors.Is(err, wantErr) {
		t.Fatalf("failure = %v, want %v", err, wantErr)
	}

	short := Redact(&recordingLevelWriter{limit: 1})
	if _, err := short.Write([]byte("event")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write = %v, want io.ErrShortWrite", err)
	}
}
