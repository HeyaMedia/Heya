package cast

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/zeroconf/v2"
	"github.com/rs/zerolog/log"
)

const googleCastServiceType = "_googlecast._tcp"

type chromecastProvider struct{}

func (p *chromecastProvider) Name() string { return "chromecast" }

func (p *chromecastProvider) Browse(ctx context.Context, found func(Device)) error {
	for {
		if err := p.browseOnce(ctx, found); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(browseIdle):
		}
	}
}

func (p *chromecastProvider) browseOnce(ctx context.Context, found func(Device)) error {
	winCtx, cancel := context.WithTimeout(ctx, browseWindow)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry, 8)
	go func() {
		for entry := range entries {
			if dev, ok := chromecastDeviceFromEntry(entry); ok {
				found(dev)
			}
		}
	}()
	return zeroconf.Browse(winCtx, googleCastServiceType, mdnsDomain, entries)
}

func chromecastDeviceFromEntry(entry *zeroconf.ServiceEntry) (Device, bool) {
	if entry == nil || len(entry.AddrIPv4) == 0 || entry.Port == 0 {
		return Device{}, false
	}
	id := strings.ToLower(strings.TrimSpace(txtValue(entry.Text, "id")))
	if id == "" {
		return Device{}, false
	}
	name := strings.TrimSpace(txtValue(entry.Text, "fn"))
	if name == "" {
		name = dnsUnescape(entry.Instance)
	}
	// CastV2's `ca` TXT field is a capability bitset: video output = 1,
	// audio output = 4. A Chromecast transport may be a speaker, display, or
	// group, so provider name alone says nothing about video support. Missing
	// or malformed capability data stays conservative: audio works on every
	// target Heya currently discovers, but video is only exposed explicitly.
	capabilities := []string{"audio"}
	if raw := strings.TrimSpace(txtValue(entry.Text, "ca")); raw != "" {
		if bits, err := strconv.ParseUint(raw, 10, 64); err == nil {
			capabilities = capabilities[:0]
			if bits&4 != 0 || bits&1 != 0 {
				capabilities = append(capabilities, "audio")
			}
			if bits&1 != 0 {
				capabilities = append(capabilities, "video")
			}
			if len(capabilities) == 0 {
				capabilities = append(capabilities, "audio")
			}
		}
	}
	return Device{
		ID:           "chromecast:" + id,
		Provider:     "chromecast",
		Capabilities: capabilities,
		Name:         name,
		Model:        txtValue(entry.Text, "md"),
		Manufacturer: "Google",
		Host:         entry.HostName,
		Addr:         entry.AddrIPv4[0].String(),
		Port:         entry.Port,
		LastSeen:     time.Now(),
		TXT:          entry.Text,
	}, true
}

func (p *chromecastProvider) NewTransport(dev Device) (Transport, error) {
	if dev.Addr == "" || dev.Port <= 0 {
		return nil, fmt.Errorf("chromecast: invalid receiver address")
	}
	return &chromecastTransport{dev: dev, events: make(chan TransportEvent, 16)}, nil
}

type chromecastTransport struct {
	dev    Device
	events chan TransportEvent

	mu       sync.Mutex
	client   *castV2Client
	cancel   context.CancelFunc
	started  bool
	stopping bool

	closeEvents sync.Once
	stopOnce    sync.Once
}

func (t *chromecastTransport) Start(ctx context.Context, track TrackInfo, volume int) error {
	if track.URL == "" || track.ContentType == "" {
		return fmt.Errorf("chromecast: media URL and content type are required")
	}
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("chromecast: transport already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	t.started = true
	t.cancel = cancel
	t.mu.Unlock()
	go t.run(runCtx, track, volume)
	return nil
}

func (t *chromecastTransport) run(ctx context.Context, track TrackInfo, volume int) {
	defer t.closeEvents.Do(func() { close(t.events) })
	client, err := dialCastV2(ctx, t.dev.Addr, t.dev.Port)
	if err != nil {
		t.failUnlessStopping(err)
		return
	}
	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		_ = client.close(false)
		return
	}
	t.client = client
	t.mu.Unlock()
	defer func() { _ = client.close(false) }()

	if err := client.connect(ctx); err != nil {
		t.failUnlessStopping(err)
		return
	}
	t.emit(TransportEvent{Kind: TransportConnected})
	if err := client.setVolume(volume); err != nil {
		log.Debug().Err(err).Str("device", t.dev.Name).Msg("chromecast: initial volume command failed")
	}
	status, err := client.load(ctx, track)
	if err != nil {
		t.failUnlessStopping(err)
		return
	}
	if done, err := t.applyStatus(status); done {
		if err != nil {
			t.failUnlessStopping(err)
		}
		return
	}
	if err := t.monitor(ctx, client); err != nil {
		t.failUnlessStopping(err)
	}
}

