package generatedwrite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/atomicfile"
	"github.com/karbowiak/heya/internal/nfo"
)

const (
	defaultIntentLease = 10 * time.Minute
	finalizeTimeout    = 10 * time.Second
)

type Outcome string

const (
	OutcomePublished Outcome = "published"
	OutcomeAttested  Outcome = "attested"
	OutcomeUserOwned Outcome = "user_owned"
)

type Observer interface {
	SuppressGeneratedWrite(Output) error
}

type publishResolution int

const (
	resolutionStable publishResolution = iota
	resolutionUserOwned
)

// RecoverLocked converges one previously loaded publication while its physical
// path advisory lock is held. userOwned reports that a mismatching predecessor
// was preserved/restored and provenance retired.
func RecoverLocked(ctx context.Context, conn *pgxpool.Conn, publication Publication) (resolved Publication, userOwned bool, err error) {
	resolution, resolved, err := recoverPublication(ctx, conn, publication)
	return resolved, resolution == resolutionUserOwned, err
}

// Publish persists an exact desired intent before making bytes visible. It
// takes ownership of prepared in every outcome.
func Publish(ctx context.Context, pool *pgxpool.Pool, observer Observer, prepared *Prepared) (output Output, outcome Outcome, returnErr error) {
	if pool == nil {
		return Output{}, "", errors.New("generatedwrite: publication database unavailable")
	}
	if prepared == nil || prepared.pending == nil || prepared.path == "" {
		return Output{}, "", errors.New("generatedwrite: invalid prepared sidecar")
	}
	defer func() { returnErr = errors.Join(returnErr, prepared.Discard()) }()

	err := WithPathLock(ctx, pool, prepared.path, func(conn *pgxpool.Conn) error {
		publication, exists, err := LoadPublication(ctx, conn, prepared.path)
		if err != nil {
			return err
		}
		if exists {
			resolution, resolved, resolveErr := recoverPublication(ctx, conn, publication)
			if resolveErr != nil {
				return resolveErr
			}
			if resolution == resolutionUserOwned {
				// Recovery proved that the old publication no longer owns the
				// occupant. Keep evaluating the exact desired bytes below: an
				// upgrade from releases without durable provenance may safely adopt
				// a deterministic byte-for-byte renderer match, while any other
				// occupant still wins.
				publication = Publication{Path: prepared.path}
				exists = false
			} else {
				publication = resolved
				exists = publication.Published != nil || publication.Pending != nil
			}
		}

		current, currentExists, err := observePath(prepared.path)
		if err != nil {
			return err
		}
		if currentExists && current.Matches(prepared.size, prepared.sha256) {
			// Exact deterministic renderer bytes are the only safe bootstrap for
			// sidecars written by pre-provenance Heya releases. A legacy NFO
			// inventory row is deliberately not ownership evidence: user NFOs were
			// recorded there too. Once attested, later edits break the signature
			// and immediately return the path to user ownership.
			if err := attestExact(ctx, conn, prepared.path, current); err != nil {
				return err
			}
			output = Attest(prepared.path, prepared.size, prepared.sha256)
			outcome = OutcomeAttested
			return nil
		}
		if currentExists && (publication.Published == nil || !publication.Published.Matches(current.Size, current.SHA256)) {
			if exists {
				if err := ClearPublication(ctx, conn, prepared.path); err != nil {
					return err
				}
			}
			output = Output{Path: prepared.path}
			outcome = OutcomeUserOwned
			return nil
		}

		intentID := uuid.New()
		previousPath := filepath.Join(filepath.Dir(prepared.path), ".heya-generated-"+intentID.String()+".previous")
		if err := persistIntent(ctx, conn, prepared, intentID, previousPath, time.Now().Add(defaultIntentLease)); err != nil {
			return err
		}

		written := false
		if !currentExists {
			created, publishErr := prepared.pending.PublishIfAbsent()
			if publishErr != nil {
				return publishErr
			}
			if !created {
				// An external writer won after the intent. Recovery classifies its
				// exact bytes; a mismatch is restored to ordinary user ownership.
				latest, _, loadErr := LoadPublication(ctx, conn, prepared.path)
				if loadErr != nil {
					return loadErr
				}
				resolution, _, recoverErr := recoverPublication(ctx, conn, latest)
				if recoverErr != nil {
					return recoverErr
				}
				if resolution == resolutionUserOwned {
					output = Output{Path: prepared.path}
					outcome = OutcomeUserOwned
					return nil
				}
				output = Attest(prepared.path, prepared.size, prepared.sha256)
				outcome = OutcomeAttested
				return nil
			}
			if err := prepared.pending.Commit(); err != nil {
				return err
			}
			written = true
		} else {
			// The current bytes were reverified against published provenance
			// above. Exchange retains the immediate predecessor atomically, so an
			// external replacement racing this point is never destroyed.
			if err := prepared.pending.Exchange(); err != nil {
				return err
			}
			if err := prepared.pending.RelocateExchangedPrevious(previousPath); err != nil {
				return err
			}
			predecessor, predecessorExists, observeErr := observePath(previousPath)
			if observeErr != nil {
				return observeErr
			}
			if !predecessorExists || publication.Published == nil || !publication.Published.Matches(predecessor.Size, predecessor.SHA256) {
				// A user value won between our pre-check and exchange. Put that exact
				// predecessor back, discard desired bytes, and retire provenance.
				if rollbackErr := prepared.pending.Rollback(); rollbackErr != nil {
					return rollbackErr
				}
				prepared.pending = nil
				if err := ClearPublication(ctx, conn, prepared.path); err != nil {
					return err
				}
				output = Output{Path: prepared.path}
				outcome = OutcomeUserOwned
				return nil
			}
			if err := prepared.pending.Commit(); err != nil {
				return err
			}
			written = true
		}
		prepared.pending = nil
		if err := atomicfile.SyncParent(prepared.path); err != nil {
			return err
		}

		current, currentExists, err = observePath(prepared.path)
		if err != nil {
			return err
		}
		if !currentExists || !current.Matches(prepared.size, prepared.sha256) {
			// Publication was immediately edited. It is user-owned; do not stamp
			// pending desired bytes as published provenance.
			if err := ClearPublication(ctx, conn, prepared.path); err != nil {
				return err
			}
			output = Output{Path: prepared.path}
			outcome = OutcomeUserOwned
			return nil
		}

		output = Published(prepared.path, prepared.size, prepared.sha256)
		output.Written = written
		var observerErr error
		if observer != nil {
			observerErr = observer.SuppressGeneratedWrite(output)
		}
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
		defer cancel()
		finalizeErr := finalizePending(finalizeCtx, conn, prepared.path, intentID, current)
		if finalizeErr != nil {
			return errors.Join(observerErr, finalizeErr)
		}
		if observerErr != nil {
			return fmt.Errorf("generatedwrite: register watcher publication: %w", observerErr)
		}
		outcome = OutcomePublished
		return nil
	})
	if err != nil {
		return output, outcome, err
	}
	return output, outcome, nil
}

