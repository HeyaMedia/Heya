// Package atomicfile publishes complete files without exposing partially
// written contents to readers.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var destinationLocks = struct {
	sync.Mutex
	entries map[string]*destinationLockEntry
}{entries: make(map[string]*destinationLockEntry)}

type destinationLockEntry struct {
	mutex sync.Mutex
	refs  int
}

// Pending is a same-directory temporary file that can replace its destination
// and, until Commit is called, restore the file it replaced.
//
// Callers must call Rollback in a defer immediately after creating a Pending.
// Rollback is harmless after Commit.
type Pending struct {
	destination string
	temporary   string
	backup      string
	file        *os.File
	closed      bool
	closeErr    error
	published   bool
	exchanged   bool
	done        bool
	lockKey     string
	lock        *destinationLockEntry
}

// Create reserves a unique temporary file beside destination. Keeping the
// temporary file on the same filesystem makes the eventual rename atomic.
func Create(destination string, mode fs.FileMode) (*Pending, error) {
	if destination == "" {
		return nil, errors.New("atomicfile: empty destination")
	}
	dir := filepath.Dir(destination)
	file, err := os.CreateTemp(dir, ".heya-atomic-"+filepath.Base(destination)+".*.tmp")
	if err != nil {
		return nil, fmt.Errorf("atomicfile: create temporary file: %w", err)
	}
	temporary := file.Name()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return nil, fmt.Errorf("atomicfile: set temporary file mode: %w", err)
	}
	return &Pending{destination: destination, temporary: temporary, file: file}, nil
}

// Write writes to the unpublished temporary file.
func (p *Pending) Write(body []byte) (int, error) {
	if p == nil || p.file == nil || p.closed {
		return 0, errors.New("atomicfile: temporary file is closed")
	}
	return p.file.Write(body)
}

// Close flushes and closes the temporary file. Publish refuses an open file,
// preventing callers from renaming a file while buffered producer writes can
// still arrive through its descriptor.
func (p *Pending) Close() error {
	if p == nil || p.closed {
		return nil
	}
	p.closed = true
	if err := syncAndClose(p.file); err != nil {
		p.closeErr = err
		return err
	}
	p.file = nil
	return nil
}

// Retarget changes the destination before publication. The new destination
// must share the temporary file's directory so publication remains atomic.
func (p *Pending) Retarget(destination string) error {
	if p == nil || p.done || p.published {
		return errors.New("atomicfile: cannot retarget completed publication")
	}
	if filepath.Clean(filepath.Dir(destination)) != filepath.Clean(filepath.Dir(p.temporary)) {
		return errors.New("atomicfile: destination must share temporary directory")
	}
	p.destination = destination
	return nil
}

// TempPath returns the unpublished path for validators and external producers.
func (p *Pending) TempPath() string {
	if p == nil {
		return ""
	}
	return p.temporary
}

// Publish atomically replaces the destination while retaining a private
// backup that Rollback can restore. The destination remains locked against
// other in-process atomicfile publishers until Commit or Rollback.
func (p *Pending) Publish() error {
	if p == nil || p.done {
		return errors.New("atomicfile: publication already completed")
	}
	if p.published {
		return errors.New("atomicfile: file already published")
	}
	if !p.closed {
		return errors.New("atomicfile: close temporary file before publishing")
	}
	if p.closeErr != nil {
		return fmt.Errorf("atomicfile: temporary file close failed: %w", p.closeErr)
	}

	p.lockKey = filepath.Clean(p.destination)
	p.lock = acquireDestinationLock(p.lockKey)

	if _, err := os.Stat(p.destination); err == nil {
		backup, backupErr := reservePath(filepath.Dir(p.destination), ".heya-atomic-"+filepath.Base(p.destination)+".*.previous")
		if backupErr != nil {
			p.unlock()
			return fmt.Errorf("atomicfile: reserve backup: %w", backupErr)
		}
		if err := backupFile(p.destination, backup); err != nil {
			_ = os.Remove(backup)
			p.unlock()
			return fmt.Errorf("atomicfile: preserve destination: %w", err)
		}
		p.backup = backup
	} else if !errors.Is(err, os.ErrNotExist) {
		p.unlock()
		return fmt.Errorf("atomicfile: inspect destination: %w", err)
	}

	if err := os.Rename(p.temporary, p.destination); err != nil {
		_ = os.Remove(p.backup)
		p.backup = ""
		p.unlock()
		return fmt.Errorf("atomicfile: publish: %w", err)
	}
	p.published = true
	return nil
}

