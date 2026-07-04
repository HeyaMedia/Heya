package jellyfin

import (
	"net/http"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Jellyfin's /socket protocol: JSON text frames shaped
// {"MessageType": "...", "MessageId": "...", "Data": ...}. The server sends
// ForceKeepAlive with a timeout (seconds); the client must send KeepAlive
// within it and the server acks each one. Clients also push subscription
// requests (SessionsStart, ActivityLogEntryStart, ScheduledTasksInfoStart...)
// which a server may silently accept. Phase 0 implements exactly that
// contract — enough for jellyfin-web and mobile clients to consider the
// connection healthy. Event pushes (LibraryChanged, UserDataChanged, remote
// control) bridge from the eventhub in a later phase.

const socketKeepAliveTimeout = 60 // seconds, mirrors Jellyfin's default

type socketMessage struct {
	MessageType string          `json:"MessageType"`
	MessageID   string          `json:"MessageId,omitempty"`
	Data        json.RawMessage `json:"Data,omitempty"`
}

var socketUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Same trust model as Heya's own /api/ws: token auth gates the upgrade,
	// origin is not meaningful for non-browser clients.
	CheckOrigin: func(*http.Request) bool { return true },
}

// GET /socket (alias /embywebsocket) — authenticated via ?api_key= (the only
// form clients use here; headers can't ride a browser WebSocket upgrade).
func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request, _ Params) {
	_, _, ok := s.resolve(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	conn, err := socketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error response
	}
	defer func() { _ = conn.Close() }()

	var writeMu sync.Mutex
	send := func(msgType string, data any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		payload := map[string]any{"MessageType": msgType}
		if data != nil {
			payload["Data"] = data
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(payload)
	}

	if err := send("ForceKeepAlive", socketKeepAliveTimeout); err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * socketKeepAliveTimeout * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * socketKeepAliveTimeout * time.Second))

		var msg socketMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Debug().Str("component", "jellyfin").Msg("socket: undecodable frame ignored")
			continue
		}
		switch msg.MessageType {
		case "KeepAlive":
			if err := send("KeepAlive", nil); err != nil {
				return
			}
		default:
			// Subscription starts/stops and player commands land here.
			// Accepting silently is valid protocol behavior; the client
			// simply receives no events for that subscription.
		}
	}
}