// recoverPublication converges an interrupted intent under the path lock.
func recoverPublication(ctx context.Context, conn *pgxpool.Conn, publication Publication) (publishResolution, Publication, error) {
	current, currentExists, err := observePath(publication.Path)
	if err != nil {
		return resolutionStable, publication, err
	}
	if publication.Pending != nil {
		pending := publication.Pending
		staged, stagedExists, stagedErr := observePath(pending.StagedPath)
		if stagedErr != nil {
			return resolutionStable, publication, stagedErr
		}
		previous, previousExists, previousErr := observePath(pending.PreviousPath)
		if previousErr != nil {
			return resolutionStable, publication, previousErr
		}
		currentPending := currentExists && pending.Signature.Matches(current.Size, current.SHA256)
		currentPublished := currentExists && publication.Published != nil && publication.Published.Matches(current.Size, current.SHA256)

		if currentPending {
			predecessorPath := ""
			predecessor := Signature{}
			if previousExists {
				predecessorPath, predecessor = pending.PreviousPath, previous
			} else if stagedExists && !pending.Signature.Matches(staged.Size, staged.SHA256) {
				predecessorPath, predecessor = pending.StagedPath, staged
			}
			if predecessorPath != "" && (publication.Published == nil || !publication.Published.Matches(predecessor.Size, predecessor.SHA256)) {
				if err := atomicfile.ExchangePaths(predecessorPath, publication.Path); err != nil {
					return resolutionStable, publication, err
				}
				_ = os.Remove(predecessorPath) // desired bytes after restoring user predecessor
				_ = removeExact(pending.StagedPath, pending.Signature)
				_ = removeExact(pending.PreviousPath, pending.Signature)
				if err := atomicfile.SyncParent(publication.Path); err != nil {
					return resolutionStable, publication, err
				}
				if err := ClearPublication(ctx, conn, publication.Path); err != nil {
					return resolutionStable, publication, err
				}
				return resolutionUserOwned, Publication{}, nil
			}
			if predecessorPath != "" {
				if err := os.Remove(predecessorPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return resolutionStable, publication, err
				}
			}
			_ = removeExact(pending.StagedPath, pending.Signature)
			if err := finalizePending(ctx, conn, publication.Path, pending.ID, current); err != nil {
				return resolutionStable, publication, err
			}
			resolved, _, err := LoadPublication(ctx, conn, publication.Path)
			return resolutionStable, resolved, err
		}

		if currentPublished && stagedExists && pending.Signature.Matches(staged.Size, staged.SHA256) {
			if err := atomicfile.ExchangePaths(pending.StagedPath, publication.Path); err != nil {
				return resolutionStable, publication, err
			}
			if err := os.Rename(pending.StagedPath, pending.PreviousPath); err != nil {
				return resolutionStable, publication, err
			}
			if err := atomicfile.SyncParent(publication.Path); err != nil {
				return resolutionStable, publication, err
			}
			return recoverPublication(ctx, conn, publication)
		}

		if !currentExists && stagedExists && pending.Signature.Matches(staged.Size, staged.SHA256) {
			if err := os.Link(pending.StagedPath, publication.Path); err != nil {
				if !errors.Is(err, os.ErrExist) {
					return resolutionStable, publication, err
				}
			} else if err := os.Remove(pending.StagedPath); err != nil {
				return resolutionStable, publication, err
			}
			if err := atomicfile.SyncParent(publication.Path); err != nil {
				return resolutionStable, publication, err
			}
			return recoverPublication(ctx, conn, publication)
		}

		if currentExists && !currentPublished {
			_ = removeExact(pending.StagedPath, pending.Signature)
			_ = removeExact(pending.PreviousPath, pending.Signature)
			if publication.Published != nil {
				_ = removeExact(pending.StagedPath, *publication.Published)
				_ = removeExact(pending.PreviousPath, *publication.Published)
			}
			if err := ClearPublication(ctx, conn, publication.Path); err != nil {
				return resolutionStable, publication, err
			}
			return resolutionUserOwned, Publication{}, nil
		}

		// A missing/tampered staging file cannot be published. Retain a valid
		// published signature, but clear the abandoned pending half.
		if err := clearPending(ctx, conn, publication.Path, pending.ID); err != nil {
			return resolutionStable, publication, err
		}
		resolved, exists, err := LoadPublication(ctx, conn, publication.Path)
		if err != nil {
			return resolutionStable, publication, err
		}
		if !exists {
			return resolutionStable, Publication{Path: publication.Path}, nil
		}
		return resolutionStable, resolved, nil
	}

	if currentExists && publication.Published != nil && publication.Published.Matches(current.Size, current.SHA256) {
		return resolutionStable, publication, nil
	}
	if currentExists {
		if err := ClearPublication(ctx, conn, publication.Path); err != nil {
			return resolutionStable, publication, err
		}
		return resolutionUserOwned, Publication{}, nil
	}
	return resolutionStable, publication, nil
}

