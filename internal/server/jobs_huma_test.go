package server

import (
	"encoding/json"
	"net/http"
	"testing"

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
