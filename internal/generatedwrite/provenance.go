package generatedwrite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type Signature struct {
	Size   int64
	MTime  time.Time
	SHA256 [sha256.Size]byte
}

func (s Signature) Matches(size int64, digest [sha256.Size]byte) bool {
	return s.Size == size && s.SHA256 == digest
}

type PendingIntent struct {
	ID             uuid.UUID
	Signature      Signature
	StagedPath     string
	PreviousPath   string
	LeaseExpiresAt time.Time
}

type Publication struct {
	Path       string
	Published  *Signature
	Pending    *PendingIntent
	VerifiedAt time.Time
}

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func LoadPublication(ctx context.Context, db DBTX, path string) (Publication, bool, error) {
	var (
		publishedSize   pgtype.Int8
		publishedMTime  pgtype.Timestamptz
		publishedDigest []byte
		pendingID       pgtype.UUID
		pendingSize     pgtype.Int8
		pendingDigest   []byte
		stagedPath      pgtype.Text
		previousPath    pgtype.Text
		lease           pgtype.Timestamptz
		verifiedAt      time.Time
	)
	err := db.QueryRow(ctx, `
		SELECT published_size, published_mtime, published_sha256,
		       pending_intent_id, pending_size, pending_sha256,
		       pending_staged_path, pending_previous_path,
		       pending_lease_expires_at, verified_at
		FROM generated_sidecar_publications
		WHERE path = $1
	`, path).Scan(
		&publishedSize, &publishedMTime, &publishedDigest,
		&pendingID, &pendingSize, &pendingDigest,
		&stagedPath, &previousPath, &lease, &verifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Publication{}, false, nil
	}
	if err != nil {
		return Publication{}, false, fmt.Errorf("generatedwrite: load publication: %w", err)
	}
	publication := Publication{Path: path, VerifiedAt: verifiedAt}
	if publishedSize.Valid {
		if !publishedMTime.Valid || len(publishedDigest) != sha256.Size {
			return Publication{}, false, errors.New("generatedwrite: corrupt published signature")
		}
		var digest [sha256.Size]byte
		copy(digest[:], publishedDigest)
		publication.Published = &Signature{Size: publishedSize.Int64, MTime: publishedMTime.Time, SHA256: digest}
	}
	if pendingID.Valid {
		if !pendingSize.Valid || len(pendingDigest) != sha256.Size || !stagedPath.Valid || !previousPath.Valid || !lease.Valid {
			return Publication{}, false, errors.New("generatedwrite: corrupt pending intent")
		}
		var digest [sha256.Size]byte
		copy(digest[:], pendingDigest)
		publication.Pending = &PendingIntent{
			ID:             uuid.UUID(pendingID.Bytes),
			Signature:      Signature{Size: pendingSize.Int64, SHA256: digest},
			StagedPath:     stagedPath.String,
			PreviousPath:   previousPath.String,
			LeaseExpiresAt: lease.Time,
		}
	}
	return publication, true, nil
}

func ClearPublication(ctx context.Context, db DBTX, path string) error {
	_, err := db.Exec(ctx, `DELETE FROM generated_sidecar_publications WHERE path = $1`, path)
	if err != nil {
		return fmt.Errorf("generatedwrite: clear publication: %w", err)
	}
	return nil
}

// ClearPublicationIfUnchanged retires only the exact state a scanner observed.
// It prevents a direct/concurrent provenance repair from being erased even if
// that writer failed to participate in the advisory-lock protocol.
func ClearPublicationIfUnchanged(ctx context.Context, db DBTX, publication Publication) (bool, error) {
	var publishedDigest []byte
	if publication.Published != nil {
		publishedDigest = publication.Published.SHA256[:]
	}
	var pendingID *uuid.UUID
	if publication.Pending != nil {
		id := publication.Pending.ID
		pendingID = &id
	}
	tag, err := db.Exec(ctx, `
		DELETE FROM generated_sidecar_publications
		WHERE path = $1
		  AND published_sha256 IS NOT DISTINCT FROM $2::bytea
		  AND pending_intent_id IS NOT DISTINCT FROM $3::uuid
	`, publication.Path, publishedDigest, pendingID)
	if err != nil {
		return false, fmt.Errorf("generatedwrite: compare-delete publication: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
