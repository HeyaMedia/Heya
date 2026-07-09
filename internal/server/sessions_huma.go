package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sessions"
)

// registerSessionRoutes mounts the live-playback presence surface:
//
//   - POST   /api/me/sessions/heartbeat — client-driven, fires every 10s while
//     the player is mounted. Position + paused + transcode info land here.
//   - DELETE /api/me/sessions/{session_id} — explicit teardown on player
//     unmount. The 30s purge sweep is the safety net; this is the polite
//     close.
//   - GET    /api/sessions/active — the activity-panel feed. Returns every
//     active session for now (single-tenant home use); restrict to
//     admin-only later if a multi-tenant story emerges.
//
// The store itself is in-memory (internal/sessions/store.go) — see there
// for the persistence and purge semantics.
func registerSessionRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodPost, "/api/me/sessions/heartbeat", "session-heartbeat", "Heartbeat a live playback session", "Sessions")),
		func(ctx context.Context, in *struct {
			Body sessionHeartbeatInput
		}) (*JSONOutput[okBody], error) {
			user := userFrom(ctx)
			body := in.Body
			if body.SessionID == "" {
				return nil, huma.Error400BadRequest("session_id is required")
			}
			fileID := int64(0)
			if body.FileID != "" {
				var ok bool
				fileID, ok = app.ResolveLibraryFileID(ctx, body.FileID)
				if !ok {
					return nil, huma.Error404NotFound("file not found")
				}
			}

			// Resolve display title from the media item — the client could
			// send it but we'd rather not trust client-rendered names for
			// the activity panel (would let one tab name another's session).
			// Per entity_type, the resolver picks the right table and
			// formats title + subtitle for the activity panel.
			disp := resolveSessionDisplay(ctx, app, body.EntityType, body.EntityID, body.MediaItemID)

			sess := sessions.Session{
				SessionID:       body.SessionID,
				UserID:          user.ID,
				Username:        user.Username,
				FileID:          fileID,
				MediaItemID:     body.MediaItemID,
				MediaTitle:      disp.Title,
				MediaSubtitle:   disp.Subtitle,
				MediaType:       disp.MediaType,
				EntityType:      body.EntityType,
				EntityID:        body.EntityID,
				SeasonNumber:    disp.SeasonNumber,
				EpisodeNumber:   disp.EpisodeNumber,
				EpisodeTitle:    disp.EpisodeTitle,
				ArtistName:      disp.ArtistName,
				AlbumTitle:      disp.AlbumTitle,
				PositionSeconds: body.PositionSeconds,
				TotalSeconds:    body.TotalSeconds,
				Paused:          body.Paused,
				PlaybackAction:  body.PlaybackAction,
				VideoCodec:      body.VideoCodec,
				AudioCodec:      body.AudioCodec,
				Container:       body.Container,
				Width:           body.Width,
				Height:          body.Height,
				BitrateKbps:     body.BitrateKbps,
				ClientUserAgent: body.ClientUserAgent,
				ClientIP:        body.ClientIP,
			}
			app.Sessions().Upsert(sess)
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/sessions/{session_id}", "end-session", "Tear down a live playback session", "Sessions")),
		func(ctx context.Context, in *struct {
			SessionID string `path:"session_id" maxLength:"128"`
		}) (*JSONOutput[okBody], error) {
			// Only end a session the caller owns — the id is client-chosen, so
			// without this any user could end another's playback presence.
			app.Sessions().EndForUser(in.SessionID, userFrom(ctx).ID)
			return noStoreJSON(okBody{Ok: true}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/sessions/active", "list-active-sessions", "Active playback sessions (own; all for admins)", "Sessions")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[activeSessionsBody], error) {
			// A non-admin sees only their own sessions; the full cross-user view
			// (other users' IP / user-agent / what they're watching) is admin-only.
			u := userFrom(ctx)
			items := app.Sessions().ListForUser(u.ID)
			if u.IsAdmin {
				items = app.Sessions().List()
			}
			return noStoreJSON(activeSessionsBody{Items: items}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/sessions/{session_id}/command", "session-command", "Send a control command (stop / message) to a live session", "Sessions")),
		func(ctx context.Context, in *struct {
			SessionID string `path:"session_id" maxLength:"128"`
			Body      sessionCommandInput
		}) (*JSONOutput[okBody], error) {
			u := userFrom(ctx)
			sess, ok := app.Sessions().Get(in.SessionID)
			if !ok {
				return nil, huma.Error404NotFound("session not found or already ended")
			}
			// Owner or admin only: a user can stop/message their own playback;
			// admins can control anyone's. Without this, the client-chosen id
			// would let one user command another's session.
			if sess.UserID != u.ID && !u.IsAdmin {
				return nil, huma.Error403Forbidden("not your session")
			}

			action := in.Body.Action
			if action != "stop" && action != "message" {
				return nil, huma.Error400BadRequest("action must be 'stop' or 'message'")
			}
			msg := strings.TrimSpace(in.Body.Message)
			if action == "message" && msg == "" {
				return nil, huma.Error400BadRequest("message is required for the message action")
			}

			app.Sessions().SendCommand(sessions.CommandPayload{
				SessionID: sess.SessionID,
				UserID:    sess.UserID,
				Action:    action,
				Message:   msg,
				By:        u.Username,
			})
			return noStoreJSON(okBody{Ok: true}), nil
		})
}

// sessionCommandInput is the wire shape for POST /api/sessions/{id}/command.
type sessionCommandInput struct {
	Action  string `json:"action" enum:"stop,message" maxLength:"16"`
	Message string `json:"message,omitempty" maxLength:"280"`
}

// sessionDisplay is what the activity panel renders. The server fills
// it per heartbeat from entity_type+entity_id so the FE never has to
// format type-specific strings ("S01E03 · Episode title" etc.).
type sessionDisplay struct {
	Title         string
	Subtitle      string
	MediaType     string
	SeasonNumber  int32
	EpisodeNumber int32
	EpisodeTitle  string
	ArtistName    string
	AlbumTitle    string
}

// resolveSessionDisplay picks the right query path based on entity_type:
//
//	"episode" → tv_episodes → tv_seasons → series media_item
//	"track"   → tracks → albums → artists
//	anything else (incl. "movie", "" default) → media_items by id
//
// Returns a zero-value struct on any lookup error — the panel renders
// "Unknown" rather than a stale FE-supplied string.
func resolveSessionDisplay(ctx context.Context, app *service.App, entityType string, entityID, mediaItemID int64) sessionDisplay {
	q := sqlc.New(app.DBPool())

	switch entityType {
	case "episode":
		if entityID == 0 {
			break
		}
		ep, err := q.GetTVEpisodeByID(ctx, entityID)
		if err != nil {
			break
		}
		// Resolve up the chain to grab the series title.
		seriesTitle := ""
		if season, err := q.GetTVSeasonByID(ctx, ep.SeasonID); err == nil {
			if series, err := q.GetTVSeriesByID(ctx, season.SeriesID); err == nil {
				if mi, err := q.GetMediaItemByID(ctx, series.MediaItemID); err == nil {
					seriesTitle = mi.Title
				}
			}
			// Build the S01E03 · Episode title subtitle.
			subtitle := fmt.Sprintf("S%02dE%02d", season.SeasonNumber, ep.EpisodeNumber)
			if ep.Title != "" {
				subtitle = fmt.Sprintf("%s · %s", subtitle, ep.Title)
			}
			return sessionDisplay{
				Title:         seriesTitle,
				Subtitle:      subtitle,
				MediaType:     "tv",
				SeasonNumber:  season.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber,
				EpisodeTitle:  ep.Title,
			}
		}

	case "track":
		if entityID == 0 {
			break
		}
		t, err := q.GetTrackByID(ctx, entityID)
		if err != nil {
			break
		}
		artistName, albumTitle := "", ""
		if album, err := q.GetAlbumByID(ctx, t.AlbumID); err == nil {
			albumTitle = album.Title
			if artist, err := q.GetArtistByID(ctx, album.ArtistID); err == nil {
				artistName = artist.Name
			}
		}
		subtitle := artistName
		if albumTitle != "" {
			if subtitle != "" {
				subtitle = fmt.Sprintf("%s — %s", subtitle, albumTitle)
			} else {
				subtitle = albumTitle
			}
		}
		return sessionDisplay{
			Title:      t.Title,
			Subtitle:   subtitle,
			MediaType:  "music",
			ArtistName: artistName,
			AlbumTitle: albumTitle,
		}
	}

	// Fall-through: movie / book / unknown — title from media_items, no subtitle.
	if mediaItemID == 0 {
		return sessionDisplay{}
	}
	mi, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return sessionDisplay{}
	}
	return sessionDisplay{
		Title:     mi.Title,
		MediaType: string(mi.MediaType),
	}
}

// sessionHeartbeatInput is the wire shape — kept in this file (not in
// the sessions package) because it's an API-binding concern. Fields the
// FE sends about the playback decision (codec, resolution, bitrate) are
// echoed back from /api/stream/{file_id}/info, so there's no security
// gain in re-deriving them server-side.
type sessionHeartbeatInput struct {
	SessionID string `json:"session_id" minLength:"1" maxLength:"128"`

	FileID      string `json:"file_id,omitempty" maxLength:"64"`
	MediaItemID int64  `json:"media_item_id" minimum:"0"`
	EntityType  string `json:"entity_type,omitempty" maxLength:"32"`
	EntityID    int64  `json:"entity_id,omitempty" minimum:"0"`

	PositionSeconds int32 `json:"position_seconds" minimum:"0"`
	TotalSeconds    int32 `json:"total_seconds" minimum:"0"`
	Paused          bool  `json:"paused"`

	PlaybackAction string `json:"playback_action,omitempty" maxLength:"32"`
	VideoCodec     string `json:"video_codec,omitempty" maxLength:"32"`
	AudioCodec     string `json:"audio_codec,omitempty" maxLength:"32"`
	Container      string `json:"container,omitempty" maxLength:"16"`
	Width          int32  `json:"width,omitempty" minimum:"0"`
	Height         int32  `json:"height,omitempty" minimum:"0"`
	BitrateKbps    int32  `json:"bitrate_kbps,omitempty" minimum:"0"`

	ClientUserAgent string `json:"client_user_agent,omitempty" maxLength:"512"`
	ClientIP        string `json:"client_ip,omitempty" maxLength:"64"`
}

type activeSessionsBody struct {
	Items []sessions.Session `json:"items"`
}
