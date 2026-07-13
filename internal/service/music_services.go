package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// External music services (ListenBrainz / Last.fm), per user: credentials,
// outbound scrobbling of Heya plays, and one-shot listen-history imports that
// turn external scrobble history into play_events rows — the taste signal the
// mixes engine feeds on. Imports run as lifetime-context goroutines (rare,
// user-triggered, resumable — re-running skips already-imported listens).

// MusicServiceView is the API shape — the token is never echoed.
type MusicServiceView struct {
	Service         string          `json:"service" enum:"listenbrainz,lastfm"`
	Username        string          `json:"username"`
	TokenSet        bool            `json:"token_set"`
	ScrobbleEnabled bool            `json:"scrobble_enabled"`
	ImportState     json.RawMessage `json:"import_state"`
}

// MusicServiceUpdate mutates one service link. Empty Token keeps the stored
// one (like the AI settings API); Username applies to Last.fm imports
// (public history reads need only a username).
type MusicServiceUpdate struct {
	Username        string `json:"username,omitempty" maxLength:"128"`
	Token           string `json:"token,omitempty" maxLength:"256" doc:"ListenBrainz user token; empty keeps the stored one"`
	ScrobbleEnabled *bool  `json:"scrobble_enabled,omitempty"`
}

func musicServiceValid(service string) bool {
	return service == "listenbrainz" || service == "lastfm"
}

