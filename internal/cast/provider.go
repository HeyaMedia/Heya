package cast

import (
	"context"
	"time"
)

// Device is a discovered remote playback target. ID is stable across
// re-discovery (provider-prefixed hardware identifier) so sessions and
// FE pickers can key on it.
type Device struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Name         string    `json:"name"`
	Model        string    `json:"model,omitempty"`
	Manufacturer string    `json:"manufacturer,omitempty"`
	Host         string    `json:"host"`
	Addr         string    `json:"addr"`
	Port         int       `json:"port"`
	LastSeen     time.Time `json:"last_seen"`

	// TXT is the raw mDNS TXT record, verbatim. AirPlay senders must
	// replay it untouched (cliap2 rejects devices whose TXT lacks
	// deviceid=), and vendor providers match on manufacturer=/model=.
	TXT []string `json:"-"`
}

// TrackInfo is everything a transport needs to play one track: the
// source file for the PCM feeder plus the display metadata pushed to
// the device once streaming starts. StartAt implements seek — a seek is
// "the same track from a new offset".
type TrackInfo struct {
	TrackID  int64
	Path     string
	Title    string
	Artist   string
	Album    string
	Duration int // seconds
	StartAt  int // seconds
}

type SessionState string

const (
	StateStarting SessionState = "starting" // process spawning / RTSP handshake
	StatePlaying  SessionState = "playing"
	StatePaused   SessionState = "paused"
	StateStopped  SessionState = "stopped"
	StateFailed   SessionState = "failed"
)

// Provider is one casting protocol (airplay now; yamaha/sony/nad/cast-v2
// later). Implementations live in one file each: airplay.go, yamaha.go, …
type Provider interface {
	Name() string

	// Browse discovers devices until ctx is done, invoking found for
	// every advertisement (repeats update LastSeen). Implementations own
	// their re-browse cadence.
	Browse(ctx context.Context, found func(Device)) error

	// NewTransport returns an unstarted transport bound to the device.
	NewTransport(dev Device) (Transport, error)
}

// Transport is one live protocol connection playing one PCM stream to
// one device. Session-level concerns (queue, seek-by-replacement,
// scrobbles, WS fan-out) live above in Session; a Transport only knows
// "play this track from this offset, and tell me what happens".
type Transport interface {
	// Start spawns the sender and begins playback. Blocks only for
	// spawn-time validation; connection progress arrives via Events.
	Start(ctx context.Context, track TrackInfo, volume int) error
	Pause() error
	Resume() error
	SetVolume(level int) error
	// Stop tears down gracefully (the device must see a proper session
	// end — dirty kills leave receivers with ghost state).
	Stop() error
	Events() <-chan TransportEvent
}

type TransportEventKind string

const (
	// TransportConnected: session established with the device (AirPlay:
	// device_activate_cb status 2). Not yet audible.
	TransportConnected TransportEventKind = "connected"
	// TransportPlaying: audio is actually streaming (AirPlay:
	// event_play_start). This is the only trustworthy "it plays" signal.
	TransportPlaying TransportEventKind = "playing"
	TransportPaused  TransportEventKind = "paused"
	TransportResumed TransportEventKind = "resumed"
	// TransportEnded: the track finished naturally (feeder EOF drained).
	TransportEnded TransportEventKind = "ended"
	// TransportFailed: unrecoverable for this transport instance; the
	// session decides whether to retry with a fresh transport.
	TransportFailed TransportEventKind = "failed"
)

type TransportEvent struct {
	Kind TransportEventKind
	Err  error // set for TransportFailed
}
