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

		ln, err := reuseAddrListen(cfg.Addr())
		if err != nil {
			return err
		}

		go func() {
			log.Info().Str("addr", cfg.Addr()).Msg("starting server")
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal().Err(err).Msg("server error")
			}
		}()

		var tsListeners []net.Listener
		if cfg.Tailscale.Enabled {
			tsListeners = startTailscale(appCtx, srv, app)
		}

		<-sigCtx.Done()
		log.Info().Msg("shutting down")

		_ = ln.Close()
		for _, l := range tsListeners {
			_ = l.Close()
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)
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
		wg.Wait()

		if ts := app.Tailscale(); ts != nil {
			if err := ts.Close(); err != nil {
				log.Warn().Err(err).Msg("tailscale shutdown error")
			}
		}

		appCancel()
		return nil
	},
}

// startTailscale brings up a tsnet node and binds the same http.Handler on
// :80 / :443 (or :443 funnel) of the tailnet listener. Returns the listeners
// so the outer shutdown can close them. Errors are logged but never fatal —
// LAN access keeps working if Tailscale onboarding fails.
func startTailscale(ctx context.Context, srv *http.Server, app *service.App) []net.Listener {
	tsLogger := log.With().Str("subsystem", "tailscale").Logger()
	ts := tsnetwrap.New(tsnetwrap.Config{
		Hostname: cfg.Tailscale.Hostname,
		AuthKey:  cfg.Tailscale.AuthKey,
		StateDir: cfg.Tailscale.StateDir,
		HTTPS:    cfg.Tailscale.HTTPS,
		Funnel:   cfg.Tailscale.Funnel,
	}, tsLogger, func(st tsnetwrap.Status) {
		app.EventHub().Emit(eventhub.EventTailscale, st)
	})
	app.SetTailscale(ts)

	if err := ts.Start(ctx); err != nil {
		tsLogger.Warn().Err(err).Msg("tailscale start failed; LAN listener continues")
		return nil
	}

	st := ts.Status()
	tsLogger.Info().
		Str("hostname", st.Hostname).
		Str("magic_dns", st.MagicDNS).
		Str("ipv4", st.IPv4).
		Bool("https", cfg.Tailscale.HTTPS).
		Bool("funnel", cfg.Tailscale.Funnel).
		Msg("tailscale node up")

	var listeners []net.Listener

	switch {
	case cfg.Tailscale.Funnel:
		fnl, err := ts.ListenFunnel()
		if err != nil {
			tsLogger.Warn().Err(err).Msg("funnel listen failed")
		} else {
			listeners = append(listeners, fnl)
			go serveOn(srv, fnl, "tailscale-funnel:443")
		}
		// Funnel exposes :443; still bind :80 for tailnet HTTP→HTTPS redirect.
		if l, err := ts.Listen(); err == nil {
			listeners = append(listeners, l)
			go serveRedirect(l, ts.HTTPRedirector(), "tailscale-redirect:80")
		}
	case cfg.Tailscale.HTTPS:
		tlsLn, err := ts.ListenTLS()
		if err != nil {
			tsLogger.Warn().Err(err).Msg("tailscale TLS listen failed; falling back to plain HTTP")
			if l, err := ts.Listen(); err == nil {
				listeners = append(listeners, l)
				go serveOn(srv, l, "tailscale-http:80")
			}
		} else {
			listeners = append(listeners, tlsLn)
			go serveOn(srv, tlsLn, "tailscale-https:443")
			if l, err := ts.Listen(); err == nil {
				listeners = append(listeners, l)
				go serveRedirect(l, ts.HTTPRedirector(), "tailscale-redirect:80")
			}
		}
	default:
		l, err := ts.Listen()
		if err != nil {
			tsLogger.Warn().Err(err).Msg("tailscale HTTP listen failed")
		} else {
			listeners = append(listeners, l)
			go serveOn(srv, l, "tailscale-http:80")
		}
	}

	return listeners
}

func serveOn(srv *http.Server, ln net.Listener, label string) {
	log.Info().Str("listener", label).Msg("listener up")
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Warn().Err(err).Str("listener", label).Msg("listener stopped")
	}
}

func serveRedirect(ln net.Listener, h http.Handler, label string) {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Info().Str("listener", label).Msg("redirect listener up")
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Warn().Err(err).Str("listener", label).Msg("redirect listener stopped")
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
