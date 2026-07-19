package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPendingRollbackRestoresPreviousFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cover.jpg")
	if err := os.WriteFile(destination, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	pending, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(pending, "new"); err != nil {
		t.Fatal(err)
	}
	if err := pending.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Publish(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("restored contents = %q, want old", got)
	}
	assertNoTemporaryFiles(t, filepath.Dir(destination))
}

func TestPendingCommitKeepsReplacement(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cover.jpg")
	if err := os.WriteFile(destination, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	pending, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(pending, "new"); err != nil {
		t.Fatal(err)
	}
	if err := pending.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Publish(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("committed contents = %q, want new", got)
	}
	assertNoTemporaryFiles(t, filepath.Dir(destination))
}

func TestCommitCleanupFailureCannotRollbackPublishedBytes(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(destination, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	pending, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(pending, "new")
	if err := pending.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pending.Publish(); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(pending.backup)
	unremovable := filepath.Join(dir, "non-empty-backup")
	if err := os.Mkdir(unremovable, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unremovable, "child"), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	pending.backup = unremovable
	if err := pending.Commit(); err == nil {
		t.Fatal("Commit unexpectedly ignored backup cleanup failure")
	}
	if err := pending.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("contents after committed cleanup failure = %q, want new", got)
	}
}

func TestWritePreservesDestinationWhenWriterFails(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "metadata.nfo")
	if err := os.WriteFile(destination, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("write failed")
	err := Write(destination, 0o644, func(writer io.Writer) error {
		_, _ = io.WriteString(writer, "partial")
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Write error = %v, want %v", err, wantErr)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("contents = %q, want old", got)
	}
	assertNoTemporaryFiles(t, filepath.Dir(destination))
}

func TestWriteIfAbsentNeverReplacesDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "metadata.nfo")
	if err := os.WriteFile(destination, []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := WriteIfAbsent(destination, 0o644, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "generated")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("WriteIfAbsent reported replacing an existing destination")
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "user-owned" {
		t.Fatalf("destination contents = %q, want user-owned", body)
	}
	assertNoTemporaryFiles(t, filepath.Dir(destination))
}

func TestWriteIfAbsentPublishesCompleteNewFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "metadata.nfo")
	created, err := WriteIfAbsent(destination, 0o644, func(writer io.Writer) error {
		_, err := io.WriteString(writer, "generated")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("WriteIfAbsent did not create an absent destination")
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "generated" {
		t.Fatalf("destination contents = %q, want generated", body)
	}
	assertNoTemporaryFiles(t, filepath.Dir(destination))
}

func TestInternalPathPredicateIsNarrow(t *testing.T) {
	for _, path := range []string{
		"/library/.heya-atomic-album.nfo.123.tmp",
		"/library/.heya-atomic-cover.jpg.456.previous",
		"/library/.heya-generated-550e8400-e29b-41d4-a716-446655440000.previous",
	} {
		if !IsInternalPath(path) {
			t.Fatalf("expected internal path: %s", path)
		}
	}
	for _, path := range []string{"/library/album.nfo", "/library/.album.nfo.tmp", "/library/.heya-not-ours.previous"} {
		if IsInternalPath(path) {
			t.Fatalf("unexpected internal path: %s", path)
		}
	}
}

func TestProduceRejectsMissingOrNonRegularOutput(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "output.bin")
	if err := Produce(destination, 0o640, os.Remove); err == nil {
		t.Fatal("Produce accepted missing output")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination exists after missing output: %v", err)
	}
}

func TestCreateUsesUniqueSameDirectoryTemporaryFiles(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cache.bin")
	one, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = one.Rollback() }()
	two, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = two.Rollback() }()
	if one.TempPath() == two.TempPath() {
		t.Fatal("temporary paths collided")
	}
	if filepath.Dir(one.TempPath()) != filepath.Dir(destination) || filepath.Dir(two.TempPath()) != filepath.Dir(destination) {
		t.Fatal("temporary path is not beside destination")
	}
}

func TestPublishRequiresClosedTemporaryFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cache.bin")
	pending, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pending.Rollback() }()
	if err := pending.Publish(); err == nil {
		t.Fatal("Publish succeeded with an open temporary file")
	}
}

func TestConcurrentPublishRollbackRestoresImmediatePredecessor(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cover.jpg")
	if err := os.WriteFile(destination, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	first, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback() }()
	_, _ = io.WriteString(first, "first")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Create(destination, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback() }()
	_, _ = io.WriteString(second, "second")
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Publish(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- second.Publish()
	}()
	<-started
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := second.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("contents after second rollback = %q, want first", got)
	}
	destinationLocks.Lock()
	defer destinationLocks.Unlock()
	if len(destinationLocks.entries) != 0 {
		t.Fatalf("destination lock entries leaked: %d", len(destinationLocks.entries))
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name()[0] == '.' {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}
