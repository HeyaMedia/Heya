package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoundedWorkerLogPayloadFitsPostgresNotify(t *testing.T) {
	payload := boundedWorkerLogPayload(logbuf.Entry{
		Level: "debug", Message: strings.Repeat("ø", 5000),
		Fields: map[string]any{"large": strings.Repeat("x", 9000)},
	})
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(raw), maxWorkerLogRelayBytes)
	assert.Equal(t, true, payload.Fields["relay_truncated"])
	assert.True(t, strings.HasSuffix(payload.Message, "…"))
}
