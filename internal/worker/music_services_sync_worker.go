package worker

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Daily music-services sync (scheduled task sync_music_services): pulls the
// last day of listens for every linked ListenBrainz/Last.fm account by
// enqueueing incremental kickoff_listen_import jobs, then retro-matches
// stored unmatched external_listens against the library — new music adopts
// its historical listens without re-fetching anything.

type KickoffMusicServicesSyncArgs struct {
	ScheduledTaskID string `json:"scheduled_task_id,omitempty"`
}

func (KickoffMusicServicesSyncArgs) Kind() string { return "kickoff_music_services_sync" }
func (KickoffMusicServicesSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_music_services_sync",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

type KickoffMusicServicesSyncWorker struct {
	river.WorkerDefaults[KickoffMusicServicesSyncArgs]
	DB          *pgxpool.Pool
	LastfmCreds LastfmCredsFn
	Progress    *TaskProgressBroadcaster
}

// Timeout lifts River's 1-minute default — retro-matching sweeps scale with
// the unmatched backlog.
func (w *KickoffMusicServicesSyncWorker) Timeout(*river.Job[KickoffMusicServicesSyncArgs]) time.Duration {
	return -1
}

func (w *KickoffMusicServicesSyncWorker) Work(ctx context.Context, job *river.Job[KickoffMusicServicesSyncArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	q := sqlc.New(w.DB)
	w.Progress.Set("sync_music_services", KickoffMusicServicesSyncArgs{}.Kind(), "linked accounts")

	// 25h lookback: an hour of overlap over the daily cadence; the insert
	// dedupe makes the overlap free.
	since := time.Now().Add(-25 * time.Hour).Unix()

	lastfmKey := ""
	if w.LastfmCreds != nil {
		lastfmKey, _ = w.LastfmCreds(ctx)
	}

	rows, err := w.DB.Query(ctx, `
		SELECT user_id, service, username, token FROM user_music_services
		WHERE (service = 'listenbrainz' AND token <> '')
		   OR (service = 'lastfm' AND username <> '')`)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	type link struct {
		userID            int64
		service, username string
	}
	var links []link
	for rows.Next() {
		var l link
		var token string
		if err := rows.Scan(&l.userID, &l.service, &l.username, &token); err == nil {
			links = append(links, l)
		}
	}
	rows.Close()

	client := river.ClientFromContext[pgx.Tx](ctx)
	enqueued := 0
	for _, l := range links {
		if l.service == "lastfm" && lastfmKey == "" {
			continue // server has no Last.fm app key — nothing to sync
		}
		if _, err := client.Insert(ctx, KickoffListenImportArgs{
			UserID: l.userID, Service: l.service, SinceTS: since,
		}, nil); err != nil {
			log.Warn().Err(err).Int64("user", l.userID).Str("service", l.service).
				Msg("music sync: incremental import enqueue failed")
			continue
		}
		enqueued++
	}

	rematched := w.rematchExternalListens(ctx)

	finishKickoff(ctx, q, taskID, startedAt, enqueued+rematched, 0, nil)
	log.Info().Int("incremental_imports", enqueued).Int("rematched", rematched).
		Msg("music services sync: complete")
	return nil
}

// rematchExternalListens sweeps unmatched stored listens against the current
// library (bounded batch per run). Fresh matches materialize: listens become
// play_events (±2min cross-service dedupe) and loves/hates become ratings —
// never overriding an existing Heya reaction.
// insertReaction mirrors the listen-import reaction write: created_at is the
// date the reaction was given on the external service (external_listens
// carries the real feedback/loved timestamp), never the sync time, and an
// existing same-value rating gets its created_at healed backward while a
// different reaction made in Heya stays untouched.
func (w *KickoffMusicServicesSyncWorker) insertReaction(ctx context.Context, userID, trackID int64, rating int16, at time.Time) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tag, err := w.DB.Exec(ctx, `
		INSERT INTO user_track_ratings (user_id, track_id, rating, created_at)
		VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, track_id) DO NOTHING`,
		userID, trackID, rating, at)
	if err == nil && tag.RowsAffected() == 0 {
		_, _ = w.DB.Exec(ctx, `
			UPDATE user_track_ratings SET created_at = LEAST(created_at, $4)
			WHERE user_id = $1 AND track_id = $2 AND rating = $3`,
			userID, trackID, rating, at)
	}
}

func (w *KickoffMusicServicesSyncWorker) rematchExternalListens(ctx context.Context) int {
	const batch = 20000
	rows, err := w.DB.Query(ctx, `
		SELECT id, user_id, kind, artist_name, track_name, recording_mbid, listened_at, duration_seconds, service
		FROM external_listens
		WHERE matched_track_id IS NULL
		ORDER BY id
		LIMIT $1`, batch)
	if err != nil {
		log.Warn().Err(err).Msg("music sync: rematch query failed")
		return 0
	}
	type row struct {
		id, userID  int64
		kind        string
		listen      scrobble.Listen
		durationSec int
		service     string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userID, &r.kind, &r.listen.ArtistName, &r.listen.TrackName,
			&r.listen.RecordingMBID, &r.listen.ListenedAt, &r.durationSec, &r.service); err == nil {
			pending = append(pending, r)
		}
	}
	rows.Close()

	matched := 0
	for _, r := range pending {
		if ctx.Err() != nil {
			return matched
		}
		trackID, duration, ok := matchListenToTrack(ctx, w.DB, r.listen)
		if !ok {
			continue
		}
		if _, err := w.DB.Exec(ctx,
			`UPDATE external_listens SET matched_track_id = $2 WHERE id = $1`, r.id, trackID); err != nil {
			continue
		}
		switch r.kind {
		case "listen":
			listened := r.durationSec
			if listened == 0 {
				listened = int(duration)
			}
			_, _ = w.DB.Exec(ctx, `
				INSERT INTO play_events (user_id, track_id, played_at, listened_seconds, completed, source)
				SELECT $1, $2, $3, $4, true, $5
				WHERE NOT EXISTS (
					SELECT 1 FROM play_events
					WHERE user_id = $1 AND track_id = $2
					  AND played_at BETWEEN $3::timestamptz - interval '120 seconds'
					                    AND $3::timestamptz + interval '120 seconds'
				)`, r.userID, trackID, r.listen.ListenedAt, listened, r.service)
		case "love":
			w.insertReaction(ctx, r.userID, trackID, 10, r.listen.ListenedAt)
		case "hate":
			w.insertReaction(ctx, r.userID, trackID, 1, r.listen.ListenedAt)
		}
		matched++
	}
	return matched
}
