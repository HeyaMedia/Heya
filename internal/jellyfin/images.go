package jellyfin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Image delivery. Anonymous, like Jellyfin's own image endpoints and Heya's
// /api/media/{id}/image/{type} — <img> tags carry no auth headers. Every
// request is dispatched IN-PROCESS to the matching native Heya image
// endpoint, which owns the full pipeline (media_assets walk, resizer,
// passive-mode proxy), and the bytes come back directly, like real Jellyfin.
// No response on this surface is ever a redirect: a 302 was tried first and
// broke Feishin, which doesn't follow image redirects — so redirects a
// native handler emits (remote cover/still URLs on heya.media that were
// never downloaded) are intercepted and the remote bytes proxied through.

// jellyfin ImageType → Heya asset type. Unknown types 404 (upstream does the
// same for types an item lacks).
var imageTypeMap = map[string]string{
	"primary":  "poster",
	"backdrop": "backdrop",
	"logo":     "logo",
	"banner":   "banner",
	"thumb":    "thumb",
	"art":      "art",
	"disc":     "disc",
}

// GET /Items/{itemId}/Images/{imageType} and .../{imageIndex}
//
// Rather than resolve + serve here, dispatch to Heya's own image endpoints.
// Those are the source of truth: they run the media_assets walk, the on-disk
// resizer, AND the passive-mode image proxy (proxiedImage) that fetches bytes
// from an upstream Heya when the local data dir has none. Serving locally here
// bypassed that proxy and 404'd in passive/dev deployments.
func (s *Server) handleItemImage(w http.ResponseWriter, r *http.Request, p Params) {
	ctx := r.Context()
	kind, id, err := DecodeID(p["itemId"])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	imgType := strings.ToLower(p["imageType"])
	index := 0
	if idx := p["imageIndex"]; idx != "" {
		index, _ = strconv.Atoi(idx)
	}

	target := ""
	switch kind {
	case KindItem:
		assetType, known := imageTypeMap[imgType]
		if !known {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/media/%d/image/%s", id, assetType)
		if imgType == "backdrop" && index > 0 {
			target += fmt.Sprintf("?sort=%d", index)
		}

	// In every branch below: a query error is a 500 (retryable — Feishin's
	// cover-art request storms surfaced transient errors being served as
	// 404s, which clients cache as "no image"), only a missing row is a 404.
	case KindSeason:
		rows, err := s.app.JFListSeasons(ctx, 0, []int64{id})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		season := rows[0]
		target = fmt.Sprintf("/api/media/%d/image/poster?label=season-%d", season.SeriesMediaItemID, season.SeasonNumber)

	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		ep := rows[0]
		// Fallback chain: a downloaded per-episode still (local asset) →
		// the episode's remote still URL (not downloaded) → series backdrop
		// → series poster. The GetMediaImagePath ok-checks read the DB only
		// (asset rows), so they work in passive mode; serving is delegated to
		// the native endpoint (or the remote URL) that we redirect to.
		label := fmt.Sprintf("s%de%d", ep.SeasonNumber, ep.EpisodeNumber)
		switch {
		case s.hasImage(ctx, ep.SeriesMediaItemID, "still", label):
			target = fmt.Sprintf("/api/media/%d/image/still?label=%s", ep.SeriesMediaItemID, label)
		case strings.HasPrefix(ep.StillPath, "http"):
			s.serveNativeImage(w, r, ep.StillPath) // absolute → proxied remote fetch
			return
		case s.hasImage(ctx, ep.SeriesMediaItemID, "backdrop", ""):
			target = fmt.Sprintf("/api/media/%d/image/backdrop", ep.SeriesMediaItemID)
		default:
			target = fmt.Sprintf("/api/media/%d/image/poster", ep.SeriesMediaItemID)
		}

	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].Slug == "" {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", rows[0].ArtistSlug, rows[0].Slug)

	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].AlbumSlug == "" {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", rows[0].ArtistSlug, rows[0].AlbumSlug)

	default: // KindLibrary, KindUser, ...
		http.NotFound(w, r)
		return
	}

	s.serveNativeImage(w, r, appendResizeQuery(target, r))
}

// serveNativeImage resolves target to actual image bytes: local paths
// dispatch through the full server mux in-process; absolute URLs (and any
// redirect a native handler answers with — e.g. a remote heya.media cover
// that was never downloaded) are fetched server-side and streamed through.
// The client never sees a redirect. Falls back to a plain 302 when no
// native handler is mounted (unit tests, exotic embeddings).
func (s *Server) serveNativeImage(w http.ResponseWriter, r *http.Request, target string) {
	if s.native == nil {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	for range 3 { // bounded redirect-resolution depth
		u, err := url.Parse(target)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if u.IsAbs() {
			s.proxyRemoteImage(w, r, target)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = u.Path
		r2.URL.RawPath = ""
		r2.URL.RawQuery = u.RawQuery
		r2.RequestURI = ""
		dw := &imageDispatchWriter{ResponseWriter: w}
		s.native.ServeHTTP(dw, r2)
		if !dw.intercepted {
			return // native handler served bytes (or a real error) directly
		}
		if dw.redirect == "" {
			http.NotFound(w, r)
			return
		}
		target = dw.redirect
	}
	http.NotFound(w, r) // redirect chain too deep — treat as missing
}

// imageDispatchWriter forwards a dispatched native response through, except
// redirects: those are captured (and their tiny HTML bodies swallowed) so
// serveNativeImage can resolve them to bytes instead.
type imageDispatchWriter struct {
	http.ResponseWriter
	redirect    string
	intercepted bool
}

func (dw *imageDispatchWriter) WriteHeader(code int) {
	if code >= 300 && code < 400 {
		dw.redirect = dw.Header().Get("Location")
		dw.Header().Del("Location")
		dw.intercepted = true
		return
	}
	dw.ResponseWriter.WriteHeader(code)
}

func (dw *imageDispatchWriter) Write(b []byte) (int, error) {
	if dw.intercepted {
		return len(b), nil
	}
	return dw.ResponseWriter.Write(b)
}

// remoteImageClient fetches never-downloaded remote assets (heya.media CDN
// covers/stills) for proxying. The URLs come from DB metadata, not the
// request — but metadata is only semi-trusted (NFO files can carry artwork
// URLs), and this runs on an ANONYMOUS endpoint, so treat it as an SSRF
// surface: the dialer rejects loopback/private/link-local/CGNAT targets at
// connect time (post-DNS — rebinding-safe, and it covers every redirect
// hop), and responses are image-typed and size-capped. Timeout bounds a
// stalled CDN so it can't pin handlers.
var remoteImageClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				return rejectNonPublicDial(address)
			},
		}).DialContext,
	},
}

