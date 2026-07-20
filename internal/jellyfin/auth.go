package jellyfin

import (
	"context"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// DeviceInfo is the client identity Jellyfin apps attach to every request
// via the MediaBrowser auth header. It is parsed per-request (never trusted
// from storage): SessionInfo, /Sessions and playstate reporting all echo it
// back, and clients key their own state on DeviceId.
type DeviceInfo struct {
	Client   string
	Device   string
	DeviceID string
	Version  string
	Token    string
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxDevice
	ctxToken
)

// UserFrom returns the authenticated user injected by requireAuth.
func UserFrom(ctx context.Context) (sqlc.User, bool) {
	u, ok := ctx.Value(ctxUser).(sqlc.User)
	return u, ok
}

// DeviceFrom returns the per-request client identity (zero value when the
// client sent no MediaBrowser header — e.g. bare api_key query auth).
func DeviceFrom(ctx context.Context) DeviceInfo {
	d, _ := ctx.Value(ctxDevice).(DeviceInfo)
	return d
}

// TokenFrom returns the raw access token backing the current request.
func TokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(ctxToken).(string)
	return t
}

// parseAuthScheme parses the `MediaBrowser key="value", key="value"` header
// form (scheme "Emby" is the pre-fork spelling some clients still send).
// Returns ok=false when the value isn't that dialect at all.
func parseAuthScheme(v string) (DeviceInfo, bool) {
	v = strings.TrimSpace(v)
	var rest string
	switch {
	case len(v) >= 12 && strings.EqualFold(v[:12], "MediaBrowser"):
		rest = v[12:]
	case len(v) >= 4 && strings.EqualFold(v[:4], "Emby"):
		rest = v[4:]
	default:
		return DeviceInfo{}, false
	}

	var d DeviceInfo
	// Values are quoted and comma-separated, but quoting is inconsistent
	// across clients (some skip quotes, some percent-encode inside them).
	// Values never contain commas in practice — Jellyfin's own parser makes
	// the same assumption.
	for _, kv := range strings.Split(rest, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "client":
			d.Client = val
		case "device":
			d.Device = val
		case "deviceid":
			d.DeviceID = val
		case "version":
			d.Version = val
		case "token":
			d.Token = val
		}
	}
	return d, true
}

// extractAuth pulls client identity + token from a request, honoring every
// credential form seen in the wild, most-specific first:
//
//  1. Authorization: MediaBrowser Token="..." (modern clients)
//  2. X-Emby-Authorization: MediaBrowser ...   (older clients)
//  3. X-Emby-Token / X-MediaBrowser-Token      (bare token headers)
//  4. ?api_key= / ?ApiKey=                     (streams, images, websocket)
func extractAuth(r *http.Request) DeviceInfo {
	var d DeviceInfo
	if parsed, ok := parseAuthScheme(r.Header.Get("Authorization")); ok {
		d = parsed
	}
	if parsed, ok := parseAuthScheme(r.Header.Get("X-Emby-Authorization")); ok {
		if d.Token == "" {
			parsed.Token = firstNonEmpty(parsed.Token, d.Token)
			d = parsed
		} else if d.Client == "" {
			token := d.Token
			d = parsed
			d.Token = token
		}
	}
	if d.Token == "" {
		d.Token = firstNonEmpty(
			r.Header.Get("X-Emby-Token"),
			r.Header.Get("X-MediaBrowser-Token"),
			queryCI(r, "api_key"),
			queryCI(r, "apikey"),
		)
	}
	if d.DeviceID == "" {
		d.DeviceID = queryCI(r, "deviceid")
	}
	return d
}

// resolve authenticates the request and returns the resolution. Jellyfin
// sessions use the same lifecycle and management table as browser sessions,
// but carry a narrower audience so a PIN login cannot call native/admin APIs.
func (s *Server) resolve(r *http.Request) (auth.SessionResolution, DeviceInfo, bool) {
	d := extractAuth(r)
	if d.Token == "" {
		return auth.SessionResolution{}, d, false
	}
	res, err := auth.ResolveSession(r.Context(), s.app.SessionLookup(), d.Token)
	if err != nil || !auth.AllowsJellyfinAPI(res.Session.Kind) {
		return auth.SessionResolution{}, d, false
	}
	auth.TouchSessionAsync(s.app.SessionLookup(), d.Token)
	return res, d, true
}

// requireAuth wraps a handler with token authentication. Jellyfin returns a
// bare 401 with no body for unauthenticated API calls; clients key their
// "session expired, re-login" flow off exactly that.
func (s *Server) requireAuth(h handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		res, d, ok := s.resolve(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, res.User)
		ctx = context.WithValue(ctx, ctxDevice, d)
		ctx = context.WithValue(ctx, ctxToken, res.Token)
		h(w, r.WithContext(ctx), p)
	}
}

// requireAuthOrTrusted admits authenticated requests like requireAuth, plus
// bare requests from trusted networks. Upstream marks the image endpoints
// AllowAnonymous and clients lean on it — Flutter apps (Fladder) and
// jellyfin-web <img> tags fetch art with no credential at all. But Heya item
// ids are enumerable (sequential row ids inside the GUID encoding), so fully
// anonymous would let a public instance's artwork be scraped; trusted
// networks keep upstream parity where real clients live while the public
// path still requires the token.
func (s *Server) requireAuthOrTrusted(h handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		if res, d, ok := s.resolve(r); ok {
			ctx := context.WithValue(r.Context(), ctxUser, res.User)
			ctx = context.WithValue(ctx, ctxDevice, d)
			ctx = context.WithValue(ctx, ctxToken, res.Token)
			h(w, r.WithContext(ctx), p)
			return
		}
		if s.app.TrustedClientIP(clientIP(r)) {
			h(w, r, p)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}
}

// requireAdmin further gates on Heya's is_admin flag.
func (s *Server) requireAdmin(h handlerFunc) handlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request, p Params) {
		u, _ := UserFrom(r.Context())
		if !u.IsAdmin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		h(w, r, p)
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// queryCI fetches a query parameter case-insensitively — ASP.NET binding is
// case-insensitive and client casing varies (api_key vs ApiKey, startIndex
// vs StartIndex).
func queryCI(r *http.Request, name string) string {
	q := r.URL.Query()
	if v := q.Get(name); v != "" {
		return v
	}
	for k, vals := range q {
		if strings.EqualFold(k, name) && len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return ""
}
