package cmd

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
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
		db, err := pgxpool.New(ctx, cfg.DatabaseURL.Value)
		if err != nil {
			return err
		}
		defer db.Close()

		tables := []string{
			"river_job",
			"media_recommendations", "media_videos", "media_certifications",
			"media_production_companies", "media_keywords",
			"media_crew", "media_cast",
			"match_candidates", "media_extras", "media_assets",
			"tracks", "albums", "artists",
			"tv_episodes", "tv_seasons", "tv_series",
			"movies", "books", "authors",
			"library_files", "media_items", "libraries",
			"people", "keywords", "production_companies", "collections",
		}

		for _, t := range tables {
			_, err := db.Exec(ctx, "DELETE FROM "+t)
			if err != nil {
				ui.Warn("Failed to wipe %s: %v", t, err)
			}
		}

		ui.Success("All media data wiped. User accounts preserved.")
		return nil
	},
}

func init() {
	dbWipeCmd.Flags().Bool("force", false, "Confirm the wipe")
	rootCmd.AddCommand(dbWipeCmd)
}
