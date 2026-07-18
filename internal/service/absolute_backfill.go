package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/rs/zerolog/log"
)

// BackfillAbsoluteEpisodes resolves absolute-numbered anime files whose
// parse_result was never reconciled onto a real season/episode — series that
// were enriched before the resolve-and-store logic existed, or files that
// otherwise slipped past the enrich/match hooks. Absolute numbering is stored
// in its own parse_result field and only resolved once the episode catalog
// (tv_episodes.absolute_number) exists, so without this backfill an already-
// enriched anime series would stay unmatched-to-episode forever (nothing re-
// triggers its enrichment).
//
// Self-limiting: it only visits series whose files are still unresolved
// (ListSeriesWithUnresolvedAbsoluteFiles), and matcher.ReconcileAbsoluteEpisodes
// is idempotent, so a steady-state run does no work. Runs async at startup and
// on demand via `heya media reconcile-absolute`. Returns the number of files
// resolved.
func (a *App) BackfillAbsoluteEpisodes(ctx context.Context) (int, error) {
	q := sqlc.New(a.db)
	ids, err := q.ListSeriesWithUnresolvedAbsoluteFiles(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := matcher.ReconcileAbsoluteEpisodes(ctx, q, id.Int64)
		if err != nil {
			log.Warn().Err(err).Int64("item_id", id.Int64).Msg("backfill: reconcile absolute episodes failed")
			continue
		}
		total += n
	}
	if total > 0 {
		log.Info().Int("resolved", total).Int("series", len(ids)).Msg("backfilled absolute anime episodes")
	}
	return total, nil
}

// StartAbsoluteEpisodeBackfill runs startup reconciliation without delaying
// the worker queue. It is admitted through the App lifecycle so shutdown waits
// for the cancellation-aware backfill before closing the database.
func (a *App) StartAbsoluteEpisodeBackfill(ctx context.Context) {
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		if _, err := a.BackfillAbsoluteEpisodes(workCtx); err != nil && workCtx.Err() == nil {
			log.Warn().Err(err).Msg("startup absolute-episode backfill failed")
		}
	})
}
