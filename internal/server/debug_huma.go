package server

import (
	"context"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

// registerDocsRoutes mounts /api/docs (Scalar HTML) as a Huma operation so
// the doc viewer itself shows up in the OpenAPI spec. The HTML payload is a
// static template — we just write it through huma.StreamResponse.
func registerDocsRoutes(api huma.API) {
	huma.Register(api, htmlOp(http.MethodGet, "/api/docs", "api-docs", "Scalar-rendered API reference", "System"),
		wrapStream(scalarHandler("/api/openapi.json")))
}

// registerDebugRoutes mounts the pprof + runtime-stats surface under Huma so
// it appears in the OpenAPI spec. All endpoints are admin-only; each one
// emits binary or text. The actual handlers come from net/http/pprof and our
// debugStatsHandler — Huma just provides typing and the admin gate.
//
// Pprof's net/http/pprof package gives one stdlib handler per profile name,
// so we register each as its own Huma operation (heap/goroutine/allocs/…)
// rather than a single wildcard. The cost is ~12 registrations of nearly-
// identical shape; the win is that every profile shows up in /api/docs.
func registerHumaDebugRoutes(api huma.API, hub *eventhub.Hub) {
	huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/", "pprof-index", "pprof profile index", "Debug"),
		wrapStream(pprof.Index))
	huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/cmdline", "pprof-cmdline", "Process command line", "Debug"),
		wrapStream(pprof.Cmdline))
	huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/profile", "pprof-cpu-profile", "30s CPU profile", "Debug"),
		wrapStream(pprof.Profile))
	huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/symbol", "pprof-symbol-get", "Resolve symbols (GET)", "Debug"),
		wrapStream(pprof.Symbol))
	huma.Register(api, adminBinary(http.MethodPost, "/api/debug/pprof/symbol", "pprof-symbol-post", "Resolve symbols (POST)", "Debug"),
		wrapStream(pprof.Symbol))
	huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/trace", "pprof-trace", "Execution trace", "Debug"),
		wrapStream(pprof.Trace))

	// Named profile leaves. pprof.Handler returns the stdlib handler for a
	// given profile; we adapt it to a HandlerFunc by closing over ServeHTTP.
	for _, name := range []string{"goroutine", "heap", "allocs", "block", "mutex", "threadcreate"} {
		profile := name
		h := pprof.Handler(profile)
		huma.Register(api, adminBinary(http.MethodGet, "/api/debug/pprof/"+profile, "pprof-"+profile, profile+" profile", "Debug"),
			wrapStream(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	}

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/debug/stats", "debug-stats", "Runtime memory/GC stats + build info", "Debug")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[debugStatsBody], error) {
			return noStoreJSON(collectDebugStats(hub)), nil
		})
}

// registerWebSocketRoutes mounts /api/ws as a Huma operation. WebSocket
// upgrades require Hijacker, which humago's response writer supports — we
// just delegate via wrapStream and let gorilla/websocket do its thing.
func registerWebSocketRoutes(api huma.API, app *service.App, hub *eventhub.Hub) {
	huma.Register(api, wsOp(http.MethodGet, "/api/ws", "websocket", "Real-time event stream (WebSocket)", "System"),
		wrapStream(handleWebSocket(hub, app.SessionLookup())))
}

// registerLogStreamRoute mounts /api/logs/stream (SSE) as a Huma operation.
// The legacy handler keeps the connection open and writes text/event-stream
// frames — wrapStream preserves that since humago's writer supports Flusher.
func registerLogStreamRoute(api huma.API, buf *logbuf.RingBuffer) {
	if buf == nil {
		return
	}
	huma.Register(api, sseOp(http.MethodGet, "/api/logs/stream", "logs-stream", "Live log stream (Server-Sent Events)", "Admin"),
		wrapStream(handleLogStream(buf)))
}

// --- Operation builders ---

// htmlOp documents a text/html response body.
func htmlOp(method, path, opID, summary, tag string) huma.Operation {
	o := op(method, path, opID, summary, tag)
	o.Responses = map[string]*huma.Response{
		"200": {
			Description: "HTML response",
			Content: map[string]*huma.MediaType{
				"text/html": {},
			},
		},
	}
	return o
}

// wsOp documents the WebSocket upgrade endpoint. OpenAPI doesn't really
// describe WebSockets, but having the operation present makes the docs
// discoverable.
func wsOp(method, path, opID, summary, tag string) huma.Operation {
	o := secured(op(method, path, opID, summary, tag))
	o.Responses = map[string]*huma.Response{
		"101": {Description: "WebSocket upgrade — switches to event-stream protocol"},
	}
	return o
}

// sseOp documents a text/event-stream response.
func sseOp(method, path, opID, summary, tag string) huma.Operation {
	o := adminSecured(op(method, path, opID, summary, tag))
	o.Responses = map[string]*huma.Response{
		"200": {
			Description: "Server-Sent Events stream — long-lived",
			Content: map[string]*huma.MediaType{
				"text/event-stream": {},
			},
		},
	}
	return o
}

// adminBinary is binaryOp + bearer auth + admin gate, for pprof.
func adminBinary(method, path, opID, summary, tag string) huma.Operation {
	return adminSecured(binaryOp(method, path, opID, summary, tag))
}

// debugStatsBody mirrors the legacy debugStats type, surfaced through Huma so
// the schema lands in the OpenAPI spec.
type debugStatsBody struct {
	Time           string         `json:"time"`
	Goroutines     int            `json:"goroutines"`
	HeapInUseBytes uint64         `json:"heap_inuse_bytes"`
	HeapAllocBytes uint64         `json:"heap_alloc_bytes"`
	SysBytes       uint64         `json:"sys_bytes"`
	NumGC          uint32         `json:"num_gc"`
	GCPauseLastNs  uint64         `json:"gc_pause_last_ns"`
	NumCPU         int            `json:"num_cpu"`
	NumCgoCall     int64          `json:"num_cgo_call"`
	MaxStackBytes  uint64         `json:"max_stack_bytes"`
	HubSubscribers int            `json:"hub_subscribers"`
	GoVersion      string         `json:"go_version"`
	BuildSettings  map[string]any `json:"build_settings,omitempty"`
}

func collectDebugStats(hub *eventhub.Hub) debugStatsBody {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	stats := debugStatsBody{
		Time:           time.Now().UTC().Format(time.RFC3339Nano),
		Goroutines:     runtime.NumGoroutine(),
		HeapInUseBytes: ms.HeapInuse,
		HeapAllocBytes: ms.HeapAlloc,
		SysBytes:       ms.Sys,
		NumGC:          ms.NumGC,
		GCPauseLastNs:  ms.PauseNs[(ms.NumGC+255)%256],
		NumCPU:         runtime.NumCPU(),
		NumCgoCall:     runtime.NumCgoCall(),
		MaxStackBytes:  ms.StackInuse,
		HubSubscribers: hub.SubscriberCount(),
		GoVersion:      runtime.Version(),
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		settings := map[string]any{
			"main_path":    bi.Path,
			"main_version": bi.Main.Version,
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.modified", "GOOS", "GOARCH":
				settings[s.Key] = s.Value
			}
		}
		stats.BuildSettings = settings
	}
	return stats
}
