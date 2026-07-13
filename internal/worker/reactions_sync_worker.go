package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// One-time bulk reaction push: every existing Heya heart (rating ≥9) becomes
// a love on the linked service, and thumbs-downs (≤3) become ListenBrainz
// hates. New reactions sync live via the SetUserTrackRating hook — this job
// backfills everything reacted BEFORE the account was linked. Durable and
// deduplicated while active; both services treat repeats as idempotent.

type SyncReactionsOutArgs struct {
	UserID  int64  `json:"user_id"`
	Service string `json:"service"`
}

func (SyncReactionsOutArgs) Kind() string { return "sync_reactions_out" }
func (SyncReactionsOutArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "sync_reactions_out",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type SyncReactionsOutWorker struct {
	river.WorkerDefaults[SyncReactionsOutArgs]
	DB          *pgxpool.Pool
	LastfmCreds LastfmCredsFn
	Progress    *TaskProgressBroadcaster
}

// Timeout lifts River's 1-minute default — thousands of loves at a polite
// pace take minutes.
func (w *SyncReactionsOutWorker) Timeout(*river.Job[SyncReactionsOutArgs]) time.Duration {
	return -1
}

func (w *SyncReactionsOutWorker) Work(ctx context.Context, job *river.Job[SyncReactionsOutArgs]) error {
	userID, svc := job.Args.UserID, job.Args.Service
	w.Progress.Set("listen_imports", SyncReactionsOutArgs{}.Kind(), svc+" reactions")

	var token string
	if err := w.DB.QueryRow(ctx,
		`SELECT token FROM user_music_services WHERE user_id = $1 AND service = $2 AND token <> ''`,
		userID, svc).Scan(&token); err != nil {
		return fmt.Errorf("%s is not connected", svc)
	}

	rows, err := w.DB.Query(ctx, `
		SELECT t.id, ar.name, t.title, t.recording_mbid, utr.rating
		FROM user_track_ratings utr
		JOIN tracks t ON t.id = utr.track_id
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE utr.user_id = $1 AND (utr.rating >= 9 OR utr.rating <= 3)
		ORDER BY t.id`, userID)
	if err != nil {
		return err
	}
	type reaction struct {
		artist, track, mbid string
		rating              int16
	}
	var reactions []reaction
	for rows.Next() {
		var r reaction
		var id int64
		if err := rows.Scan(&id, &r.artist, &r.track, &r.mbid, &r.rating); err == nil {
			reactions = append(reactions, r)
		}
	}
	rows.Close()

	synced, skipped := 0, 0
	var lf *scrobble.LastFM
	if svc == "lastfm" {
		key, secret := "", ""
		if w.LastfmCreds != nil {
			key, secret = w.LastfmCreds(ctx)
		}
		if key == "" || secret == "" {
			return fmt.Errorf("last.fm app credentials are not configured on the server")
		}
		lf = &scrobble.LastFM{APIKey: key, Secret: secret, SessionKey: token}
	}
	lb := &scrobble.ListenBrainz{Token: token}

	for _, r := range reactions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		switch svc {
		case "listenbrainz":
			if r.mbid == "" {
				skipped++ // LB feedback is MBID-keyed
				continue
			}
			score := 1
			if r.rating <= 3 {
				score = -1
			}
			err = lb.SubmitFeedback(ctx, r.mbid, score)
		case "lastfm":
			if r.rating <= 3 {
				skipped++ // last.fm has no dislike concept
				continue
			}
			err = lf.Love(ctx, r.artist, r.track)
		}
		if err != nil {
			// One retry, then skip — a single flaky track must not sink the run.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
			if err = retryReaction(ctx, svc, lb, lf, r.mbid, r.artist, r.track, r.rating); err != nil {
				skipped++
				continue
			}
		}
		synced++
		select { // both services rate-limit; stay polite
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}

	mergeImportState(ctx, w.DB, userID, svc, map[string]any{
		"reactions_synced": synced, "reactions_skipped": skipped,
	})
	log.Info().Str("service", svc).Int64("user", userID).Int("synced", synced).Int("skipped", skipped).
		Msg("reaction sync complete")
	return nil
}

func retryReaction(ctx context.Context, svc string, lb *scrobble.ListenBrainz, lf *scrobble.LastFM, mbid, artist, track string, rating int16) error {
	switch svc {
	case "listenbrainz":
		score := 1
		if rating <= 3 {
			score = -1
		}
		return lb.SubmitFeedback(ctx, mbid, score)
	case "lastfm":
		return lf.Love(ctx, artist, track)
	}
	return nil
}
