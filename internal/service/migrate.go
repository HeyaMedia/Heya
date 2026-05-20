package service

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/karbowiak/heya/migrations"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

func AutoMigrate(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	goose.SetLogger(goose.NopLogger())

	current, _ := goose.GetDBVersion(db)

	if err := goose.Up(db, "."); err != nil {
		return err
	}

	after, _ := goose.GetDBVersion(db)
	if after > current {
		log.Info().Int64("from", current).Int64("to", after).Msg("database migrations applied")
	}

	return nil
}
