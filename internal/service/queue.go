package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
)

// Server-owned play queue (docs/queue-plan.md): ONE queue per user, fully
// materialized in play_queue_items, mirrored to every client via the
// per-user queue.changed event. Every structural mutation runs in a tx
// that bumps play_queues.version; heartbeats deliberately don't.

const (
	// Sparse ordering gap — reorders drop into gaps; rewrites land above
	// max(ord) so the unique constraint never sees transient collisions.
	queueOrdGap = 1024
	// Hard cap per materialization. 25k tracks ≈ two months of continuous
	// audio; a shuffled source larger than this gets a true random 25k
	// sample (the LIMIT applies after the random ordering).
	queueMaxItems = 25_000
	// Played history kept behind the pointer for the "Played" section and
	// prev-track; pruned on advance.
	queueHistoryKeep = 200
	// Default window size for reads.
	queueWindowDefault = 100
)

// ErrQueueNotActiveOutput tells a renderer its heartbeat lost the race —
// another output claimed playback and this client should stop rendering.
var ErrQueueNotActiveOutput = errors.New("queue: not the active output")

// QueueSource describes what a queue was materialized from. Kept as
// provenance on the queue row for re-shuffle and future radio-mode.
type QueueSource struct {
	Kind     string  `json:"kind" enum:"album,artist,playlist,genre,library,tracks" doc:"What to materialize from"`
	ID       int64   `json:"id,omitempty" doc:"album/artist/playlist id (kind-dependent)"`
	Genre    string  `json:"genre,omitempty" doc:"Genre name for kind=genre"`
	TrackIDs []int64 `json:"track_ids,omitempty" doc:"Explicit tracks for kind=tracks (mixes, selections)"`
}

// QueueItemView is one windowed row — the FE Track shape plus the queue
// identity (item id + ord) mutations address.
type QueueItemView struct {
	ItemID      int64  `json:"item_id"`
	Ord         int64  `json:"ord"`
	TrackID     int64  `json:"track_id"`
	Title       string `json:"title"`
	Duration    int32  `json:"duration"`
	DiscNumber  int32  `json:"disc_number"`
	TrackNumber int32  `json:"track_number"`
	AlbumID     int64  `json:"album_id"`
	AlbumTitle  string `json:"album_title"`
	AlbumSlug   string `json:"album_slug"`
	ArtistID    int64  `json:"artist_id"`
	ArtistName  string `json:"artist_name"`
	ArtistSlug  string `json:"artist_slug"`
	DJGenerated bool   `json:"dj_generated"`
	DJMode      string `json:"dj_mode,omitempty"`
}

// QueueView is the windowed client mirror: meta + a contiguous item
// window. Items never carries the whole queue — clients page with
// `around`.
type QueueView struct {
	Version          int64           `json:"version"`
	CurrentItemID    int64           `json:"current_item_id,omitempty"`
	CurrentIndex     int64           `json:"current_index"` // 0-based; -1 without a pointer
	Total            int64           `json:"total"`
	PositionSeconds  float64         `json:"position_seconds"`
	Playing          bool            `json:"playing"`
	RepeatMode       string          `json:"repeat_mode"`
	Shuffled         bool            `json:"shuffled"`
	DJMode           string          `json:"dj_mode"`
	ActiveOutput     string          `json:"active_output,omitempty"`
	Source           *QueueSource    `json:"source,omitempty"`
	Items            []QueueItemView `json:"items"`
	WindowStartIndex int64           `json:"window_start_index"` // index of Items[0]
}

func queueItemView(id, ord, trackID, djSession int64, djMode, title string, duration, disc, track int32, albumID int64, albumTitle, albumSlug string, artistID int64, artistName, artistSlug string) QueueItemView {
	return QueueItemView{
		ItemID: id, Ord: ord, TrackID: trackID,
		Title: title, Duration: duration, DiscNumber: disc, TrackNumber: track,
		AlbumID: albumID, AlbumTitle: albumTitle, AlbumSlug: albumSlug,
		ArtistID: artistID, ArtistName: artistName, ArtistSlug: artistSlug,
		DJGenerated: djSession > 0, DJMode: djMode,
	}
}