// PublishIfAbsent atomically publishes the completed temporary file only when
// destination does not already exist. It never replaces source data that may
// have appeared after the caller began staging its output.
//
// The temporary and destination live in the same directory, so a hard link is
// both atomic and no-clobber: the kernel either installs the complete inode at
// destination or reports that another file already owns the name.
func (p *Pending) PublishIfAbsent() (bool, error) {
	if p == nil || p.done {
		return false, errors.New("atomicfile: publication already completed")
	}
	if p.published {
		return false, errors.New("atomicfile: file already published")
	}
	if !p.closed {
		return false, errors.New("atomicfile: close temporary file before publishing")
	}
	if p.closeErr != nil {
		return false, fmt.Errorf("atomicfile: temporary file close failed: %w", p.closeErr)
	}

	p.lockKey = filepath.Clean(p.destination)
	p.lock = acquireDestinationLock(p.lockKey)
	if err := os.Link(p.temporary, p.destination); err != nil {
		p.unlock()
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("atomicfile: publish without replacement: %w", err)
	}
	if err := os.Remove(p.temporary); err != nil {
		// Undo the destination link while the in-process destination lock is
		// still held. Deferred Rollback then owns the original temporary.
		removeErr := os.Remove(p.destination)
		p.unlock()
		return false, errors.Join(fmt.Errorf("atomicfile: remove linked temporary: %w", err), removeErr)
	}
	p.published = true
	return true, nil
}

// Exchange atomically swaps the completed temporary file with an existing
// destination. After it returns, destination contains the staged bytes and
// TempPath contains the exact predecessor. Keeping the predecessor addressable
// lets a caller validate it before Commit or restore it with Rollback.
func (p *Pending) Exchange() error {
	if p == nil || p.done {
		return errors.New("atomicfile: publication already completed")
	}
	if p.published {
		return errors.New("atomicfile: file already published")
	}
	if !p.closed {
		return errors.New("atomicfile: close temporary file before publishing")
	}
	if p.closeErr != nil {
		return fmt.Errorf("atomicfile: temporary file close failed: %w", p.closeErr)
	}

	p.lockKey = filepath.Clean(p.destination)
	p.lock = acquireDestinationLock(p.lockKey)
	if err := exchangePaths(p.temporary, p.destination); err != nil {
		p.unlock()
		return fmt.Errorf("atomicfile: exchange publication: %w", err)
	}
	p.published = true
	p.exchanged = true
	return nil
}

