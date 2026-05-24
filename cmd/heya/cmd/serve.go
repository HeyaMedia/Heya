package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

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

		app, err := service.New(appCtx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		if err := app.StartWorkers(appCtx); err != nil {
			return err
		}
		log.Info().Msg("river workers started")

		if err := app.StartWatchers(appCtx); err != nil {
			log.Warn().Err(err).Msg("failed to start watchers")
		}

		bridgeLogToHub(appCtx, logRing, app.EventHub())
		app.EventHub().StartPeriodicEmitters(appCtx, app.DBPool())
		go logRuntimeStatsPeriodically(appCtx, app.EventHub())

		taskRunner := scheduler.NewRunner(app.DBPool(), app.EventHub(), cfg.DataDir.Value)
		taskRunner.Register(&scheduler.GenerateTrickplayTask{DB: app.DBPool(), DataDir: cfg.DataDir.Value})
		taskRunner.Register(&scheduler.GenerateThumbnailsTask{DB: app.DBPool(), DataDir: cfg.DataDir.Value})
		taskRunner.Register(app.ScanLibrariesTask())
		taskRunner.Register(&scheduler.RefreshStaleItemsTask{DB: app.DBPool(), River: app.RiverClient()})
		taskRunner.Register(&scheduler.ScanMusicLoudnessTask{DB: app.DBPool(), River: app.RiverClient()})
		if a := app.SonicAnalyzer(); a != nil {
			taskRunner.Register(&scheduler.AnalyzeMusicTask{
				DB:       app.DBPool(),
				Analyzer: a,
				Fetcher:  app.ModelFetcher(),
				Enabled:  app.SonicAnalysisEnabled,
			})
		}
		app.SetScheduler(taskRunner)
		taskRunner.Start(appCtx)

		// Kick off the model fetcher in the background. No-op when
		// sonic-analysis is disabled in config.
		app.StartSonicAnalysis(appCtx)

		srv := server.New(cfg, app,
			server.WithLogBuffer(logRing),
			server.WithEventHub(app.EventHub()),
			server.WithBaseContext(appCtx),
		)

		// Wire the Tailscale manager with the same handler as the LAN
		// listener, so toggling it on at runtime serves the same routes.
		tsLogger := log.With().Str("subsystem", "tailscale").Logger()
		tsServer := tsnetwrap.New(srv.Handler, tsLogger, func(st tsnetwrap.Status) {
			app.EventHub().Emit(eventhub.EventTailscale, st)
		})
		app.SetTailscale(tsServer)

		if cfg.Tailscale.Enabled.Value {
			go func() {
				if err := tsServer.Enable(appCtx, tsnetwrap.Config{
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
			app.StopWorkers(shutdownCtx)
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