// GetQueue returns the user's queue with a window of `limit` items around
// `aroundOrd` (nil = around the current pointer). A user with no queue
// yet gets an empty view (version 0) rather than a 404 — the FE treats
// "no queue" and "empty queue" identically.
func (a *App) GetQueue(ctx context.Context, userID int64, deviceID string, aroundOrd *int64, limit int) (QueueView, error) {
	if limit <= 0 || limit > 500 {
		limit = queueWindowDefault
	}
	q := sqlc.New(a.db)
	pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return QueueView{CurrentIndex: -1, RepeatMode: "off", Items: []QueueItemView{}}, nil
	}
	if err != nil {
		return QueueView{}, err
	}
	return a.queueViewWindow(ctx, q, pq, aroundOrd, limit)
}

func (a *App) queueViewWindow(ctx context.Context, q *sqlc.Queries, pq sqlc.PlayQueue, aroundOrd *int64, limit int) (QueueView, error) {
	view := QueueView{
		Version:         pq.Version,
		CurrentIndex:    -1,
		PositionSeconds: float64(pq.PositionSeconds),
		Playing:         pq.Playing,
		RepeatMode:      pq.RepeatMode,
		Shuffled:        pq.Shuffled,
		DJMode:          pq.DjMode,
		ActiveOutput:    pq.ActiveOutput,
		Items:           []QueueItemView{},
	}
	if len(pq.Source) > 0 && string(pq.Source) != "{}" {
		var src QueueSource
		if json.Unmarshal(pq.Source, &src) == nil && src.Kind != "" {
			// TrackIDs provenance can be huge — never echo it back.
			src.TrackIDs = nil
			view.Source = &src
		}
	}

	total, err := q.CountQueueItems(ctx, pq.ID)
	if err != nil {
		return QueueView{}, err
	}
	view.Total = total
	if total == 0 {
		return view, nil
	}

	// Anchor: explicit ord, else the current item, else the head.
	var anchor int64
	if aroundOrd != nil {
		anchor = *aroundOrd
	} else if pq.CurrentItemID.Valid {
		cur, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: pq.CurrentItemID.Int64, QueueID: pq.ID})
		if err == nil {
			anchor = cur.Ord
		}
	}
	if pq.CurrentItemID.Valid {
		view.CurrentItemID = pq.CurrentItemID.Int64
		idx, err := q.CountQueueItemsBefore(ctx, sqlc.CountQueueItemsBeforeParams{QueueID: pq.ID, Ord: anchorOrDefault(ctx, q, pq)})
		if err == nil {
			view.CurrentIndex = idx
		}
	}

	// A quarter of the window behind the anchor, the rest ahead.
	beforeN := int32(limit / 4) //nolint:gosec // limit clamped to <=500
	afterN := int32(limit) - beforeN

	before, err := q.ListQueueWindowBefore(ctx, sqlc.ListQueueWindowBeforeParams{QueueID: pq.ID, Ord: anchor, Limit: beforeN})
	if err != nil {
		return QueueView{}, err
	}
	after, err := q.ListQueueWindow(ctx, sqlc.ListQueueWindowParams{QueueID: pq.ID, Ord: anchor, Limit: afterN})
	if err != nil {
		return QueueView{}, err
	}

	items := make([]QueueItemView, 0, len(before)+len(after))
	for i := len(before) - 1; i >= 0; i-- { // DESC → ascend
		r := before[i]
		items = append(items, queueItemView(r.ID, r.Ord, r.TrackID, r.DjSession, r.DjMode, r.Title, r.Duration, r.DiscNumber, r.TrackNumber, r.AlbumID, r.AlbumTitle, r.AlbumSlug, r.ArtistID, r.ArtistName, r.ArtistSlug))
	}
	for _, r := range after {
		items = append(items, queueItemView(r.ID, r.Ord, r.TrackID, r.DjSession, r.DjMode, r.Title, r.Duration, r.DiscNumber, r.TrackNumber, r.AlbumID, r.AlbumTitle, r.AlbumSlug, r.ArtistID, r.ArtistName, r.ArtistSlug))
	}
	view.Items = items
	if len(items) > 0 {
		startIdx, err := q.CountQueueItemsBefore(ctx, sqlc.CountQueueItemsBeforeParams{QueueID: pq.ID, Ord: items[0].Ord})
		if err == nil {
			view.WindowStartIndex = startIdx
		}
	}
	return view, nil
}

