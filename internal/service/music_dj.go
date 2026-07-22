package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog/log"
)

// Heya's DJs are queue-insertion strategies, not replacement queues. State is
// persisted on the per-device queue; generated ownership is persisted on each
// item so switching/off removes only future DJ contributions.
const (
	DJModeOff       = "off"
	DJModeEcho      = "echo"      // closest musical neighbour, another artist
	DJModeFlow      = "flow"      // maintain a two-track recommendation runway
	DJModeVoyage    = "voyage"    // three interpolated steps toward next user track
	DJModeEncore    = "encore"    // one more from this artist
	DJModeSpotlight = "spotlight" // keep the current artist going
	DJModeTimewarp  = "timewarp"  // keep the era going
)

const (
	djMinDurationSeconds = 60
	djMaxDurationSeconds = 20 * 60
	djIncrementalRunway  = 2
)

var djModes = map[string]bool{
	DJModeOff: true, DJModeEcho: true, DJModeFlow: true,
	DJModeVoyage: true, DJModeEncore: true,
	DJModeSpotlight: true, DJModeTimewarp: true,
}

func djModeIncremental(mode string) bool {
	return mode == DJModeFlow || mode == DJModeSpotlight || mode == DJModeTimewarp
}

func djModeBatchSize(mode string) int {
	if mode == DJModeVoyage {
		return 3
	}
	if djModeIncremental(mode) {
		return djIncrementalRunway
	}
	return 1
}

type djSnapshot struct {
	queueID     int64
	mode        string
	session     int64
	current     sqlc.PlayQueueItem
	anchor      sqlc.PlayQueueItem
	targetTrack int64
	need        int
	exclude     []int64
}

// SetQueueDJ switches the active strategy for one device queue. A mode switch
// first removes future contributions from every previous DJ session, bumps the
// session token, then fills the new strategy's initial runway.
func (a *App) SetQueueDJ(ctx context.Context, userID int64, deviceID, mode string) (QueueView, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !djModes[mode] {
		return QueueView{}, fmt.Errorf("unknown DJ mode %q", mode)
	}

	var out sqlc.PlayQueue
	var changed bool
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		if !pq.CurrentItemID.Valid {
			return fmt.Errorf("play something before choosing a DJ")
		}
		if pq.DjMode == mode {
			out = pq
			return nil
		}
		current, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: pq.CurrentItemID.Int64, QueueID: pq.ID})
		if err != nil {
			return fmt.Errorf("current queue item is missing")
		}
		if _, err := q.DeleteUpcomingDJQueueItems(ctx, sqlc.DeleteUpcomingDJQueueItemsParams{
			QueueID: pq.ID, AfterOrd: current.Ord,
		}); err != nil {
			return err
		}
		out, err = q.SetQueueDJMode(ctx, sqlc.SetQueueDJModeParams{QueueID: pq.ID, DjMode: mode})
		changed = err == nil
		return err
	})
	if err != nil {
		return QueueView{}, err
	}
	if changed {
		// Structural kind because switching may have removed generated items.
		a.emitQueue(userID, out, "items", 0)
	}
	if mode != DJModeOff {
		// The mode switch is already committed. Treat recommendation failures
		// as retryable extension work rather than returning an error that makes
		// the client believe the DJ was not enabled.
		a.processQueueDJBestEffort(ctx, userID, deviceID)
	}
	return a.GetQueue(ctx, userID, deviceID, nil, queueWindowDefault)
}

// processQueueDJ is intentionally synchronous at a track boundary: the queue
// is filled before the renderer asks for its next item. Recommendation work is
// done outside a transaction; the session/current pointer is revalidated
// inside commitDJTracks so a concurrent switch or queue replacement wins.
func (a *App) processQueueDJ(ctx context.Context, userID int64, deviceID string) error {
	snap, err := a.snapshotQueueDJ(ctx, userID, deviceID)
	if err != nil || snap == nil {
		return err
	}
	trackIDs, err := a.generateDJTracks(ctx, userID, *snap)
	if err != nil {
		return err
	}
	_, err = a.commitDJTracks(ctx, userID, deviceID, *snap, trackIDs)
	return err
}

