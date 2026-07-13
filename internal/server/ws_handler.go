package server

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClientMessage struct {
	Type     string                 `json:"type"`
	Events   []string               `json:"events"`
	Device   *eventhub.ClientDevice `json:"device,omitempty"`
	DeviceID string                 `json:"device_id,omitempty"`
	State    map[string]any         `json:"state,omitempty"`
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
		rawEvents := r.URL.Query().Get("events") == "raw"
		clientSubscriptions := r.URL.Query().Get("subscriptions") == "1"
		var registeredDeviceID atomic.Value
		registeredDeviceID.Store("")
		var wantsLogs atomic.Bool
		// Old frontend bundles never send subscription controls. Keep their
		// behavior intact across a backend restart; only negotiated clients opt
		// into suppressing the otherwise-global log stream.
		wantsLogs.Store(rawEvents || !clientSubscriptions)
		var coalescer *wsEventCoalescer
		var flushTicker *time.Ticker
		var flush <-chan time.Time
		if !rawEvents {
			coalescer = newWSEventCoalescer()
			flushTicker = time.NewTicker(250 * time.Millisecond)
			flush = flushTicker.C
			defer flushTicker.Stop()
		}

		writeEvent := func(event eventhub.Event) error {
			data, err := json.Marshal(event)
			if err != nil {
				return nil
			}
			if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			return conn.WriteMessage(websocket.TextMessage, data)
		}

		done := make(chan struct{})

		go func() {
			defer close(done)
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if rawEvents || !clientSubscriptions {
					continue
				}
				var message wsClientMessage
				if json.Unmarshal(data, &message) != nil {
					continue
				}
				if message.Type == "device.hello" && message.Device != nil {
					registeredDeviceID.Store(message.Device.ID)
					hub.UpsertDevice(resolved.User.ID, *message.Device)
					hub.EmitToUser(resolved.User.ID, eventhub.EventDeviceState, *message.Device)
					continue
				}
				if message.Type == "device.heartbeat" && message.DeviceID == registeredDeviceID.Load().(string) && message.DeviceID != "" {
					devices := hub.ClientDevices(resolved.User.ID)
					for _, d := range devices {
						if d.ID == message.DeviceID {
							d.State = message.State
							hub.UpsertDevice(resolved.User.ID, d)
							hub.EmitToUser(resolved.User.ID, eventhub.EventDeviceState, d)
							break
						}
					}
					continue
				}
				if message.Type != "subscribe" {
					continue
				}
				wantsLogEvents := false
				for _, eventType := range message.Events {
					if eventType == string(eventhub.EventLog) {
						wantsLogEvents = true
						break
					}
				}
				wantsLogs.Store(wantsLogEvents)
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
			case <-flush:
				for _, event := range coalescer.Drain() {
					if err := writeEvent(event); err != nil {
						return
					}
				}
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.Type == eventhub.EventLog && !wantsLogs.Load() {
					continue
				}
				if coalescer != nil && coalescer.Queue(event) {
					continue
				}
				if err := writeEvent(event); err != nil {
					return
				}
			}
		}
	}
}