// anchorOrDefault resolves the current item's ord for index math (0 when
// the pointer dangles — e.g. the track was deleted).
func anchorOrDefault(ctx context.Context, q *sqlc.Queries, pq sqlc.PlayQueue) int64 {
	if !pq.CurrentItemID.Valid {
		return 0
	}
	cur, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: pq.CurrentItemID.Int64, QueueID: pq.ID})
	if err != nil {
		return 0
	}
	return cur.Ord
}

// materializeSource runs the right INSERT for the source kind. Ownership
// of playlists is enforced here (the only source kind that's per-user).
func (a *App) materializeSource(ctx context.Context, q *sqlc.Queries, queueID, userID int64, src QueueSource, shuffle bool) (int64, error) {
	switch src.Kind {
	case "album":
		return q.InsertQueueItemsFromAlbum(ctx, sqlc.InsertQueueItemsFromAlbumParams{QueueID: queueID, Shuffle: shuffle, AlbumID: src.ID, MaxItems: queueMaxItems})
	case "artist":
		return q.InsertQueueItemsFromArtist(ctx, sqlc.InsertQueueItemsFromArtistParams{QueueID: queueID, Shuffle: shuffle, ArtistID: src.ID, MaxItems: queueMaxItems})
	case "playlist":
		if _, err := q.GetUserPlaylist(ctx, sqlc.GetUserPlaylistParams{ID: src.ID, UserID: userID}); err != nil {
			return 0, fmt.Errorf("playlist %d not found", src.ID)
		}
		return q.InsertQueueItemsFromPlaylist(ctx, sqlc.InsertQueueItemsFromPlaylistParams{QueueID: queueID, Shuffle: shuffle, PlaylistID: src.ID, MaxItems: queueMaxItems})
	case "genre":
		if strings.TrimSpace(src.Genre) == "" {
			return 0, fmt.Errorf("genre source needs a genre")
		}
		return q.InsertQueueItemsFromGenre(ctx, sqlc.InsertQueueItemsFromGenreParams{QueueID: queueID, Shuffle: shuffle, Genre: src.Genre, MaxItems: queueMaxItems})
	case "library":
		return q.InsertQueueItemsFromLibrary(ctx, sqlc.InsertQueueItemsFromLibraryParams{QueueID: queueID, Shuffle: shuffle, MaxItems: queueMaxItems})
	case "tracks":
		if len(src.TrackIDs) == 0 {
			return 0, fmt.Errorf("tracks source needs track_ids")
		}
		return q.InsertQueueItemsFromTrackIDs(ctx, sqlc.InsertQueueItemsFromTrackIDsParams{QueueID: queueID, Shuffle: shuffle, TrackIds: src.TrackIDs, MaxItems: queueMaxItems})
	default:
		return 0, fmt.Errorf("unknown queue source kind %q", src.Kind)
	}
}

