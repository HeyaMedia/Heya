package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/stretchr/testify/require"
)

func TestWebSocketRawModeRequiresAdmin(t *testing.T) {
	hub := eventhub.New()
	server := httptest.NewServer(handleWebSocket(hub, fakeSessions{}))
	t.Cleanup(server.Close)
	wsBase := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("regular user denied raw mode", func(t *testing.T) {
		headers := http.Header{"Cookie": []string{"session_token=user-token"}}
		conn, response, err := websocket.DefaultDialer.Dial(wsBase+"?events=raw", headers)
		if conn != nil {
			_ = conn.Close()
		}
		require.Error(t, err)
		require.NotNil(t, response)
		t.Cleanup(func() { _ = response.Body.Close() })
		require.Equal(t, http.StatusForbidden, response.StatusCode)
	})

	t.Run("regular user may use normal stream", func(t *testing.T) {
		headers := http.Header{"Cookie": []string{"session_token=user-token"}}
		conn, response, err := websocket.DefaultDialer.Dial(wsBase+"?subscriptions=1", headers)
		if response != nil {
			t.Cleanup(func() { _ = response.Body.Close() })
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
	})

	t.Run("admin may use raw mode", func(t *testing.T) {
		headers := http.Header{"Cookie": []string{"session_token=admin-token"}}
		conn, response, err := websocket.DefaultDialer.Dial(wsBase+"?events=raw", headers)
		if response != nil {
			t.Cleanup(func() { _ = response.Body.Close() })
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
	})
}

func TestWebSocketRejectsCrossOriginBrowser(t *testing.T) {
	hub := eventhub.New()
	server := httptest.NewServer(handleWebSocket(hub, fakeSessions{}))
	t.Cleanup(server.Close)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	header := http.Header{}
	header.Set("Origin", "https://attacker.example")
	header.Set("Cookie", "session_token=user-token")
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, header)
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err)
	require.NotNil(t, response)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusForbidden, response.StatusCode)
}