// ListUserMusicServices returns both service links (rows exist only once
// configured; absent services are returned with zero values so the FE renders
// a stable two-card layout).
func (a *App) ListUserMusicServices(ctx context.Context, userID int64) ([]MusicServiceView, error) {
	rows, err := a.db.Query(ctx, `
		SELECT service, username, token <> '', scrobble_enabled, import_state
		FROM user_music_services WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byService := map[string]MusicServiceView{}
	for rows.Next() {
		var v MusicServiceView
		if err := rows.Scan(&v.Service, &v.Username, &v.TokenSet, &v.ScrobbleEnabled, &v.ImportState); err != nil {
			return nil, err
		}
		byService[v.Service] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]MusicServiceView, 0, 2)
	for _, s := range []string{"listenbrainz", "lastfm"} {
		if v, ok := byService[s]; ok {
			out = append(out, v)
			continue
		}
		out = append(out, MusicServiceView{Service: s, ImportState: json.RawMessage(`{}`)})
	}
	return out, nil
}

// SetUserMusicService updates one link. A new ListenBrainz token is validated
// against the API and the username it belongs to is stored alongside.
func (a *App) SetUserMusicService(ctx context.Context, userID int64, service string, upd MusicServiceUpdate) (MusicServiceView, error) {
	if !musicServiceValid(service) {
		return MusicServiceView{}, fmt.Errorf("unknown service %q", service)
	}
	username := strings.TrimSpace(upd.Username)
	token := strings.TrimSpace(upd.Token)
	if service == "listenbrainz" && token != "" {
		lb := &scrobble.ListenBrainz{Token: token}
		name, err := lb.ValidateToken(ctx)
		if err != nil {
			return MusicServiceView{}, err
		}
		username = name
	}

	_, err := a.db.Exec(ctx, `
		INSERT INTO user_music_services (user_id, service, username, token, scrobble_enabled)
		VALUES ($1, $2, $3, $4, COALESCE($5, false))
		ON CONFLICT (user_id, service) DO UPDATE SET
			username = CASE WHEN $3 <> '' OR $4 <> '' THEN $3 ELSE user_music_services.username END,
			token = CASE WHEN $4 <> '' THEN $4 ELSE user_music_services.token END,
			scrobble_enabled = COALESCE($5, user_music_services.scrobble_enabled),
			updated_at = now()`,
		userID, service, username, token, upd.ScrobbleEnabled)
	if err != nil {
		return MusicServiceView{}, err
	}
	views, err := a.ListUserMusicServices(ctx, userID)
	if err != nil {
		return MusicServiceView{}, err
	}
	for _, v := range views {
		if v.Service == service {
			return v, nil
		}
	}
	return MusicServiceView{}, fmt.Errorf("service row vanished")
}

// lastfmCredentials resolves the server-level Last.fm app key pair with the
// usual provenance: env wins (HEYA_LASTFM_API_KEY/SECRET), else the
// admin-managed system_settings "lastfm" blob (Settings → Providers).
func (a *App) lastfmCredentials(ctx context.Context) (apiKey, secret string) {
	apiKey, secret = a.config.LastfmAPIKey.Value, a.config.LastfmSecret.Value
	if apiKey != "" {
		return apiKey, secret
	}
	raw, err := a.GetSystemSetting(ctx, "lastfm")
	if err != nil || len(raw) == 0 {
		return apiKey, secret
	}
	var v struct {
		APIKey string `json:"api_key"`
		Secret string `json:"secret"`
	}
	if json.Unmarshal(raw, &v) == nil {
		apiKey = v.APIKey
		if secret == "" {
			secret = v.Secret
		}
	}
	return apiKey, secret
}

func (a *App) lastfmClient(ctx context.Context, sessionKey string) (*scrobble.LastFM, error) {
	apiKey, secret := a.lastfmCredentials(ctx)
	if apiKey == "" {
		return nil, fmt.Errorf("last.fm is not configured on this server — add the API key in Settings → Providers (or set HEYA_LASTFM_API_KEY)")
	}
	return &scrobble.LastFM{
		APIKey:     apiKey,
		Secret:     secret,
		SessionKey: sessionKey,
	}, nil
}

// LastfmAuthStart begins the desktop auth flow: returns the URL the user must
// open and approve, plus the request token the FE hands back to complete.
func (a *App) LastfmAuthStart(ctx context.Context) (authURL, token string, err error) {
	lf, err := a.lastfmClient(ctx, "")
	if err != nil {
		return "", "", err
	}
	if lf.Secret == "" {
		return "", "", fmt.Errorf("the Last.fm shared secret is required for the connect flow — add it in Settings → Providers (or set HEYA_LASTFM_SECRET)")
	}
	token, err = lf.GetToken(ctx)
	if err != nil {
		return "", "", err
	}
	return "https://www.last.fm/api/auth/?api_key=" + lf.APIKey + "&token=" + token, token, nil
}

// LastfmAuthComplete exchanges the approved request token for a session key.
func (a *App) LastfmAuthComplete(ctx context.Context, userID int64, token string) (MusicServiceView, error) {
	lf, err := a.lastfmClient(ctx, "")
	if err != nil {
		return MusicServiceView{}, err
	}
	sessionKey, username, err := lf.GetSession(ctx, token)
	if err != nil {
		return MusicServiceView{}, err
	}
	return a.SetUserMusicService(ctx, userID, "lastfm", MusicServiceUpdate{Username: username, Token: sessionKey})
}

// StartListenImport kicks the history import for one service in the
// background. Re-running is safe — already-imported listens are skipped.
func (a *App) StartListenImport(ctx context.Context, userID int64, service string) error {
	if !musicServiceValid(service) {
		return fmt.Errorf("unknown service %q", service)
	}
	var username, token string
	err := a.db.QueryRow(ctx,
		`SELECT username, token FROM user_music_services WHERE user_id = $1 AND service = $2`,
		userID, service).Scan(&username, &token)
	if err != nil {
		return fmt.Errorf("%s is not configured yet", service)
	}
	if service == "listenbrainz" && token == "" {
		return fmt.Errorf("listenbrainz needs a token before importing")
	}
	if username == "" {
		return fmt.Errorf("%s needs a username before importing", service)
	}
	if service == "lastfm" {
		if key, _ := a.lastfmCredentials(ctx); key == "" {
			return fmt.Errorf("last.fm import needs the server API key — Settings → Providers (or HEYA_LASTFM_API_KEY)")
		}
	}

	// A completed import restarts from scratch; a failed/interrupted one
	// resumes from its cursor (the kickoff worker reads it from the state).
	if _, err := a.db.Exec(ctx, `
		UPDATE user_music_services
		SET import_state = CASE WHEN import_state->>'status' = 'done' THEN '{}'::jsonb ELSE import_state END
		WHERE user_id = $1 AND service = $2`, userID, service); err != nil {
		return err
	}

	// Durable queue work from here: the kickoff job pages the external
	// history and fans out match/insert batches — it survives restarts and
	// is deduplicated while active (uniqueWhileActive on the args).
	res, err := a.river.Insert(ctx, worker.KickoffListenImportArgs{UserID: userID, Service: service}, nil)
	if err != nil {
		return fmt.Errorf("enqueue import: %w", err)
	}
	if res.UniqueSkippedAsDuplicate {
		return fmt.Errorf("an import for %s is already running", service)
	}
	return nil
}

// matchListen resolves an external listen to a library track: exact recording
// MBID first, then normalized (artist, title). Shared with playlist sync; the
// import workers carry their own copy (worker/ cannot import service/).
func (a *App) matchListen(ctx context.Context, l scrobble.Listen) (trackID int64, duration int32, ok bool) {
	if l.RecordingMBID != "" {
		err := a.db.QueryRow(ctx,
			`SELECT id, duration FROM tracks WHERE recording_mbid = $1 LIMIT 1`,
			l.RecordingMBID).Scan(&trackID, &duration)
		if err == nil {
			return trackID, duration, true
		}
	}
	if l.ArtistName == "" || l.TrackName == "" {
		return 0, 0, false
	}
	err := a.db.QueryRow(ctx, `
		SELECT t.id, t.duration
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE lower(t.title) = lower($1) AND lower(ar.name) = lower($2)
		ORDER BY t.id
		LIMIT 1`, strings.TrimSpace(l.TrackName), strings.TrimSpace(l.ArtistName)).Scan(&trackID, &duration)
	if err != nil {
		return 0, 0, false
	}
	return trackID, duration, true
}

// StartReactionsSync bulk-pushes the user's existing hearts (and, for
// ListenBrainz, dislikes) to one linked service as a durable background job.
func (a *App) StartReactionsSync(ctx context.Context, userID int64, service string) error {
	if !musicServiceValid(service) {
		return fmt.Errorf("unknown service %q", service)
	}
	var token string
	if err := a.db.QueryRow(ctx,
		`SELECT token FROM user_music_services WHERE user_id = $1 AND service = $2`,
		userID, service).Scan(&token); err != nil || token == "" {
		return fmt.Errorf("%s is not connected yet", service)
	}
	if service == "lastfm" {
		if key, secret := a.lastfmCredentials(ctx); key == "" || secret == "" {
			return fmt.Errorf("last.fm sync needs the server API key + secret — Settings → Providers")
		}
	}
	res, err := a.river.Insert(ctx, worker.SyncReactionsOutArgs{UserID: userID, Service: service}, nil)
	if err != nil {
		return fmt.Errorf("enqueue reaction sync: %w", err)
	}
	if res.UniqueSkippedAsDuplicate {
		return fmt.Errorf("a reaction sync for %s is already running", service)
	}
	return nil
}

// reactionBand maps a rating onto the outbound sync band: 1 = love,
// -1 = hate, 0 = neutral/clear.
func reactionBand(rating int16) int {
	switch {
	case rating >= 9:
		return 1
	case rating >= 1 && rating <= 3:
		return -1
	default:
		return 0
	}
}

// ReactionOutbound syncs a track reaction to every service the user has
// scrobbling enabled on, when the reaction band actually changed. Heart →
// love, thumbs-down → ListenBrainz hate (Last.fm has no dislike), leaving
// the band → clear. Fire-and-forget with one retry.
func (a *App) ReactionOutbound(userID, trackID int64, oldRating, newRating int16) {
	oldBand, newBand := reactionBand(oldRating), reactionBand(newRating)
	if oldBand == newBand {
		return
	}
	ctx := a.LifetimeContext()
	go func() {
		rows, err := a.db.Query(ctx, `
			SELECT service, token FROM user_music_services
			WHERE user_id = $1 AND scrobble_enabled = true AND token <> ''`, userID)
		if err != nil {
			return
		}
		type svc struct{ service, token string }
		var services []svc
		for rows.Next() {
			var s svc
			if rows.Scan(&s.service, &s.token) == nil {
				services = append(services, s)
			}
		}
		rows.Close()
		if len(services) == 0 {
			return
		}

		var artist, track, mbid string
		if err := a.db.QueryRow(ctx, `
			SELECT ar.name, t.title, t.recording_mbid
			FROM tracks t
			JOIN albums al ON al.id = t.album_id
			JOIN artists ar ON ar.id = al.artist_id
			WHERE t.id = $1`, trackID).Scan(&artist, &track, &mbid); err != nil {
			return
		}

		for _, s := range services {
			submit := func() error {
				switch s.service {
				case "listenbrainz":
					if mbid == "" {
						return nil // LB feedback is MBID-keyed; nothing to send
					}
					lb := &scrobble.ListenBrainz{Token: s.token}
					return lb.SubmitFeedback(ctx, mbid, newBand)
				case "lastfm":
					lf, err := a.lastfmClient(ctx, s.token)
					if err != nil {
						return err
					}
					if newBand == 1 {
						return lf.Love(ctx, artist, track)
					}
					if oldBand == 1 {
						return lf.Unlove(ctx, artist, track) // left the heart band
					}
					return nil // last.fm has no dislike to sync
				}
				return nil
			}
			if err := submit(); err != nil {
				time.Sleep(5 * time.Second)
				if err := submit(); err != nil {
					log.Warn().Err(err).Str("service", s.service).Msg("outbound reaction sync failed")
				}
			}
		}
	}()
}

// ScrobbleOutbound pushes one completed Heya play to every service the user
// has scrobbling enabled on. Fire-and-forget with one retry — a missed
// scrobble must never fail the playback path.
func (a *App) ScrobbleOutbound(userID, trackID int64, playedAt time.Time) {
	ctx := a.LifetimeContext()
	go func() {
		rows, err := a.db.Query(ctx, `
			SELECT service, username, token FROM user_music_services
			WHERE user_id = $1 AND scrobble_enabled = true AND token <> ''`, userID)
		if err != nil {
			return
		}
		type svc struct{ service, username, token string }
		var services []svc
		for rows.Next() {
			var s svc
			if rows.Scan(&s.service, &s.username, &s.token) == nil {
				services = append(services, s)
			}
		}
		rows.Close()
		if len(services) == 0 {
			return
		}

		var l scrobble.Listen
		var dur int32
		err = a.db.QueryRow(ctx, `
			SELECT t.title, ar.name, al.title, t.recording_mbid, t.duration
			FROM tracks t
			JOIN albums al ON al.id = t.album_id
			JOIN artists ar ON ar.id = al.artist_id
			WHERE t.id = $1`, trackID).Scan(&l.TrackName, &l.ArtistName, &l.ReleaseName, &l.RecordingMBID, &dur)
		if err != nil {
			return
		}
		l.ListenedAt = playedAt
		l.DurationSec = int(dur)

		for _, s := range services {
			submit := func() error {
				switch s.service {
				case "listenbrainz":
					lb := &scrobble.ListenBrainz{Token: s.token}
					return lb.Submit(ctx, "single", []scrobble.Listen{l})
				case "lastfm":
					lf, err := a.lastfmClient(ctx, s.token)
					if err != nil {
						return err
					}
					return lf.Scrobble(ctx, []scrobble.Listen{l})
				}
				return nil
			}
			if err := submit(); err != nil {
				time.Sleep(5 * time.Second)
				if err := submit(); err != nil {
					log.Warn().Err(err).Str("service", s.service).Msg("outbound scrobble failed")
				}
			}
		}
	}()
}
