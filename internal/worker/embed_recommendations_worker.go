package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// EmbedBackfillFn runs the recommendations-embedding backfill without the
// worker package importing service/ (same indirection as SonicEnabledFn).
// Implementations must treat "engine disabled" as a clean no-op, not an error.
type EmbedBackfillFn func(ctx context.Context, force bool) (embedded, skipped int, err error)

// KickoffEmbedRecommendationsWorker is the scheduled self-heal sweep for
// recommendation embeddings: every item/episode/canonical-recording doc is
// recomposed and hash-compared against what its stored embedding was computed
// from, so metadata changes re-embed on the next run.
// An unchanged library costs a few queries and zero model inference.
type KickoffEmbedRecommendationsWorker struct {
	river.WorkerDefaults[KickoffEmbedRecommendationsArgs]
	DB            *pgxpool.Pool
	EmbedBackfill EmbedBackfillFn
	Progress      *TaskProgressBroadcaster
}

// Timeout lifts River's default 1-minute job deadline — a first full embed of
// a large library legitimately runs for many minutes.
func (w *KickoffEmbedRecommendationsWorker) Timeout(*river.Job[KickoffEmbedRecommendationsArgs]) time.Duration {
	return -1
}

func (w *KickoffEmbedRecommendationsWorker) Work(ctx context.Context, job *river.Job[KickoffEmbedRecommendationsArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	w.Progress.Set("embed_recommendations", KickoffEmbedRecommendationsArgs{}.Kind(), "recommendation embeddings")

	if w.EmbedBackfill == nil { // tests / partial wiring
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, nil)
		return nil
	}
	embedded, skippedDocs, err := w.EmbedBackfill(ctx, false)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, embedded, skippedDocs, err)
		return err
	}
	finishKickoff(ctx, q, taskID, startedAt, embedded, skippedDocs, nil)
	log.Info().Int("embedded", embedded).Int("skipped", skippedDocs).Msg("embed_recommendations: complete")
	return nil
}
