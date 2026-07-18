package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Manage the job queue",
	Long:  "Process, inspect, and manage background jobs.",
}

var queueProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Process queued jobs until empty",
	Long:  "Start workers, drain all pending jobs, then exit. Use this after 'heya library scan' to process metadata, images, etc.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if cfg.PassiveMode.Value {
			return fmt.Errorf("queue processing is disabled with HEYA_PASSIVE_MODE=true")
		}
		if err := validateActiveRuntimeDatabase(cfg, false); err != nil {
			return err
		}

		app, err := service.NewQueueProcessor(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()
		riverCtx, cancelRiver := newRiverStartContext(ctx)
		defer cancelRiver()

		if err := app.StartWorkers(riverCtx); err != nil {
			return err
		}
		log.Info().Msg("workers started, processing queue")

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		idleCount := 0
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("interrupted, stopping workers")
				if stopErr := stopRiverWorkers(app, cancelRiver); stopErr != nil {
					log.Warn().Err(stopErr).Msg("queue worker shutdown error")
				}
				return nil
			case <-ticker.C:
				pending, running := app.QueueCounts(ctx)
				if pending == 0 && running == 0 {
					idleCount++
					if idleCount >= 3 {
						log.Info().Msg("queue drained, stopping workers")
						if stopErr := stopRiverWorkers(app, cancelRiver); stopErr != nil {
							log.Warn().Err(stopErr).Msg("queue worker shutdown error")
						}
						ui.Success("All jobs processed")
						return nil
					}
				} else {
					idleCount = 0
					log.Info().Int("pending", pending).Int("running", running).Msg("processing")
				}
			}
		}
	},
}

var queueStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show job queue status",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL.Value, database.Options{
			MaxConns: int32(cfg.DatabaseMaxConns.Value),
			MinConns: int32(cfg.DatabaseMinConns.Value),
		})
		if err != nil {
			return err
		}
		defer db.Close()

		rows, err := db.Query(ctx, "SELECT state, kind, count(*) FROM river_job GROUP BY state, kind ORDER BY state, kind")
		if err != nil {
			return err
		}
		defer rows.Close()

		t := ui.NewTable("STATE", "KIND", "COUNT")
		total := 0
		for rows.Next() {
			var state, kind string
			var count int
			if err := rows.Scan(&state, &kind, &count); err != nil {
				return fmt.Errorf("scan queue status: %w", err)
			}
			t.AddRow(state, kind, fmt.Sprintf("%d", count))
			total += count
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("read queue status: %w", err)
		}

		if total == 0 {
			ui.Info("Queue", "empty")
			return nil
		}

		fmt.Println(t.Render())
		ui.Info("Total", fmt.Sprintf("%d jobs", total))
		return nil
	},
}

var queueClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear completed and failed jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		allFlag, _ := cmd.Flags().GetBool("all")

		ctx := context.Background()
		db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL.Value, database.Options{
			MaxConns: int32(cfg.DatabaseMaxConns.Value),
			MinConns: int32(cfg.DatabaseMinConns.Value),
		})
		if err != nil {
			return err
		}
		defer db.Close()

		var result int64
		if allFlag {
			tag, err := db.Exec(ctx, "DELETE FROM river_job")
			if err != nil {
				return err
			}
			result = tag.RowsAffected()
		} else {
			tag, err := db.Exec(ctx, "DELETE FROM river_job WHERE state IN ('completed', 'discarded', 'cancelled')")
			if err != nil {
				return err
			}
			result = tag.RowsAffected()
		}

		ui.Success("Cleared %d jobs", result)
		return nil
	},
}

func init() {
	queueClearCmd.Flags().Bool("all", false, "Clear ALL jobs including pending/running")

	queueCmd.AddCommand(queueProcessCmd)
	queueCmd.AddCommand(queueStatusCmd)
	queueCmd.AddCommand(queueClearCmd)
}
