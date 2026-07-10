package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/server"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	Long:  "Start the Heya HTTP API server and background workers. With tailscale.enabled, also exposes the same API on the tailnet.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		appCtx, appCancel := context.WithCancel(context.Background())
		defer appCancel()

		logRing := logbuf.New(2000)

		var baseWriter zerolog.LevelWriter
		if cfg.LogFormat.Value == "console" {
			baseWriter = zerolog.MultiLevelWriter(
				zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
				logRing,
			)
		} else {
			baseWriter = zerolog.MultiLevelWriter(os.Stderr, logRing)
		}
		log.Logger = zerolog.New(baseWriter).With().Timestamp().Logger()

		// Resolve run mode and enforce the active-mode safety guard BEFORE
		// service.New(): New() auto-migrates and active startup then bootstraps
		// libraries — both MUTATE the target DB. A guard placed after New() would
		// already have altered a remote/prod schema. Active mode (workers +
		// watchers + scanner) must never run against a non-local DB from a
		// source/dev checkout — it would join that DB's job queue and scan local
		// paths into it. The deployed container opts in with
		// HEYA_ALLOW_REMOTE_ACTIVE=true (it owns its remote DB); --dev-backend can
		// never opt in; a local DB always passes.
		passive := cfg.PassiveMode.Value
		devBackend, _ := cmd.Flags().GetBool("dev-backend")
		// Dev should reuse an already-installed Claude/Codex CLI and its normal
		// login by default. Production remains isolated under HEYA_DATA_DIR.
		// An explicit env value (including false) always wins.
		if devBackend {
			if _, configured := os.LookupEnv("HEYA_AI_USE_SYSTEM_AGENTS"); !configured {
				_ = os.Setenv("HEYA_AI_USE_SYSTEM_AGENTS", "true")
			}
		}
		if !passive {
			// Classify the host pgx will ACTUALLY dial (via pgx's own parser), not
			// a naive URL parse — otherwise a ?host=, DSN keyword form, PGHOST env,
			// or multi-host fallback could point the real connection at prod while
			// the URL authority reads localhost.
			localDB, dbHost, perr := database.AllHostsLocal(cfg.DatabaseURL.Value)
			if perr != nil {
				return fmt.Errorf("refusing to start active mode: cannot parse HEYA_DATABASE_URL to verify the database host is local: %w", perr)
			}
			if !localDB && (devBackend || !cfg.AllowRemoteActive.Value) {
				return fmt.Errorf("refusing to start active mode against non-local database host %q: "+
					"set HEYA_PASSIVE_MODE=true to use it read-only, point HEYA_DATABASE_URL at a local DB, "+
					"or set HEYA_ALLOW_REMOTE_ACTIVE=true if this instance is meant to own that DB "+
					"(--dev-backend can never run active against a remote DB)", dbHost)
			}
		}

		app, err := service.New(appCtx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		// Passive mode (HEYA_PASSIVE_MODE=true): the server is a read-mostly
		// guest on its DB — typically local dev pointed at a production
		// database to build UI against real data. We must NOT run any worker,
		// watcher, scheduler, or startup job-rescue: River's queue lives in the
		// same Postgres, so starting workers here turns this process into a
		// second worker pool on prod's queue. It would pull prod's jobs and run
		// a disk scan against a /storage path that doesn't exist locally,
		// soft-deleting the whole library. See docs/development.md.
		if passive {
			log.Warn().Msg("passive mode: workers, watchers, scheduler, sonic-analysis and orphan-rescue are DISABLED — this process will not process jobs or touch the filesystem")
		}

		// Register the scheduler trigger up front so request handlers can resolve
		// it (in passive mode too, to avoid a nil-deref); its 60s tick loop only
		// starts with the other background services further down.
		taskTrigger := scheduler.NewTrigger(app.DBPool(), app.RiverClient())
		app.SetScheduler(taskTrigger)

		srv := server.New(cfg, app,
			server.WithLogBuffer(logRing),
			server.WithEventHub(app.EventHub()),
			server.WithBaseContext(appCtx),
		)

		// Wire the Tailscale manager. In prod it's an in-process tsnet node
		// serving the same handler as the LAN listener, so toggling it on at
		// runtime serves the same routes. With --dev-backend the node instead
		// lives in the stable `heya dev-proxy` front-door (so it survives air
		// rebuilds) and we drive it over a localhost control socket — the
		// DB-backed enable/disable flow through the handlers is identical.
		tsLogger := log.With().Str("subsystem", "tailscale").Logger()
		onTSStatus := func(st tsnetwrap.Status) {
			app.EventHub().Emit(eventhub.EventTailscale, st)
		}
		var tsManager tsnetwrap.Manager
		if devBackend {
			tsManager = tsnetwrap.NewRemoteClient(devTSControlSocket(), tsLogger, onTSStatus)
			tsLogger.Info().Str("socket", devTSControlSocket()).Msg("dev-backend: driving tailscale via dev-proxy control socket")
		} else {
			tsManager = tsnetwrap.New(srv.Handler, tsLogger, onTSStatus)
		}
		app.SetTailscale(tsManager)

		if cfg.Tailscale.Enabled.Value {
			go func() {
				if err := tsManager.Enable(appCtx, tsnetwrap.Config{
					Enabled:  true,
					Hostname: cfg.Tailscale.Hostname.Value,
					AuthKey:  cfg.Tailscale.AuthKey.Value,
					StateDir: cfg.Tailscale.StateDir.Value,
					HTTPS:    cfg.Tailscale.HTTPS.Value,
					Funnel:   cfg.Tailscale.Funnel.Value,
				}); err != nil {
					tsLogger.Warn().Err(err).Msg("tailscale enable failed; LAN listener continues")
				}
			}()
		}

		// Bring the HTTP listener up FIRST — before the (potentially slow,
		// SMB-bound) worker + watcher startup below — so :8080 answers health
		// probes within a second instead of only after the recursive watch setup
		// on a large library finishes. Everything past this point runs while the
		// server is already accepting connections, so a slow StartWatchers can no
		// longer hold the startup/readiness gate hostage and crash-loop the pod.
		ln, err := reuseAddrListen(cfg.Addr())
		if err != nil {
			return err
		}

		go func() {
			log.Info().Str("addr", cfg.Addr()).Msg("starting server")
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				log.Fatal().Err(err).Msg("server error")
			}
		}()

		// --- Background services. The listener is already live above, so a slow
		// startup here only delays job processing / file watching, never the
		// health gate. ---
		if !passive {
			// Rescue any jobs left in state='running' by a previous process
			// (e.g. an `air` hot-reload killed the worker mid-job). Without
			// this, those rows sit "running" until River's periodic rescuer
			// catches them after RescueStuckJobsAfter (10min) — long enough
			// to look like real concurrency violations in the UI.
			if n, err := app.RescueOrphanedJobsAtStartup(appCtx); err != nil {
				log.Warn().Err(err).Msg("startup orphan-rescue failed")
			} else if n > 0 {
				log.Info().Int64("rescued", n).Msg("released orphaned jobs from previous process")
			}

			if err := app.StartWorkers(appCtx); err != nil {
				return err
			}
			log.Info().Msg("river workers started")

			// One-time, self-limiting backfill: resolve absolute-numbered anime
			// files whose parse_result predates the resolve-and-store logic (a
			// series enriched on an older build has no re-enrich trigger). Async
			// so a large library's sweep never delays job processing; idempotent,
			// so a steady-state boot does no work.
			go func() {
				if _, err := app.BackfillAbsoluteEpisodes(appCtx); err != nil {
					log.Warn().Err(err).Msg("startup absolute-episode backfill failed")
				}
			}()

			// Recursive watch setup can take minutes on a large SMB-mounted
			// library; it runs here, after the listener is live, so it never
			// gates startup health.
			if err := app.StartWatchers(appCtx); err != nil {
				log.Warn().Err(err).Msg("failed to start watchers")
			}
		}

		bridgeLogToHub(appCtx, logRing, app.EventHub())
		app.EventHub().StartPeriodicEmitters(appCtx, app.DBPool())
		// Bridge events published from other processes (e.g. a `heya library
		// remove` CLI call) onto this process's live hub → WebSocket clients.
		app.EventHub().StartCrossProcessRelay(appCtx, app.DBPool())
		go logRuntimeStatsPeriodically(appCtx, app.EventHub())

		if !passive {
			// React to bridged delete events: a CLI delete must also tear down
			// this process's file watcher for the removed library. Pointless
			// when no watchers are running.
			app.StartDeletedLibraryReaper(appCtx)
			taskTrigger.Start(appCtx)

			// Kick off the model fetcher in the background. No-op when
			// sonic-analysis is disabled in config.
			app.StartSonicAnalysis(appCtx)
			// Same for the optional embedding recommendation engine.
			app.StartRecommendationsML(appCtx)
		}

		// Cast discovery (mDNS browse + receiver sessions). Runs in
		// passive mode too — casting streams local files and touches
		// no scanner/worker state on the borrowed DB.
		app.LoadCastFromDB(appCtx)
		app.StartCast(appCtx)

		<-sigCtx.Done()
		log.Info().Msg("shutting down")

		// Hard backstop: if anything in the shutdown sequence hangs,
		// kill the process forcefully. 8 seconds is enough for the
		// graceful path below (which gives tsnet a real chance to
		// flush its state dir + close magicsock cleanly — abandoning
		// tsnet mid-teardown leaves the state dir in a partial state
		// that can put the *next* start into a busy loop, which is
		// almost certainly what's causing the 100% CPU after rapid
		// air reloads).
		go func() {
			<-time.After(8 * time.Second)
			log.Warn().Msg("shutdown took >8s, forcing exit")
			os.Exit(1)
		}()

		_ = ln.Close()

		// Cancel appCtx first so every derived context (workers,
		// watchers, periodic emitters, task scheduler, bridgeLogToHub)
		// observes cancellation before we touch their resources.
		appCancel()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()

		// Three independent shutdowns in parallel. The 6s budget covers
		// the slowest one (tsnet); workers + http server finish in well
		// under a second.
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			// Workers were never started in passive mode; River's Stop on a
			// never-started client has nothing to do, so skip it.
			if !passive {
				if err := app.StopWorkers(shutdownCtx); err != nil {
					log.Warn().Err(err).Msg("worker shutdown error")
				}
			}
		}()
		go func() {
			defer wg.Done()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Warn().Err(err).Msg("http shutdown error")
			}
		}()
		go func() {
			defer wg.Done()
			if ts := app.Tailscale(); ts != nil {
				done := make(chan struct{})
				go func() {
					if err := ts.Close(); err != nil {
						log.Warn().Err(err).Msg("tailscale shutdown error")
					}
					close(done)
				}()
				select {
				case <-done:
					log.Info().Msg("tailscale shut down cleanly")
				case <-shutdownCtx.Done():
					log.Warn().Msg("tailscale shutdown timed out — state dir may be partial, next start may need extra time")
				}
			}
		}()

		// Bounded wait — give shutdown the full 6s budget plus a small
		// grace before we trust waitWithDeadline to give up.
		waitWithDeadline(&wg, 6500*time.Millisecond)

		// Bypass the defer chain entirely: pgxpool.Close in deferred
		// app.Close has been observed to block when River goroutines
		// retry queries against a closing pool. Explicit exit is the
		// only reliable way out.
		log.Info().Msg("clean shutdown complete")
		os.Exit(0)
		return nil
	},
}

