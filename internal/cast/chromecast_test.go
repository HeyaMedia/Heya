package cast

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

func TestChromecastDeviceFromEntry(t *testing.T) {
	entry := &zeroconf.ServiceEntry{
		ServiceRecord: zeroconf.ServiceRecord{Instance: "Fallback name"},
		HostName:      "living-room.local.",
		Port:          8009,
		AddrIPv4:      []net.IP{net.ParseIP("192.168.20.50")},
		Text:          []string{"id=ABCDEF", "fn=Living Room", "md=Chromecast Ultra", "ca=5"},
	}
	dev, ok := chromecastDeviceFromEntry(entry)
	if !ok {
		t.Fatal("valid Google Cast entry was rejected")
	}
	if dev.ID != "chromecast:abcdef" || dev.Name != "Living Room" || dev.Model != "Chromecast Ultra" || dev.Port != 8009 {
		t.Fatalf("device = %#v", dev)
	}
	if len(dev.Capabilities) != 2 || dev.Capabilities[0] != "audio" || dev.Capabilities[1] != "video" {
		t.Fatalf("capabilities = %v, want audio+video", dev.Capabilities)
	}
}

func TestChromecastAudioOnlyCapability(t *testing.T) {
	entry := &zeroconf.ServiceEntry{
		ServiceRecord: zeroconf.ServiceRecord{Instance: "JBL Speaker"},
		Port:          8009,
		AddrIPv4:      []net.IP{net.ParseIP("192.168.20.51")},
		Text:          []string{"id=SPEAKER", "fn=JBL Speaker", "md=JBL", "ca=4"},
	}
	dev, ok := chromecastDeviceFromEntry(entry)
	if !ok {
		t.Fatal("valid audio-only Cast device was rejected")
	}
	if len(dev.Capabilities) != 1 || dev.Capabilities[0] != "audio" {
		t.Fatalf("capabilities = %v, want audio only", dev.Capabilities)
	}
}

func TestChromecastTransportAgainstFakeReceiver(t *testing.T) {
	receiver := newFakeCastV2Receiver(t)
	host, portText, err := net.SplitHostPort(receiver.addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	transport := &chromecastTransport{
		dev:    Device{Name: "Fake Cast", Addr: host, Port: port},
		events: make(chan TransportEvent, 16),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := transport.Start(ctx, TrackInfo{
		URL:         "http://192.168.20.10:8080/api/cast/media/video/file-a?cast_token=test",
		ContentType: "video/mp4",
		MediaKind:   "video",
		Title:       "Protocol test",
		Duration:    180,
		TextTrack: &TextTrackInfo{
			TrackID: 1, Name: "English", Language: "en",
			URL: "http://192.168.20.10:8080/api/cast/media/video/file-a/subtitles/7?cast_token=test",
		},
	}, 23); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTransportEvent(t, transport.Events(), TransportConnected)
	waitTransportEvent(t, transport.Events(), TransportPlaying)
	if err := transport.Pause(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	waitTransportEvent(t, transport.Events(), TransportPaused)
	if err := transport.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitTransportEvent(t, transport.Events(), TransportPlaying)
	if err := transport.Seek(65); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if err := transport.SetVolume(31); err != nil {
		t.Fatalf("volume: %v", err)
	}
	if err := transport.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	receiver.waitFor(t, "LAUNCH", "LOAD", "PAUSE", "PLAY", "SEEK", "SET_VOLUME", "STOP", "PONG")
	load := receiver.payloadFor(t, "LOAD")
	active, ok := load["activeTrackIds"].([]any)
	if !ok || len(active) != 1 || active[0] != float64(1) {
		t.Fatalf("LOAD activeTrackIds = %#v", load["activeTrackIds"])
	}
	media, ok := load["media"].(map[string]any)
	if !ok {
		t.Fatalf("LOAD media = %#v", load["media"])
	}
	tracks, ok := media["tracks"].([]any)
	if !ok || len(tracks) != 1 {
		t.Fatalf("LOAD tracks = %#v", media["tracks"])
	}
	text, ok := tracks[0].(map[string]any)
	if !ok || text["type"] != "TEXT" || text["trackContentType"] != "text/vtt" || text["language"] != "en" {
		t.Fatalf("LOAD text track = %#v", tracks[0])
	}
}

func waitTransportEvent(t *testing.T, events <-chan TransportEvent, want TransportEventKind) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("events closed before %s", want)
			}
			if event.Kind == TransportFailed {
				t.Fatalf("transport failed: %v", event.Err)
			}
			if event.Kind == want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

type fakeCastV2Receiver struct {
	addr string
	ln   net.Listener

	mu       sync.Mutex
	commands []string
	payloads []map[string]any
	done     chan struct{}
}

func newFakeCastV2Receiver(t *testing.T) *fakeCastV2Receiver {
	t.Helper()
	seed := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	tlsConfig := &tls.Config{Certificates: seed.TLS.Certificates}
	seed.Close()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeCastV2Receiver{addr: ln.Addr().String(), ln: ln, done: make(chan struct{})}
	go r.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-r.done:
		case <-time.After(time.Second):
		}
	})
	return r
}

