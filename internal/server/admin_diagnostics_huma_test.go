package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectLogSummaryCountsRecentLevelsAndKeepsNewestSignals(t *testing.T) {
	buf := logbuf.New(16)
	for i := 0; i < 10; i++ {
		level := "warn"
		if i%3 == 0 {
			level = "error"
		}
		payload, err := json.Marshal(map[string]any{
			"time": time.Now().UTC().Format(time.RFC3339Nano), "level": level, "message": "signal",
		})
		require.NoError(t, err)
		_, err = buf.Write(payload)
		require.NoError(t, err)
	}

	summary := collectLogSummary(buf)
	assert.Equal(t, 10, summary.Buffered)
	assert.Equal(t, 16, summary.Capacity)
	assert.Equal(t, 4, summary.Counts["error"])
	assert.Equal(t, 6, summary.Counts["warn"])
	assert.Len(t, summary.Recent, 8)
}

func TestDiagnosticFindingsEscalatesCurrentPressure(t *testing.T) {
	body := adminDiagnosticsBody{
		Status:   "healthy",
		Database: adminDBBody{TotalConnections: 9, AcquiredConnections: 9, MaxConnections: 10},
		Logs:     adminLogSummary{Last5Minutes: map[string]int{"error": 1}},
	}
	findings := diagnosticFindings(body)
	require.NotEmpty(t, findings)
	assert.Contains(t, findingTitles(findings), "Database pool nearly exhausted")
	assert.Contains(t, findingTitles(findings), "Recent errors in the log")
}

func findingTitles(findings []adminDiagnosticFinding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.Title)
	}
	return out
}
