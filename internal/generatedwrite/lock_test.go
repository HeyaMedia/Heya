package generatedwrite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/testutil"
)

func TestWithPathLockSerializesCanonicalPathAcrossConnections(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "artist.nfo")
	equivalentPath := filepath.Join(dir, "unused", "..", "artist.nfo")

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithPathLock(ctx, pool, path, func(_ *pgxpool.Conn) error {
			close(firstEntered)
			select {
			case <-releaseFirst:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-firstEntered:
	case err := <-firstDone:
		t.Fatalf("first path lock failed before entering: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- WithPathLock(ctx, pool, equivalentPath, func(_ *pgxpool.Conn) error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("equivalent canonical path entered while first session still held the advisory lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondEntered:
	case <-ctx.Done():
		t.Fatalf("second path lock did not enter after release: %v", ctx.Err())
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}