// ReplaceQueue materializes a fresh queue from the source and points it
// at startTrackID (or the head). With shuffle on, the chosen start track
// is moved to the front — "play this, then surprise me".
func (a *App) ReplaceQueue(ctx context.Context, userID int64, deviceID string, src QueueSource, startTrackID int64, shuffle bool, output string) (QueueView, error) {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.EnsurePlayQueue(ctx, sqlc.EnsurePlayQueueParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		if err := q.DeleteAllQueueItems(ctx, pq.ID); err != nil {
			return err
		}
		n, err := a.materializeSource(ctx, q, pq.ID, userID, src, shuffle)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("nothing playable in this source")
		}

		var current sqlc.PlayQueueItem
		if startTrackID > 0 {
			current, err = q.FindQueueItemByTrack(ctx, sqlc.FindQueueItemByTrackParams{QueueID: pq.ID, TrackID: startTrackID})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if current.ID == 0 {
			current, err = q.FirstQueueItem(ctx, pq.ID)
			if err != nil {
				return err
			}
		} else if shuffle {
			// The tapped track leads; the shuffled rest follows.
			first, err := q.FirstQueueItem(ctx, pq.ID)
			if err != nil {
				return err
			}
			if first.ID != current.ID {
				if err := q.SetQueueItemOrd(ctx, sqlc.SetQueueItemOrdParams{ID: current.ID, QueueID: pq.ID, Ord: first.Ord - queueOrdGap}); err != nil {
					return err
				}
			}
		}

		srcJSON, err := json.Marshal(src)
		if err != nil {
			return err
		}
		if _, err := q.SetQueueReplaced(ctx, sqlc.SetQueueReplacedParams{
			QueueID: pq.ID, Source: srcJSON, Shuffled: shuffle,
			CurrentItemID: pgtype.Int8{Int64: current.ID, Valid: true},
			Playing:       true,
		}); err != nil {
			return err
		}
		out, err = q.SetQueueOutput(ctx, sqlc.SetQueueOutputParams{QueueID: pq.ID, ActiveOutput: output})
		return err
	})
	if err != nil {
		return QueueView{}, err
	}
	a.emitQueue(userID, out, "replaced", 0)
	return a.GetQueue(ctx, userID, deviceID, nil, queueWindowDefault)
}

// EnqueueTracks appends (at="end") or inserts after the current item
// (at="next"), de-duplicating against the upcoming slice. Returns how
// many actually landed.
func (a *App) EnqueueTracks(ctx context.Context, userID int64, deviceID string, trackIDs []int64, at string) (int64, error) {
	if len(trackIDs) == 0 {
		return 0, nil
	}
	var added int64
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.EnsurePlayQueue(ctx, sqlc.EnsurePlayQueueParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return err
		}
		curOrd := anchorOrDefault(ctx, q, pq)
		upcoming, err := q.ListUpcomingQueueTrackIDs(ctx, sqlc.ListUpcomingQueueTrackIDsParams{QueueID: pq.ID, Ord: curOrd})
		if err != nil {
			return err
		}
		seen := make(map[int64]bool, len(upcoming))
		for _, id := range upcoming {
			seen[id] = true
		}
		fresh := make([]int64, 0, len(trackIDs))
		for _, id := range trackIDs {
			if !seen[id] {
				fresh = append(fresh, id)
				seen[id] = true
			}
		}
		if len(fresh) == 0 {
			return nil
		}

		maxSrc, err := q.MaxQueueSrcOrd(ctx, pq.ID)
		if err != nil {
			return err
		}
		var base, step int64
		if at == "next" && pq.CurrentItemID.Valid {
			next, err := q.NextQueueItem(ctx, sqlc.NextQueueItemParams{QueueID: pq.ID, Ord: curOrd})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// Nothing after current — same as append.
				base, step = curOrd, queueOrdGap
			case err != nil:
				return err
			default:
				gap := next.Ord - curOrd
				if gap <= int64(len(fresh)) {
					if err := q.RenumberQueueItems(ctx, pq.ID); err != nil {
						return err
					}
					cur, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: pq.CurrentItemID.Int64, QueueID: pq.ID})
					if err != nil {
						return err
					}
					curOrd = cur.Ord
					next, err = q.NextQueueItem(ctx, sqlc.NextQueueItemParams{QueueID: pq.ID, Ord: curOrd})
					if err != nil {
						return err
					}
					gap = next.Ord - curOrd
				}
				base, step = curOrd, gap/int64(len(fresh)+1)
			}
		} else {
			maxOrd, err := q.MaxQueueOrd(ctx, pq.ID)
			if err != nil {
				return err
			}
			base, step = maxOrd, queueOrdGap
		}

		added, err = q.InsertQueueItemsAt(ctx, sqlc.InsertQueueItemsAtParams{
			QueueID: pq.ID, BaseOrd: base, Step: step, BaseSrc: maxSrc, TrackIds: fresh,
		})
		if err != nil {
			return err
		}
		out, err = q.BumpQueueVersion(ctx, pq.ID)
		return err
	})
	if err != nil {
		return 0, err
	}
	if added > 0 {
		a.emitQueue(userID, out, "items", 0)
	}
	return added, nil
}