func (t *chromecastTransport) monitor(ctx context.Context, client *castV2Client) error {
	poll := time.NewTicker(castV2StatusPollInterval)
	defer poll.Stop()
	for {
		select {
		case msg := <-client.messages:
			status, ok := decodeCastV2MediaStatus(msg)
			if !ok {
				continue
			}
			client.setMediaStatus(status)
			if done, err := t.applyStatus(status); done {
				return err
			}
		case <-poll.C:
			if err := client.sendMedia(map[string]any{"type": "GET_STATUS"}, true); err != nil {
				return err
			}
		case <-client.done:
			return client.readFailure()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *chromecastTransport) applyStatus(status castV2MediaStatus) (bool, error) {
	switch status.PlayerState {
	case "PLAYING":
		t.emit(TransportEvent{Kind: TransportPlaying})
	case "PAUSED":
		t.emit(TransportEvent{Kind: TransportPaused})
	case "IDLE":
		switch status.IdleReason {
		case "FINISHED":
			t.emit(TransportEvent{Kind: TransportEnded})
			return true, nil
		case "ERROR":
			return true, fmt.Errorf("chromecast: receiver reported media error")
		case "CANCELLED", "INTERRUPTED":
			return true, fmt.Errorf("chromecast: playback was %s by another controller", strings.ToLower(status.IdleReason))
		}
	}
	return false, nil
}

func (t *chromecastTransport) failUnlessStopping(err error) {
	t.mu.Lock()
	stopping := t.stopping
	t.mu.Unlock()
	if stopping || err == context.Canceled {
		return
	}
	t.emit(TransportEvent{Kind: TransportFailed, Err: err})
}

func (t *chromecastTransport) emit(event TransportEvent) {
	t.mu.Lock()
	stopping := t.stopping
	t.mu.Unlock()
	if stopping && event.Kind != TransportFailed {
		return
	}
	select {
	case t.events <- event:
	default:
		log.Warn().Str("device", t.dev.Name).Str("event", string(event.Kind)).Msg("chromecast: dropping full event buffer")
	}
}

func (t *chromecastTransport) withClient(do func(*castV2Client) error) error {
	t.mu.Lock()
	client, stopping := t.client, t.stopping
	t.mu.Unlock()
	if client == nil || stopping {
		return fmt.Errorf("chromecast: receiver is not connected")
	}
	return do(client)
}

func (t *chromecastTransport) Pause() error {
	return t.withClient(func(client *castV2Client) error { return client.mediaCommand("PAUSE", nil) })
}

func (t *chromecastTransport) Resume() error {
	return t.withClient(func(client *castV2Client) error { return client.mediaCommand("PLAY", nil) })
}

func (t *chromecastTransport) Seek(seconds int) error {
	return t.withClient(func(client *castV2Client) error {
		return client.mediaCommand("SEEK", map[string]any{"currentTime": float64(seconds), "resumeState": "PLAYBACK_UNCHANGED"})
	})
}

func (t *chromecastTransport) SetVolume(level int) error {
	return t.withClient(func(client *castV2Client) error { return client.setVolume(level) })
}

func (t *chromecastTransport) Stop() error {
	var result error
	t.stopOnce.Do(func() {
		t.mu.Lock()
		t.stopping = true
		client, cancel := t.client, t.cancel
		t.mu.Unlock()
		if client != nil {
			result = client.close(true)
		}
		if cancel != nil {
			cancel()
		}
	})
	return result
}

func (t *chromecastTransport) Events() <-chan TransportEvent { return t.events }

var _ NativeSeekTransport = (*chromecastTransport)(nil)
