package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(hub *eventhub.Hub, sessionLookup auth.SessionLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := auth.TokenFromContext(r.Context())
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		// Resolve the token to a user regardless of which transport carried it:
		// the connection subscribes under that user id so PublishToUser events
		// (e.g. session commands) reach only this user's connections and never
		// leak over the global broadcast.
		resolved, err := auth.ResolveSession(r.Context(), sessionLookup, token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Warn().Err(err).Msg("websocket upgrade failed")
			return
		}
		defer conn.Close()

		ch := hub.SubscribeUser(resolved.User.ID)
		defer hub.Unsubscribe(ch)

		ctx := r.Context()

		done := make(chan struct{})

		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
					time.Now().Add(time.Second),
				)
				return
			case <-done:
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}
		}
	}
}
