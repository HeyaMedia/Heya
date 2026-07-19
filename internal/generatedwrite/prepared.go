package generatedwrite

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/karbowiak/heya/internal/atomicfile"
)

// Prepared is a fully written, synced, same-directory desired sidecar. Its
// exact signature is known before any durable intent is created. Publish takes
// ownership; callers must otherwise call Discard.
type Prepared struct {
	path    string
	size    int64
	sha256  [sha256.Size]byte
	pending *atomicfile.Pending
}

func PrepareBytes(path string, mode os.FileMode, content []byte) (*Prepared, error) {
	return prepare(path, mode, func(writer io.Writer) (int64, [sha256.Size]byte, error) {
		digest := sha256.Sum256(content)
		written, err := writer.Write(content)
		return int64(written), digest, err
	})
}

// PrepareFile snapshots source into a synced same-directory staging file. A
// later cache-file rewrite cannot change the bytes covered by the intent.
func PrepareFile(path string, mode os.FileMode, source string) (*Prepared, error) {
	input, err := os.Open(source) //nolint:gosec // source is a database-selected cached image
	if err != nil {
		return nil, err
	}
	defer func() { _ = input.Close() }()
	return prepare(path, mode, func(writer io.Writer) (int64, [sha256.Size]byte, error) {
		hasher := sha256.New()
		size, err := io.Copy(io.MultiWriter(writer, hasher), input)
		var digest [sha256.Size]byte
		copy(digest[:], hasher.Sum(nil))
		return size, digest, err
	})
}

func prepare(path string, mode os.FileMode, write func(io.Writer) (int64, [sha256.Size]byte, error)) (_ *Prepared, returnErr error) {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return nil, err
	}
	pending, err := atomicfile.Create(canonical, mode)
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, pending.Rollback())
		}
	}()
	size, digest, err := write(pending)
	if err != nil {
		return nil, err
	}
	if err := pending.Close(); err != nil {
		return nil, err
	}
	info, err := os.Stat(pending.TempPath())
	if err != nil {
		return nil, fmt.Errorf("generatedwrite: inspect staged sidecar: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return nil, errors.New("generatedwrite: staged sidecar signature changed")
	}
	return &Prepared{path: canonical, size: size, sha256: digest, pending: pending}, nil
}

func (p *Prepared) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

func (p *Prepared) Size() int64 {
	if p == nil {
		return 0
	}
	return p.size
}

func (p *Prepared) SHA256() [sha256.Size]byte {
	if p == nil {
		return [sha256.Size]byte{}
	}
	return p.sha256
}

func (p *Prepared) Discard() error {
	if p == nil || p.pending == nil {
		return nil
	}
	err := p.pending.Rollback()
	p.pending = nil
	return err
}
