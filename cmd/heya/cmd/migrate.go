package cmd

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/karbowiak/heya/migrations"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration commands",
	Long:  "Run, rollback, or inspect database migrations.",
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Run all pending migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigrationDB()
		if err != nil {
			return err
		}
		defer db.Close()

		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("setting dialect: %w", err)
		}

		if err := goose.Up(db, "."); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}

		log.Info().Msg("migrations applied successfully")
		return nil
	},
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back one migration",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigrationDB()
		if err != nil {
			return err
		}
		defer db.Close()

		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("setting dialect: %w", err)
		}

		if err := goose.Down(db, "."); err != nil {
			return fmt.Errorf("rolling back migration: %w", err)
		}

		log.Info().Msg("migration rolled back successfully")
		return nil
	},
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigrationDB()
		if err != nil {
			return err
		}
		defer db.Close()

		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("setting dialect: %w", err)
		}

		return goose.Status(db, ".")
	},
}

var migrateResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Roll back all migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openMigrationDB()
		if err != nil {
			return err
		}
		defer db.Close()

		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("setting dialect: %w", err)
		}

		if err := goose.Reset(db, "."); err != nil {
			return fmt.Errorf("resetting migrations: %w", err)
		}

		log.Info().Msg("all migrations rolled back")
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(migrateStatusCmd)
	migrateCmd.AddCommand(migrateResetCmd)
}

func openMigrationDB() (*sql.DB, error) {
	ctx := context.Background()
	_ = ctx

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return db, nil
}
