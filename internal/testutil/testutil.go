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

func DatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://kura:kura@localhost:5440/kura?sslmode=disable"
	}
	return url
}

func SetupDB(t *testing.T) *pgxpool.Pool {
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
	goose.SetDialect("postgres")
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(db, "."); err != nil {
		db.Close()
		t.Fatalf("running migrations: %v", err)
	}
	db.Close()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestUserID(t *testing.T, pool *pgxpool.Pool) int64 {
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

func CleanupLibrary(t *testing.T, pool *pgxpool.Pool, libraryID int64) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, "DELETE FROM library_files WHERE library_id = $1", libraryID)
	pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", libraryID)
}
