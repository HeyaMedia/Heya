// Package sessions tracks active playback sessions in memory. Each player
// (one per user-per-device-per-file currently being watched) heartbeats
// every ~10s with its position and pause state; the store keeps the latest
// snapshot keyed by a client-minted session_id.
//
// The store is purely ephemeral — sessions disappear on server restart and
// after a heartbeat-timeout (handles ungraceful disconnects). Persistent
// watch progress lives elsewhere (user_watch_progress upserts).
//
// Why in-memory and not DB: the data is high-write / low-read (10s
// heartbeats per active session) and only useful while live. DB round-trips
// would dominate the cost for zero durability benefit.
package sessions

import (
	"context"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
)

// SessionTimeout is how long after a session's last heartbeat we consider
// it dead and purge it. 30s comfortably covers a 10s heartbeat plus a
// couple of network blips; longer would keep zombies around when a tab
// gets closed without a clean DELETE.
const SessionTimeout = 30 * time.Second

// Session is the live snapshot of one in-progress playback. All fields
// come from a mix of heartbeat-time client data and server-resolved
// metadata (title, transcode decision) so the client can't lie about
// what it's actually doing.
//
// MediaTitle + MediaSubtitle are the two display lines the activity
// panel uses. Per entity type:
//
//	movie:   title = movie title       subtitle = "" (or year)
//	episode: title = series title      subtitle = "S01E03 · Episode title"
//	track:   title = song title        subtitle = "Artist — Album"
//
// The server fills both fields from the entity_type+entity_id pair so
// the FE doesn't need to know how to format them per-type.
type Session struct {
	SessionID     string `json:"session_id"`
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	FileID        int64  `json:"file_id"`
	MediaItemID   int64  `json:"media_item_id"`
	MediaTitle    string `json:"media_title"`
	MediaSubtitle string `json:"media_subtitle,omitempty"`
	MediaType     string `json:"media_type"` // movie | tv | music | book
	EntityType    string `json:"entity_type,omitempty"`
	EntityID      int64  `json:"entity_id,omitempty"`

	// Per-type structured fields — useful for clients that want to
	// render their own layout (e.g. a dedicated activity page). The
	// activity panel reads MediaTitle + MediaSubtitle instead.
	SeasonNumber  int32  `json:"season_number,omitempty"`
	EpisodeNumber int32  `json:"episode_number,omitempty"`
	EpisodeTitle  string `json:"episode_title,omitempty"`
	ArtistName    string `json:"artist_name,omitempty"`
	AlbumTitle    string `json:"album_title,omitempty"`

	PositionSeconds int32 `json:"position_seconds"`
	TotalSeconds    int32 `json:"total_seconds"`
	Paused          bool  `json:"paused"`

	// Server-resolved transcode info (the FE just echoes back the
	// decision it received from /api/stream/{id}/info; we trust that
	// because the server told us about it in the first place — there's
	// no security gain in re-resolving on every heartbeat).
	PlaybackAction string `json:"playback_action,omitempty"` // direct_play | transcode
	VideoCodec     string `json:"video_codec,omitempty"`
	AudioCodec     string `json:"audio_codec,omitempty"`
	Container      string `json:"container,omitempty"`
	Width          int32  `json:"width,omitempty"`
	Height         int32  `json:"height,omitempty"`
	BitrateKbps    int32  `json:"bitrate_kbps,omitempty"`

	ClientUserAgent string    `json:"client_user_agent,omitempty"`
	ClientIP        string    `json:"client_ip,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
}

// Store is the concurrent map of active sessions. Use New to create one
// with the background purge goroutine running.
type Store struct {
	mu   sync.RWMutex
	hub  *eventhub.Hub
	data map[string]*Session
}

// New constructs a Store and kicks off a background purge goroutine that
// removes expired sessions every 10 seconds. The goroutine exits when
// ctx is cancelled — pass the app's lifetime context here.
func New(ctx context.Context, hub *eventhub.Hub) *Store {
	s := &Store{
		hub:  hub,
		data: make(map[string]*Session),
	}
	go s.purgeLoop(ctx)
	return s
}

// Upsert applies an incoming heartbeat. New sessions get StartedAt set;
// existing sessions update position/pause and bump LastHeartbeatAt.
// Returns the (possibly new) session pointer.
func (s *Store) Upsert(incoming Session) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if existing, ok := s.data[incoming.SessionID]; ok {
		// Preserve StartedAt as the original session start. Everything
		// else can update — the FE may have lacked the stream-info
		// response on the first beat (so transcode fields were empty),
		// and the user may navigate between episodes within one player
		// instance (so title/episode info refreshes).
		existing.PositionSeconds = incoming.PositionSeconds
		existing.TotalSeconds = incoming.TotalSeconds
		existing.Paused = incoming.Paused
		existing.LastHeartbeatAt = now
		// Title + per-type display fields — server resolves on every
		// beat, so always update.
		if incoming.MediaTitle != "" {
			existing.MediaTitle = incoming.MediaTitle
		}
		existing.MediaSubtitle = incoming.MediaSubtitle
		existing.MediaType = incoming.MediaType
		existing.EntityType = incoming.EntityType
		existing.EntityID = incoming.EntityID
		existing.SeasonNumber = incoming.SeasonNumber
		existing.EpisodeNumber = incoming.EpisodeNumber
		existing.EpisodeTitle = incoming.EpisodeTitle
		existing.ArtistName = incoming.ArtistName
		existing.AlbumTitle = incoming.AlbumTitle
		// Transcode info — late-fill OK.
		if incoming.PlaybackAction != "" {
			existing.PlaybackAction = incoming.PlaybackAction
			existing.VideoCodec = incoming.VideoCodec
			existing.AudioCodec = incoming.AudioCodec
			existing.Container = incoming.Container
			existing.Width = incoming.Width
			existing.Height = incoming.Height
			existing.BitrateKbps = incoming.BitrateKbps
		}
		s.broadcast()
		return existing
	}

	incoming.StartedAt = now
	incoming.LastHeartbeatAt = now
	s.data[incoming.SessionID] = &incoming
	s.broadcast()
	return &incoming
}

// End removes a session from the store. Idempotent — calling it on an
// already-purged session is a no-op.
func (s *Store) End(sessionID string) {
	s.mu.Lock()
	_, existed := s.data[sessionID]
	delete(s.data, sessionID)
	s.mu.Unlock()
	if existed {
		s.broadcast()
	}
}

// EndForUser tears down a session only when it belongs to userID, so a user
// can't end another user's live playback session by guessing its (client-chosen)
// id. Returns whether a session was actually removed.
func (s *Store) EndForUser(sessionID string, userID int64) bool {
	s.mu.Lock()
	sess, ok := s.data[sessionID]
	owned := ok && sess.UserID == userID
	if owned {
		delete(s.data, sessionID)
	}
	s.mu.Unlock()
	if owned {
		s.broadcast()
	}
	return owned
}

// List returns a snapshot of every active session. The slice is a copy;
// mutating it doesn't affect the store. Ordered by StartedAt ascending
// so the activity panel's row order is stable across renders.
func (s *Store) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0, len(s.data))
	for _, sess := range s.data {
		out = append(out, *sess)
	}
	// In-place sort by StartedAt — stable order keyed by start time.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].StartedAt.After(out[j].StartedAt); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ListForUser returns only the sessions belonging to a given user. Used
// when the caller isn't allowed to see other users' activity (non-admin
// path); admins call List instead.
func (s *Store) ListForUser(userID int64) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0)
	for _, sess := range s.data {
		if sess.UserID == userID {
			out = append(out, *sess)
		}
	}
	return out
}

// purgeLoop runs the background expiry sweep. Cheap: O(sessions) per
// tick, sessions ≪ 100 even on a large household, ticks every 10s.
func (s *Store) purgeLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purge()
		}
	}
}

func (s *Store) purge() {
	cutoff := time.Now().Add(-SessionTimeout)
	s.mu.Lock()
	changed := false
	for id, sess := range s.data {
		if sess.LastHeartbeatAt.Before(cutoff) {
			delete(s.data, id)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.broadcast()
	}
}

// broadcast emits a session.update event with the current full list.
// Called any time the set changes — upsert, end, purge. Keeps the FE
// activity panel honest without forcing a poll.
//
// broadcast is signal-only on purpose. The WebSocket fan-out is a single
// global stream with no per-recipient filtering, so putting the (cross-user)
// session list in the payload would leak every user's IP / user-agent /
// now-playing to every connected client. Instead we emit an empty "sessions
// changed" ping; clients re-fetch through the auth-scoped /api/sessions/active
// endpoint (own-only for non-admins, full list for admins).
func (s *Store) broadcast() {
	if s.hub == nil {
		return
	}
	s.hub.Emit(EventSessionUpdate, SessionUpdatePayload{})
}

// EventSessionUpdate is a change notification only — it carries no session data
// (see broadcast). SessionUpdatePayload is kept as an explicit empty type so the
// event's "no payload" contract is documented rather than implicit.
const EventSessionUpdate eventhub.EventType = "session.update"

type SessionUpdatePayload struct {
}