// RelocateExchangedPrevious moves the displaced predecessor to another unique
// name in the same directory while retaining Rollback/Commit ownership.
func (p *Pending) RelocateExchangedPrevious(path string) error {
	if p == nil || !p.exchanged || !p.published || p.done {
		return errors.New("atomicfile: no exchanged predecessor to relocate")
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(filepath.Dir(p.temporary)) {
		return errors.New("atomicfile: predecessor path must share temporary directory")
	}
	if err := os.Rename(p.temporary, path); err != nil {
		return fmt.Errorf("atomicfile: relocate exchanged predecessor: %w", err)
	}
	p.temporary = path
	return nil
}

// Commit accepts the published file and removes its rollback backup.
func (p *Pending) Commit() error {
	if p == nil || p.done {
		return nil
	}
	if !p.published {
		return errors.New("atomicfile: cannot commit unpublished file")
	}
	cleanupPath := p.backup
	if p.exchanged {
		cleanupPath = p.temporary
	}
	err := removeIfPresent(cleanupPath)
	if err == nil {
		p.backup = ""
	}
	// Publication is logically committed even if deleting the private backup
	// fails. DB-backed callers commit metadata before this method; allowing a
	// deferred Rollback to restore old bytes would make that durable metadata
	// point at the wrong contents. Keep backup's path for diagnostics/cleanup.
	p.done = true
	p.unlock()
	return err
}

// Rollback removes an unpublished temporary file, or restores the destination
// that existed before Publish. It is idempotent.
func (p *Pending) Rollback() error {
	if p == nil || p.done {
		return nil
	}
	var err error
	if !p.published {
		if closeErr := p.Close(); closeErr != nil {
			err = closeErr
		}
		err = errors.Join(err, removeIfPresent(p.temporary))
	} else if p.exchanged {
		if exchangeErr := exchangePaths(p.temporary, p.destination); exchangeErr != nil {
			err = fmt.Errorf("atomicfile: restore exchanged destination: %w", exchangeErr)
		} else {
			err = removeIfPresent(p.temporary)
		}
	} else if p.backup != "" {
		if renameErr := os.Rename(p.backup, p.destination); renameErr != nil {
			err = fmt.Errorf("atomicfile: restore destination: %w", renameErr)
		} else {
			p.backup = ""
		}
	} else {
		err = removeIfPresent(p.destination)
	}
	if err == nil {
		p.done = true
	}
	p.unlock()
	return err
}

// ExchangePaths atomically swaps two existing directory entries. It is used
// by crash reconciliation when a process died after publishing a replacement
// but before proving whether the displaced predecessor was Heya-owned.
func ExchangePaths(left, right string) error {
	if left == "" || right == "" {
		return errors.New("atomicfile: exchange path is empty")
	}
	if filepath.Clean(filepath.Dir(left)) != filepath.Clean(filepath.Dir(right)) {
		return errors.New("atomicfile: exchanged paths must share a directory")
	}
	return exchangePaths(left, right)
}

// SyncParent flushes a publication's directory entry. The staged file itself
// is synced before publication; syncing the parent closes the power-loss gap
// before durable provenance is finalized.
func SyncParent(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("atomicfile: open parent directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("atomicfile: sync parent directory: %w", err)
	}
	return nil
}

// IsInternalPath recognizes only temporary names minted by this package or by
// the generated-sidecar exchange protocol. Watchers may ignore these hidden
// implementation entries; final destination events remain independently
// signature-guarded.
func IsInternalPath(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, ".heya-atomic-") && (strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".previous")) ||
		strings.HasPrefix(name, ".heya-generated-") && strings.HasSuffix(name, ".previous")
}

func (p *Pending) unlock() {
	if p.lock != nil {
		releaseDestinationLock(p.lockKey, p.lock)
		p.lock = nil
		p.lockKey = ""
	}
}

// Write stages and atomically replaces destination after write succeeds.
func Write(destination string, mode fs.FileMode, write func(io.Writer) error) (returnErr error) {
	if write == nil {
		return errors.New("atomicfile: nil writer")
	}
	pending, err := Create(destination, mode)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, pending.Rollback()) }()

	if err := write(pending); err != nil {
		return err
	}
	if err := pending.Close(); err != nil {
		return err
	}
	if err := pending.Publish(); err != nil {
		return err
	}
	return pending.Commit()
}