func (r *fakeCastV2Receiver) serve() {
	defer close(r.done)
	conn, err := r.ln.Accept()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	launched := false
	for {
		msg, err := readCastV2Frame(conn)
		if err != nil {
			return
		}
		var payload map[string]any
		if json.Unmarshal([]byte(msg.PayloadUTF8), &payload) != nil {
			continue
		}
		kind, _ := payload["type"].(string)
		r.mu.Lock()
		r.commands = append(r.commands, kind)
		r.payloads = append(r.payloads, payload)
		r.mu.Unlock()
		switch kind {
		case "GET_STATUS":
			if msg.Namespace == castV2NSReceiver {
				apps := []any{}
				if launched {
					apps = append(apps, map[string]any{"appId": castV2DefaultReceiverApp, "sessionId": "session-1", "transportId": "transport-1"})
				}
				r.send(conn, castV2NSReceiver, castV2ReceiverID, map[string]any{"type": "RECEIVER_STATUS", "requestId": payload["requestId"], "status": map[string]any{"applications": apps}})
			} else {
				r.sendMediaStatus(conn, "PLAYING", "")
			}
		case "LAUNCH":
			launched = true
			r.send(conn, castV2NSReceiver, castV2ReceiverID, map[string]any{"type": "RECEIVER_STATUS", "requestId": payload["requestId"], "status": map[string]any{"applications": []any{map[string]any{"appId": castV2DefaultReceiverApp, "sessionId": "session-1", "transportId": "transport-1"}}}})
		case "LOAD":
			r.sendMediaStatus(conn, "BUFFERING", "")
			r.send(conn, castV2NSHeartbeat, castV2ReceiverID, map[string]any{"type": "PING"})
			r.sendMediaStatus(conn, "PLAYING", "")
		case "PAUSE":
			r.sendMediaStatus(conn, "PAUSED", "")
		case "PLAY", "SEEK":
			r.sendMediaStatus(conn, "PLAYING", "")
		case "STOP":
			r.sendMediaStatus(conn, "IDLE", "CANCELLED")
		case "CLOSE":
			if msg.DestinationID == castV2ReceiverID {
				return
			}
		}
	}
}

func (r *fakeCastV2Receiver) payloadFor(t *testing.T, kind string) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, command := range r.commands {
		if command == kind {
			return r.payloads[i]
		}
	}
	t.Fatalf("receiver never saw %s", kind)
	return nil
}

func (r *fakeCastV2Receiver) sendMediaStatus(conn net.Conn, state, idleReason string) {
	status := map[string]any{"mediaSessionId": 7, "playerState": state, "currentTime": 12}
	if idleReason != "" {
		status["idleReason"] = idleReason
	}
	r.send(conn, castV2NSMedia, "transport-1", map[string]any{"type": "MEDIA_STATUS", "status": []any{status}})
}

func (r *fakeCastV2Receiver) send(conn net.Conn, namespace, source string, payload map[string]any) {
	data, _ := json.Marshal(payload)
	_ = writeCastV2Frame(conn, castV2Envelope{SourceID: source, DestinationID: castV2SenderID, Namespace: namespace, PayloadUTF8: string(data)})
}

func (r *fakeCastV2Receiver) waitFor(t *testing.T, kinds ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		seen := make(map[string]bool, len(r.commands))
		for _, kind := range r.commands {
			seen[kind] = true
		}
		r.mu.Unlock()
		all := true
		for _, kind := range kinds {
			all = all && seen[kind]
		}
		if all {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("receiver commands = %v, want %v", r.commands, kinds)
}
