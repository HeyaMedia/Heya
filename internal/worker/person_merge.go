package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// mergePersonInto folds the src person row into dst. Two person rows can
// resolve to the same upstream person (the same actor scanned under name
// variants across titles); they collide on idx_people_heya_slug. Only the
// credit *links* (media_cast / media_crew) need to survive — people's derived
// children (biographies, profiles, external_credits) are ON DELETE CASCADE and
// regenerate from dst's own enrichment, and people carry no ratings/favorites.
//
// All work runs in one transaction so a failure leaves no half-moved links.
// No-op when src == dst.
func mergePersonInto(ctx context.Context, db *pgxpool.Pool, q *sqlc.Queries, dstID, srcID int64) error {
	if dstID == srcID {
		return nil
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mergePersonIntoTx(ctx, q.WithTx(tx), dstID, srcID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// mergePersonIntoTx runs the person merge inside a caller-supplied
// transaction. Split out from mergePersonInto so tests can drive it inside a
// rollback tx without committing to the database.
func mergePersonIntoTx(ctx context.Context, qtx *sqlc.Queries, dstID, srcID int64) error {
	if dstID == srcID {
		return nil
	}
	locked, err := qtx.LockPeopleForMerge(ctx, []int64{dstID, srcID})
	if err != nil {
		return fmt.Errorf("lock people: %w", err)
	}
	if len(locked) != 2 {
		return fmt.Errorf("lock people: %d of 2 people still exist", len(locked))
	}

	// Cast links: drop src rows that would collide with dst on
	// (media_item_id, character), then move the survivors.
	if err := qtx.DeleteCollidingPersonCast(ctx, sqlc.DeleteCollidingPersonCastParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("dedupe cast: %w", err)
	}
	if err := qtx.ReparentPersonCast(ctx, sqlc.ReparentPersonCastParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("reparent cast: %w", err)
	}
	// Crew links: same, keyed on (media_item_id, job).
	if err := qtx.DeleteCollidingPersonCrew(ctx, sqlc.DeleteCollidingPersonCrewParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("dedupe crew: %w", err)
	}
	if err := qtx.ReparentPersonCrew(ctx, sqlc.ReparentPersonCrewParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("reparent crew: %w", err)
	}
	// CASCADE clears src's biographies / profiles / external_credits.
	if err := qtx.DeletePerson(ctx, srcID); err != nil {
		return fmt.Errorf("delete src person: %w", err)
	}
	return nil
}
