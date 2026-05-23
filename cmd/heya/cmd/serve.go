package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
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
		if cfg.LogFormat == "console" {
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

		taskRunner := scheduler.NewRunner(app.DBPool(), app.EventHub(), cfg.DataDir)
		taskRunner.Register(&scheduler.GenerateTrickplayTask{DB: app.DBPool(), DataDir: cfg.DataDir})
		taskRunner.Register(&scheduler.GenerateThumbnailsTask{DB: app.DBPool(), DataDir: cfg.DataDir})
		taskRunner.Register(app.ScanLibrariesTask())
		taskRunner.Register(&scheduler.RefreshMetadataTask{DB: app.DBPool(), River: app.RiverClient()})
		app.SetScheduler(taskRunner)
		taskRunner.Start(appCtx)

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

		if cfg.Tailscale.Enabled {
			go func() {
				if err := tsServer.Enable(appCtx, tsnetwrap.Config{
					Enabled:  true,
					Hostname: cfg.Tailscale.Hostname,
					AuthKey:  cfg.Tailscale.AuthKey,
					StateDir: cfg.Tailscale.StateDir,
					HTTPS:    cfg.Tailscale.HTTPS,
					Funnel:   cfg.Tailscale.Funnel,
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

		// Hard backstop: if anything in the shutdown sequence hangs
		// (tsnet's WireGuard / magicsock teardown has been the worst
		// offender — it spins goroutines that don't always notice Close
		// in dev cycles, pegging a CPU until the kernel reaps them),
		// kill the process forcefully. 3s is enough for the graceful
		// path below to finish in the happy case.
		go func() {
			<-time.After(3 * time.Second)
			log.Warn().Msg("shutdown took >3s, forcing exit")
			os.Exit(1)
		}()

		_ = ln.Close()

		// Cancel appCtx first so every derived context (workers,
		// watchers, periodic emitters, task scheduler, bridgeLogToHub)
		// observes cancellation before we touch their resources.
		appCancel()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Each step runs in its own goroutine with the shared 2s budget.
		// Tailscale Close especially can spin if magicsock is mid-handshake;
		// time-boxing it keeps a stuck tsnet from blocking the whole exit.
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
				case <-shutdownCtx.Done():
					log.Warn().Msg("tailscale shutdown timed out, abandoning")
				}
			}
		}()

		// Bounded wait — never block beyond shutdownCtx + a tiny grace.
		waitWithDeadline(&wg, 2500*time.Millisecond)

		// Bypass the defer chain entirely: pgxpool.Close in deferred
		// app.Close has been observed to block when River goroutines
		// retry queries against a closing pool, and we don't trust the
		// runtime to tear cleanly with tsnet's cgo goroutines still
		// active. Explicit exit is the only reliable way out.
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
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			}); err != nil {
				return err
			}
			return opErr
		},
	}
	return lc.Listen(context.Background(), "tcp", addr)
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
