package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// The recency-decayed play affinity (completed plays weighted by POWER(0.5,
// age/30d)) is the expensive core of musicAffinityCTE — ~760ms over the
// heaviest user's 66k completed plays — and Mixes for You used to recompute it
// in every one of ~11 candidate queries per cold load. It decays slowly, so it
// is materialized into user_play_affinity and refreshed lazily; musicAffinityCTE
// now reads play_aff from that table. ensureUserPlayAffinity must run before
// any musicAffinityCTE query for a user — the mixes and taste-profile entry
// points call it, and it is a cheap indexed read when the cache is warm.

// userPlayAffinityTTL bounds how stale the materialized affinity may be. New
// listening is reflected within this window, which is well inside the page's
// existing 1h response cache. Kept short so an actively-listening user still
// sees their mixes shift the same session.
const userPlayAffinityTTL = 10 * time.Minute

// userPlayAffinityRefreshSQL is the recency-decayed play-affinity computation,
// moved here from musicAffinityCTE when it became a materialized cache. Taste
// is inferred ONLY from completed plays (never skip/listen-position), and the
// implicit completion signal stays bounded by LEAST(2.0, …) so explicit
// reactions always dominate — TestMusicAffinityIgnoresSkipsAndKeepsCompletionWeak
// guards both invariants against this string.
const userPlayAffinityRefreshSQL = `
	INSERT INTO user_play_affinity (user_id, track_id, score)
	SELECT $1, pe.track_id,
	       LEAST(2.0, SUM(0.25 * POWER(0.5, EXTRACT(EPOCH FROM (now() - pe.played_at)) / 2592000.0)))
	FROM play_events pe
	WHERE pe.user_id = $1 AND pe.completed
	GROUP BY pe.track_id`

// queryRower is the read surface shared by *pgxpool.Pool and pgx.Tx, so the
// freshness probe can run either on the pool (fast path) or inside the refresh
// transaction (double-checked locking).
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func playAffinityFresh(ctx context.Context, q queryRower, userID int64, ttl time.Duration) (bool, error) {
	var fresh bool
	err := q.QueryRow(ctx, `
		SELECT COALESCE(MAX(refreshed_at) > now() - ($2::float8 * interval '1 second'), false)
		FROM user_play_affinity_state
		WHERE user_id = $1`, userID, ttl.Seconds()).Scan(&fresh)
	return fresh, err
}

// ensureUserPlayAffinity refreshes the user's materialized play affinity when it
// is missing or older than userPlayAffinityTTL. The refresh runs under a
// per-user advisory lock (double-checked against the freshness probe) so
// concurrent mixes/radio requests don't stampede the recompute, and the
// DELETE+INSERT lives in one transaction so readers never observe an empty
// window — MVCC keeps them on the previous snapshot until commit.
func (a *App) ensureUserPlayAffinity(ctx context.Context, userID int64) error {
	if fresh, err := playAffinityFresh(ctx, a.db, userID, userPlayAffinityTTL); err != nil {
		return err
	} else if fresh {
		return nil
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize refreshes per user; the lock releases on commit/rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242, $1::int)`, userID); err != nil {
		return err
	}
	// Someone may have refreshed it while we waited for the lock.
	if fresh, err := playAffinityFresh(ctx, tx, userID, userPlayAffinityTTL); err != nil {
		return err
	} else if fresh {
		return nil
	}

	if _, err := tx.Exec(ctx, `DELETE FROM user_play_affinity WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, userPlayAffinityRefreshSQL, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_play_affinity_state (user_id, refreshed_at)
		VALUES ($1, now())
		ON CONFLICT (user_id) DO UPDATE SET refreshed_at = now()`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
