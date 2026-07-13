package cast

import (
	"context"
	"time"
)

// Device is a discovered remote playback target. ID is stable across
// re-discovery (provider-prefixed hardware identifier) so sessions and
// FE pickers can key on it.
type Device struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Capabilities []string `json:"capabilities,omitempty"`
	Name         string   `json:"name"`
	Model        string   `json:"model,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Host         string   `json:"host"`
	Addr         string   `json:"addr"`
	Port         int      `json:"port"`
	// MediaOrigin is Heya's receiver-facing HTTP(S) origin selected for this
	// device. URL-pull receivers must be able to route back to it. It is filled
	// on API snapshots rather than persisted in discovery state.
	MediaOrigin string    `json:"media_origin,omitempty"`
	LastSeen    time.Time `json:"last_seen"`

	// TXT is the raw mDNS TXT record, verbatim. AirPlay senders must
	// replay it untouched (cliap2 rejects devices whose TXT lacks
	// deviceid=), and vendor providers match on manufacturer=/model=.
	TXT []string `json:"-"`
}

// TrackInfo is the provider-neutral description of one playable item. Push
// providers such as AirPlay decode Path locally; pull providers such as
// Chromecast consume URL. PullPath and PullQuery are internal inputs used by
// Manager to mint a user-scoped URL after the receiver is selected.
type TrackInfo struct {
	TrackID     int64
	FileID      string
	MediaItemID int64
	EntityType  string
	EntityID    int64
	Path        string
	URL         string
	PullPath    string
	// PullScopePath optionally broadens the signed receiver URL from the
	// exact PullPath to one resource subtree. Video HLS needs this because a
	// receiver first opens master.m3u8 and then follows variant/segment URLs.
	// The token validator still requires a path-boundary match, so it cannot
	// escape into another file or another cast media namespace.
	PullScopePath string
	PullQuery     string
	MediaKind     string
	ContentType   string
	Title         string
	Artist        string
	Album         string
	Duration      int // seconds
	StartAt       int // seconds
	StartPaused   bool
	// Video selections are session state, not initiating-browser state. They
	// are mirrored through SessionSnapshot so any of the user's clients can
	// present and change the same remote controls.
	AudioTrack int
	Quality    string
	TextTrack  *TextTrackInfo
}

// TextTrackInfo describes one out-of-band subtitle selected for a Cast LOAD.
// PullPath is signed alongside the video's HLS/direct subtree; Manager.Play
// fills URL with that same short-lived token before the transport starts.
type TextTrackInfo struct {
	SelectionIndex int
	StreamIndex    int
	TrackID        int
	Name           string
	Language       string
	PullPath       string
	URL            string
}

// NativeSeekTransport marks URL-pull transports that keep a receiver session
// alive across pause/resume/seek. AirPlay intentionally does not implement it:
// its reliable control path remains stop-and-respawn at the frozen position.
type NativeSeekTransport interface {
	Transport
	Seek(seconds int) error
}

type SessionState string

const (
	StateStarting SessionState = "starting" // process spawning / RTSP handshake
	StatePlaying  SessionState = "playing"
	StatePaused   SessionState = "paused"
	StateStopped  SessionState = "stopped"
	StateFailed   SessionState = "failed"
)

// Provider is one casting protocol. Implementations live in focused files:
// AirPlay and Google Cast today; DLNA/Yamaha/WiiM follow the same contract.
type Provider interface {
	Name() string

	// Browse discovers devices until ctx is done, invoking found for
	// every advertisement (repeats update LastSeen). Implementations own
	// their re-browse cadence.
	Browse(ctx context.Context, found func(Device)) error

	// NewTransport returns an unstarted transport bound to the device.
	NewTransport(dev Device) (Transport, error)
}

// Transport is one live protocol connection playing one media item on one
// device. Session-level concerns (queue, seek-by-replacement,
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
