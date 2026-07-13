package cast

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	castV2SenderID           = "sender-0"
	castV2ReceiverID         = "receiver-0"
	castV2DefaultReceiverApp = "CC1AD845"
	castV2NSConnection       = "urn:x-cast:com.google.cast.tp.connection"
	castV2NSHeartbeat        = "urn:x-cast:com.google.cast.tp.heartbeat"
	castV2NSReceiver         = "urn:x-cast:com.google.cast.receiver"
	castV2NSMedia            = "urn:x-cast:com.google.cast.media"
	castV2ConnectTimeout     = 7 * time.Second
	castV2ResponseTimeout    = 15 * time.Second
	castV2StatusPollInterval = 5 * time.Second
	castV2WriteTimeout       = 3 * time.Second
)

type castV2Client struct {
	conn net.Conn

	sendMu  sync.Mutex
	mu      sync.RWMutex
	nextID  int
	app     castV2Application
	media   castV2MediaStatus
	readErr error

	messages     chan castV2Envelope
	done         chan struct{}
	closeOnce    sync.Once
	shutdownOnce sync.Once
	shutdownErr  error
}

type castV2Application struct {
	AppID       string `json:"appId"`
	SessionID   string `json:"sessionId"`
	TransportID string `json:"transportId"`
}

type castV2ReceiverStatus struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId"`
	Status    struct {
		Applications []castV2Application `json:"applications"`
	} `json:"status"`
}

type castV2MediaStatus struct {
	MediaSessionID int     `json:"mediaSessionId"`
	PlayerState    string  `json:"playerState"`
	IdleReason     string  `json:"idleReason"`
	CurrentTime    float64 `json:"currentTime"`
}

type castV2MediaStatusMessage struct {
	Type      string              `json:"type"`
	RequestID int                 `json:"requestId"`
	Status    []castV2MediaStatus `json:"status"`
}

type castV2Header struct {
	Type      string `json:"type"`
	RequestID int    `json:"requestId"`
}

func dialCastV2(ctx context.Context, addr string, port int) (*castV2Client, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: castV2ConnectTimeout, KeepAlive: 30 * time.Second},
		// Cast receivers use device-local/self-signed certificates. The TLS
		// channel provides protocol encryption but has no public PKI identity;
		// discovery supplies the LAN endpoint we intentionally connect to.
		Config: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Cast v2 receiver certificates are not publicly verifiable
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("cast v2 connect to %s:%d: %w", addr, port, err)
	}
	c := &castV2Client{
		conn:     conn,
		nextID:   1,
		messages: make(chan castV2Envelope, 32),
		done:     make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *castV2Client) readLoop() {
	defer c.closeDone()
	for {
		msg, err := readCastV2Frame(c.conn)
		if err != nil {
			c.mu.Lock()
			c.readErr = err
			c.mu.Unlock()
			return
		}
		var header castV2Header
		if err := json.Unmarshal([]byte(msg.PayloadUTF8), &header); err != nil {
			continue
		}
		if msg.Namespace == castV2NSHeartbeat && header.Type == "PING" {
			_ = c.send(msg.SourceID, castV2NSHeartbeat, map[string]any{"type": "PONG"}, false)
			continue
		}
		select {
		case c.messages <- msg:
		case <-c.done:
			return
		}
	}
}

func (c *castV2Client) closeDone() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *castV2Client) readFailure() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.readErr != nil {
		return c.readErr
	}
	return errors.New("cast v2 connection closed")
}

func (c *castV2Client) send(destination, namespace string, payload map[string]any, request bool) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if request {
		c.mu.Lock()
		payload["requestId"] = c.nextID
		c.nextID++
		c.mu.Unlock()
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(castV2WriteTimeout))
	err = writeCastV2Frame(c.conn, castV2Envelope{
		SourceID:      castV2SenderID,
		DestinationID: destination,
		Namespace:     namespace,
		PayloadUTF8:   string(data),
	})
	_ = c.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("cast v2 send %s: %w", payload["type"], err)
	}
	return nil
}

func (c *castV2Client) connect(ctx context.Context) error {
	if err := c.send(castV2ReceiverID, castV2NSConnection, map[string]any{"type": "CONNECT"}, false); err != nil {
		return err
	}
	if err := c.send(castV2ReceiverID, castV2NSReceiver, map[string]any{"type": "GET_STATUS"}, true); err != nil {
		return err
	}
	app, running, err := c.waitReceiverStatus(ctx, false)
	if err != nil {
		return err
	}
	if !running {
		if err := c.send(castV2ReceiverID, castV2NSReceiver, map[string]any{
			"type": "LAUNCH", "appId": castV2DefaultReceiverApp,
		}, true); err != nil {
			return err
		}
		app, _, err = c.waitReceiverStatus(ctx, true)
		if err != nil {
			return err
		}
	}
	if app.TransportID == "" {
		return fmt.Errorf("cast v2: Default Media Receiver returned no transport ID")
	}
	c.mu.Lock()
	c.app = app
	c.mu.Unlock()
	return c.send(app.TransportID, castV2NSConnection, map[string]any{"type": "CONNECT"}, false)
}

