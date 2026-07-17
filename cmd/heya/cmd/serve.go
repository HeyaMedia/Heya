package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/ingress"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/remote"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/server"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start Heya with embedded Caddy",
	Long:  "Start Heya's embedded Caddy ingress and background workers. With tailscale.enabled, also exposes the same application on the tailnet.",
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
		if err := validateActiveRuntimeDatabase(cfg, devBackend); err != nil {
			return err
		}

		app, err := service.New(appCtx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		// Serve is always queue-passive now: it owns ingress, APIs, WebSockets,
		// casting and network control planes, while `heya worker` owns River,
		// filesystem watchers and scheduled background work. Passive mode also
		// suppresses schema/bootstrap writes in service.New.
		if passive {
			log.Warn().Msg("passive mode: serving read-mostly API without schema/bootstrap writes")
		}

		// The API-side trigger only inserts manual jobs. Its periodic scheduling
		// loop is started exclusively by the dedicated worker process.
		taskTrigger := scheduler.NewTrigger(app.DBPool(), app.RiverClient())
		app.SetScheduler(taskTrigger)

		handler := server.NewHandler(cfg, app,
			server.WithLogBuffer(logRing),
			server.WithEventHub(app.EventHub()),
		)
		ingressLogger := log.With().Str("subsystem", "caddy").Logger()
		ingressManager := ingress.New(handler, ingressLogger)
		app.SetIngress(ingressManager)
		if err := ingressManager.Start(appCtx, ingress.HostConfig{
			Address: cfg.Addr(), HTTPS: !devBackend, DataDir: cfg.DataDir.Value,
			LogLevel: cfg.LogLevel.Value,
		}); err != nil {
			return fmt.Errorf("starting embedded Caddy ingress: %w", err)
		}
		defer func() { _ = ingressManager.Close() }()

		// Wire the network control planes into Caddy: Tailscale supplies tsnet
		// listeners/certificates; remote supplies UPnP, DNS, certificates and
		// outside-in checks. Both are PRODUCTION-ONLY — under
		// --dev-backend the managers stay nil and the API reports them as
		// unavailable. The dev-proxy is a dumb reverse proxy on purpose.
		if devBackend {
			log.Info().Msg("dev-backend: tailscale + remote access are production-only, skipping")
		} else {
			tsLogger := log.With().Str("subsystem", "tailscale").Logger()
			tsManager := tsnetwrap.New(
				tsLogger,
				func(st tsnetwrap.Status) { app.EventHub().Emit(eventhub.EventTailscale, st) },
				func(ctx context.Context, tail tsnetwrap.IngressConfig) error {
					return ingressManager.SetTailnet(ctx, ingress.TailnetConfig{
						Address: tail.Address, CertDomain: tail.CertDomain,
						HTTPS: tail.HTTPS, Funnel: tail.Funnel, Source: tail.Source,
					})
				},
				ingressManager.ClearTailnet,
			)
			app.SetTailscale(tsManager)
			defer func() { _ = tsManager.Close() }()

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

			remoteLogger := log.With().Str("subsystem", "remote").Logger()
			remoteMgr := remote.NewManager(
				remoteLogger,
				func(st remote.RemoteStatus) { app.EventHub().Emit(eventhub.EventRemote, st) },
				func(ctx context.Context, remoteCfg remote.IngressConfig) error {
					return ingressManager.SetRemote(ctx, ingress.RemoteConfig{
						Port: remoteCfg.Port, Names: remoteCfg.Names, DefaultSNI: remoteCfg.DefaultSNI,
						CertificateMode: remoteCfg.CertificateMode, GetCertificate: remoteCfg.GetCertificate,
					})
				},
				ingressManager.ClearRemote,
			)
			app.SetRemote(remoteMgr)
			defer func() { _ = remoteMgr.Close() }()

			if cfg.Remote.Enabled.Value {
				go func() {
					rc, err := app.RemoteRuntimeConfig(appCtx)
					if err != nil {
						remoteLogger.Warn().Err(err).Msg("remote access boot config failed")
						return
					}
					if err := remoteMgr.Enable(appCtx, rc); err != nil {
						remoteLogger.Warn().Err(err).Msg("remote access enable failed; LAN listener continues")
					}
				}()
			}
		}

		// No River queue, worker migration, or filesystem watcher is started in
		// this process. Caddy therefore remains responsive even when the worker
		// process is recovering a very large production backlog.
		log.Info().Str("addr", cfg.Addr()).Bool("https", !devBackend).Msg("embedded Caddy ingress started")

		bridgeLogToHub(appCtx, logRing, app.EventHub())
		app.EventHub().StartPeriodicEmitters(appCtx, app.DBPool())
		// Bridge events published from other processes (e.g. a `heya library
		// remove` CLI call) onto this process's live hub → WebSocket clients.
		app.EventHub().StartCrossProcessRelay(appCtx, app.DBPool())
		go logRuntimeStatsPeriodically(appCtx, app.EventHub())

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

		// Cancel appCtx first so every derived context (periodic emitters,
		// network managers, bridgeLogToHub)
		// observes cancellation before we touch their resources.
		appCancel()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()

		// Network owners are deliberately ordered: detach remote/tailnet from
		// Caddy, close tsnet, then stop the remaining host ingress.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The UPnP mapping is left in place on restart; only explicit
			// Disable unmaps it. The Caddy remote listener is still detached.
			if rm := app.Remote(); rm != nil {
				if err := rm.Close(); err != nil {
					log.Warn().Err(err).Msg("remote access shutdown error")
				}
			}
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
			if err := ingressManager.Close(); err != nil {
				log.Warn().Err(err).Msg("Caddy ingress shutdown error")
			}
		}()

		// Bounded wait — give shutdown the full 6s budget plus a small
		// grace before we trust waitWithDeadline to give up.
		waitWithDeadline(&wg, 6500*time.Millisecond)

		// Bypass the defer chain so a slow external runtime teardown cannot
		// leave an orphan process holding the ingress port.
		log.Info().Msg("clean shutdown complete")
		os.Exit(0)
		return nil
	},
}

func init() {
	serveCmd.Flags().Bool("dev-backend", false,
		"Dev mode: run plaintext Caddy on this port and skip production-only Tailscale/remote access (used by `make dev`)")
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
