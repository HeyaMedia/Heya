package cmd

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var workerIdleInPassive bool

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run Heya's background queue and filesystem services",
	Long: `Run the long-lived River worker runtime, filesystem watchers, scheduler,
model/background maintenance, and orphan recovery without opening an HTTP port.
Production deployments should run exactly one worker process alongside one or
more "heya serve" API/ingress processes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if cfg.PassiveMode.Value {
			if !workerIdleInPassive {
				return errors.New("worker cannot run with HEYA_PASSIVE_MODE=true")
			}
			log.Warn().Msg("passive mode: worker intentionally idle")
			<-ctx.Done()
			return nil
		}
		if err := validateActiveRuntimeDatabase(cfg, false); err != nil {
			return err
		}

		app, err := service.NewWorker(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		// This hub receives library lifecycle events from the API process. River
		// workers publish through a separate Postgres-backed publisher configured
		// by service.NewWorker, avoiding an event relay feedback loop.
		app.EventHub().StartCrossProcessRelay(ctx, app.DBPool())
		app.StartLibraryWatcherReconciler(ctx)

		taskTrigger := scheduler.NewTrigger(app.DBPool(), app.RiverClient())
		app.SetScheduler(taskTrigger)

		if n, rescueErr := app.RescueOrphanedJobsAtStartup(ctx); rescueErr != nil {
			log.Warn().Err(rescueErr).Msg("startup orphan-rescue failed")
		} else if n > 0 {
			log.Info().Int64("rescued", n).Msg("released orphaned jobs from previous worker process")
		}

		if err := app.StartWorkers(ctx); err != nil {
			return err
		}
		app.StartWorkerRuntimeHeartbeat(ctx)
		log.Info().Msg("dedicated River worker started")

		// Watch discovery may be slow on a large or unavailable SMB mount. It is
		// independent from queue/scheduler startup and therefore never gates it.
		go func() {
			if err := app.StartWatchers(ctx); err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Msg("failed to start filesystem watchers")
			}
		}()

		taskTrigger.Start(ctx)
		app.StartPlaylistSync()
		app.StartSonicAnalysis(ctx)
		app.StartRecommendationsML(ctx)
		go func() {
			if _, err := app.BackfillAbsoluteEpisodes(ctx); err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Msg("startup absolute-episode backfill failed")
			}
		}()

		<-ctx.Done()
		log.Info().Msg("shutting down dedicated worker")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := app.StopWorkers(shutdownCtx); err != nil {
			log.Warn().Err(err).Msg("worker shutdown error")
		}
		log.Info().Msg("dedicated worker stopped")
		return nil
	},
}

func init() {
	workerCmd.Flags().BoolVar(&workerIdleInPassive, "idle-in-passive", false,
		"Stay running but process no work when HEYA_PASSIVE_MODE=true (development supervisor use)")
}