func (a *App) processQueueDJBestEffort(ctx context.Context, userID int64, deviceID string) {
	if err := a.processQueueDJ(ctx, userID, deviceID); err != nil {
		log.Warn().Err(err).Int64("user_id", userID).Str("device_id", deviceID).
			Msg("music DJ could not extend queue")
	}
}

func (a *App) snapshotQueueDJ(ctx context.Context, userID int64, deviceID string) (*djSnapshot, error) {
	q := sqlc.New(a.db)
	pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pq.DjMode == DJModeOff || !pq.CurrentItemID.Valid {
		return nil, nil
	}
	current, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: pq.CurrentItemID.Int64, QueueID: pq.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if current.DjProcessedSession == pq.DjSession {
		return nil, nil
	}

	snap := &djSnapshot{
		queueID: pq.ID, mode: pq.DjMode,
		session: pq.DjSession, current: current, anchor: current,
		need: djModeBatchSize(pq.DjMode),
	}
	if djModeIncremental(pq.DjMode) {
		nextItems, err := q.ListNextTwoQueueItems(ctx, sqlc.ListNextTwoQueueItemsParams{
			QueueID: pq.ID, AfterOrd: current.Ord,
		})
		if err != nil {
			return nil, err
		}
		runway := consecutiveDJRunway(nextItems, pq.DjSession)
		snap.need = max(0, djIncrementalRunway-len(runway))
		if len(runway) > 0 {
			snap.anchor, err = q.GetQueueItem(ctx, sqlc.GetQueueItemParams{
				ID: runway[len(runway)-1].ID, QueueID: pq.ID,
			})
			if err != nil {
				return nil, err
			}
		}
	} else if current.DjSession > 0 {
		// Echo/Encore/Voyage decorate user-owned tracks. A generated item is a
		// bridge, not another anchor, which keeps the original queue moving.
		return nil, nil
	}

	if snap.need == 0 {
		return snap, nil
	}
	if pq.DjMode == DJModeVoyage {
		target, err := q.FirstUserQueueItemAfter(ctx, sqlc.FirstUserQueueItemAfterParams{QueueID: pq.ID, AfterOrd: current.Ord})
		if errors.Is(err, pgx.ErrNoRows) {
			snap.need = 0
		} else if err != nil {
			return nil, err
		} else {
			snap.targetTrack = target.TrackID
		}
	}
	snap.exclude, err = q.ListQueueTrackIDs(ctx, pq.ID)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func (a *App) generateDJTracks(ctx context.Context, userID int64, snap djSnapshot) ([]int64, error) {
	if snap.need <= 0 {
		return nil, nil
	}
	var candidates []int64
	var err error
	seedTrackID := snap.current.TrackID
	if djModeIncremental(snap.mode) {
		// Extend from the end of the existing runway, not from the track two
		// positions behind it. This keeps Flow cohesive as one item is consumed.
		seedTrackID = snap.anchor.TrackID
	}
	switch snap.mode {
	case DJModeEcho:
		candidates, err = a.echoDJCandidates(ctx, userID, snap.current.TrackID, snap.exclude, max(30, snap.need*10))
	case DJModeFlow:
		candidates, err = a.flowDJCandidates(ctx, userID, seedTrackID, snap.exclude, max(20, snap.need*8))
	case DJModeVoyage:
		candidates, err = a.voyageDJCandidates(ctx, userID, snap.current.TrackID, snap.targetTrack, snap.exclude, snap.need)
	case DJModeEncore, DJModeSpotlight:
		candidates, err = a.artistDJCandidates(ctx, userID, seedTrackID, snap.exclude, snap.session, max(30, snap.need*10))
	case DJModeTimewarp:
		candidates, err = a.timewarpDJCandidates(ctx, userID, seedTrackID, snap.exclude, snap.session, max(40, snap.need*12))
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a.filterDJTrackIDs(ctx, userID, candidates, snap.exclude, snap.need)
}

func (a *App) echoDJCandidates(ctx context.Context, userID, seedTrackID int64, exclude []int64, limit int) ([]int64, error) {
	rows, err := a.db.Query(ctx, `
		WITH seed AS (
			SELECT facets.track_embedding, album.artist_id
			FROM track_facets facets
			JOIN tracks track ON track.id = facets.track_id
			JOIN albums album ON album.id = track.album_id
			WHERE facets.track_id = $2 AND facets.track_embedding IS NOT NULL
		)
		SELECT track.id
		FROM seed
		JOIN track_facets facets ON facets.track_embedding IS NOT NULL
		JOIN tracks track ON track.id = facets.track_id
		JOIN albums album ON album.id = track.album_id
		LEFT JOIN user_track_ratings rating ON rating.user_id = $1 AND rating.track_id = track.id
		WHERE album.artist_id <> seed.artist_id
		  AND NOT (track.id = ANY($3::bigint[]))
		  AND (rating.rating IS NULL OR rating.rating > 3)
		  AND EXISTS (SELECT 1 FROM track_files file JOIN library_files library_file ON library_file.id = file.library_file_id
		              WHERE file.track_id = track.id AND library_file.deleted_at IS NULL)
		ORDER BY facets.track_embedding <=> seed.track_embedding
		LIMIT $4`, userID, seedTrackID, exclude, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids, err := scanInt64Rows(rows)
	if err != nil || len(ids) > 0 {
		return ids, err
	}
	// An unanalysed seed still gets a useful Echo from the metadata/provider
	// fallbacks in the shared radio engine.
	return a.flowDJCandidates(ctx, userID, seedTrackID, exclude, limit)
}

func (a *App) flowDJCandidates(ctx context.Context, userID, seedTrackID int64, exclude []int64, limit int) ([]int64, error) {
	res, err := a.BuildRadio(ctx, userID, RadioRequest{
		Seed:  RadioSeed{Kind: "track", TrackID: seedTrackID},
		Limit: int32(limit), ExcludeTrackIDs: exclude,
	})
	if errors.Is(err, ErrNoRadioSeed) {
		return []int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(res.Tracks))
	for _, track := range res.Tracks {
		ids = append(ids, track.TrackID)
	}
	return ids, nil
}

func (a *App) artistDJCandidates(ctx context.Context, userID, seedTrackID int64, exclude []int64, session int64, limit int) ([]int64, error) {
	rows, err := a.db.Query(ctx, `
		SELECT candidate.id
		FROM tracks seed
		JOIN albums seed_album ON seed_album.id = seed.album_id
		JOIN albums album ON album.artist_id = seed_album.artist_id
		JOIN tracks candidate ON candidate.album_id = album.id
		LEFT JOIN user_track_ratings rating ON rating.user_id = $1 AND rating.track_id = candidate.id
		LEFT JOIN track_facets facets ON facets.track_id = candidate.id AND facets.track_embedding IS NOT NULL
		WHERE seed.id = $2
		  AND NOT (candidate.id = ANY($3::bigint[]))
		  AND (rating.rating IS NULL OR rating.rating > 3)
		  AND EXISTS (SELECT 1 FROM track_files file JOIN library_files library_file ON library_file.id = file.library_file_id
		              WHERE file.track_id = candidate.id AND library_file.deleted_at IS NULL)
		ORDER BY (facets.track_id IS NOT NULL) DESC,
		         (SELECT count(*) FROM play_events event WHERE event.track_id = candidate.id AND event.completed) DESC,
		         md5(candidate.id::text || ':' || ($4::bigint)::text)
		LIMIT $5`, userID, seedTrackID, exclude, session, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInt64Rows(rows)
}

func (a *App) timewarpDJCandidates(ctx context.Context, userID, seedTrackID int64, exclude []int64, session int64, limit int) ([]int64, error) {
	rows, err := a.db.Query(ctx, `
		WITH seed AS (
			SELECT CASE WHEN album.year ~ '[0-9]{4}' THEN substring(album.year FROM '[0-9]{4}')::int ELSE NULL END AS year
			FROM tracks track JOIN albums album ON album.id = track.album_id
			WHERE track.id = $2
		)
		SELECT track.id
		FROM seed
		JOIN albums album ON seed.year IS NOT NULL
		JOIN tracks track ON track.album_id = album.id
		LEFT JOIN user_track_ratings rating ON rating.user_id = $1 AND rating.track_id = track.id
		WHERE CASE WHEN album.year ~ '[0-9]{4}' THEN substring(album.year FROM '[0-9]{4}')::int ELSE NULL END
		      BETWEEN seed.year - 2 AND seed.year + 2
		  AND lower(album.album_type) NOT LIKE '%compilation%'
		  AND NOT ('compilation' = ANY(album.secondary_types))
		  AND NOT (track.id = ANY($3::bigint[]))
		  AND (rating.rating IS NULL OR rating.rating > 3)
		  AND EXISTS (SELECT 1 FROM track_files file JOIN library_files library_file ON library_file.id = file.library_file_id
		              WHERE file.track_id = track.id AND library_file.deleted_at IS NULL)
		ORDER BY abs((substring(album.year FROM '[0-9]{4}')::int) - seed.year),
		         md5(track.id::text || ':' || ($4::bigint)::text)
		LIMIT $5`, userID, seedTrackID, exclude, session, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids, err := scanInt64Rows(rows)
	if err != nil || len(ids) > 0 {
		return ids, err
	}
	// Missing/invalid year: Flow is a more honest fallback than pretending a
	// global random track belongs to the same era.
	return a.flowDJCandidates(ctx, userID, seedTrackID, exclude, limit)
}

func (a *App) voyageDJCandidates(ctx context.Context, userID, startTrackID, endTrackID int64, exclude []int64, steps int) ([]int64, error) {
	if endTrackID <= 0 || steps <= 0 {
		return []int64{}, nil
	}
	q := sqlc.New(a.db)
	start, startErr := q.GetTrackFacets(ctx, startTrackID)
	end, endErr := q.GetTrackFacets(ctx, endTrackID)
	if startErr != nil || endErr != nil || len(start.TrackEmbedding.Slice()) == 0 || len(end.TrackEmbedding.Slice()) == 0 {
		return a.flowDJCandidates(ctx, userID, startTrackID, append(exclude, endTrackID), max(steps*8, 24))
	}

	blocked := make(map[int64]bool, len(exclude)+steps+2)
	for _, id := range exclude {
		blocked[id] = true
	}
	blocked[startTrackID], blocked[endTrackID] = true, true
	ids := make([]int64, 0, steps)
	for step := 1; step <= steps; step++ {
		ratio := float32(step) / float32(steps+1)
		point := interpolateDJVector(start.TrackEmbedding, end.TrackEmbedding, ratio)
		rows, err := a.tasteNeighborTracks(ctx, userID, point, 60)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if blocked[row.TrackID] || !djDurationEligible(row.Duration) {
				continue
			}
			blocked[row.TrackID] = true
			ids = append(ids, row.TrackID)
			break
		}
	}
	return ids, nil
}

func interpolateDJVector(start, end pgvector.Vector, ratio float32) pgvector.Vector {
	a, b := start.Slice(), end.Slice()
	if len(a) == 0 || len(a) != len(b) {
		return pgvector.Vector{}
	}
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i]*(1-ratio) + b[i]*ratio
	}
	return pgvector.NewVector(out)
}

func (a *App) filterDJTrackIDs(ctx context.Context, userID int64, candidates, exclude []int64, limit int) ([]int64, error) {
	if len(candidates) == 0 || limit <= 0 {
		return []int64{}, nil
	}
	rows, err := a.db.Query(ctx, `
		WITH candidate AS (
			SELECT track_id, min(rank) AS rank
			FROM unnest($2::bigint[]) WITH ORDINALITY input(track_id, rank)
			GROUP BY track_id
		)
		SELECT track.id
		FROM candidate
		JOIN tracks track ON track.id = candidate.track_id
		LEFT JOIN user_track_ratings rating ON rating.user_id = $1 AND rating.track_id = track.id
		WHERE NOT (track.id = ANY($3::bigint[]))
		  AND track.duration BETWEEN $4 AND $5
		  AND (rating.rating IS NULL OR rating.rating > 3)
		  AND EXISTS (SELECT 1 FROM track_files file JOIN library_files library_file ON library_file.id = file.library_file_id
		              WHERE file.track_id = track.id AND library_file.deleted_at IS NULL)
		ORDER BY candidate.rank
		LIMIT $6`, userID, candidates, exclude, djMinDurationSeconds, djMaxDurationSeconds, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInt64Rows(rows)
}

func djDurationEligible(duration int32) bool {
	return duration >= djMinDurationSeconds && duration <= djMaxDurationSeconds
}

func scanInt64Rows(rows pgx.Rows) ([]int64, error) {
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (a *App) commitDJTracks(ctx context.Context, userID int64, deviceID string, snap djSnapshot, candidates []int64) (int64, error) {
	var added int64
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		if pq.ID != snap.queueID || pq.DjMode != snap.mode || pq.DjSession != snap.session ||
			!pq.CurrentItemID.Valid || pq.CurrentItemID.Int64 != snap.current.ID {
			return nil // a newer queue/DJ/pointer owns the result
		}
		current, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: snap.current.ID, QueueID: pq.ID})
		if err != nil || current.DjProcessedSession == pq.DjSession {
			return err
		}
		if !djModeIncremental(pq.DjMode) && current.DjSession > 0 {
			return nil
		}

		anchor := current
		need := djModeBatchSize(pq.DjMode)
		if djModeIncremental(pq.DjMode) {
			nextItems, err := q.ListNextTwoQueueItems(ctx, sqlc.ListNextTwoQueueItemsParams{
				QueueID: pq.ID, AfterOrd: current.Ord,
			})
			if err != nil {
				return err
			}
			runway := consecutiveDJRunway(nextItems, pq.DjSession)
			need = max(0, djIncrementalRunway-len(runway))
			if len(runway) > 0 {
				anchor, err = q.GetQueueItem(ctx, sqlc.GetQueueItemParams{
					ID: runway[len(runway)-1].ID, QueueID: pq.ID,
				})
				if err != nil {
					return err
				}
			}
		}
		if pq.DjMode == DJModeVoyage {
			target, err := q.FirstUserQueueItemAfter(ctx, sqlc.FirstUserQueueItemAfterParams{QueueID: pq.ID, AfterOrd: current.Ord})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && target.TrackID != snap.targetTrack) {
				need = 0
			} else if err != nil {
				return err
			}
		}
		if err := q.MarkQueueItemDJProcessed(ctx, sqlc.MarkQueueItemDJProcessedParams{
			QueueID: pq.ID, ItemID: current.ID, DjSession: pq.DjSession,
		}); err != nil {
			return err
		}
		if need <= 0 || len(candidates) == 0 {
			return nil
		}
		if len(candidates) > need {
			candidates = candidates[:need]
		}

		// Resolve a sparse insertion slot after the runway anchor. Renumber once
		// if user reorders consumed the gap while recommendations were loading.
		for attempt := 0; attempt < 2; attempt++ {
			freshAnchor, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: anchor.ID, QueueID: pq.ID})
			if err != nil {
				return err
			}
			anchor = freshAnchor
			next, err := q.NextQueueItem(ctx, sqlc.NextQueueItemParams{QueueID: pq.ID, Ord: anchor.Ord})
			step := int64(queueOrdGap)
			if err == nil {
				gap := next.Ord - anchor.Ord
				if gap <= int64(len(candidates)) {
					if err := q.RenumberQueueItems(ctx, pq.ID); err != nil {
						return err
					}
					continue
				}
				step = gap / int64(len(candidates)+1)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			maxSrc, err := q.MaxQueueSrcOrd(ctx, pq.ID)
			if err != nil {
				return err
			}
			added, err = q.InsertDJQueueItemsAt(ctx, sqlc.InsertDJQueueItemsAtParams{
				QueueID: pq.ID, BaseOrd: anchor.Ord, Step: step, BaseSrc: maxSrc,
				DjSession: pq.DjSession, DjMode: pq.DjMode, TrackIds: candidates,
			})
			if err != nil {
				return err
			}
			if added > 0 {
				out, err = q.BumpQueueVersion(ctx, pq.ID)
				return err
			}
			return nil
		}
		return fmt.Errorf("DJ could not find a queue insertion slot")
	})
	if err != nil {
		return 0, err
	}
	if added > 0 {
		a.emitQueue(userID, out, "items", 0)
	}
	return added, nil
}

func consecutiveDJRunway(items []sqlc.PlayQueueItem, session int64) []sqlc.PlayQueueItem {
	runway := make([]sqlc.PlayQueueItem, 0, len(items))
	for _, item := range items {
		if item.DjSession != session {
			break
		}
		runway = append(runway, item)
	}
	return runway
}
