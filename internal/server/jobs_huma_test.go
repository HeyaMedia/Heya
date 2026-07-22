package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobWorkerSettingsRouteReturnsJSON(t *testing.T) {
	api := authedAPI(t)
	resp := api.Get("/api/jobs/worker-settings", "Authorization: Bearer admin-token")

	require.Equal(t, http.StatusOK, resp.Result().StatusCode)
	require.Contains(t, resp.Result().Header.Get("Content-Type"), "application/json")

	var body struct {
		Workers []struct {
			Kind  string `json:"kind"`
			Value int    `json:"value"`
		} `json:"workers"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Workers)

	byKind := map[string]int{}
	for _, worker := range body.Workers {
		byKind[worker.Kind] = worker.Value
	}
	assert.Equal(t, 4, byKind["process_scan"])
	assert.Equal(t, 4, byKind["search_metadata"])
	assert.Equal(t, 4, byKind["search_metadata_poll"])
	assert.Equal(t, 4, byKind["fetch_metadata"])
	assert.Equal(t, 4, byKind["fetch_metadata_poll"])
	assert.Equal(t, 4, byKind["apply_metadata"])
}

func TestTaskResponseDoesNotEmbedCoverageStats(t *testing.T) {
	encoded, err := json.Marshal(taskToResponse(sqlc.ScheduledTask{}, nil))
	require.NoError(t, err)

	var task map[string]any
	require.NoError(t, json.Unmarshal(encoded, &task))
	assert.NotContains(t, task, "stats", "basic task response should not wait for coverage stats")
}
