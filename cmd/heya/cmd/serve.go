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

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	Long:  "Start the Kura HTTP API server and background workers.",
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

		<-sigCtx.Done()
		log.Info().Msg("shutting down")

		ln.Close()

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

		appCancel()
		return nil
	},
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
