package cmd

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/karbowiak/kura/internal/server"
	"github.com/karbowiak/kura/internal/service"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	Long:  "Start the Kura HTTP API server and background workers.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		if err := app.StartWorkers(ctx); err != nil {
			return err
		}
		log.Info().Msg("river workers started")

		srv := server.New(cfg, app)

		go func() {
			log.Info().Str("addr", cfg.Addr()).Msg("starting server")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal().Err(err).Msg("server error")
			}
		}()

		<-ctx.Done()
		log.Info().Msg("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		app.StopWorkers(shutdownCtx)
		return srv.Shutdown(shutdownCtx)
	},
}