func persistIntent(ctx context.Context, conn *pgxpool.Conn, prepared *Prepared, id uuid.UUID, previousPath string, lease time.Time) error {
	libraryIDs, err := containingLibraries(ctx, conn, prepared.path)
	if err != nil {
		return err
	}
	if len(libraryIDs) == 0 {
		return errors.New("generatedwrite: destination is outside every configured library")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO generated_sidecar_publications (
			path, pending_intent_id, pending_size, pending_sha256,
			pending_staged_path, pending_previous_path, pending_lease_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (path) DO UPDATE SET
			pending_intent_id = EXCLUDED.pending_intent_id,
			pending_size = EXCLUDED.pending_size,
			pending_sha256 = EXCLUDED.pending_sha256,
			pending_staged_path = EXCLUDED.pending_staged_path,
			pending_previous_path = EXCLUDED.pending_previous_path,
			pending_lease_expires_at = EXCLUDED.pending_lease_expires_at,
			updated_at = now()
	`, prepared.path, id, prepared.size, prepared.sha256[:], prepared.pending.TempPath(), previousPath, lease)
	if err != nil {
		return fmt.Errorf("generatedwrite: persist publication intent: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM library_generated_sidecars WHERE path = $1 AND NOT (library_id = ANY($2::bigint[]))`, prepared.path, libraryIDs); err != nil {
		return err
	}
	for _, libraryID := range libraryIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO library_generated_sidecars (library_id, path) VALUES ($1, $2) ON CONFLICT DO NOTHING`, libraryID, prepared.path); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// attestExact adopts an already-visible deterministic renderer output without
// touching its inode or timestamps. This is the conservative upgrade bridge
// for sidecars produced before generated_sidecar_publications existed: the
// legacy NFO directory inventory is not ownership proof, but exact desired
// bytes at the intended path are. Memberships and the NFO change-detection
// baseline commit with the publication signature.
func attestExact(ctx context.Context, conn *pgxpool.Conn, path string, signature Signature) error {
	libraryIDs, err := containingLibraries(ctx, conn, path)
	if err != nil {
		return err
	}
	if len(libraryIDs) == 0 {
		return errors.New("generatedwrite: destination is outside every configured library")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO generated_sidecar_publications (
			path, published_size, published_mtime, published_sha256, published_at
		) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (path) DO UPDATE SET
			published_size = EXCLUDED.published_size,
			published_mtime = EXCLUDED.published_mtime,
			published_sha256 = EXCLUDED.published_sha256,
			published_at = EXCLUDED.published_at,
			pending_intent_id = NULL,
			pending_size = NULL,
			pending_sha256 = NULL,
			pending_staged_path = NULL,
			pending_previous_path = NULL,
			pending_lease_expires_at = NULL,
			updated_at = now(), verified_at = now()
	`, path, signature.Size, signature.MTime.Truncate(time.Microsecond), signature.SHA256[:])
	if err != nil {
		return fmt.Errorf("generatedwrite: attest exact publication: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM library_generated_sidecars WHERE path = $1 AND NOT (library_id = ANY($2::bigint[]))`, path, libraryIDs); err != nil {
		return err
	}
	for _, libraryID := range libraryIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO library_generated_sidecars (library_id, path) VALUES ($1, $2) ON CONFLICT DO NOTHING`, libraryID, path); err != nil {
			return err
		}
	}
	if err := baselineNFO(ctx, tx, path, signature.MTime); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func finalizePending(ctx context.Context, conn *pgxpool.Conn, path string, intentID uuid.UUID, signature Signature) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE generated_sidecar_publications SET
			published_size = pending_size,
			published_mtime = $3,
			published_sha256 = pending_sha256,
			published_at = now(),
			pending_intent_id = NULL,
			pending_size = NULL,
			pending_sha256 = NULL,
			pending_staged_path = NULL,
			pending_previous_path = NULL,
			pending_lease_expires_at = NULL,
			updated_at = now(), verified_at = now()
		WHERE path = $1 AND pending_intent_id = $2
	`, path, intentID, signature.MTime.Truncate(time.Microsecond))
	if err != nil {
		return fmt.Errorf("generatedwrite: finalize publication: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("generatedwrite: publication intent changed before finalization")
	}
	if err := baselineNFO(ctx, tx, path, signature.MTime); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func clearPending(ctx context.Context, conn *pgxpool.Conn, path string, intentID uuid.UUID) error {
	_, err := conn.Exec(ctx, `
		UPDATE generated_sidecar_publications SET
			pending_intent_id = NULL, pending_size = NULL, pending_sha256 = NULL,
			pending_staged_path = NULL, pending_previous_path = NULL,
			pending_lease_expires_at = NULL, updated_at = now()
		WHERE path = $1 AND pending_intent_id = $2 AND published_sha256 IS NOT NULL
	`, path, intentID)
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `DELETE FROM generated_sidecar_publications WHERE path = $1 AND published_sha256 IS NULL`, path)
	return err
}

func observePath(path string) (Signature, bool, error) {
	var signature Signature
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return signature, false, nil
	}
	if err != nil {
		return signature, false, err
	}
	if !info.Mode().IsRegular() {
		return signature, true, nil
	}
	file, err := os.Open(path) //nolint:gosec // path is a library sidecar selected by Heya
	if err != nil {
		return signature, true, err
	}
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil {
		return signature, true, err
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return signature, true, err
	}
	after, err := file.Stat()
	if err != nil {
		return signature, true, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return signature, true, err
	}
	if size != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !pathInfo.Mode().IsRegular() || !os.SameFile(after, pathInfo) {
		return signature, true, errors.New("generatedwrite: sidecar changed while hashing")
	}
	copy(signature.SHA256[:], hasher.Sum(nil))
	signature.Size = size
	signature.MTime = after.ModTime()
	return signature, true, nil
}

