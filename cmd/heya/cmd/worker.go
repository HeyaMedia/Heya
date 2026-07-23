package cmd

import (
	"context"
	"errors"
	"fmt"
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
	Short: "Run Heya's singleton background coordinator",
	Long: `Run the long-lived River worker runtime, filesystem watchers, scheduler,
model/background maintenance, and orphan recovery without opening an HTTP port.
Heya enforces exactly one coordinator per database with a session-level
PostgreSQL advisory lease. Run it alongside "heya serve". Replicating the API
role remains a separate deployment concern because playback sessions and
user-targeted live events are currently process-local.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		workerCtx, cancelWorker := context.WithCancel(ctx)
		defer cancelWorker()

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

		logRing := configureRuntimeLogRing(2000, "worker")
		app, err := service.NewWorker(workerCtx, cfg)
		if err != nil {
			return err
		}
		app.ConfigureProcessControl(service.ProcessRoleWorker, stop)
		defer app.Close()
		app.StartWorkerLogRelay(workerCtx, logRing)

		// River treats cancellation of its Start context as an immediate job
		// cancellation. Detach it from the signal context so the signal stops
		// auxiliary loops first, then StopWorkers gets the full graceful window.
		riverCtx, cancelRiver := newRiverStartContext(workerCtx)
		defer cancelRiver()
		leaseFailure := watchWorkerLease(
			workerCtx,
			app.CoordinatorLeaseLost(),
			cancelWorker,
			cancelRiver,
		)

		// This hub receives library lifecycle events from the API process. River
		// workers publish through a separate Postgres-backed publisher configured
		// by service.NewWorker, avoiding an event relay feedback loop.
		app.EventHub().StartCrossProcessRelay(workerCtx, app.DBPool())
		app.StartLibraryWatcherReconciler(workerCtx)

		taskTrigger := scheduler.NewTrigger(app.DBPool(), app.RiverClient())
		app.SetScheduler(taskTrigger)

		if n, rescueErr := app.RescueOrphanedJobsAtStartup(workerCtx); rescueErr != nil {
			log.Warn().Err(rescueErr).Msg("startup orphan-rescue failed")
		} else if n > 0 {
			log.Info().Int64("rescued", n).Msg("released orphaned jobs from previous worker process")
		}

		if err := app.StartWorkers(riverCtx); err != nil {
			return err
		}
		app.StartWorkerRuntimeHeartbeat(workerCtx)
		app.StartSonicRuntimeHeartbeat(workerCtx)
		app.StartProcessRestartWatcher(workerCtx)
		log.Info().Msg("dedicated River worker started")

		// Watch discovery may be slow on a large or unavailable mounted share. It is
		// independent from queue/scheduler startup and therefore never gates it.
		app.StartWatchersBackground(workerCtx)

		app.StartScheduler(workerCtx)
		app.StartPlaylistSync(workerCtx)
		app.StartSonicAnalysis(workerCtx)
		app.StartRecommendationsML(workerCtx)
		app.StartAbsoluteEpisodeBackfill(workerCtx)

		shutdownErr := waitForWorkerShutdown(ctx, leaseFailure)
		log.Info().Msg("shutting down dedicated worker")

		if stopErr := stopRiverWorkers(app, cancelRiver); stopErr != nil {
			log.Warn().Err(stopErr).Msg("worker shutdown error")
		}
		log.Info().Msg("dedicated worker stopped")
		return shutdownErr
	},
}

func watchWorkerLease(
	workerCtx context.Context,
	leaseLost <-chan error,
	cancelWorker context.CancelFunc,
	cancelRiver context.CancelFunc,
) <-chan error {
	failure := make(chan error, 1)
	go func() {
		select {
		case <-workerCtx.Done():
			return
		case err := <-leaseLost:
			if err == nil {
				err = errors.New("coordinator lease monitor stopped unexpectedly")
			}
			// Fail closed: stop every auxiliary loop and hard-cancel River work
			// as soon as PostgreSQL may admit a replacement coordinator.
			cancelWorker()
			cancelRiver()
			failure <- fmt.Errorf("worker coordinator lease lost: %w", err)
		}
	}()
	return failure
}

func waitForWorkerShutdown(signalCtx context.Context, leaseFailure <-chan error) error {
	select {
	case <-signalCtx.Done():
		return nil
	case err := <-leaseFailure:
		return err
	}
}

func newRiverStartContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(parent))
}

func stopRiverWorkers(app *service.App, cancelRiver context.CancelFunc) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	defer cancelRiver()
	return app.StopWorkers(shutdownCtx)
}

func init() {
	workerCmd.Flags().BoolVar(&workerIdleInPassive, "idle-in-passive", false,
		"Stay running but process no work when HEYA_PASSIVE_MODE=true (development supervisor use)")
}
