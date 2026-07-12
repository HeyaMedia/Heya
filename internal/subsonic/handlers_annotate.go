package subsonic

import (
	"net/http"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sessions"
)

// Media annotation, users, play queue, scanning.

// handleStar / unstar — maps onto Heya's loved state, the same
// user_favorites rows the web player's hearts read. Accepts the spec's
// three id-param spellings: id (typed), albumId, artistId (all repeatable).
func (s *Server) handleStar(loved bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := userFrom(r.Context())
		ctx := r.Context()

		type target struct {
			entity string
			id     int64
		}
		var targets []target
		for _, raw := range paramAll(r, "id") {
			kind, id, err := DecodeID(raw)
			if err != nil {
				respondError(w, r, errNotFound, "unknown id: "+raw)
				return
			}
			switch kind {
			case KindTrack:
				targets = append(targets, target{"track", id})
			case KindAlbum:
				targets = append(targets, target{"album", id})
			case KindArtist:
				targets = append(targets, target{"artist", id})
			default:
				respondError(w, r, errNotFound, "unknown id: "+raw)
				return
			}
		}
		for _, raw := range paramAll(r, "albumId") {
			id, err := DecodeIDKind(raw, KindAlbum)
			if err != nil {
				respondError(w, r, errNotFound, "unknown albumId: "+raw)
				return
			}
			targets = append(targets, target{"album", id})
		}
		for _, raw := range paramAll(r, "artistId") {
			id, err := DecodeIDKind(raw, KindArtist)
			if err != nil {
				respondError(w, r, errNotFound, "unknown artistId: "+raw)
				return
			}
			targets = append(targets, target{"artist", id})
		}
		if len(targets) == 0 {
			respondError(w, r, errMissingParameter, "no id given")
			return
		}
		// Stars ARE hearts: write the unified rating store (heart = 10,
		// unstar clears) so Subsonic clients feed the same taste signal the
		// web app's reactions do.
		rating := int16(0)
		if loved {
			rating = 10
		}
		for _, t := range targets {
			var err error
			switch t.entity {
			case "track":
				err = s.app.SetUserTrackRating(ctx, u.ID, t.id, rating)
			case "album":
				err = s.app.SetUserAlbumRating(ctx, u.ID, t.id, rating)
			case "artist":
				err = s.app.SetUserArtistRating(ctx, u.ID, t.id, rating)
			}
			if err != nil {
				respondError(w, r, errGeneric, "star update failed")
				return
			}
		}
		respond(w, r, "", nil)
	}
}

// setRating — Subsonic's 1..5 stars onto Heya's 1..10 ratings (0 clears).
func (s *Server) handleSetRating(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	rating, err := strconv.Atoi(param(r, "rating"))
	if err != nil || rating < 0 || rating > 5 {
		respondError(w, r, errMissingParameter, "rating must be 0..5")
		return
	}
	kind, id, err := DecodeID(param(r, "id"))
	if err != nil {
		respondError(w, r, errNotFound, "unknown id")
		return
	}
	heyaRating := int16(rating * 2) //nolint:gosec // bounded 0..10 above

	var setErr error
	switch kind {
	case KindTrack:
		setErr = s.app.SetUserTrackRating(r.Context(), u.ID, id, heyaRating)
	case KindAlbum:
		setErr = s.app.SetUserAlbumRating(r.Context(), u.ID, id, heyaRating)
	case KindArtist:
		setErr = s.app.SetUserArtistRating(r.Context(), u.ID, id, heyaRating)
	default:
		respondError(w, r, errNotFound, "unknown id")
		return
	}
	if setErr != nil {
		respondError(w, r, errGeneric, "rating update failed")
		return
	}
	respond(w, r, "", nil)
}

// scrobble — submission=true (default) appends play_events, the same rows
// the web player's history and stats read; submission=false is a
// now-playing report mirrored into the live session store so the activity
// panel (and getNowPlaying) see Subsonic clients.
func (s *Server) handleScrobble(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	ids := paramAll(r, "id")
	if len(ids) == 0 {
		respondError(w, r, errMissingParameter, `required parameter "id" is missing`)
		return
	}
	submission := param(r, "submission") != "false"

	for _, raw := range ids {
		trackID, err := DecodeIDKind(raw, KindTrack)
		if err != nil {
			continue // foreign ids are dropped, not fatal — batch scrobbles must survive one bad row
		}
		rows, _, err := s.app.JFListTracks(ctx, jfTracksByIDs(trackID))
		if err != nil || len(rows) == 0 {
			continue
		}
		tr := rows[0]

		if submission {
			_ = s.app.RecordPlayback(ctx, u.ID, service.PlaybackEvent{
				EntityType:      "track",
				EntityID:        trackID,
				PositionSeconds: tr.Duration,
				TotalSeconds:    tr.Duration,
				Completed:       true,
				Source:          "subsonic",
			})
			continue
		}
		if store := s.app.Sessions(); store != nil {
			store.Upsert(sessions.Session{
				SessionID:       "subsonic-" + strconv.FormatInt(u.ID, 10),
				UserID:          u.ID,
				Username:        u.Username,
				MediaItemID:     tr.ArtistMediaItemID,
				MediaTitle:      tr.Title,
				MediaSubtitle:   tr.ArtistName + " — " + tr.AlbumTitle,
				MediaType:       "music",
				EntityType:      "track",
				EntityID:        trackID,
				ArtistName:      tr.ArtistName,
				AlbumTitle:      tr.AlbumTitle,
				TotalSeconds:    tr.Duration,
				PlaybackAction:  "direct_play",
				ClientUserAgent: r.UserAgent(),
				StartedAt:       time.Now(),
				LastHeartbeatAt: time.Now(),
			})
		}
	}
	respond(w, r, "", nil)
}