func removeExact(path string, expected Signature) error {
	if path == "" {
		return nil
	}
	current, exists, err := observePath(path)
	if err != nil || !exists || !expected.Matches(current.Size, current.SHA256) {
		return err
	}
	return os.Remove(path)
}

func containingLibraries(ctx context.Context, db DBTX, path string) ([]int64, error) {
	// DBTX intentionally exposes only QueryRow; use a JSON aggregate to keep the
	// helper usable with both a pooled connection and transaction.
	var ids []int64
	var libraryIDs []int64
	var libraryPaths [][]string
	rows, ok := db.(interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	})
	if !ok {
		return nil, errors.New("generatedwrite: database cannot list libraries")
	}
	result, err := rows.Query(ctx, `SELECT id, paths FROM libraries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	for result.Next() {
		var id int64
		var paths []string
		if err := result.Scan(&id, &paths); err != nil {
			return nil, err
		}
		libraryIDs = append(libraryIDs, id)
		libraryPaths = append(libraryPaths, paths)
	}
	if err := result.Err(); err != nil {
		return nil, err
	}
	for index, paths := range libraryPaths {
		for _, root := range paths {
			root = strings.TrimSpace(root)
			if root == "" || strings.Contains(root, "://") {
				continue
			}
			absolute, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			realRoot, err := filepath.EvalSymlinks(absolute)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(filepath.Clean(realRoot), path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				ids = append(ids, libraryIDs[index])
				break
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func baselineNFO(ctx context.Context, tx pgx.Tx, path string, mtime time.Time) error {
	_, _, isNFO := nfo.CanonicalNFO(strings.ToLower(filepath.Base(path)))
	if !isNFO {
		return nil
	}
	representative, err := firstCanonicalNFO(filepath.Dir(path))
	if err != nil || representative != path {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT library_id FROM library_generated_sidecars WHERE path = $1 ORDER BY library_id`, path)
	if err != nil {
		return err
	}
	libraryIDs := make([]int64, 0)
	for rows.Next() {
		var libraryID int64
		if err := rows.Scan(&libraryID); err != nil {
			rows.Close()
			return err
		}
		libraryIDs = append(libraryIDs, libraryID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, libraryID := range libraryIDs {
		_, err := tx.Exec(ctx, `
			INSERT INTO library_nfo_dirs (library_id, dir_path, nfo_name, mtime)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (library_id, dir_path) DO UPDATE
			SET nfo_name = EXCLUDED.nfo_name, mtime = EXCLUDED.mtime
		`, libraryID, filepath.Dir(path), filepath.Base(path), pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true})
		if err != nil {
			return err
		}
	}
	return nil
}

func firstCanonicalNFO(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, _, ok := nfo.CanonicalNFO(strings.ToLower(entry.Name())); ok {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	return filepath.Join(dir, names[0]), nil
}
