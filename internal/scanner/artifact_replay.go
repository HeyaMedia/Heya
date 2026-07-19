package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type scannerApplyGuard struct {
	preflight func(context.Context) error
	commit    func(context.Context, pgx.Tx) error
}
type scannerApplyCommitGuardContextKey struct{}

// WithScannerApplyCommitGuard installs a pipeline-owned validation hook that
// every media-domain apply transaction runs immediately before its writes and
// again immediately before commit. This closes the long materialize/apply
// window where a replaced file could otherwise be committed from a stale
// artifact. Standalone/fixture apply callers remain unchanged when no guard is
// installed.
func WithScannerApplyCommitGuard(ctx context.Context, guard func(context.Context) error) context.Context {
	if guard == nil {
		return ctx
	}
	return context.WithValue(ctx, scannerApplyCommitGuardContextKey{}, scannerApplyGuard{
		preflight: guard,
		commit: func(guardCtx context.Context, _ pgx.Tx) error {
			return guard(guardCtx)
		},
	})
}

// WithScannerApplyTransactionGuard separates the cheap preflight validation
// from the authoritative commit check. The latter receives the domain apply
// transaction and must lock scanner lineage through commit. Keeping that lock
// until the end also preserves the review path's identity -> scanner-entity
// lock order: domain apply performs canonical identity work first, then takes
// the scanner lock only at commit.
func WithScannerApplyTransactionGuard(
	ctx context.Context,
	preflight func(context.Context) error,
	commit func(context.Context, pgx.Tx) error,
) context.Context {
	if preflight == nil && commit == nil {
		return ctx
	}
	return context.WithValue(ctx, scannerApplyCommitGuardContextKey{}, scannerApplyGuard{
		preflight: preflight,
		commit:    commit,
	})
}

func runScannerApplyPreflightGuard(ctx context.Context) error {
	guard, _ := ctx.Value(scannerApplyCommitGuardContextKey{}).(scannerApplyGuard)
	if guard.preflight == nil {
		return nil
	}
	return guard.preflight(ctx)
}

func runScannerApplyCommitGuard(ctx context.Context, tx pgx.Tx) error {
	guard, _ := ctx.Value(scannerApplyCommitGuardContextKey{}).(scannerApplyGuard)
	if guard.commit == nil {
		return nil
	}
	return guard.commit(ctx, tx)
}

// ArtifactReplayError means a retained local analysis is no longer a safe
// input to the current matcher. The caller should enqueue fresh scoped
// analysis instead of retrying the artifact unchanged.
type ArtifactReplayError struct {
	Reason string
	Path   string
}

func (e *ArtifactReplayError) Error() string {
	if e.Path == "" {
		return "scanner analysis artifact requires fresh analysis: " + e.Reason
	}
	return fmt.Sprintf("scanner analysis artifact requires fresh analysis: %s (%s)", e.Reason, e.Path)
}

// ValidateScannerAnalysisArtifactReplay checks both semantic compatibility
// and the exact source observations captured by analysis. Artifact schema
// compatibility alone is not enough: replaying an artifact after a tag/NFO
// removal or an audio replacement can resurrect the stale identity.
func ValidateScannerAnalysisArtifactReplay(artifact sqlc.ScannerEntityArtifact) error {
	return ValidateScannerAnalysisArtifactReplayWithDB(context.Background(), nil, artifact)
}

// ValidateScannerAnalysisArtifactReplayWithDB additionally recognizes exact
// generated-sidecar provenance while checking the owner source set. Production
// resumptions must use this form so a Heya NFO/art publication between stages
// does not invalidate its own pipeline generation.
func ValidateScannerAnalysisArtifactReplayWithDB(ctx context.Context, db *pgxpool.Pool, artifact sqlc.ScannerEntityArtifact) error {
	if artifact.Stage != scanArtifactKindAnalyze {
		return &ArtifactReplayError{Reason: "artifact is not an analysis result"}
	}
	return ValidateScannerArtifactSourcesWithDB(ctx, db, artifact)
}

// ValidateScannerArtifactSources applies the replay contract to every durable
// pipeline hand-off. Search, fetch, and apply artifacts all carry the original
// inventory snapshot; checking only the first resume would still allow a file
// to be replaced while remote metadata is parked.
func ValidateScannerArtifactSources(artifact sqlc.ScannerEntityArtifact) error {
	return ValidateScannerArtifactSourcesWithDB(context.Background(), nil, artifact)
}

// ValidateScannerArtifactSourcesWithDB applies provenance-aware source-set
// validation. DB-less callers remain conservative and treat a newly appearing
// sidecar as user input because they cannot prove otherwise.
func ValidateScannerArtifactSourcesWithDB(ctx context.Context, db *pgxpool.Pool, artifact sqlc.ScannerEntityArtifact) error {
	var envelope scanRunArtifact
	if err := json.Unmarshal(artifact.Data, &envelope); err != nil {
		return &ArtifactReplayError{Reason: "artifact payload is unreadable"}
	}
	if envelope.SchemaVersion != int(scanArtifactSchemaV1) {
		return &ArtifactReplayError{Reason: fmt.Sprintf("unsupported schema version %d", envelope.SchemaVersion)}
	}
	if envelope.PipelineRevision != scanArtifactPipelineRevision {
		return &ArtifactReplayError{Reason: fmt.Sprintf("pipeline revision %d is not current revision %d", envelope.PipelineRevision, scanArtifactPipelineRevision)}
	}
	for _, root := range envelope.Inventory.Roots {
		for _, file := range root.Files {
			info, err := os.Stat(file.Path)
			if err != nil {
				if os.IsNotExist(err) {
					return &ArtifactReplayError{Reason: "source disappeared", Path: file.Path}
				}
				return &ArtifactReplayError{Reason: "source cannot be inspected", Path: file.Path}
			}
			if info.Size() != file.Size {
				return &ArtifactReplayError{Reason: "source size changed", Path: file.Path}
			}
			if !file.MTime.IsZero() && !sameArtifactSourceTime(info.ModTime(), file.MTime) {
				return &ArtifactReplayError{Reason: "source modification time changed", Path: file.Path}
			}
			if file.SourceSHA256 != "" {
				expected, err := hex.DecodeString(file.SourceSHA256)
				if err != nil || len(expected) != sha256.Size {
					return &ArtifactReplayError{Reason: "sidecar source signature is invalid", Path: file.Path}
				}
				input, err := os.Open(file.Path) //nolint:gosec // artifact path came from the library inventory
				if err != nil {
					return &ArtifactReplayError{Reason: "sidecar source cannot be opened", Path: file.Path}
				}
				hash := sha256.New()
				_, copyErr := io.Copy(hash, input)
				closeErr := input.Close()
				if copyErr != nil || closeErr != nil {
					return &ArtifactReplayError{Reason: "sidecar source cannot be hashed", Path: file.Path}
				}
				if !equalBytes(hash.Sum(nil), expected) {
					reason := "sidecar source content changed"
					if file.Generated {
						reason = "generated source content changed"
					}
					return &ArtifactReplayError{Reason: reason, Path: file.Path}
				}
			}
		}
	}
	return validateArtifactSourceSet(ctx, db, envelope.SourceSet)
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}

func sameArtifactSourceTime(left, right time.Time) bool {
	return left.Truncate(time.Microsecond).Equal(right.Truncate(time.Microsecond))
}
