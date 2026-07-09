package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Service passthroughs for the Subsonic-compatible API (internal/subsonic).
// Same rule as the Jellyfin layer: handlers never touch sqlc/pgx directly —
// every read goes through App. These queries are raw pgx on purpose (see
// subsonic_credentials.go): purpose-built row shapes for the Subsonic DTOs,
// no codegen, no collision with concurrently-edited query files. The heavy
// generic listers (artists/albums/tracks by ids/search) are reused from the
// JF* passthroughs in jellyfin_query.go.

// SubsonicArtistRow backs getArtists/getIndexes/getArtist: one artist with
// its album count and the media-item context needed for cover art + slugs.
type SubsonicArtistRow struct {
	ArtistID      int64
	Name          string
	SortName      string
	MediaItemID   int64
	Slug          string
	AlbumCount    int64
	MusicbrainzID string
	Biography     string
}

const subsonicArtistSelect = `
	SELECT ar.id, ar.name, ar.sort_name, ar.media_item_id, mi.slug,
	       (SELECT count(*) FROM albums al WHERE al.artist_id = ar.id) AS album_count,
	       ar.musicbrainz_id, ar.biography
	FROM artists ar
	JOIN media_item_cards mi ON mi.id = ar.media_item_id
	JOIN libraries l ON l.id = mi.library_id
	WHERE l.media_type = 'music'
`

