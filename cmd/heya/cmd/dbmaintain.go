package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

// CLI aliases for the wire op names — `heya db:maintain vacuum` reads better
// than vacuum_analyze, but the canonical names are accepted too.
var dbMaintainOps = map[string]service.DBMaintenanceOp{
	"vacuum":      service.DBMaintenanceVacuumAnalyze,
	"analyze":     service.DBMaintenanceAnalyze,
	"reindex":     service.DBMaintenanceReindex,
	"reset-stats": service.DBMaintenanceResetQueryStats,
}

var dbMaintainCmd = &cobra.Command{
	Use:   "db:maintain <vacuum|analyze|reindex|reset-stats>",
	Short: "Run PostgreSQL maintenance to keep the database healthy",
	Long: "Runs a maintenance operation across every user table, the same code path\n" +
		"as the Settings → Database maintenance buttons:\n\n" +
		"  vacuum       VACUUM (ANALYZE) — reclaim dead rows and refresh planner stats\n" +
		"  analyze      ANALYZE only — refresh planner statistics (fast)\n" +
		"  reindex      REINDEX CONCURRENTLY — rebuild indexes without blocking writes\n" +
		"  reset-stats  Zero pg_stat_statements so query timings measure a fresh window\n\n" +
		"Operations run table by table, biggest first, and keep going past\n" +
		"per-table failures (reported at the end). One operation runs at a time.",
	Args:         cobra.ExactArgs(1),
	ValidArgs:    []string{"vacuum", "analyze", "reindex", "reset-stats"},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		op, ok := dbMaintainOps[args[0]]
		if !ok {
			var err error
			if op, err = service.ParseDBMaintenanceOp(args[0]); err != nil {
				return err
			}
		}

		// Same passive-mode trick as doctor: a maintenance run must never
		// let a newer CLI binary auto-migrate the schema out from under a
		// running older server.
		origPassive := cfg.PassiveMode.Value
		cfg.PassiveMode.Value = true
		return withApp(func(ctx context.Context, app *service.App) error {
			cfg.PassiveMode.Value = origPassive

			result, err := app.RunDBMaintenance(ctx, op, func(p service.DBMaintenanceProgress) {
				if !ui.JSONMode && p.Table != "" {
					ui.Info(fmt.Sprintf("[%d/%d]", p.Done+1, p.Total), p.Table)
				}
			})
			if ui.JSONMode {
				if jsonErr := ui.OutputJSON(result); jsonErr != nil {
					return jsonErr
				}
				return err
			}
			if err != nil {
				return err
			}

			for _, tableErr := range result.Errors {
				ui.Warn("%s", tableErr)
			}
			summary := fmt.Sprintf("%s finished in %s", args[0], (time.Duration(result.DurationMS) * time.Millisecond).String())
			if result.Tables > 0 {
				summary += " across " + strconv.Itoa(result.Tables) + " tables"
			}
			if len(result.Errors) > 0 {
				summary += fmt.Sprintf(" (%d table(s) failed, see warnings above)", len(result.Errors))
			}
			ui.Success("%s", summary)
			if strings.HasPrefix(string(op), "vacuum") {
				ui.Println(ui.Dim("Note: VACUUM frees space for reuse inside PostgreSQL; the on-disk file only shrinks from trailing free pages."))
			}
			return nil
		})
	},
}

func init() {
	rootCmd.AddCommand(dbMaintainCmd)
}
