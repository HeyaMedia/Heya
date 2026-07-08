package cmd

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var dbWipeCmd = &cobra.Command{
	Use:   "db:wipe",
	Short: "Wipe all media data from the database",
	Long:  "Deletes all libraries, media items, assets, extras, people, and related data. User accounts are preserved.",
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			return fmt.Errorf("this will delete ALL media data. Use --force to confirm")
		}

		ctx := context.Background()
		db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL.Value, database.Options{
			MaxConns: int32(cfg.DatabaseMaxConns.Value),
			MinConns: int32(cfg.DatabaseMinConns.Value),
		})
		if err != nil {
			return err
		}
		defer db.Close()

		tables := []string{
			"river_job",
			"media_recommendations", "media_videos", "media_certifications",
			"media_production_companies", "media_keywords",
			"media_crew", "media_cast",
			"match_candidates", "media_assets",
			"tracks", "albums", "artists",
			"tv_episodes", "tv_seasons", "tv_series",
			"movies", "books", "authors",
			"library_files", "media_items", "libraries",
			"people", "keywords", "production_companies", "collections",
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		for _, t := range tables {
			_, err := tx.Exec(ctx, "DELETE FROM "+t)
			if err != nil {
				return fmt.Errorf("wipe %s: %w", t, err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}

		ui.Success("All media data wiped. User accounts preserved.")
		return nil
	},
}

func init() {
	dbWipeCmd.Flags().Bool("force", false, "Confirm the wipe")
	rootCmd.AddCommand(dbWipeCmd)
}