// RemoveQueueItem drops one non-current item.
func (a *App) RemoveQueueItem(ctx context.Context, userID int64, deviceID string, itemID int64) error {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		if pq.CurrentItemID.Valid && pq.CurrentItemID.Int64 == itemID {
			return fmt.Errorf("cannot remove the playing item")
		}
		n, err := q.DeleteQueueItem(ctx, sqlc.DeleteQueueItemParams{ID: itemID, QueueID: pq.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("no such queue item")
		}
		out, err = q.BumpQueueVersion(ctx, pq.ID)
		return err
	})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "items", 0)
	return nil
}

// MoveQueueItem places itemID directly after afterItemID (0 = directly
// after the current item, i.e. head of the upcoming slice).
func (a *App) MoveQueueItem(ctx context.Context, userID int64, deviceID string, itemID, afterItemID int64) error {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		item, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: itemID, QueueID: pq.ID})
		if err != nil {
			return fmt.Errorf("no such queue item")
		}
		anchorID := afterItemID
		if anchorID == 0 && pq.CurrentItemID.Valid {
			anchorID = pq.CurrentItemID.Int64
		}

		// Two attempts: if the gap after the anchor is exhausted, renumber
		// (which rewrites every ord) and re-resolve the anchor by id.
		for attempt := 0; attempt < 2; attempt++ {
			var aOrd int64
			if anchorID != 0 {
				after, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: anchorID, QueueID: pq.ID})
				if err != nil {
					return fmt.Errorf("no such anchor item")
				}
				aOrd = after.Ord
			} else {
				// No pointer at all — move to the absolute head.
				first, err := q.FirstQueueItem(ctx, pq.ID)
				if err != nil {
					return err
				}
				aOrd = first.Ord - 2*queueOrdGap
			}

			next, err := q.NextQueueItem(ctx, sqlc.NextQueueItemParams{QueueID: pq.ID, Ord: aOrd})
			var newOrd int64
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				newOrd = aOrd + queueOrdGap // tail slot
			case err != nil:
				return err
			case next.ID == item.ID:
				newOrd = next.Ord // already in place — no-op
			case next.Ord-aOrd > 1:
				newOrd = aOrd + (next.Ord-aOrd)/2
			default:
				if err := q.RenumberQueueItems(ctx, pq.ID); err != nil {
					return err
				}
				continue
			}
			item, err = q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: item.ID, QueueID: pq.ID})
			if err != nil {
				return err
			}
			if newOrd != item.Ord {
				if err := q.SetQueueItemOrd(ctx, sqlc.SetQueueItemOrdParams{ID: item.ID, QueueID: pq.ID, Ord: newOrd}); err != nil {
					return err
				}
			}
			out, err = q.BumpQueueVersion(ctx, pq.ID)
			return err
		}
		return fmt.Errorf("no slot found after renumber")
	})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "items", 0)
	return nil
}

// JumpToQueueItem moves the pointer to an arbitrary item (sidebar click).
func (a *App) JumpToQueueItem(ctx context.Context, userID int64, deviceID string, itemID int64) (QueueView, error) {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		if _, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: itemID, QueueID: pq.ID}); err != nil {
			return fmt.Errorf("no such queue item")
		}
		out, err = q.SetQueuePointer(ctx, sqlc.SetQueuePointerParams{
			QueueID:         pq.ID,
			CurrentItemID:   pgtype.Int8{Int64: itemID, Valid: true},
			PositionSeconds: 0, Playing: true,
		})
		return err
	})
	if err != nil {
		return QueueView{}, err
	}
	a.emitQueue(userID, out, "pointer", 0)
	return a.GetQueue(ctx, userID, deviceID, nil, queueWindowDefault)
}

