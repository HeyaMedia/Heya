package generatedwrite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const pathUnlockTimeout = 5 * time.Second

// CanonicalPath returns the exact path representation used both for durable
// provenance keys and PostgreSQL advisory-lock keys.
func CanonicalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("generatedwrite: empty sidecar path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("generatedwrite: canonicalize sidecar path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	// The destination may not exist yet. Resolve the longest existing ancestor
	// so symlinked/duplicate roots share one key, while a maintenance sweep can
	// still lock and retire provenance after an entire media directory vanished.
	dir := filepath.Dir(absolute)
	suffix := []string{filepath.Base(absolute)}
	for {
		realDir, resolveErr := filepath.EvalSymlinks(dir)
		if resolveErr == nil {
			parts := append([]string{realDir}, suffix...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", fmt.Errorf("generatedwrite: resolve sidecar directory: %w", resolveErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("generatedwrite: resolve sidecar directory: %w", resolveErr)
		}
		suffix = append([]string{filepath.Base(dir)}, suffix...)
		dir = parent
	}
}

// WithPathLock serializes publication, provenance acknowledgement, and scanner
// revalidation for one canonical sidecar path across every Heya process. The
// session advisory lock is acquired and released on the same pooled connection.
// If unlock itself fails, that session is destroyed rather than returned to the
// pool carrying a leaked advisory lock.
func WithPathLock(ctx context.Context, pool *pgxpool.Pool, path string, fn func(*pgxpool.Conn) error) (returnErr error) {
	_, returnErr = withPathLock(ctx, pool, path, false, fn)
	return returnErr
}

// TryWithPathLock runs fn only when the physical path lock is immediately
// available. Cleanup uses it to stay bounded behind active publishers.
func TryWithPathLock(ctx context.Context, pool *pgxpool.Pool, path string, fn func(*pgxpool.Conn) error) (acquired bool, returnErr error) {
	return withPathLock(ctx, pool, path, true, fn)
}

func withPathLock(ctx context.Context, pool *pgxpool.Pool, path string, try bool, fn func(*pgxpool.Conn) error) (acquired bool, returnErr error) {
	if pool == nil {
		return false, errors.New("generatedwrite: path lock database unavailable")
	}
	if fn == nil {
		return false, errors.New("generatedwrite: nil path lock callback")
	}
	canonical, err := CanonicalPath(path)
	if err != nil {
		return false, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("generatedwrite: acquire path lock connection: %w", err)
	}
	releaseNormally := true
	defer func() {
		if releaseNormally {
			conn.Release()
			return
		}
		raw := conn.Hijack()
		closeCtx, cancel := context.WithTimeout(context.Background(), pathUnlockTimeout)
		defer cancel()
		_ = raw.Close(closeCtx)
	}()

	if try {
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1::text, 0))`, canonical).Scan(&acquired); err != nil {
			return false, fmt.Errorf("generatedwrite: try path advisory lock: %w", err)
		}
		if !acquired {
			return false, nil
		}
	} else if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1::text, 0))`, canonical); err != nil {
		return false, fmt.Errorf("generatedwrite: acquire path advisory lock: %w", err)
	} else {
		acquired = true
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), pathUnlockTimeout)
		defer cancel()
		var unlocked bool
		unlockErr := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1::text, 0))`, canonical).Scan(&unlocked)
		if unlockErr != nil {
			releaseNormally = false
			returnErr = errors.Join(returnErr, fmt.Errorf("generatedwrite: release path advisory lock: %w", unlockErr))
			return
		}
		if !unlocked {
			returnErr = errors.Join(returnErr, errors.New("generatedwrite: path advisory lock was not held at release"))
		}
	}()

	return true, fn(conn)
}