// rejectNonPublicDial refuses connections to addresses an anonymous caller
// must never be able to make this server fetch from.
func rejectNonPublicDial(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("remote image dial to unresolved host %q", host)
	}
	// 100.64.0.0/10 (CGNAT) is what tailnets squat on — private in practice.
	inCGNAT := false
	if v4 := ip.To4(); v4 != nil {
		inCGNAT = v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || inCGNAT {
		return fmt.Errorf("remote image dial to non-public address %s refused", ip)
	}
	return nil
}

const remoteImageMaxBytes = 32 << 20 // no legitimate cover/still is larger

func (s *Server) proxyRemoteImage(w http.ResponseWriter, r *http.Request, rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	res, err := remoteImageClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer func() { _ = res.Body.Close() }()
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		http.NotFound(w, r) // remote miss or non-image = no image
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	if cl := res.Header.Get("Content-Length"); cl != "" {
		h.Set("Content-Length", cl)
	}
	h.Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, io.LimitReader(res.Body, remoteImageMaxBytes))
	}
}

// hasImage reports whether a media item has a resolvable image of the given
// type/label (DB check only — no file access), so we can pick which native
// endpoint to redirect to without serving locally.
func (s *Server) hasImage(ctx context.Context, mediaItemID int64, assetType, label string) bool {
	_, ok := s.app.GetMediaImagePath(ctx, mediaItemID, assetType, -1, label)
	return ok
}

// appendResizeQuery carries Jellyfin's image sizing params onto the native
// image URL as the resizer's own param names (w/h/q).
func appendResizeQuery(target string, r *http.Request) string {
	v := url.Values{}
	if wv := firstNonEmpty(queryCI(r, "maxWidth"), queryCI(r, "fillWidth"), queryCI(r, "width")); wv != "" {
		v.Set("w", wv)
	}
	if hv := firstNonEmpty(queryCI(r, "maxHeight"), queryCI(r, "fillHeight"), queryCI(r, "height")); hv != "" {
		v.Set("h", hv)
	}
	if qv := queryCI(r, "quality"); qv != "" {
		v.Set("q", qv)
	}
	if len(v) == 0 {
		return target
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + v.Encode()
}
