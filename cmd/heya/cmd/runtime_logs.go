package cmd

import (
	"os"
	"time"

	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/safelog"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func configureRuntimeLogRing(size int, source string) *logbuf.RingBuffer {
	ring := logbuf.NewWithSource(size, source)
	var baseWriter zerolog.LevelWriter
	if cfg.LogFormat.Value == "console" {
		baseWriter = zerolog.MultiLevelWriter(
			safelog.Redact(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}),
			ring,
		)
	} else {
		baseWriter = zerolog.MultiLevelWriter(safelog.Redact(os.Stderr), ring)
	}
	log.Logger = zerolog.New(baseWriter).With().Timestamp().Logger()
	return ring
}
