package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/scrobble"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Listen-history import as queue work: one kickoff job per (user, service)
// pages the external history (resume cursor + per-page retry + pacing) and
// fans out durable batch jobs that match listens to library tracks and insert
// play_events. Reactions (Last.fm loved tracks, ListenBrainz ±1 feedback)
// import inline in the kickoff — small volume. Progress lives in
// user_music_services.import_state, updated with atomic jsonb arithmetic so
// concurrent batch workers never lose counts; the last finished batch flips
// the state to done.

// LastfmCredsFn resolves the server-level Last.fm app credentials without the
// worker package importing service/ (same indirection as SonicEnabledFn).
type LastfmCredsFn func(ctx context.Context) (apiKey, secret string)

// KickoffListenImportArgs starts one import run. SinceTS > 0 makes the run
// incremental: paging stops once listens older than the bound are reached and
// the full-import resume cursor is left untouched.
type KickoffListenImportArgs struct {
	UserID  int64  `json:"user_id"`
	Service string `json:"service"`
	SinceTS int64  `json:"since_ts,omitempty"`
}

func (KickoffListenImportArgs) Kind() string { return "kickoff_listen_import" }
func (KickoffListenImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "kickoff_listen_import",
		MaxAttempts: 1,
		UniqueOpts:  uniqueWhileActive(),
	}
}

// ImportListensBatchArgs carries one page of listens to match and insert.
type ImportListensBatchArgs struct {
	UserID  int64             `json:"user_id"`
	Service string            `json:"service"`
	Listens []scrobble.Listen `json:"listens"`
}

func (ImportListensBatchArgs) Kind() string { return "import_listens_batch" }
func (ImportListensBatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "import_listens_batch",
		MaxAttempts: 3,
	}
}

type KickoffListenImportWorker struct {
	river.WorkerDefaults[KickoffListenImportArgs]
	DB          *pgxpool.Pool
	LastfmCreds LastfmCredsFn
	Progress    *TaskProgressBroadcaster
}

// Timeout lifts River's default 1-minute deadline — paging a decade of
// scrobbles takes as long as it takes.
func (w *KickoffListenImportWorker) Timeout(*river.Job[KickoffListenImportArgs]) time.Duration {
	return -1
}

