package cmd

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata/studios"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var studiosCmd = &cobra.Command{
	Use:   "studios",
	Short: "Manage studio/network logos",
}

var studiosSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Download logos for all known production companies",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		q := sqlc.New(app.DBPool())
		companies, err := q.ListAllProductionCompanies(ctx)
		if err != nil {
			return err
		}

		if len(companies) == 0 {
			ui.Warn("No production companies in database. Scan a library first.")
			return nil
		}

		var names []string
		for _, c := range companies {
			names = append(names, c.Name)
		}

		ui.Info("Companies", ui.Dim(fmt.Sprintf("%d known", len(names))))

		resolver := studios.NewResolver(cfg.DataDir.Value)
		downloaded, skipped, err := resolver.Sync(ctx, names)
		if err != nil {
			return err
		}

		ui.Success("Sync complete: %d downloaded, %d skipped", downloaded, skipped)
		return nil
	},
}

func init() {
	studiosCmd.AddCommand(studiosSyncCmd)
}
