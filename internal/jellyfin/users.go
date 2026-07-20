package jellyfin

import (
	"net/http"
	"time"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/requestmeta"
	"github.com/rs/zerolog/log"
)

type authenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	// Password is the legacy SHA1-era field. Clients that still send it put
	// the plaintext there when Pw is absent (post-10.7 servers ignore the
	// hashed forms entirely, and so do we).
	Password string `json:"Password"`
}

// POST /Users/AuthenticateByName — the login. Creates a Jellyfin-scoped Heya
// session so devices appear in Heya's session management UI and revocation
// applies to them without giving a short PIN access to native/admin APIs.
func (s *Server) handleAuthenticateByName(w http.ResponseWriter, r *http.Request, _ Params) {
	var req authenticateByNameRequest
	if err := decodeJSON(r, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	password := req.Pw
	if password == "" {
		password = req.Password
	}

	ip := clientIP(r)
	guard := s.app.LoginGuard()
	accountKey := auth.AccountKey(req.Username)
	if !guard.Allow(ip, req.Username) {
		log.Warn().Str("surface", "jellyfin").Str("client_ip", ip).Str("account_key", accountKey).
			Msg("login throttled")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	release, ok := guard.BeginPasswordCheck()
	if !ok {
		log.Warn().Str("surface", "jellyfin").Str("client_ip", ip).Msg("password verifier saturated")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	defer release()

	// Account password or the user's Jellyfin PIN — the PIN is valid only
	// on this surface.
	user, err := s.app.AuthenticateJellyfin(r.Context(), req.Username, password)
	if err != nil {
		// Jellyfin answers failed logins with a bare 401; clients render
		// their own "invalid credentials" copy.
		log.Warn().Str("surface", "jellyfin").Str("client_ip", ip).Str("account_key", accountKey).
			Msg("login failed")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	guard.ClearAccount(req.Username)

	device := extractAuth(r)
	ua := deviceUserAgent(device, r)
	token, err := s.app.CreateJellyfinSession(r.Context(), user.ID, ua, clientIP(r))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	serverID := s.serverID(r)
	writeJSON(w, http.StatusOK, authenticationResult{
		User:        s.userDto(user, serverID),
		SessionInfo: s.sessionInfo(user, device, serverID, token, clientIP(r)),
		AccessToken: token,
		ServerID:    serverID,
	})
}

// GET /Users/Me
func (s *Server) handleUsersMe(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	writeJSON(w, http.StatusOK, s.userDto(u, s.serverID(r)))
}

// GET /Users/{userId} — self, or any user for admins.
func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request, p Params) {
	cur, _ := UserFrom(r.Context())
	id, err := DecodeIDKind(p["userId"], KindUser)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if id != cur.ID && !cur.IsAdmin {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	target := cur
	if id != cur.ID {
		target, err = s.app.SessionLookup().GetUserByID(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.userDto(target, s.serverID(r)))
}

// GET /Users — admin-only user list (jellyfin-web dashboard).
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request, _ Params) {
	users, err := s.app.ListUsers(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	serverID := s.serverID(r)
	out := make([]userDto, 0, len(users))
	for _, u := range users {
		out = append(out, s.userDto(u, serverID))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /Users/Public — users offered on the login screen without auth. Heya
// doesn't distinguish "publicly visible" users, so expose none: every client
// falls back to manual username entry, which always works.
func (s *Server) handleUsersPublic(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, []userDto{})
}

// POST /Sessions/Logout — revoke the calling token.
func (s *Server) handleSessionsLogout(w http.ResponseWriter, r *http.Request, _ Params) {
	if tok := TokenFrom(r.Context()); tok != "" {
		_ = s.app.DeleteSession(r.Context(), tok)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) userDto(u sqlc.User, serverID string) userDto {
	return userDto{
		Name:                  u.Username,
		ServerID:              serverID,
		ID:                    EncodeID(KindUser, u.ID),
		HasPassword:           true,
		HasConfiguredPassword: true,
		LastLoginDate:         time.Now().UTC(),
		LastActivityDate:      time.Now().UTC(),
		Configuration: userConfiguration{
			PlayDefaultAudioTrack:      true,
			SubtitleMode:               "Default",
			GroupedFolders:             []string{},
			OrderedViews:               []string{},
			LatestItemsExcludes:        []string{},
			MyMediaExcludes:            []string{},
			HidePlayedInLatest:         true,
			RememberAudioSelections:    true,
			RememberSubtitleSelections: true,
			EnableNextEpisodeAutoPlay:  true,
		},
		//nolint:gosec // G101 false positive: the provider type names below contain "Password" but are upstream class names, not credentials
		Policy: userPolicy{
			IsAdministrator:                  u.IsAdmin,
			BlockedTags:                      []string{},
			AllowedTags:                      []string{},
			EnableUserPreferenceAccess:       true,
			AccessSchedules:                  []any{},
			BlockUnratedItems:                []string{},
			EnableSharedDeviceControl:        true,
			EnableRemoteAccess:               true,
			EnableMediaPlayback:              true,
			EnableAudioPlaybackTranscoding:   true,
			EnableVideoPlaybackTranscoding:   true,
			EnablePlaybackRemuxing:           true,
			EnableContentDeletionFromFolders: []string{},
			EnableContentDownloading:         true,
			EnableSyncTranscoding:            true,
			EnabledDevices:                   []string{},
			EnableAllDevices:                 true,
			EnabledChannels:                  []string{},
			EnabledFolders:                   []string{},
			// Heya has no per-library ACL — every user sees every library.
			EnableAllFolders:           true,
			EnableCollectionManagement: u.IsAdmin,
			EnableSubtitleManagement:   u.IsAdmin,
			EnableLyricManagement:      u.IsAdmin,
			LoginAttemptsBeforeLockout: -1,
			BlockedMediaFolders:        []string{},
			BlockedChannels:            []string{},
			AuthenticationProviderID:   "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",
			PasswordResetProviderID:    "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",
			SyncPlayAccess:             "CreateAndJoinGroups",
		},
	}
}

func (s *Server) sessionInfo(u sqlc.User, d DeviceInfo, serverID, token, ip string) sessionInfo {
	return sessionInfo{
		PlayState:          playerStateInfo{RepeatMode: "RepeatNone", PlaybackOrder: "Default"},
		AdditionalUsers:    []any{},
		Capabilities:       clientCapabilities{PlayableMediaTypes: []string{}, SupportedCommands: []string{}, SupportsPersistentIdentifier: true},
		RemoteEndPoint:     ip,
		PlayableMediaTypes: []string{},
		// Stable per-token id; the token hash is already hex and opaque.
		ID:                       auth.TokenHash(token)[:32],
		UserID:                   EncodeID(KindUser, u.ID),
		UserName:                 u.Username,
		Client:                   d.Client,
		LastActivityDate:         time.Now().UTC(),
		DeviceName:               d.Device,
		DeviceID:                 d.DeviceID,
		ApplicationVersion:       d.Version,
		IsActive:                 true,
		ServerID:                 serverID,
		SupportedCommands:        []string{},
		NowPlayingQueue:          []any{},
		NowPlayingQueueFullItems: []any{},
	}
}

// deviceUserAgent renders the MediaBrowser identity into the session row's
// user_agent column so Heya's session UI shows "Finamp on Pixel 8" instead
// of an opaque HTTP UA.
func deviceUserAgent(d DeviceInfo, r *http.Request) string {
	if d.Client != "" {
		ua := d.Client
		if d.Version != "" {
			ua += "/" + d.Version
		}
		if d.Device != "" {
			ua += " (" + d.Device + ")"
		}
		return "Jellyfin: " + ua
	}
	if ua := r.UserAgent(); ua != "" {
		return "Jellyfin: " + ua
	}
	return "Jellyfin client"
}

func clientIP(r *http.Request) string {
	return requestmeta.ClientIP(r.Context())
}
