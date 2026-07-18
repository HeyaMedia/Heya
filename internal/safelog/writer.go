// Package safelog provides log-output boundaries that remove credentials from
// complete zerolog events before they reach a console or JSON sink.
package safelog

import (
	"io"

	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/rs/zerolog"
)

// Redact wraps writer as a zerolog.LevelWriter. Zerolog hands WriteLevel one
// complete encoded event, allowing URL userinfo to be sanitized reliably even
// when the underlying io.Writer would split output into arbitrary chunks.
func Redact(writer io.Writer) zerolog.LevelWriter {
	if writer == nil {
		writer = io.Discard
	}
	levelWriter, ok := writer.(zerolog.LevelWriter)
	if !ok {
		levelWriter = zerolog.LevelWriterAdapter{Writer: writer}
	}
	return &redactingWriter{next: levelWriter}
}

type redactingWriter struct {
	next zerolog.LevelWriter
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	return w.write(zerolog.NoLevel, p)
}

func (w *redactingWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	return w.write(level, p)
}

func (w *redactingWriter) write(level zerolog.Level, p []byte) (int, error) {
	redacted := []byte(secrettext.Redact(string(p)))
	written, err := w.next.WriteLevel(level, redacted)
	if err != nil {
		return 0, err
	}
	if written != len(redacted) {
		return 0, io.ErrShortWrite
	}
	// io.Writer counts bytes consumed from its input, not bytes emitted after a
	// transformation. Redaction may shorten the event substantially.
	return len(p), nil
}
