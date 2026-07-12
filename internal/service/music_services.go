package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/scrobble"
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

type listenImportState struct {
	Status    string `json:"status"` // idle | running | done | failed
	Imported  int    `json:"imported"`
	Matched   int    `json:"matched"`
	Unmatched int    `json:"unmatched"`
	Scanned   int    `json:"scanned"`
	Error     string `json:"error,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// importRunning guards one import per (user, service) per process.
var importRunning sync.Map

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

func (a *App) lastfmClient(sessionKey string) (*scrobble.LastFM, error) {
	if a.config.LastfmAPIKey.Value == "" {
		return nil, fmt.Errorf("last.fm is not configured on this server — set HEYA_LASTFM_API_KEY (and HEYA_LASTFM_SECRET for scrobbling)")
	}
	return &scrobble.LastFM{
		APIKey:     a.config.LastfmAPIKey.Value,
		Secret:     a.config.LastfmSecret.Value,
		SessionKey: sessionKey,
	}, nil
}

// LastfmAuthStart begins the desktop auth flow: returns the URL the user must
// open and approve, plus the request token the FE hands back to complete.
func (a *App) LastfmAuthStart(ctx context.Context) (authURL, token string, err error) {
	lf, err := a.lastfmClient("")
	if err != nil {
		return "", "", err
	}
	if a.config.LastfmSecret.Value == "" {
		return "", "", fmt.Errorf("HEYA_LASTFM_SECRET is required for the Last.fm auth flow")
	}
	token, err = lf.GetToken(ctx)
	if err != nil {
		return "", "", err
	}
	return "https://www.last.fm/api/auth/?api_key=" + a.config.LastfmAPIKey.Value + "&token=" + token, token, nil
}

// LastfmAuthComplete exchanges the approved request token for a session key.
func (a *App) LastfmAuthComplete(ctx context.Context, userID int64, token string) (MusicServiceView, error) {
	lf, err := a.lastfmClient("")
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
	if service == "lastfm" && a.config.LastfmAPIKey.Value == "" {
		return fmt.Errorf("last.fm import needs HEYA_LASTFM_API_KEY on the server")
	}

	key := fmt.Sprintf("%d/%s", userID, service)
	if _, loaded := importRunning.LoadOrStore(key, true); loaded {
		return fmt.Errorf("an import for %s is already running", service)
	}
	go func() {
		defer importRunning.Delete(key)
		a.runListenImport(a.LifetimeContext(), userID, service, username, token)
	}()
	return nil
}

func (a *App) setImportState(ctx context.Context, userID int64, service string, st listenImportState) {
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(st)
	if _, err := a.db.Exec(ctx,
		`UPDATE user_music_services SET import_state = $3, updated_at = now() WHERE user_id = $1 AND service = $2`,
		userID, service, raw); err != nil {
		log.Warn().Err(err).Msg("music services: import state write failed")
	}
}

// runListenImport pages through the service's full history (newest → oldest),
// matches each listen to a library track (recording MBID first, normalized
// artist+title fallback), and inserts play_events rows for matches.
func (a *App) runListenImport(ctx context.Context, userID int64, service, username, token string) {
	st := listenImportState{Status: "running"}
	a.setImportState(ctx, userID, service, st)
	fail := func(err error) {
		st.Status = "failed"
		st.Error = err.Error()
		a.setImportState(ctx, userID, service, st)
		log.Warn().Err(err).Str("service", service).Int64("user", userID).Msg("listen import failed")
	}

	fetchPage := a.listenPager(service, username, token)
	for {
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		listens, done, err := fetchPage(ctx)
		if err != nil {
			fail(err)
			return
		}
		st.Scanned += len(listens)
		matched, imported, err := a.importListenBatch(ctx, userID, service, listens)
		if err != nil {
			fail(err)
			return
		}
		st.Matched += matched
		st.Unmatched += len(listens) - matched
		st.Imported += imported
		a.setImportState(ctx, userID, service, st)
		if done {
			break
		}
	}
	st.Status = "done"
	a.setImportState(ctx, userID, service, st)
	log.Info().Str("service", service).Int64("user", userID).
		Int("scanned", st.Scanned).Int("matched", st.Matched).Int("imported", st.Imported).
		Msg("listen import complete")
}

// listenPager returns a closure that yields successive history pages.
func (a *App) listenPager(service, username, token string) func(ctx context.Context) ([]scrobble.Listen, bool, error) {
	switch service {
	case "listenbrainz":
		lb := &scrobble.ListenBrainz{Token: token}
		cursor := time.Now().Add(time.Hour)
		return func(ctx context.Context) ([]scrobble.Listen, bool, error) {
			listens, next, err := lb.Listens(ctx, username, cursor, 100)
			if err != nil {
				return nil, false, err
			}
			if next.IsZero() || !next.Before(cursor) {
				return listens, true, nil
			}
			cursor = next
			return listens, false, nil
		}
	default: // lastfm
		page := 1
		return func(ctx context.Context) ([]scrobble.Listen, bool, error) {
			lf, err := a.lastfmClient("")
			if err != nil {
				return nil, false, err
			}
			listens, totalPages, err := lf.RecentTracks(ctx, username, page, 200)
			if err != nil {
				return nil, false, err
			}
			done := page >= totalPages || len(listens) == 0
			page++
			return listens, done, nil
		}
	}
}

// importListenBatch matches one page of listens and inserts the new ones.
func (a *App) importListenBatch(ctx context.Context, userID int64, source string, listens []scrobble.Listen) (matched, imported int, err error) {
	for _, l := range listens {
		trackID, duration, ok := a.matchListen(ctx, l)
		if !ok {
			continue
		}
		matched++
		listened := l.DurationSec
		if listened == 0 {
			listened = int(duration)
		}
		tag, err := a.db.Exec(ctx, `
			INSERT INTO play_events (user_id, track_id, played_at, listened_seconds, completed, source)
			SELECT $1, $2, $3, $4, true, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM play_events WHERE user_id = $1 AND track_id = $2 AND played_at = $3
			)`, userID, trackID, l.ListenedAt, listened, source)
		if err != nil {
			return matched, imported, err
		}
		imported += int(tag.RowsAffected())
	}
	return matched, imported, nil
}

// matchListen resolves an external listen to a library track: exact recording
// MBID first, then normalized (artist, title).
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
					lf, err := a.lastfmClient(s.token)
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