// WriteIfAbsent stages complete bytes and atomically installs them without
// replacing an existing destination. created is false when another file owns
// destination; in that case the existing file is left untouched.
func WriteIfAbsent(destination string, mode fs.FileMode, write func(io.Writer) error) (created bool, returnErr error) {
	if write == nil {
		return false, errors.New("atomicfile: nil writer")
	}
	pending, err := Create(destination, mode)
	if err != nil {
		return false, err
	}
	defer func() { returnErr = errors.Join(returnErr, pending.Rollback()) }()

	if err := write(pending); err != nil {
		return false, err
	}
	if err := pending.Close(); err != nil {
		return false, err
	}
	created, err = pending.PublishIfAbsent()
	if err != nil || !created {
		return created, err
	}
	return true, pending.Commit()
}

// Copy atomically replaces destination with source.
func Copy(destination string, mode fs.FileMode, source io.Reader) (int64, error) {
	var size int64
	err := Write(destination, mode, func(writer io.Writer) error {
		var err error
		size, err = io.Copy(writer, source)
		return err
	})
	return size, err
}

// CopyIfAbsent is Copy's no-clobber counterpart.
func CopyIfAbsent(destination string, mode fs.FileMode, source io.Reader) (size int64, created bool, err error) {
	created, err = WriteIfAbsent(destination, mode, func(writer io.Writer) error {
		var copyErr error
		size, copyErr = io.Copy(writer, source)
		return copyErr
	})
	return size, created, err
}

// Reserve creates and closes a unique same-directory temporary file for a
// caller that must manage publication itself (for example, while coordinating
// an external cache lock). The caller owns removal of the returned path.
func Reserve(destination string, mode fs.FileMode) (string, error) {
	pending, err := Create(destination, mode)
	if err != nil {
		return "", err
	}
	if err := pending.Close(); err != nil {
		_ = pending.Rollback()
		return "", err
	}
	return pending.TempPath(), nil
}

// Produce reserves a unique same-directory path for a path-based producer and
// atomically publishes it only when the producer succeeds.
func Produce(destination string, mode fs.FileMode, produce func(string) error) (returnErr error) {
	if produce == nil {
		return errors.New("atomicfile: nil producer")
	}
	pending, err := Create(destination, mode)
	if err != nil {
		return err
	}
	if err := pending.Close(); err != nil {
		_ = pending.Rollback()
		return fmt.Errorf("atomicfile: close reserved file: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, pending.Rollback()) }()

	if err := produce(pending.TempPath()); err != nil {
		return err
	}
	if err := prepareProducedFile(pending.TempPath(), mode); err != nil {
		return err
	}
	if err := pending.Publish(); err != nil {
		return err
	}
	return pending.Commit()
}

func acquireDestinationLock(key string) *destinationLockEntry {
	destinationLocks.Lock()
	entry := destinationLocks.entries[key]
	if entry == nil {
		entry = &destinationLockEntry{}
		destinationLocks.entries[key] = entry
	}
	entry.refs++
	destinationLocks.Unlock()
	entry.mutex.Lock()
	return entry
}

func releaseDestinationLock(key string, entry *destinationLockEntry) {
	destinationLocks.Lock()
	entry.refs--
	entry.mutex.Unlock()
	if entry.refs == 0 {
		delete(destinationLocks.entries, key)
	}
	destinationLocks.Unlock()
}

func reservePath(dir, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func backupFile(source, destination string) error {
	if err := os.Link(source, destination); err == nil {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	in, err := os.Open(source) //nolint:gosec // source is the explicitly selected destination backup
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()) //nolint:gosec
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := syncAndClose(out); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func syncAndClose(file *os.File) error {
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("atomicfile: sync temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("atomicfile: close temporary file: %w", err)
	}
	return nil
}

func prepareProducedFile(path string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("atomicfile: inspect produced file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("atomicfile: producer output is not a regular file")
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // unique path reserved for the caller's producer
	if err != nil {
		return fmt.Errorf("atomicfile: reopen produced file: %w", err)
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("atomicfile: set produced file mode: %w", err)
	}
	if err := syncAndClose(file); err != nil {
		return err
	}
	return nil
}

func removeIfPresent(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
