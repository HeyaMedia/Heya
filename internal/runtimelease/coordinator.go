// Package runtimelease owns process-lifetime PostgreSQL advisory leases used
// to enforce singleton runtime roles.
package runtimelease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCoordinatorAlreadyRunning means another process currently owns Heya's
// singleton worker-coordinator role.
var ErrCoordinatorAlreadyRunning = errors.New("worker coordinator is already running")

// ErrCoordinatorLeaseLost means the PostgreSQL session that owned the
// coordinator advisory lock ended unexpectedly. The worker must stop all
// coordinator-owned work and exit: once the session ends PostgreSQL releases
// the lock, so another process may already be taking over the role.
var ErrCoordinatorLeaseLost = errors.New("worker coordinator lease lost")

// "HEYACOOR" as a stable positive int64. PostgreSQL advisory-lock keys share
// one database-wide namespace, so keep this distinct from the migration lock.
const coordinatorAdvisoryLockID int64 = 0x48455941434f4f52

const (
	releaseTimeout           = 5 * time.Second
	leaseHealthCheckInterval = 2 * time.Second
	leaseHealthCheckTimeout  = 2 * time.Second
)

// Lease holds a session-level PostgreSQL advisory lock on a dedicated direct
// connection. It deliberately does not borrow from the application's pool: a
// process-lifetime reservation would silently reduce River's configured pool
// capacity and can deadlock jobs that need a second connection while holding a
// transaction. A process crash closes the PostgreSQL session, which releases
// the lock server-side without any recovery action.
type Lease struct {
	conn *pgx.Conn
	key  int64

	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	lost          chan error
	lossErr       error

	closeOnce sync.Once
	closeErr  error
}

// AcquireCoordinator attempts to become Heya's singleton worker coordinator.
// It never waits behind an existing coordinator; callers receive
// ErrCoordinatorAlreadyRunning immediately instead.
func AcquireCoordinator(ctx context.Context, pool *pgxpool.Pool) (*Lease, error) {
	return acquire(ctx, pool, coordinatorAdvisoryLockID)
}

func acquire(ctx context.Context, pool *pgxpool.Pool, key int64) (*Lease, error) {
	return acquireWithHealthCheck(ctx, pool, key, leaseHealthCheckInterval, leaseHealthCheckTimeout)
}

func acquireWithHealthCheck(
	ctx context.Context,
	pool *pgxpool.Pool,
	key int64,
	healthInterval time.Duration,
	healthTimeout time.Duration,
) (*Lease, error) {
	if pool == nil {
		return nil, errors.New("acquire coordinator lease: nil database pool")
	}

	// Reuse pgxpool's fully parsed connection configuration (TLS, fallbacks,
	// runtime parameters, password callback), but establish an independent
	// physical session that does not count against pool.MaxConns.
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("connect coordinator lease session: %w", err)
	}

	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		// The server may have acquired the lock even if its response was lost.
		// Destroy the session instead of leaving that ambiguity behind.
		closeErr := closeSession(conn)
		return nil, errors.Join(
			fmt.Errorf("acquire coordinator advisory lock: %w", err),
			closeErr,
		)
	}
	if !acquired {
		_ = closeSession(conn)
		return nil, ErrCoordinatorAlreadyRunning
	}

	if healthInterval <= 0 {
		healthInterval = leaseHealthCheckInterval
	}
	if healthTimeout <= 0 {
		healthTimeout = leaseHealthCheckTimeout
	}
	// Preserve tracing/request values from acquisition but make lease lifetime
	// explicit: startup cancellation must not silently disable fencing before
	// Lease.Close joins the monitor.
	monitorCtx, monitorCancel := context.WithCancel(context.WithoutCancel(ctx))
	lease := &Lease{
		conn:          conn,
		key:           key,
		monitorCancel: monitorCancel,
		monitorDone:   make(chan struct{}),
		lost:          make(chan error, 1),
	}
	//nolint:gosec // G118: process-role lease intentionally outlives the startup context; Close cancels and joins it.
	go lease.monitor(monitorCtx, healthInterval, healthTimeout)
	return lease, nil
}

// Lost receives exactly one non-nil error if the dedicated PostgreSQL session
// ends before Close. It remains silent during an intentional Close. Callers
// should treat receipt as a fencing event and terminate coordinator-owned work
// immediately because PostgreSQL has released (or is about to release) the
// advisory lock for another process.
func (l *Lease) Lost() <-chan error {
	if l == nil {
		return nil
	}
	return l.lost
}

func (l *Lease) monitor(ctx context.Context, interval, timeout time.Duration) {
	defer close(l.monitorDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Use a fresh background timeout rather than deriving it from the
		// monitor context. Close can then request shutdown without interrupting
		// a pgx operation and reusing a connection left in an ambiguous state;
		// it waits at most timeout for this final check to finish.
		checkCtx, cancel := context.WithTimeout(context.Background(), timeout)
		err := l.conn.Ping(checkCtx)
		cancel()
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return
		}

		// Fail closed. A timed-out health check may still own the lock server-
		// side, so destroy the physical session before telling the worker to
		// stand down. Conversely, if the server already ended the session, Close
		// is idempotent and simply confirms the local socket is gone.
		l.lossErr = fmt.Errorf("%w: PostgreSQL session health check failed: %w", ErrCoordinatorLeaseLost, err)
		if closeErr := closeSession(l.conn); closeErr != nil {
			l.lossErr = errors.Join(l.lossErr, fmt.Errorf("close lost coordinator session: %w", closeErr))
		}
		l.lost <- l.lossErr
		return
	}
}

// Close releases the advisory lock and closes its dedicated session. It is safe
// to call more than once. If the explicit unlock fails, ending the physical
// PostgreSQL session remains the lock-release backstop.
func (l *Lease) Close() error {
	if l == nil {
		return nil
	}

	l.closeOnce.Do(func() {
		if l.conn == nil {
			return
		}
		l.monitorCancel()
		<-l.monitorDone
		if l.lossErr != nil {
			l.closeErr = l.lossErr
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
		defer cancel()

		var unlocked bool
		if err := l.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", l.key).Scan(&unlocked); err != nil {
			// Never leave a possibly lock-owning session alive. pgx closes the
			// underlying net.Conn even if its graceful close hits the timeout.
			closeErr := closeSession(l.conn)
			l.closeErr = errors.Join(
				fmt.Errorf("release coordinator advisory lock: %w", err),
				closeErr,
			)
			return
		}
		if !unlocked {
			l.closeErr = errors.New("release coordinator advisory lock: lease was not held by its session")
		}
		if err := l.conn.Close(ctx); err != nil {
			l.closeErr = errors.Join(l.closeErr, fmt.Errorf("close coordinator lease session: %w", err))
		}
	})

	return l.closeErr
}

func closeSession(conn *pgx.Conn) error {
	if conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
	defer cancel()
	return conn.Close(ctx)
}