// AdvanceQueue moves the pointer per queue order. Idempotent: the caller
// names the item it finished (fromItemID); a stale double-fire (already
// advanced) is a silent no-op. reason ∈ ended | skip | prev.
func (a *App) AdvanceQueue(ctx context.Context, userID int64, deviceID string, fromItemID int64, reason string) (QueueView, error) {
	var out sqlc.PlayQueue
	var moved, pruned bool
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		if !pq.CurrentItemID.Valid || pq.CurrentItemID.Int64 != fromItemID {
			return nil // stale — someone already moved the pointer
		}
		cur, err := q.GetQueueItem(ctx, sqlc.GetQueueItemParams{ID: fromItemID, QueueID: pq.ID})
		if err != nil {
			return fmt.Errorf("pointer item missing")
		}

		var target sqlc.PlayQueueItem
		switch {
		case reason == "prev":
			target, err = q.PrevQueueItem(ctx, sqlc.PrevQueueItemParams{QueueID: pq.ID, Ord: cur.Ord})
			if errors.Is(err, pgx.ErrNoRows) {
				target, err = cur, nil // at the head — restart current
			}
		case reason == "ended" && pq.RepeatMode == "one":
			target = cur
		default:
			target, err = q.NextQueueItem(ctx, sqlc.NextQueueItemParams{QueueID: pq.ID, Ord: cur.Ord})
			if errors.Is(err, pgx.ErrNoRows) {
				if pq.RepeatMode == "all" {
					target, err = q.FirstQueueItem(ctx, pq.ID)
				} else {
					// Queue ended: pointer stays, playback stops.
					out, err = q.SetQueuePointer(ctx, sqlc.SetQueuePointerParams{
						QueueID:         pq.ID,
						CurrentItemID:   pq.CurrentItemID,
						PositionSeconds: 0, Playing: false,
					})
					moved = true
					return err
				}
			}
		}
		if err != nil {
			return err
		}

		out, err = q.SetQueuePointer(ctx, sqlc.SetQueuePointerParams{
			QueueID:         pq.ID,
			CurrentItemID:   pgtype.Int8{Int64: target.ID, Valid: true},
			PositionSeconds: 0, Playing: true,
		})
		if err != nil {
			return err
		}
		moved = true

		// Trim history behind the new pointer.
		cutoff, err := q.QueueHistoryCutoff(ctx, sqlc.QueueHistoryCutoffParams{QueueID: pq.ID, Ord: target.Ord, Offset: queueHistoryKeep})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err := q.DeleteQueueItemsThrough(ctx, sqlc.DeleteQueueItemsThroughParams{QueueID: pq.ID, Ord: cutoff})
		pruned = n > 0
		return err
	})
	if err != nil {
		return QueueView{}, err
	}
	if moved {
		kind := "pointer"
		if pruned {
			kind = "items"
		}
		a.emitQueue(userID, out, kind, 0)
		a.processQueueDJBestEffort(ctx, userID, deviceID)
	}
	return a.GetQueue(ctx, userID, deviceID, nil, queueWindowDefault)
}

// SetQueueShuffle reorders the upcoming slice server-side: on = fresh
// random order, off = the source's natural order (src_ord). Played
// history and the current track never move.
func (a *App) SetQueueShuffle(ctx context.Context, userID int64, deviceID string, on bool) error {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if err != nil {
			return fmt.Errorf("no queue")
		}
		if pq.DjMode != DJModeOff {
			return fmt.Errorf("turn off the DJ before changing shuffle")
		}
		if err := q.ReorderUpcoming(ctx, sqlc.ReorderUpcomingParams{QueueID: pq.ID, Shuffle: on, AfterOrd: anchorOrDefault(ctx, q, pq)}); err != nil {
			return err
		}
		out, err = q.SetQueueModes(ctx, sqlc.SetQueueModesParams{QueueID: pq.ID, RepeatMode: pq.RepeatMode, Shuffled: on})
		return err
	})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "items", 0)
	return nil
}

// SetQueueRepeat sets the repeat mode.
func (a *App) SetQueueRepeat(ctx context.Context, userID int64, deviceID, mode string) error {
	if mode != "off" && mode != "all" && mode != "one" {
		return fmt.Errorf("invalid repeat mode %q", mode)
	}
	q := sqlc.New(a.db)
	pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
	if err != nil {
		return fmt.Errorf("no queue")
	}
	out, err := q.SetQueueModes(ctx, sqlc.SetQueueModesParams{QueueID: pq.ID, RepeatMode: mode, Shuffled: pq.Shuffled})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "modes", 0)
	return nil
}