func (w *KickoffListenImportWorker) Work(ctx context.Context, job *river.Job[KickoffListenImportArgs]) error {
	userID, svc := job.Args.UserID, job.Args.Service
	w.Progress.Set("listen_imports", KickoffListenImportArgs{}.Kind(), svc+" history")

	var username, token string
	var cursor int64
	if err := w.DB.QueryRow(ctx, `
		SELECT username, token, COALESCE((import_state->>'cursor')::bigint, 0)
		FROM user_music_services WHERE user_id = $1 AND service = $2`,
		userID, svc).Scan(&username, &token, &cursor); err != nil {
		return fmt.Errorf("service link missing: %w", err)
	}
	if job.Args.SinceTS > 0 {
		cursor = 0 // incremental runs page from the newest listen down to SinceTS
	}

	w.stateMerge(ctx, userID, svc, map[string]any{
		"status": "running", "error": "", "paging_done": false,
	})
	failState := func(err error) error {
		w.stateMerge(ctx, userID, svc, map[string]any{"status": "failed", "error": err.Error()})
		return err
	}

	fetchPage := w.pager(svc, username, token, cursor)
	client := river.ClientFromContext[pgx.Tx](ctx)
	for {
		if ctx.Err() != nil {
			return failState(ctx.Err())
		}
		listens, done, err := fetchWithRetry(ctx, fetchPage)
		if err != nil {
			return failState(err)
		}
		if len(listens) > 0 {
			if _, err := client.Insert(ctx, ImportListensBatchArgs{UserID: userID, Service: svc, Listens: listens}, nil); err != nil {
				return failState(fmt.Errorf("enqueue batch: %w", err))
			}
			oldest := listens[0].ListenedAt.Unix()
			for _, l := range listens {
				if uts := l.ListenedAt.Unix(); uts < oldest {
					oldest = uts
				}
			}
			w.stateArithmetic(ctx, userID, svc, len(listens), 0, 0, 0, +1)
			if job.Args.SinceTS == 0 {
				// Only full imports own the resume cursor — an incremental
				// run reaching "now-24h" must not clobber a failed full
				// import's deep-history position.
				w.stateMerge(ctx, userID, svc, map[string]any{"cursor": oldest})
			}
			if job.Args.SinceTS > 0 && oldest <= job.Args.SinceTS {
				break // incremental bound reached
			}
		}
		if done {
			break
		}
		select { // politeness pacing — hammering pages provokes Last.fm's code-8s
		case <-ctx.Done():
			return failState(ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}

	// Reactions: loves/hates become ratings (heart=10, hate=1) — never
	// overriding a reaction the user already made in Heya.
	loved, hated := w.importReactions(ctx, userID, svc, username, token)
	w.stateMerge(ctx, userID, svc, map[string]any{
		"paging_done": true, "loved_imported": loved, "hated_imported": hated,
	})
	w.finalize(ctx, userID, svc)
	log.Info().Str("service", svc).Int64("user", userID).Int("loves", loved).Int("hates", hated).
		Msg("listen import: paging complete, batches draining")
	return nil
}

// pager yields history pages, resuming below cursor when set.
func (w *KickoffListenImportWorker) pager(svc, username, token string, cursor int64) func(ctx context.Context) ([]scrobble.Listen, bool, error) {
	switch svc {
	case "listenbrainz":
		lb := &scrobble.ListenBrainz{Token: token}
		maxTS := time.Now().Add(time.Hour)
		if cursor > 0 {
			maxTS = time.Unix(cursor, 0)
		}
		return func(ctx context.Context) ([]scrobble.Listen, bool, error) {
			listens, next, err := lb.Listens(ctx, username, maxTS, 100)
			if err != nil {
				return nil, false, err
			}
			if next.IsZero() || !next.Before(maxTS) {
				return listens, true, nil
			}
			maxTS = next
			return listens, false, nil
		}
	default: // lastfm — cursor-anchored shallow pages; deep pagination is
		// where Last.fm's API reliably fails with transient code 8.
		to := cursor
		return func(ctx context.Context) ([]scrobble.Listen, bool, error) {
			key, _ := w.LastfmCreds(ctx)
			if key == "" {
				return nil, false, fmt.Errorf("last.fm API key is not configured")
			}
			lf := &scrobble.LastFM{APIKey: key}
			listens, _, err := lf.RecentTracks(ctx, username, 1, 200, to)
			if err != nil {
				return nil, false, err
			}
			if len(listens) == 0 {
				return nil, true, nil
			}
			oldest := listens[0].ListenedAt.Unix()
			for _, l := range listens {
				if uts := l.ListenedAt.Unix(); uts < oldest {
					oldest = uts
				}
			}
			if to > 0 && oldest >= to {
				return listens, true, nil // no progress — bound reached
			}
			to = oldest
			return listens, false, nil
		}
	}
}

// importReactions pulls loves (both services) and hates (ListenBrainz) and
// writes them as ratings. Failures log and skip — reactions must not fail an
// otherwise-good history import.
func (w *KickoffListenImportWorker) importReactions(ctx context.Context, userID int64, svc, username, token string) (loved, hated int) {
	apply := func(items []scrobble.Listen, kind string, rating int16) int {
		n := 0
		for _, l := range items {
			trackID, _, ok := matchListenToTrack(ctx, w.DB, l)
			var matchedID any
			if ok {
				matchedID = trackID
			}
			at := l.ListenedAt
			if at.IsZero() {
				at = time.Now().UTC()
			}
			_, _ = w.DB.Exec(ctx, `
				INSERT INTO external_listens (user_id, service, kind, artist_name, track_name, recording_mbid, listened_at, matched_track_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT ON CONSTRAINT external_listens_dedupe_key DO NOTHING`,
				userID, svc, kind, l.ArtistName, l.TrackName, l.RecordingMBID, at, matchedID)
			if !ok {
				continue
			}
			tag, err := w.DB.Exec(ctx, `
				INSERT INTO user_track_ratings (user_id, track_id, rating)
				VALUES ($1, $2, $3) ON CONFLICT (user_id, track_id) DO NOTHING`,
				userID, trackID, rating)
			if err == nil {
				n += int(tag.RowsAffected())
			}
		}
		return n
	}

	switch svc {
	case "listenbrainz":
		lb := &scrobble.ListenBrainz{Token: token}
		for offset := 0; ; offset += 100 {
			loves, hates, total, err := lb.Feedback(ctx, username, offset, 100)
			if err != nil {
				log.Warn().Err(err).Msg("listen import: feedback fetch failed — skipping reactions")
				break
			}
			loved += apply(loves, "love", 10)
			hated += apply(hates, "hate", 1)
			if offset+100 >= total || (len(loves) == 0 && len(hates) == 0) {
				break
			}
		}
	default: // lastfm — loved tracks only; last.fm has no dislike concept
		key, _ := w.LastfmCreds(ctx)
		if key == "" {
			return 0, 0
		}
		lf := &scrobble.LastFM{APIKey: key}
		for page := 1; ; page++ {
			loves, totalPages, err := lf.LovedTracks(ctx, username, page)
			if err != nil {
				log.Warn().Err(err).Msg("listen import: loved tracks fetch failed — skipping reactions")
				break
			}
			loved += apply(loves, "love", 10)
			if page >= totalPages || len(loves) == 0 {
				break
			}
		}
	}
	return loved, hated
}

// stateMerge shallow-merges fields into import_state (single-statement, safe
// under concurrency for disjoint keys).
func (w *KickoffListenImportWorker) stateMerge(ctx context.Context, userID int64, svc string, fields map[string]any) {
	mergeImportState(ctx, w.DB, userID, svc, fields)
}

// stateArithmetic atomically increments the counter fields.
func (w *KickoffListenImportWorker) stateArithmetic(ctx context.Context, userID int64, svc string, scanned, matched, imported, unmatched, pendingDelta int) {
	importStateArithmetic(ctx, w.DB, userID, svc, scanned, matched, imported, unmatched, pendingDelta)
}

func (w *KickoffListenImportWorker) finalize(ctx context.Context, userID int64, svc string) {
	finalizeImportState(ctx, w.DB, userID, svc)
}

type ImportListensBatchWorker struct {
	river.WorkerDefaults[ImportListensBatchArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ImportListensBatchWorker) Timeout(*river.Job[ImportListensBatchArgs]) time.Duration {
	return -1
}

func (w *ImportListensBatchWorker) Work(ctx context.Context, job *river.Job[ImportListensBatchArgs]) error {
	userID, svc := job.Args.UserID, job.Args.Service
	matched, imported := 0, 0
	for _, l := range job.Args.Listens {
		trackID, duration, ok := matchListenToTrack(ctx, w.DB, l)
		// Every listen lands in external_listens whether it matched or not —
		// unmatched rows retro-match later as the library grows, and double
		// as the "most-listened music you don't own" signal.
		var matchedID any
		if ok {
			matchedID = trackID
		}
		if _, err := w.DB.Exec(ctx, `
			INSERT INTO external_listens (user_id, service, kind, artist_name, track_name, release_name, recording_mbid, listened_at, duration_seconds, matched_track_id)
			VALUES ($1, $2, 'listen', $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT ON CONSTRAINT external_listens_dedupe_key DO NOTHING`,
			userID, svc, l.ArtistName, l.TrackName, l.ReleaseName, l.RecordingMBID, l.ListenedAt, l.DurationSec, matchedID); err != nil {
			return err
		}
		if !ok {
			continue
		}
		matched++
		listened := l.DurationSec
		if listened == 0 {
			listened = int(duration)
		}
		// Cross-service dedupe: the same listen scrobbled to both Last.fm and
		// ListenBrainz arrives with (nearly) the same timestamp — a ±2 minute
		// window per (user, track) collapses them to one play event.
		tag, err := w.DB.Exec(ctx, `
			INSERT INTO play_events (user_id, track_id, played_at, listened_seconds, completed, source)
			SELECT $1, $2, $3, $4, true, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM play_events
				WHERE user_id = $1 AND track_id = $2
				  AND played_at BETWEEN $3::timestamptz - interval '120 seconds'
				                    AND $3::timestamptz + interval '120 seconds'
			)`, userID, trackID, l.ListenedAt, listened, svc)
		if err != nil {
			return err // river retries the batch; inserts are idempotent
		}
		imported += int(tag.RowsAffected())
	}
	importStateArithmetic(ctx, w.DB, userID, svc, 0, matched, imported, len(job.Args.Listens)-matched, -1)
	finalizeImportState(ctx, w.DB, userID, svc)
	return nil
}

// matchListenToTrack resolves an external listen to a library track: exact
// recording MBID first, then normalized (artist, title).
func matchListenToTrack(ctx context.Context, db *pgxpool.Pool, l scrobble.Listen) (trackID int64, duration int32, ok bool) {
	if l.RecordingMBID != "" {
		if err := db.QueryRow(ctx,
			`SELECT id, duration FROM tracks WHERE recording_mbid = $1 LIMIT 1`,
			l.RecordingMBID).Scan(&trackID, &duration); err == nil {
			return trackID, duration, true
		}
	}
	if l.ArtistName == "" || l.TrackName == "" {
		return 0, 0, false
	}
	if err := db.QueryRow(ctx, `
		SELECT t.id, t.duration
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE lower(t.title) = lower($1) AND lower(ar.name) = lower($2)
		ORDER BY t.id
		LIMIT 1`, strings.TrimSpace(l.TrackName), strings.TrimSpace(l.ArtistName)).Scan(&trackID, &duration); err == nil {
		return trackID, duration, true
	}
	// Tier 3 — normalized: strip "(Remastered)" / "[Live]"-style suffixes from
	// the title and "feat./ft. …" tails from the artist, then compare against
	// the expression-indexed normalized track title.
	err := db.QueryRow(ctx, `
		SELECT t.id, t.duration
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		JOIN artists ar ON ar.id = al.artist_id
		WHERE lower(regexp_replace(t.title, '\s*[\(\[].*$', '')) = $1 AND lower(ar.name) = $2
		ORDER BY t.id
		LIMIT 1`, normalizeListenTitle(l.TrackName), normalizeListenArtist(l.ArtistName)).Scan(&trackID, &duration)
	if err != nil {
		return 0, 0, false
	}
	return trackID, duration, true
}

// normalizeListenTitle lowercases and strips parenthetical/bracket suffixes:
// "Song (Remastered 2011)" → "song".
func normalizeListenTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, sep := range []string{" (", " ["} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// normalizeListenArtist lowercases and strips featuring credits:
// "Artist feat. Guest" → "artist".
func normalizeListenArtist(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, sep := range []string{" feat.", " feat ", " ft.", " ft ", " featuring "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// fetchWithRetry retries one page fetch through transient upstream failures
// (Last.fm's code 8/16, network blips) with growing backoff.
func fetchWithRetry(ctx context.Context, fetch func(ctx context.Context) ([]scrobble.Listen, bool, error)) ([]scrobble.Listen, bool, error) {
	backoffs := []time.Duration{0, 2 * time.Second, 8 * time.Second, 20 * time.Second}
	var lastErr error
	for _, wait := range backoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(wait):
			}
		}
		listens, done, err := fetch(ctx)
		if err == nil {
			return listens, done, nil
		}
		lastErr = err
	}
	return nil, false, lastErr
}

func mergeImportState(ctx context.Context, db *pgxpool.Pool, userID int64, svc string, fields map[string]any) {
	fields["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(fields)
	if err != nil {
		return
	}
	if _, err := db.Exec(ctx, `
		UPDATE user_music_services SET import_state = import_state || $3::jsonb, updated_at = now()
		WHERE user_id = $1 AND service = $2`, userID, svc, raw); err != nil {
		log.Warn().Err(err).Str("service", svc).Msg("listen import: state merge failed")
	}
}

// importStateArithmetic atomically adds to the counters in one UPDATE.
func importStateArithmetic(ctx context.Context, db *pgxpool.Pool, userID int64, svc string, scanned, matched, imported, unmatched, pendingDelta int) {
	if _, err := db.Exec(ctx, `
		UPDATE user_music_services SET import_state = import_state || jsonb_build_object(
			'scanned',   COALESCE((import_state->>'scanned')::int, 0) + $3,
			'matched',   COALESCE((import_state->>'matched')::int, 0) + $4,
			'imported',  COALESCE((import_state->>'imported')::int, 0) + $5,
			'unmatched', COALESCE((import_state->>'unmatched')::int, 0) + $6,
			'pending_batches', GREATEST(COALESCE((import_state->>'pending_batches')::int, 0) + $7, 0),
			'updated_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		), updated_at = now()
		WHERE user_id = $1 AND service = $2`,
		userID, svc, scanned, matched, imported, unmatched, pendingDelta); err != nil {
		log.Warn().Err(err).Str("service", svc).Msg("listen import: counter update failed")
	}
}

// finalizeImportState flips running → done once paging finished and every
// batch drained. Guarded in SQL so whichever worker finishes last wins.
func finalizeImportState(ctx context.Context, db *pgxpool.Pool, userID int64, svc string) {
	if _, err := db.Exec(ctx, `
		UPDATE user_music_services SET import_state = import_state || jsonb_build_object('status', 'done'),
			updated_at = now()
		WHERE user_id = $1 AND service = $2
		  AND import_state->>'status' = 'running'
		  AND COALESCE((import_state->>'paging_done')::bool, false)
		  AND COALESCE((import_state->>'pending_batches')::int, 0) = 0`,
		userID, svc); err != nil {
		log.Warn().Err(err).Str("service", svc).Msg("listen import: finalize failed")
	}
}