func scanSubsonicArtists(rows pgx.Rows) ([]SubsonicArtistRow, error) {
	defer rows.Close()
	var out []SubsonicArtistRow
	for rows.Next() {
		var r SubsonicArtistRow
		if err := rows.Scan(&r.ArtistID, &r.Name, &r.SortName, &r.MediaItemID, &r.Slug,
			&r.AlbumCount, &r.MusicbrainzID, &r.Biography); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SubsonicListArtists returns every music artist (optionally scoped to one
// library), name-ordered. Personal-library scale: no paging needed —
// getArtists returns the full index in one response by spec.
func (a *App) SubsonicListArtists(ctx context.Context, libraryID int64) ([]SubsonicArtistRow, error) {
	rows, err := a.db.Query(ctx, subsonicArtistSelect+`
	  AND ($1::bigint = 0 OR mi.library_id = $1)
	ORDER BY lower(COALESCE(NULLIF(ar.sort_name, ''), ar.name)) ASC`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("subsonic list artists: %w", err)
	}
	return scanSubsonicArtists(rows)
}

// SubsonicArtistByID resolves one artist by artists.id.
func (a *App) SubsonicArtistByID(ctx context.Context, artistID int64) (SubsonicArtistRow, error) {
	rows, err := a.db.Query(ctx, subsonicArtistSelect+` AND ar.id = $1`, artistID)
	if err != nil {
		return SubsonicArtistRow{}, fmt.Errorf("subsonic artist by id: %w", err)
	}
	out, err := scanSubsonicArtists(rows)
	if err != nil {
		return SubsonicArtistRow{}, err
	}
	if len(out) == 0 {
		return SubsonicArtistRow{}, pgx.ErrNoRows
	}
	return out[0], nil
}

// SubsonicArtistByName resolves an artist by exact (case-insensitive) name —
// getTopSongs addresses artists by name, not id.
func (a *App) SubsonicArtistByName(ctx context.Context, name string) (SubsonicArtistRow, error) {
	rows, err := a.db.Query(ctx, subsonicArtistSelect+` AND lower(ar.name) = lower($1) LIMIT 1`, name)
	if err != nil {
		return SubsonicArtistRow{}, fmt.Errorf("subsonic artist by name: %w", err)
	}
	out, err := scanSubsonicArtists(rows)
	if err != nil {
		return SubsonicArtistRow{}, err
	}
	if len(out) == 0 {
		return SubsonicArtistRow{}, pgx.ErrNoRows
	}
	return out[0], nil
}

// SubsonicGenreRow is one getGenres entry: albums carry the genre tags, and
// a song inherits its album's genres (tracks have no own genre column).
type SubsonicGenreRow struct {
	Name       string
	AlbumCount int64
	SongCount  int64
}

// SubsonicListGenres aggregates album genre tags across the music libraries.
func (a *App) SubsonicListGenres(ctx context.Context) ([]SubsonicGenreRow, error) {
	rows, err := a.db.Query(ctx, `
		SELECT g.genre,
		       count(DISTINCT al.id) AS album_count,
		       count(t.id) AS song_count
		FROM albums al
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_item_cards mi ON mi.id = ar.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		CROSS JOIN LATERAL unnest(al.genres) AS g(genre)
		LEFT JOIN tracks t ON t.album_id = al.id
		WHERE l.media_type = 'music' AND g.genre <> ''
		GROUP BY g.genre
		ORDER BY count(t.id) DESC, g.genre ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("subsonic list genres: %w", err)
	}
	defer rows.Close()
	var out []SubsonicGenreRow
	for rows.Next() {
		var r SubsonicGenreRow
		if err := rows.Scan(&r.Name, &r.AlbumCount, &r.SongCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SubsonicTrackIDsByGenre returns track ids whose album carries the genre
// (case-insensitive), album-ordered, paginated.
func (a *App) SubsonicTrackIDsByGenre(ctx context.Context, genre string, limit, offset int32) ([]int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 10
	}
	rows, err := a.db.Query(ctx, `
		SELECT t.id
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		WHERE EXISTS (SELECT 1 FROM unnest(al.genres) g WHERE lower(g) = lower($1))
		ORDER BY al.id ASC, t.disc_number ASC, t.track_number ASC
		LIMIT $2 OFFSET $3
	`, genre, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("subsonic tracks by genre: %w", err)
	}
	return scanIDs(rows)
}

// SubsonicRandomTrackIDs returns random track ids with the spec's optional
// genre / year-range filters (year and genre both live on the album).
func (a *App) SubsonicRandomTrackIDs(ctx context.Context, size int32, genre string, fromYear, toYear int32) ([]int64, error) {
	if size <= 0 || size > 500 {
		size = 10
	}
	rows, err := a.db.Query(ctx, `
		SELECT t.id
		FROM tracks t
		JOIN albums al ON al.id = t.album_id
		WHERE ($2::text = '' OR EXISTS (SELECT 1 FROM unnest(al.genres) g WHERE lower(g) = lower($2)))
		  AND ($3::int = 0 OR COALESCE(NULLIF(al.year, '')::int, 0) >= $3)
		  AND ($4::int = 0 OR COALESCE(NULLIF(al.year, '')::int, 0) <= $4)
		ORDER BY random()
		LIMIT $1
	`, size, genre, fromYear, toYear)
	if err != nil {
		return nil, fmt.Errorf("subsonic random tracks: %w", err)
	}
	return scanIDs(rows)
}

// SubsonicAlbumIDsByList backs getAlbumList2. listType dispatch:
//
//	newest    — most recently added (min track_file created_at per album)
//	frequent  — user's most-played albums (play_events)
//	recent    — user's most recently played albums
//	byYear    — album year within [fromYear, toYear] (swapped = reversed)
//	byGenre   — album genre tag match
//
// alphabetical/random/starred variants are served from existing listers by
// the caller. Returns ids in result order — the caller re-orders hydrated
// rows to match.
func (a *App) SubsonicAlbumIDsByList(ctx context.Context, listType string, userID int64, size, offset int32, genre string, fromYear, toYear int32) ([]int64, error) {
	if size <= 0 || size > 500 {
		size = 10
	}
	switch listType {
	case "newest":
		rows, err := a.db.Query(ctx, `
			SELECT al.id
			FROM albums al
			JOIN artists ar ON ar.id = al.artist_id
			JOIN media_item_cards mi ON mi.id = ar.media_item_id
			JOIN libraries l ON l.id = mi.library_id
			WHERE l.media_type = 'music'
			ORDER BY (SELECT max(tf.created_at) FROM track_files tf
			          JOIN tracks t ON t.id = tf.track_id
			          WHERE t.album_id = al.id) DESC NULLS LAST, al.id DESC
			LIMIT $1 OFFSET $2
		`, size, offset)
		if err != nil {
			return nil, fmt.Errorf("subsonic newest albums: %w", err)
		}
		return scanIDs(rows)
	case "frequent":
		rows, err := a.db.Query(ctx, `
			SELECT t.album_id
			FROM play_events pe
			JOIN tracks t ON t.id = pe.track_id
			WHERE pe.user_id = $1
			GROUP BY t.album_id
			ORDER BY count(*) DESC
			LIMIT $2 OFFSET $3
		`, userID, size, offset)
		if err != nil {
			return nil, fmt.Errorf("subsonic frequent albums: %w", err)
		}
		return scanIDs(rows)
	case "recent":
		rows, err := a.db.Query(ctx, `
			SELECT t.album_id
			FROM play_events pe
			JOIN tracks t ON t.id = pe.track_id
			WHERE pe.user_id = $1
			GROUP BY t.album_id
			ORDER BY max(pe.played_at) DESC
			LIMIT $2 OFFSET $3
		`, userID, size, offset)
		if err != nil {
			return nil, fmt.Errorf("subsonic recent albums: %w", err)
		}
		return scanIDs(rows)
	case "byYear":
		lo, hi, desc := fromYear, toYear, false
		if lo > hi {
			lo, hi, desc = hi, lo, true
		}
		rows, err := a.db.Query(ctx, `
			SELECT al.id
			FROM albums al
			JOIN artists ar ON ar.id = al.artist_id
			JOIN media_item_cards mi ON mi.id = ar.media_item_id
			JOIN libraries l ON l.id = mi.library_id
			WHERE l.media_type = 'music'
			  AND COALESCE(NULLIF(al.year, '')::int, 0) BETWEEN $1 AND $2
			ORDER BY
			  CASE WHEN $5::bool THEN NULLIF(al.year, '') END DESC NULLS LAST,
			  CASE WHEN NOT $5::bool THEN NULLIF(al.year, '') END ASC NULLS LAST,
			  lower(al.title) ASC
			LIMIT $3 OFFSET $4
		`, lo, hi, size, offset, desc)
		if err != nil {
			return nil, fmt.Errorf("subsonic albums by year: %w", err)
		}
		return scanIDs(rows)
	case "byGenre":
		rows, err := a.db.Query(ctx, `
			SELECT al.id
			FROM albums al
			JOIN artists ar ON ar.id = al.artist_id
			JOIN media_item_cards mi ON mi.id = ar.media_item_id
			JOIN libraries l ON l.id = mi.library_id
			WHERE l.media_type = 'music'
			  AND EXISTS (SELECT 1 FROM unnest(al.genres) g WHERE lower(g) = lower($1))
			ORDER BY lower(al.title) ASC
			LIMIT $2 OFFSET $3
		`, genre, size, offset)
		if err != nil {
			return nil, fmt.Errorf("subsonic albums by genre: %w", err)
		}
		return scanIDs(rows)
	}
	return nil, fmt.Errorf("unknown album list type %q", listType)
}

// SubsonicAlbumAddedAt batch-resolves the "created" timestamp Subsonic DTOs
// carry per album: the newest track file's arrival time.
func (a *App) SubsonicAlbumAddedAt(ctx context.Context, albumIDs []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time, len(albumIDs))
	if len(albumIDs) == 0 {
		return out, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT t.album_id, max(tf.created_at)
		FROM track_files tf
		JOIN tracks t ON t.id = tf.track_id
		WHERE t.album_id = ANY($1::bigint[])
		GROUP BY t.album_id
	`, albumIDs)
	if err != nil {
		return nil, fmt.Errorf("subsonic album added-at: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ts time.Time
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		out[id] = ts
	}
	return out, rows.Err()
}

// SubsonicTrackFileInfo is the per-track best-file decoration for Child DTOs
// (suffix, bitrate, size) and the stream/download byte source.
type SubsonicTrackFileInfo struct {
	TrackID       int64
	TrackFileID   int64
	LibraryFileID int64
	Format        string
	BitrateKbps   int32
	SizeBytes     int64
	Duration      int32
	Path          string
}

// SubsonicTrackBestFiles resolves the best (highest quality_score) file per
// track, batched.
func (a *App) SubsonicTrackBestFiles(ctx context.Context, trackIDs []int64) (map[int64]SubsonicTrackFileInfo, error) {
	out := make(map[int64]SubsonicTrackFileInfo, len(trackIDs))
	if len(trackIDs) == 0 {
		return out, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT ON (tf.track_id)
		       tf.track_id, tf.id, tf.library_file_id, tf.format, tf.bitrate_kbps,
		       tf.size_bytes, tf.duration, lf.path
		FROM track_files tf
		JOIN library_files lf ON lf.id = tf.library_file_id
		WHERE tf.track_id = ANY($1::bigint[]) AND lf.deleted_at IS NULL
		ORDER BY tf.track_id, tf.quality_score DESC, tf.id ASC
	`, trackIDs)
	if err != nil {
		return nil, fmt.Errorf("subsonic track files: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r SubsonicTrackFileInfo
		if err := rows.Scan(&r.TrackID, &r.TrackFileID, &r.LibraryFileID, &r.Format,
			&r.BitrateKbps, &r.SizeBytes, &r.Duration, &r.Path); err != nil {
			return nil, err
		}
		out[r.TrackID] = r
	}
	return out, rows.Err()
}

// SubsonicPlayQueue is the getPlayQueue/savePlayQueue state (one per user).
type SubsonicPlayQueue struct {
	TrackIDs       []int64
	CurrentTrackID int64
	PositionMs     int64
	ChangedAt      time.Time
	ChangedBy      string
}

// GetSubsonicPlayQueue returns the saved queue, ok=false when none exists.
func (a *App) GetSubsonicPlayQueue(ctx context.Context, userID int64) (SubsonicPlayQueue, bool, error) {
	var q SubsonicPlayQueue
	err := a.db.QueryRow(ctx, `
		SELECT track_ids, current_track_id, position_ms, changed_at, changed_by
		FROM subsonic_play_queues WHERE user_id = $1
	`, userID).Scan(&q.TrackIDs, &q.CurrentTrackID, &q.PositionMs, &q.ChangedAt, &q.ChangedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubsonicPlayQueue{}, false, nil
		}
		return SubsonicPlayQueue{}, false, fmt.Errorf("get subsonic play queue: %w", err)
	}
	return q, true, nil
}

// SaveSubsonicPlayQueue upserts the user's queue (last writer wins, per
// spec). An empty id list clears the saved queue.
func (a *App) SaveSubsonicPlayQueue(ctx context.Context, userID int64, q SubsonicPlayQueue) error {
	if len(q.TrackIDs) == 0 {
		_, err := a.db.Exec(ctx, `DELETE FROM subsonic_play_queues WHERE user_id = $1`, userID)
		return err
	}
	_, err := a.db.Exec(ctx, `
		INSERT INTO subsonic_play_queues (user_id, track_ids, current_track_id, position_ms, changed_at, changed_by)
		VALUES ($1, $2, $3, $4, now(), $5)
		ON CONFLICT (user_id) DO UPDATE SET
		  track_ids = EXCLUDED.track_ids,
		  current_track_id = EXCLUDED.current_track_id,
		  position_ms = EXCLUDED.position_ms,
		  changed_at = now(),
		  changed_by = EXCLUDED.changed_by
	`, userID, q.TrackIDs, q.CurrentTrackID, q.PositionMs, q.ChangedBy)
	if err != nil {
		return fmt.Errorf("save subsonic play queue: %w", err)
	}
	return nil
}

// SubsonicTrackPlayCounts returns the user's play_events count per track —
// Child.playCount decoration for the page of tracks being rendered.
func (a *App) SubsonicTrackPlayCounts(ctx context.Context, userID int64, trackIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(trackIDs))
	if len(trackIDs) == 0 {
		return out, nil
	}
	rows, err := a.db.Query(ctx, `
		SELECT track_id, count(*)
		FROM play_events
		WHERE user_id = $1 AND track_id = ANY($2::bigint[])
		GROUP BY track_id
	`, userID, trackIDs)
	if err != nil {
		return nil, fmt.Errorf("subsonic play counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func scanIDs(rows pgx.Rows) ([]int64, error) {
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
