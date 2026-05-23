package server

import (
	"encoding/json"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
)

// registerDebugRoutes mounts the stdlib pprof handlers and a runtime stats
// endpoint at /api/debug/*. Everything here is admin-only — these endpoints
// expose memory contents, full goroutine stacks, and CPU traces. Never wire
// them up without the admin gate.
func registerDebugRoutes(mux *http.ServeMux, hub *eventhub.Hub) {
	// pprof index lists the available profiles and links them. Each leaf
	// (heap, goroutine, profile, allocs, mutex, block, threadcreate) has
	// its own handler.
	mux.HandleFunc("GET /api/debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /api/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /api/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /api/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("POST /api/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /api/debug/pprof/trace", pprof.Trace)

	// Named profile leaves (heap, goroutine, allocs, mutex, block,
	// threadcreate) — these are served by pprof.Index but we expose
	// each one explicitly so `curl ?debug=2` works without the index
	// page rendering.
	for _, name := range []string{"goroutine", "heap", "allocs", "block", "mutex", "threadcreate"} {
		mux.Handle("GET /api/debug/pprof/"+name, pprof.Handler(name))
	}

	mux.HandleFunc("GET /api/debug/stats", debugStatsHandler(hub))
}

type debugStats struct {
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

func debugStatsHandler(hub *eventhub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)

		stats := debugStats{
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

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(stats)
	}
}
