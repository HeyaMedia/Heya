package jellyfin

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/gorilla/websocket"
	"github.com/karbowiak/heya/internal/eventhub"
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

// socketConn is one connected client, registered for event pushes.
type socketConn struct {
	userID int64
	send   func(msgType string, data any) error
}

// GET /socket (alias /embywebsocket) — authenticated via ?api_key= (the only
// form clients use here; headers can't ride a browser WebSocket upgrade).
func (s *Server) handleSocket(w http.ResponseWriter, r *http.Request, _ Params) {
	res, _, ok := s.resolve(r)
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

	sc := &socketConn{userID: res.User.ID, send: send}
	s.registerSocket(sc)
	defer s.unregisterSocket(sc)

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

func (s *Server) registerSocket(sc *socketConn) {
	s.socketsMu.Lock()
	if s.sockets == nil {
		s.sockets = map[*socketConn]struct{}{}
	}
	s.sockets[sc] = struct{}{}
	s.socketsMu.Unlock()
}

func (s *Server) unregisterSocket(sc *socketConn) {
	s.socketsMu.Lock()
	delete(s.sockets, sc)
	s.socketsMu.Unlock()
}

// broadcastSocket pushes a message to every connected client (userID 0) or
// to one user's clients. Send errors are ignored — the read loop notices the
// dead conn and unregisters it.
func (s *Server) broadcastSocket(userID int64, msgType string, data any) {
	s.socketsMu.RLock()
	targets := make([]*socketConn, 0, len(s.sockets))
	for sc := range s.sockets {
		if userID == 0 || sc.userID == userID {
			targets = append(targets, sc)
		}
	}
	s.socketsMu.RUnlock()
	for _, sc := range targets {
		_ = sc.send(msgType, data)
	}
}

// bridgeEvents forwards Heya event-hub broadcasts to connected Jellyfin
// sockets in their protocol. Runs for the App lifetime; started once from
// NewMiddleware when a hub is present.
func (s *Server) bridgeEvents() {
	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	ctx := s.app.LifetimeContext()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !s.app.JellyfinEnabled() {
				continue
			}
			switch ev.Type {
			case eventhub.EventScanCompleted, eventhub.EventMediaAdded, eventhub.EventMediaRemoved, eventhub.EventLibraryDeleted:
				s.broadcastSocket(0, "LibraryChanged", map[string]any{
					"FoldersAddedTo":     []string{},
					"FoldersRemovedFrom": []string{},
					"ItemsAdded":         []string{},
					"ItemsRemoved":       []string{},
					"ItemsUpdated":       []string{},
					"CollectionFolders":  []string{},
					"IsEmpty":            false,
				})
			case eventhub.EventMediaWatched:
				if p, ok := ev.Payload.(eventhub.WatchPayload); ok {
					// Every UserItemDataDto field the kotlin SDK marks
					// required must be present or the whole socket frame
					// fails to deserialize client-side. PlayCount/IsFavorite
					// aren't in the payload; clients use this as an
					// invalidation hint, so zero values are fine.
					s.broadcastSocket(p.UserID, "UserDataChanged", map[string]any{
						"UserId": EncodeID(KindUser, p.UserID),
						"UserDataList": []map[string]any{{
							"Key":                   strconv.FormatInt(p.MediaItemID, 10),
							"ItemId":                EncodeID(KindItem, p.MediaItemID),
							"PlaybackPositionTicks": int64(p.Progress) * ticksPerSecond,
							"PlayCount":             0,
							"IsFavorite":            false,
							"Played":                p.Completed,
						}},
					})
				}
			}
		}
	}
}
