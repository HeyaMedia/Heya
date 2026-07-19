package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/generatedwrite"
)

type generatedSidecarRef struct {
	rootIndex int
	fileIndex int
}

type generatedSidecarCandidate struct {
	path string
	refs []generatedSidecarRef
}

// markGeneratedSidecars hashes every identity-affecting sidecar and retains it
// in the inventory snapshot. Exact published OR pending Heya bytes are marked
// Generated and excluded from matcher evidence. A value matching neither is
// user input; a live pending lease requests retry, while an expired intent is
// retired and the stable current bytes are used immediately.
func markGeneratedSidecars(ctx context.Context, db *pgxpool.Pool, libraryID int64, inv *Inventory) (int, error) {
	return markGeneratedSidecarsWithHooks(ctx, db, libraryID, inv, generatedSidecarHooks{})
}

func markGeneratedSidecarsWithHook(ctx context.Context, db *pgxpool.Pool, libraryID int64, inv *Inventory, beforeCompareDelete func()) (int, error) {
	return markGeneratedSidecarsWithHooks(ctx, db, libraryID, inv, generatedSidecarHooks{beforeCompareDelete: beforeCompareDelete})
}

type generatedSidecarHooks struct {
	beforePathLock      func()
	beforeCompareDelete func()
}

func markGeneratedSidecarsWithHooks(ctx context.Context, db *pgxpool.Pool, libraryID int64, inv *Inventory, hooks generatedSidecarHooks) (int, error) {
	if inv == nil {
		return 0, nil
	}
	marked := 0
	lockHookPending := hooks.beforePathLock != nil
	compareHookPending := hooks.beforeCompareDelete != nil
	for _, candidate := range generatedSidecarCandidates(*inv) {
		first := candidate.refs[0]
		firstRoot := &inv.Roots[first.rootIndex]
		firstFile := &firstRoot.Files[first.fileIndex]
		for _, ref := range candidate.refs {
			file := &inv.Roots[ref.rootIndex].Files[ref.fileIndex]
			file.Generated = false
			file.SourceSHA256 = ""
			if err := validateInventoryFileStat(inv.Roots[ref.rootIndex].FS, file.RelPath, file.Size, file.MTime); err != nil {
				return marked, fmt.Errorf("sidecar changed since inventory; retry analysis: %w", err)
			}
		}
		digest, err := hashStableInventoryFile(firstRoot.FS, firstFile.RelPath, firstFile.Size, firstFile.MTime)
		if err != nil {
			return marked, fmt.Errorf("snapshot sidecar %s: %w", firstFile.RelPath, err)
		}
		encoded := hex.EncodeToString(digest[:])
		for _, ref := range candidate.refs {
			inv.Roots[ref.rootIndex].Files[ref.fileIndex].SourceSHA256 = encoded
		}
		if db == nil {
			continue
		}
		if lockHookPending {
			lockHookPending = false
			hooks.beforePathLock()
		}

		err = generatedwrite.WithPathLock(ctx, db, candidate.path, func(conn *pgxpool.Conn) error {
			// Revalidate the physical path after waiting for a publisher lock.
			if err := validateInventoryFileStat(firstRoot.FS, firstFile.RelPath, firstFile.Size, firstFile.MTime); err != nil {
				return fmt.Errorf("sidecar changed since inventory; retry analysis: %w", err)
			}
			// Size+mtime are not content identity: a user can preserve both while
			// this scanner waits behind a publisher. Rehash under the acquired
			// path lock and use only that digest for classification/artifacts.
			lockedDigest, err := hashStableInventoryFile(firstRoot.FS, firstFile.RelPath, firstFile.Size, firstFile.MTime)
			if err != nil {
				return fmt.Errorf("sidecar changed while waiting for publication lock: %w", err)
			}
			digest = lockedDigest
			encoded = hex.EncodeToString(digest[:])
			for _, ref := range candidate.refs {
				inv.Roots[ref.rootIndex].Files[ref.fileIndex].SourceSHA256 = encoded
			}
			publication, exists, err := generatedwrite.LoadPublication(ctx, conn, candidate.path)
			if err != nil || !exists {
				return err
			}
			if publication.Pending != nil && !time.Now().Before(publication.Pending.LeaseExpiresAt) {
				resolved, userOwned, recoverErr := generatedwrite.RecoverLocked(ctx, conn, publication)
				if recoverErr != nil {
					return recoverErr
				}
				publication = resolved
				if err := validateInventoryFileStat(firstRoot.FS, firstFile.RelPath, firstFile.Size, firstFile.MTime); err != nil {
					return fmt.Errorf("expired sidecar intent changed source; retry analysis: %w", err)
				}
				freshDigest, hashErr := hashStableInventoryFile(firstRoot.FS, firstFile.RelPath, firstFile.Size, firstFile.MTime)
				if hashErr != nil || freshDigest != digest {
					return fmt.Errorf("expired sidecar intent changed content; retry analysis")
				}
				if userOwned {
					return nil
				}
			}
			matchesPublished := publication.Published != nil && publication.Published.Matches(firstFile.Size, digest)
			matchesPending := publication.Pending != nil && publication.Pending.Signature.Matches(firstFile.Size, digest)
			if matchesPublished || matchesPending {
				// Backfill a newly-added duplicate/nested library without making
				// membership part of the physical provenance lookup.
				if libraryID > 0 {
					if _, err := conn.Exec(ctx, `
						INSERT INTO library_generated_sidecars (library_id, path)
						VALUES ($1, $2) ON CONFLICT DO NOTHING
					`, libraryID, candidate.path); err != nil {
						return err
					}
				}
				for _, ref := range candidate.refs {
					inv.Roots[ref.rootIndex].Files[ref.fileIndex].Generated = true
				}
				return nil
			}
			if publication.Pending != nil && time.Now().Before(publication.Pending.LeaseExpiresAt) {
				return fmt.Errorf("generated sidecar publication is pending; retry analysis after %s", publication.Pending.LeaseExpiresAt.Format(time.RFC3339))
			}
			if compareHookPending {
				compareHookPending = false
				hooks.beforeCompareDelete()
			}
			deleted, err := generatedwrite.ClearPublicationIfUnchanged(ctx, conn, publication)
			if err != nil {
				return err
			}
			if !deleted {
				return fmt.Errorf("generated sidecar provenance changed concurrently; retry analysis")
			}
			return nil
		})
		if err != nil {
			return marked, err
		}
		if inv.Roots[first.rootIndex].Files[first.fileIndex].Generated {
			marked += len(candidate.refs)
		}
	}
	return marked, nil
}

