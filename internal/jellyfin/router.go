package jellyfin

import (
	"net/http"
	"strings"
)

// Params holds captured path parameters, keyed by the {name} used in the
// route pattern. Values keep the request's original casing (patterns match
// case-insensitively, but ids and filenames must round-trip untouched).
type Params map[string]string

type handlerFunc func(w http.ResponseWriter, r *http.Request, p Params)

// router is a tiny case-insensitive segment matcher. Jellyfin's server is
// ASP.NET, whose routing ignores case — a decade of clients (and the /emby
// alias inherited from Emby) relies on that, so stdlib ServeMux patterns
// (case-sensitive) can't host this surface. Route patterns are written
// byte-identical to the upstream OpenAPI spec (e.g. "/Users/{userId}") so the
// coverage manifest can string-match them against the vendored spec.
//
// Pattern segments may contain multiple dot-separated parts, each a literal
// or a {param}: "stream.{container}", "{segmentId}.{container}". Literals
// compare case-insensitively; params capture original-cased request parts.
type router struct {
	routes []*route
	// byMethod → exact-pattern lookup is unnecessary at this scale: the
	// whole table is a few hundred entries and matching is a linear scan
	// over pre-split segments, ~O(segments) per candidate. Profile before
	// getting cleverer.
}

type route struct {
	method  string
	pattern string // spec-exact, for the coverage manifest
	segs    []patternSeg
	handler handlerFunc
}

type patternSeg struct {
	parts []segPart
}

type segPart struct {
	literal string // lowercased literal; empty when this part is a param
	param   string
}

func newRouter() *router { return &router{} }

func (rt *router) handle(method, pattern string, h handlerFunc) {
	rt.routes = append(rt.routes, &route{
		method:  method,
		pattern: pattern,
		segs:    splitPattern(pattern),
		handler: h,
	})
}

// patterns returns "METHOD /Spec/Cased/Path" strings for the coverage tests.
func (rt *router) patterns() []string {
	out := make([]string, 0, len(rt.routes))
	for _, r := range rt.routes {
		out = append(out, r.method+" "+r.pattern)
	}
	return out
}

func splitPattern(pattern string) []patternSeg {
	raw := strings.Split(strings.Trim(pattern, "/"), "/")
	segs := make([]patternSeg, 0, len(raw))
	for _, seg := range raw {
		var ps patternSeg
		for _, part := range strings.Split(seg, ".") {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				ps.parts = append(ps.parts, segPart{param: part[1 : len(part)-1]})
			} else {
				ps.parts = append(ps.parts, segPart{literal: strings.ToLower(part)})
			}
		}
		segs = append(segs, ps)
	}
	return segs
}

// match finds a handler for method+path. Returns (handler, params, ok).
// HEAD falls back to GET routes — net/http discards the body for HEAD
// responses, so a GET handler is always a valid HEAD handler.
func (rt *router) match(method, path string) (handlerFunc, Params, bool) {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if h, p, ok := rt.matchMethod(method, segs); ok {
		return h, p, true
	}
	if method == http.MethodHead {
		return rt.matchMethod(http.MethodGet, segs)
	}
	return nil, nil, false
}

func (rt *router) matchMethod(method string, segs []string) (handlerFunc, Params, bool) {
	for _, r := range rt.routes {
		if r.method != method || len(r.segs) != len(segs) {
			continue
		}
		if p, ok := matchSegs(r.segs, segs); ok {
			return r.handler, p, true
		}
	}
	return nil, nil, false
}

// claims reports whether any route (any method) matches the path. The dev
// proxy uses this to decide backend-vs-Nuxt forwarding, so it must be exact:
// a lazy "first segment looks Jellyfin-ish" test would steal SPA routes like
// /search from the Vite dev server.
func (rt *router) claims(path string) bool {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, r := range rt.routes {
		if len(r.segs) != len(segs) {
			continue
		}
		if _, ok := matchSegs(r.segs, segs); ok {
			return true
		}
	}
	return false
}

func matchSegs(pattern []patternSeg, segs []string) (Params, bool) {
	var params Params
	for i, ps := range pattern {
		seg := segs[i]
		if len(ps.parts) == 1 {
			// Fast path: whole-segment literal or param, no dot splitting —
			// param values here may legitimately contain dots.
			p := ps.parts[0]
			if p.param != "" {
				if seg == "" {
					return nil, false
				}
				if params == nil {
					params = make(Params, 4)
				}
				params[p.param] = seg
				continue
			}
			if !strings.EqualFold(p.literal, seg) {
				return nil, false
			}
			continue
		}
		parts := strings.Split(seg, ".")
		if len(parts) != len(ps.parts) {
			return nil, false
		}
		for j, p := range ps.parts {
			if p.param != "" {
				if parts[j] == "" {
					return nil, false
				}
				if params == nil {
					params = make(Params, 4)
				}
				params[p.param] = parts[j]
				continue
			}
			if !strings.EqualFold(p.literal, parts[j]) {
				return nil, false
			}
		}
	}
	return params, true
}

// stripEmbyPrefix removes the legacy "/emby" alias Jellyfin inherited from
// Emby ("/emby/System/Info/Public" ≡ "/System/Info/Public"). Case-insensitive
// like everything else. Returns the path unchanged when the prefix is absent.
func stripEmbyPrefix(path string) string {
	if len(path) >= 5 && strings.EqualFold(path[:5], "/emby") {
		rest := path[5:]
		if rest == "" {
			return "/"
		}
		if rest[0] == '/' {
			return rest
		}
	}
	return path
}
