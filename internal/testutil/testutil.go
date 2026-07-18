package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/karbowiak/heya/migrations"
	"github.com/pressly/goose/v3"
)

var (
	testUserID   int64
	testUserOnce sync.Once
)

func DatabaseURL(t testing.TB) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://heya:heya@localhost:5440/heya?sslmode=disable" //nolint:gosec // local test DB credential, not a real secret
	}
	return url
}

func SetupDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	url := DatabaseURL(t)

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		_ = db.Close()
		t.Fatalf("setting migration dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())
	// AllowMissing mirrors service.AutoMigrate: concurrent sessions race
	// migration numbers; tests must not refuse the shared dev DB over it.
	if err := goose.Up(db, ".", goose.WithAllowMissing()); err != nil {
		_ = db.Close()
		t.Fatalf("running migrations: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing migration database: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestUserID(t testing.TB, pool *pgxpool.Pool) int64 {
	t.Helper()
	testUserOnce.Do(func() {
		ctx := context.Background()
		err := pool.QueryRow(ctx,
			`INSERT INTO users (username, email, password_hash, is_admin)
			 VALUES ('test-scanner', 'test-scanner@test.local', '$2a$10$fakehash', true)
			 ON CONFLICT (username) DO UPDATE SET username = EXCLUDED.username
			 RETURNING id`,
		).Scan(&testUserID)
		if err != nil {
			panic(fmt.Sprintf("creating test user: %v", err))
		}
	})
	return testUserID
}

func CleanupLibrary(t testing.TB, pool *pgxpool.Pool, libraryID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM library_files WHERE library_id = $1", libraryID); err != nil {
		t.Errorf("cleaning up library files for library %d: %v", libraryID, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", libraryID); err != nil {
		t.Errorf("cleaning up library %d: %v", libraryID, err)
	}
}