func generatedSidecarCandidates(inv Inventory) []generatedSidecarCandidate {
	byPath := make(map[string][]generatedSidecarRef)
	for rootIndex := range inv.Roots {
		for fileIndex := range inv.Roots[rootIndex].Files {
			file := inv.Roots[rootIndex].Files[fileIndex]
			if file.Class != ClassNFO && file.Class != ClassArtwork && file.Class != ClassPlexmatch {
				continue
			}
			path, err := generatedwrite.CanonicalPath(file.Path)
			if err != nil {
				path = filepath.Clean(file.Path)
			}
			byPath[path] = append(byPath[path], generatedSidecarRef{rootIndex: rootIndex, fileIndex: fileIndex})
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	candidates := make([]generatedSidecarCandidate, 0, len(paths))
	for _, path := range paths {
		candidates = append(candidates, generatedSidecarCandidate{path: path, refs: byPath[path]})
	}
	return candidates
}

func validateInventoryFileStat(fsys fs.FS, relPath string, expectedSize int64, expectedMTime time.Time) error {
	if fsys == nil {
		return fmt.Errorf("inventory filesystem is unavailable")
	}
	info, err := fs.Stat(fsys, relPath)
	if err != nil {
		return err
	}
	if info.Size() != expectedSize || !sameGeneratedSidecarTime(info.ModTime(), expectedMTime) {
		return fmt.Errorf("size or modification time changed")
	}
	return nil
}

func hashStableInventoryFile(fsys fs.FS, relPath string, expectedSize int64, expectedMTime time.Time) (digest [sha256.Size]byte, returnErr error) {
	if fsys == nil {
		return digest, fmt.Errorf("inventory filesystem is unavailable")
	}
	file, err := fsys.Open(relPath)
	if err != nil {
		return digest, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
	}()
	before, err := fs.Stat(fsys, relPath)
	if err != nil {
		return digest, err
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return digest, err
	}
	after, err := fs.Stat(fsys, relPath)
	if err != nil {
		return digest, err
	}
	if written != expectedSize || before.Size() != expectedSize || after.Size() != expectedSize ||
		!sameGeneratedSidecarTime(before.ModTime(), expectedMTime) ||
		!sameGeneratedSidecarTime(after.ModTime(), expectedMTime) {
		return digest, fmt.Errorf("sidecar changed while validating provenance")
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func sameGeneratedSidecarTime(left, right time.Time) bool {
	return left.Truncate(time.Microsecond).Equal(right.Truncate(time.Microsecond))
}
