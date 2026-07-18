package service

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/karbowiak/heya/migrations"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
)

// All Heya processes auto-migrate on boot. A session-level advisory lock
// serializes goose across the API and worker deployments; without it both can
// execute the same DDL after reading the same version, which is how migration
// 00056 raced on CREATE TABLE's implicit composite type.
const migrationAdvisoryLockID int64 = 0x484559414d494752 // "HEYAMIGR"

func AutoMigrate(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	goose.SetLogger(goose.NopLogger())

	lockCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	lockConn, err := db.Conn(lockCtx)
	if err != nil {
		return err
	}
	defer func() { _ = lockConn.Close() }()
	if _, err := lockConn.ExecContext(lockCtx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		return err
	}
	defer func() {
		_, _ = lockConn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID)
	}()

	current, _ := goose.GetDBVersion(db)

	// AllowMissing: concurrent dev sessions race migration numbers (a
	// renumbered file can land below an already-recorded version). Apply
	// the stragglers out of order instead of refusing to boot — every
	// migration here is written to be independently applicable, and the
	// pre-alpha consolidation pass squashes the numbering anyway.
	if err := goose.Up(db, ".", goose.WithAllowMissing()); err != nil {
		return err
	}

	after, _ := goose.GetDBVersion(db)
	if after > current {
		log.Info().Int64("from", current).Int64("to", after).Msg("database migrations applied")
	}

	return nil
}
