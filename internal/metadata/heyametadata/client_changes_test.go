package heyametadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testStreamID   = "9dcc2b52-3af0-4d4c-90b1-f30c05f97dba"
	testEntityID   = "cc9065a1-4a31-4f96-a868-ae278f915a35"
	testWorkflowID = "122ca081-208f-4031-be0e-20328769c8c4"
)

func TestChangesCarriesStreamIdentityAndCursors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/changes", r.URL.Path)
		require.Equal(t, "12", r.URL.Query().Get("after"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))
		require.Equal(t, testStreamID, r.URL.Query().Get("stream_id"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "stream_id": %q,
            "head_cursor": 15,
            "next_cursor": 13,
            "entries": [{
                "sequence": 13,
                "entity_id": %q,
                "entity_kind": "movie",
                "slug": "example",
                "change_type": "updated",
                "changed_scopes": ["credits"],
                "projection_version": 8,
                "created_at": "2026-07-15T12:00:00Z"
            }]
        }`, testStreamID, testEntityID)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	page, err := client.Changes(context.Background(), 12, 500, testStreamID)
	require.NoError(t, err)
	require.Equal(t, testStreamID, page.StreamID)
	require.EqualValues(t, 15, page.HeadCursor)
	require.EqualValues(t, 13, page.NextCursor)
	require.Len(t, page.Entries, 1)
	require.Equal(t, testEntityID, page.Entries[0].EntityID)
	require.Equal(t, []string{"credits"}, page.Entries[0].ChangedScopes)
}

func TestChangesReturnsTypedResetConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{
            "type": "about:blank",
            "title": "Change stream changed",
            "status": 409,
            "code": "change_stream_changed",
            "stream_id": %q,
            "head_cursor": 41
        }`, testStreamID)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	_, err = client.Changes(context.Background(), 99, 500, "")
	var conflict *ChangeStreamConflict
	require.True(t, errors.As(err, &conflict))
	require.Equal(t, "change_stream_changed", conflict.Code)
	require.Equal(t, testStreamID, conflict.StreamID)
	require.EqualValues(t, 41, conflict.HeadCursor)
}

func TestWorkflowEventsCarriesCompletionData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/workflow-events", r.URL.Path)
		require.Equal(t, "40", r.URL.Query().Get("after"))
		require.Equal(t, "500", r.URL.Query().Get("limit"))
		require.Equal(t, testStreamID, r.URL.Query().Get("stream_id"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
            "stream_id": %q,
            "head_cursor": 42,
            "next_cursor": 41,
            "events": [{
                "sequence": 41,
                "kind": "discovery",
                "id": %q,
                "state": "completed",
                "completed_at": "2026-07-17T17:40:23.384155Z"
            }]
        }`, testStreamID, testWorkflowID)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	page, err := client.WorkflowEvents(context.Background(), 40, 500, testStreamID)
	require.NoError(t, err)
	require.Equal(t, testStreamID, page.StreamID)
	require.EqualValues(t, 42, page.HeadCursor)
	require.EqualValues(t, 41, page.NextCursor)
	require.Len(t, page.Events, 1)
	require.Equal(t, testWorkflowID, page.Events[0].ID)
	require.Equal(t, "discovery", page.Events[0].Kind)
	require.Equal(t, "completed", page.Events[0].State)
	require.Equal(t, "2026-07-17T17:40:23.384155Z", page.Events[0].CompletedAt.Format(time.RFC3339Nano))
}

func TestWorkflowEventsReturnsTypedResetConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{
            "type": "about:blank",
            "title": "Workflow stream changed",
            "status": 409,
            "code": "workflow_stream_changed",
            "stream_id": %q,
            "head_cursor": 41
        }`, testStreamID)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	_, err = client.WorkflowEvents(context.Background(), 99, 500, "")
	var conflict *WorkflowStreamConflict
	require.True(t, errors.As(err, &conflict))
	require.Equal(t, "workflow_stream_changed", conflict.Code)
	require.Equal(t, testStreamID, conflict.StreamID)
	require.EqualValues(t, 41, conflict.HeadCursor)
}

func TestCreditsRequestsMaximumPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v2/entities/"+testEntityID+"/credits", r.URL.Path)
		require.Equal(t, "5000", r.URL.Query().Get("limit"))
		require.Equal(t, "0", r.URL.Query().Get("offset"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[],"total":0,"offset":0,"limit":5000}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	require.NoError(t, err)
	credits, err := client.Credits(context.Background(), testEntityID, ProviderCredentials{})
	require.NoError(t, err)
	require.Empty(t, credits)
}