// --- Users ---

func (s *Server) userDTO(u sqlc.User) User {
	out := User{
		Username:          u.Username,
		Email:             u.Email,
		ScrobblingEnabled: true,
		AdminRole:         u.IsAdmin,
		SettingsRole:      true,
		DownloadRole:      true,
		PlaylistRole:      true,
		CoverArtRole:      true,
		StreamRole:        true,
		ShareRole:         false,
		JukeboxRole:       false,
		PodcastRole:       false,
		CommentRole:       false,
		UploadRole:        false,
	}
	return out
}

// getUser — the caller's account (or any account for admins).
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	requested := param(r, "username")
	if requested != "" && requested != u.Username {
		if !u.IsAdmin {
			respondError(w, r, errNotAuthorized, "not authorized to view other users")
			return
		}
		users, err := s.app.ListUsers(r.Context())
		if err != nil {
			respondError(w, r, errGeneric, "user lookup failed")
			return
		}
		for _, other := range users {
			if other.Username == requested {
				u = other
			}
		}
		if u.Username != requested {
			respondError(w, r, errNotFound, "user not found")
			return
		}
	}
	dto := s.userDTO(u)
	dto.Folders = s.musicFolderIDs(r)
	respond(w, r, "user", &dto)
}

// getUsers — admin roster.
func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.app.ListUsers(r.Context())
	if err != nil {
		respondError(w, r, errGeneric, "user listing failed")
		return
	}
	folders := s.musicFolderIDs(r)
	out := Users{Users: []User{}}
	for _, u := range users {
		dto := s.userDTO(u)
		dto.Folders = folders
		out.Users = append(out.Users, dto)
	}
	respond(w, r, "users", &out)
}

func (s *Server) musicFolderIDs(r *http.Request) []int64 {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		return nil
	}
	var out []int64
	for _, l := range libs {
		if l.MediaType == sqlc.MediaTypeMusic {
			out = append(out, l.ID)
		}
	}
	return out
}

// refuseUserMutation — Heya manages accounts itself; like the Jellyfin
// layer's library mutations, the answer is a validated refusal, never a
// lying success.
func (s *Server) refuseUserMutation(w http.ResponseWriter, r *http.Request) {
	respondError(w, r, errNotAuthorized, "user accounts are managed in Heya itself")
}

// --- Play queue ---

// getPlayQueue — restore the cross-device queue.
func (s *Server) handleGetPlayQueue(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	q, ok, err := s.app.GetSubsonicPlayQueue(r.Context(), u.ID)
	if err != nil {
		respondError(w, r, errGeneric, "play queue lookup failed")
		return
	}
	if !ok {
		respondError(w, r, errNotFound, "no saved play queue")
		return
	}
	out := PlayQueue{
		Username:  u.Username,
		Position:  q.PositionMs,
		Changed:   subTimePtr(q.ChangedAt),
		ChangedBy: q.ChangedBy,
		Entries:   s.tracksByIDs(r.Context(), u.ID, q.TrackIDs),
	}
	if q.CurrentTrackID > 0 {
		out.Current = EncodeID(KindTrack, q.CurrentTrackID)
	}
	respond(w, r, "playQueue", &out)
}

// savePlayQueue — persist the queue (empty id list clears it).
func (s *Server) handleSavePlayQueue(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var trackIDs []int64
	for _, raw := range paramAll(r, "id") {
		id, err := DecodeIDKind(raw, KindTrack)
		if err != nil {
			respondError(w, r, errNotFound, "unknown id: "+raw)
			return
		}
		trackIDs = append(trackIDs, id)
	}
	var current int64
	if c := param(r, "current"); c != "" {
		if id, err := DecodeIDKind(c, KindTrack); err == nil {
			current = id
		}
	}
	position, _ := strconv.ParseInt(param(r, "position"), 10, 64)
	err := s.app.SaveSubsonicPlayQueue(r.Context(), u.ID, service.SubsonicPlayQueue{
		TrackIDs:       trackIDs,
		CurrentTrackID: current,
		PositionMs:     position,
		ChangedBy:      param(r, "c"),
	})
	if err != nil {
		respondError(w, r, errGeneric, "saving play queue failed")
		return
	}
	respond(w, r, "", nil)
}

// --- Library scanning ---

// getScanStatus — Heya scans run through the job queue; this surface
// reports "not scanning" plus the song count (clients only render the
// count). Live scan progress here is a follow-up.
func (s *Server) handleGetScanStatus(w http.ResponseWriter, r *http.Request) {
	var count int64
	if c, err := s.app.GetMusicCounts(r.Context()); err == nil {
		count = c.Tracks
	}
	respond(w, r, "scanStatus", &ScanStatus{Scanning: false, Count: count})
}

// startScan — kicks a scan of every music library (admin gate applied at
// registration).
func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		respondError(w, r, errGeneric, "listing libraries failed")
		return
	}
	for _, l := range libs {
		if l.MediaType == sqlc.MediaTypeMusic {
			s.app.EnqueueScanLibrary(l.ID, false)
		}
	}
	var count int64
	if c, err := s.app.GetMusicCounts(r.Context()); err == nil {
		count = c.Tracks
	}
	respond(w, r, "scanStatus", &ScanStatus{Scanning: true, Count: count})
}

// getBookmarks — no bookmark storage for music; honest empty list.
func (s *Server) handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "bookmarks", &Bookmarks{Bookmarks: []struct{}{}})
}