func init() {
	serveCmd.Flags().Bool("dev-backend", false,
		"Dev mode: serve the API on this port only and drive Tailscale via the `heya dev-proxy` control socket instead of an in-process node (used by `make dev`)")
}

// devTSControlSocket is the localhost unix socket the `heya dev-proxy`
// front-door listens on for tailscale control and the --dev-backend server
// dials. Both processes run from the repo root, so the default relative path
// lines up; override with HEYA_DEV_TS_CONTROL if needed.
func devTSControlSocket() string {
	if v := os.Getenv("HEYA_DEV_TS_CONTROL"); v != "" {
		return v
	}
	return "tmp/heya-dev-ts.sock"
}

// waitWithDeadline returns when wg.Wait() completes or when the deadline
// elapses, whichever comes first. The wg goroutines keep running on
// timeout — that's fine, we're about to os.Exit anyway.
func waitWithDeadline(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func reuseAddrListen(addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		// SO_REUSEADDR alone lets us bind a port that's in TIME_WAIT,
		// but on macOS/BSD it does NOT let us bind a port whose previous
		// owner just exited with active connections still draining (the
		// usual air-reload case: old proc has WS handlers + in-flight
		// requests, kernel needs ~a second to FIN them all). Adding
		// SO_REUSEPORT bypasses that wait: the new process can grab
		// the listener even while the old socket is mid-teardown.
		// Both flags are safe under Linux (REUSEPORT is the load-
		// balancing flag there, but our use case never has two heya
		// processes alive together).
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); e != nil {
					opErr = e
					return
				}
				if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); e != nil {
					opErr = e
					return
				}
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	return lc.Listen(context.Background(), "tcp", addr)
}

// logRuntimeStatsPeriodically emits a single-line trend signal every 30s so
// we can spot goroutine leaks / heap growth from the logs without active
// pprof. If goroutines climb monotonically while CPU sits high, something
// is leaking; if they're stable but huge, something is *populated* badly.
func logRuntimeStatsPeriodically(ctx context.Context, hub *eventhub.Hub) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			log.Debug().
				Int("goroutines", runtime.NumGoroutine()).
				Int("hub_subs", hub.SubscriberCount()).
				Uint64("heap_inuse_mb", ms.HeapInuse>>20).
				Int64("cgo_calls", runtime.NumCgoCall()).
				Msg("runtime stats")
		}
	}
}

func bridgeLogToHub(ctx context.Context, ring *logbuf.RingBuffer, hub *eventhub.Hub) {
	ch := ring.Subscribe()
	go func() {
		defer ring.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-ch:
				if !ok {
					return
				}
				hub.Emit(eventhub.EventLog, eventhub.LogPayload{
					Level:   entry.Level,
					Message: entry.Message,
					Fields:  entry.Fields,
				})
			}
		}
	}()
}
