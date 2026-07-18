package runtimelease

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testLockSequence atomic.Int64

func TestCoordinatorLeaseRejectsSecondContender(t *testing.T) {
	pool := coordinatorTestPool(t)
	key := uniqueTestLockID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := acquire(ctx, pool, key)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = first.Close() }()

	second, err := acquire(ctx, pool, key)
	if second != nil {
		_ = second.Close()
		t.Fatal("second contender unexpectedly acquired the coordinator lease")
	}
	if !errors.Is(err, ErrCoordinatorAlreadyRunning) {
		t.Fatalf("second acquire error = %v, want ErrCoordinatorAlreadyRunning", err)
	}
}

func TestCoordinatorLeaseDoesNotConsumePoolCapacity(t *testing.T) {
	pool := coordinatorTestPool(t)
	key := uniqueTestLockID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	before := pool.Stat().AcquiredConns()
	lease, err := acquire(ctx, pool, key)
	if err != nil {
		t.Fatalf("acquire coordinator lease: %v", err)
	}
	defer func() { _ = lease.Close() }()

	if after := pool.Stat().AcquiredConns(); after != before {
		t.Fatalf("pool acquired connections changed from %d to %d while holding direct lease", before, after)
	}
	pooled, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire ordinary pooled connection while lease held: %v", err)
	}
	pooled.Release()
}

func TestCoordinatorLeaseCloseReleasesAndIsIdempotent(t *testing.T) {
	pool := coordinatorTestPool(t)
	key := uniqueTestLockID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := acquire(ctx, pool, key)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}

	second, err := acquire(ctx, pool, key)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestCoordinatorLeaseReportsSessionLoss(t *testing.T) {
	pool := coordinatorTestPool(t)
	key := uniqueTestLockID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease, err := acquireWithHealthCheck(ctx, pool, key, 10*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("acquire coordinator lease: %v", err)
	}
	defer func() { _ = lease.Close() }()

	backendPID := lease.conn.PgConn().PID()
	var terminated bool
	if err := pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", backendPID).Scan(&terminated); err != nil {
		t.Fatalf("terminate coordinator backend %d: %v", backendPID, err)
	}
	if !terminated {
		t.Fatalf("coordinator backend %d was not terminated", backendPID)
	}

	select {
	case lossErr := <-lease.Lost():
		if !errors.Is(lossErr, ErrCoordinatorLeaseLost) {
			t.Fatalf("lease loss error = %v, want ErrCoordinatorLeaseLost", lossErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lease monitor did not report terminated PostgreSQL session")
	}

	// The dead session released the server-side lock, so a replacement can
	// claim the coordinator role. The old process must therefore exit when it
	// receives the loss notification above.
	replacement, err := acquire(ctx, pool, key)
	if err != nil {
		t.Fatalf("replacement acquire after session loss: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement close: %v", err)
	}
}

func TestCoordinatorLeaseCloseDoesNotReportLoss(t *testing.T) {
	pool := coordinatorTestPool(t)
	key := uniqueTestLockID()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease, err := acquireWithHealthCheck(ctx, pool, key, 10*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("acquire coordinator lease: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("close coordinator lease: %v", err)
	}
	select {
	case lossErr := <-lease.Lost():
		t.Fatalf("intentional close reported lease loss: %v", lossErr)
	default:
	}
}

func coordinatorTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.MaxConns = 4

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not available: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func uniqueTestLockID() int64 {
	return time.Now().UnixNano() ^ int64(os.Getpid()) ^ testLockSequence.Add(1)
}