func (c *castV2Client) waitReceiverStatus(ctx context.Context, requireApp bool) (castV2Application, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, castV2ResponseTimeout)
	defer cancel()
	for {
		msg, err := c.nextMessage(ctx)
		if err != nil {
			return castV2Application{}, false, err
		}
		if msg.Namespace != castV2NSReceiver {
			continue
		}
		var status castV2ReceiverStatus
		if err := json.Unmarshal([]byte(msg.PayloadUTF8), &status); err != nil {
			continue
		}
		if status.Type == "LAUNCH_ERROR" {
			return castV2Application{}, false, fmt.Errorf("cast v2: receiver rejected Default Media Receiver launch")
		}
		if status.Type != "RECEIVER_STATUS" {
			continue
		}
		for _, app := range status.Status.Applications {
			if app.AppID == castV2DefaultReceiverApp {
				return app, true, nil
			}
		}
		// The first GET_STATUS legitimately reports no DMR. After LAUNCH,
		// ignore empty transitional statuses until the app appears.
		if !requireApp {
			return castV2Application{}, false, nil
		}
	}
}

func (c *castV2Client) load(ctx context.Context, track TrackInfo) (castV2MediaStatus, error) {
	metadataType := 3 // music track
	if track.MediaKind == "video" {
		metadataType = 0
	}
	media := map[string]any{
		"contentId":   track.URL,
		"contentType": track.ContentType,
		"streamType":  "BUFFERED",
		"duration":    float64(track.Duration),
		"metadata": map[string]any{
			"metadataType": metadataType,
			"title":        track.Title,
			"artist":       track.Artist,
			"albumName":    track.Album,
		},
	}
	if err := c.sendMedia(map[string]any{
		"type": "LOAD", "autoplay": true, "currentTime": float64(track.StartAt), "media": media,
	}, true); err != nil {
		return castV2MediaStatus{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, castV2ResponseTimeout)
	defer cancel()
	for {
		msg, err := c.nextMessage(waitCtx)
		if err != nil {
			return castV2MediaStatus{}, err
		}
		var header castV2Header
		if err := json.Unmarshal([]byte(msg.PayloadUTF8), &header); err != nil {
			continue
		}
		if header.Type == "LOAD_FAILED" || header.Type == "INVALID_REQUEST" {
			return castV2MediaStatus{}, fmt.Errorf("cast v2: receiver rejected media load (%s)", header.Type)
		}
		status, ok := decodeCastV2MediaStatus(msg)
		if !ok {
			continue
		}
		c.setMediaStatus(status)
		return status, nil
	}
}

func (c *castV2Client) nextMessage(ctx context.Context) (castV2Envelope, error) {
	select {
	case msg := <-c.messages:
		return msg, nil
	case <-c.done:
		return castV2Envelope{}, c.readFailure()
	case <-ctx.Done():
		return castV2Envelope{}, ctx.Err()
	}
}

func decodeCastV2MediaStatus(msg castV2Envelope) (castV2MediaStatus, bool) {
	if msg.Namespace != castV2NSMedia {
		return castV2MediaStatus{}, false
	}
	var payload castV2MediaStatusMessage
	if err := json.Unmarshal([]byte(msg.PayloadUTF8), &payload); err != nil || payload.Type != "MEDIA_STATUS" || len(payload.Status) == 0 {
		return castV2MediaStatus{}, false
	}
	return payload.Status[0], true
}

func (c *castV2Client) setMediaStatus(status castV2MediaStatus) {
	c.mu.Lock()
	c.media = status
	c.mu.Unlock()
}

func (c *castV2Client) sendMedia(payload map[string]any, request bool) error {
	c.mu.RLock()
	destination := c.app.TransportID
	c.mu.RUnlock()
	if destination == "" {
		return fmt.Errorf("cast v2: media receiver is not connected")
	}
	return c.send(destination, castV2NSMedia, payload, request)
}

func (c *castV2Client) mediaCommand(kind string, extra map[string]any) error {
	c.mu.RLock()
	mediaSessionID := c.media.MediaSessionID
	c.mu.RUnlock()
	if mediaSessionID == 0 {
		return fmt.Errorf("cast v2: no active media session")
	}
	payload := map[string]any{"type": kind, "mediaSessionId": mediaSessionID}
	for key, value := range extra {
		payload[key] = value
	}
	return c.sendMedia(payload, true)
}

func (c *castV2Client) setVolume(level int) error {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	return c.send(castV2ReceiverID, castV2NSReceiver, map[string]any{
		"type": "SET_VOLUME", "volume": map[string]any{"level": float64(level) / 100},
	}, true)
}

func (c *castV2Client) close(stopMedia bool) error {
	c.shutdownOnce.Do(func() {
		var first error
		c.mu.RLock()
		hasMedia := c.media.MediaSessionID != 0
		destination := c.app.TransportID
		c.mu.RUnlock()
		if stopMedia && hasMedia {
			if err := c.mediaCommand("STOP", nil); err != nil && !errors.Is(err, net.ErrClosed) {
				first = err
			}
		}
		if destination != "" {
			_ = c.send(destination, castV2NSConnection, map[string]any{"type": "CLOSE"}, false)
		}
		_ = c.send(castV2ReceiverID, castV2NSConnection, map[string]any{"type": "CLOSE"}, false)
		if err := c.conn.Close(); first == nil && err != nil {
			first = err
		}
		c.closeDone()
		c.shutdownErr = first
	})
	return c.shutdownErr
}