// QueueHeartbeat records the renderer's coarse position (~15s cadence
// while playing). Only the active output may report; anything else gets
// ErrQueueNotActiveOutput and should stop rendering.
func (a *App) QueueHeartbeat(ctx context.Context, userID int64, deviceID, output string, positionSec float64, playing bool) error {
	q := sqlc.New(a.db)
	pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
	if err != nil {
		return fmt.Errorf("no queue")
	}
	if pq.ActiveOutput != "" && output != "" && pq.ActiveOutput != output {
		return ErrQueueNotActiveOutput
	}
	out, err := q.SetQueueTransport(ctx, sqlc.SetQueueTransportParams{
		QueueID: pq.ID, PositionSeconds: float32(positionSec), Playing: playing,
	})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "transport", 0)
	return nil
}

// ClaimQueueOutput makes `output` the one renderer. Every other client
// sees the event and drops to mirror mode.
func (a *App) ClaimQueueOutput(ctx context.Context, userID int64, deviceID, output string) error {
	q := sqlc.New(a.db)
	pq, err := q.EnsurePlayQueue(ctx, sqlc.EnsurePlayQueueParams{UserID: userID, DeviceID: deviceID})
	if err != nil {
		return err
	}
	out, err := q.SetQueueOutput(ctx, sqlc.SetQueueOutputParams{QueueID: pq.ID, ActiveOutput: output})
	if err != nil {
		return err
	}
	a.emitQueue(userID, out, "output", 0)
	return nil
}

// ClearUpcoming drops everything after the current item (the sidebar's
// "Clear" on the Up Next header); history and the playing track stay.
func (a *App) ClearUpcoming(ctx context.Context, userID int64, deviceID string) error {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		n, err := q.DeleteUpcomingQueueItems(ctx, sqlc.DeleteUpcomingQueueItemsParams{
			QueueID: pq.ID, Ord: anchorOrDefault(ctx, q, pq),
		})
		if err != nil {
			return err
		}
		if pq.DjMode != DJModeOff {
			out, err = q.SetQueueDJMode(ctx, sqlc.SetQueueDJModeParams{QueueID: pq.ID, DjMode: DJModeOff})
			return err
		}
		if n == 0 {
			return nil
		}
		out, err = q.BumpQueueVersion(ctx, pq.ID)
		return err
	})
	if err != nil {
		return err
	}
	if out.ID != 0 {
		a.emitQueue(userID, out, "items", 0)
	}
	return nil
}

// ClearQueue empties the queue (pointer included).
func (a *App) ClearQueue(ctx context.Context, userID int64, deviceID string) error {
	var out sqlc.PlayQueue
	err := a.withTx(ctx, func(q *sqlc.Queries) error {
		pq, err := q.GetPlayQueueByUserDevice(ctx, sqlc.GetPlayQueueByUserDeviceParams{UserID: userID, DeviceID: deviceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.DeleteAllQueueItems(ctx, pq.ID); err != nil {
			return err
		}
		out, err = q.SetQueueReplaced(ctx, sqlc.SetQueueReplacedParams{
			QueueID: pq.ID, Source: []byte(`{}`), Shuffled: false,
			CurrentItemID: pgtype.Int8{}, Playing: false,
		})
		return err
	})
	if err != nil {
		return err
	}
	if out.ID != 0 {
		a.emitQueue(userID, out, "replaced", 0)
	}
	return nil
}

func (a *App) emitQueue(userID int64, pq sqlc.PlayQueue, kind string, trackID int64) {
	if a.hub == nil {
		return
	}
	payload := eventhub.QueueChangedPayload{
		DeviceID:     pq.DeviceID,
		Version:      pq.Version,
		Kind:         kind,
		PositionSec:  float64(pq.PositionSeconds),
		Playing:      pq.Playing,
		RepeatMode:   pq.RepeatMode,
		Shuffled:     pq.Shuffled,
		DJMode:       pq.DjMode,
		ActiveOutput: pq.ActiveOutput,
		TrackID:      trackID,
	}
	if pq.CurrentItemID.Valid {
		payload.CurrentItemID = pq.CurrentItemID.Int64
	}
	a.hub.EmitToUser(userID, eventhub.EventQueueChanged, payload)
}
