package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Release calendar: date-anchored events across every domain — episodes
// airing, movies releasing, albums dropping, books publishing. The service is
// deliberately not manager-specific: the manager route is one (admin) consumer,
// and the public library sections will embed the same data later with their
// own scoping. All dates are calendar dates (no times are stored upstream).

type CalendarEventView struct {
	ID          string `json:"id" doc:"Stable opaque event id, e.g. ep:123 / movie:9 / album:d42 — media_item_id alone is not unique for episodes or artist releases"`
	Date        string `json:"date" doc:"YYYY-MM-DD"`
	Kind        string `json:"kind" enum:"episode,movie,album,book" doc:"What kind of release this is"`
	LibraryID   int64  `json:"library_id"`
	MediaType   string `json:"media_type"`
	MediaItemID int64  `json:"media_item_id" doc:"The owning item: series, movie, artist, or book"`
	Slug        string `json:"slug"`
	Title       string `json:"title" doc:"Series / movie / artist / book name"`
	Season      int32  `json:"season,omitempty"`
	Episode     int32  `json:"episode,omitempty"`
	EpisodeName string `json:"episode_name,omitempty"`
	Milestone   string `json:"milestone,omitempty" enum:",series_premiere,season_premiere,season_finale,series_finale" doc:"Season/series boundary markers for episode events"`
	Special     bool   `json:"special,omitempty"`
	AlbumTitle  string `json:"album_title,omitempty"`
	AlbumType   string `json:"album_type,omitempty" doc:"Resolved release bucket: album | ep | single | live | ..."`
	AlbumRef    string `json:"album_ref,omitempty" doc:"Manager album page ref: local id or d<discography id>"`
	HasFile     bool   `json:"has_file" doc:"Episode/movie/book: a live file exists. Album: the linked local copy is complete."`
	Monitored   bool   `json:"monitored"`
}

type CalendarParams struct {
	From       time.Time
	To         time.Time
	LibraryIDs []int64 // empty = every library
	Monitored  bool    // true = monitored items only
}

// calendarMaxWindowDays bounds one request: a padded month view needs 42
// days, a quarter agenda 92 — anything longer is a data export, not a
// calendar, and the per-domain scans stop being cheap.
const calendarMaxWindowDays = 93

func (a *App) CalendarEvents(ctx context.Context, p CalendarParams) ([]CalendarEventView, error) {
	if p.To.Before(p.From) {
		return nil, fmt.Errorf("calendar: to precedes from")
	}
	// +4h slop: callers may mix local-time defaults with UTC-parsed bounds,
	// and a DST transition inside the span adds an hour — a 93-day calendar
	// window must not be rejected for being 93 days and one hour long.
	if p.To.Sub(p.From) > calendarMaxWindowDays*24*time.Hour+4*time.Hour {
		return nil, fmt.Errorf("calendar: window exceeds %d days", calendarMaxWindowDays)
	}

	// Domain queries share the bind layout: $1 from, $2 to, $3 library filter
	// ($3 empty = all), $4 monitored-only. The filter must be a non-nil slice:
	// pgx encodes nil as SQL NULL, and cardinality(NULL) is NULL — which
	// silently rejects every row instead of meaning "all libraries".
	libraryIDs := p.LibraryIDs
	if libraryIDs == nil {
		libraryIDs = []int64{}
	}
	args := []any{p.From, p.To, libraryIDs, p.Monitored}

	events := []CalendarEventView{}
	collect := func(query, kind string, scan func(rows pgx.Rows, date *pgtype.Date, ev *CalendarEventView) error) error {
		rows, err := a.db.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("calendar %s: %w", kind, err)
		}
		defer rows.Close()
		for rows.Next() {
			ev := CalendarEventView{Kind: kind}
			var date pgtype.Date
			if err := scan(rows, &date, &ev); err != nil {
				return err
			}
			ev.Date = dateToString(date)
			events = append(events, ev)
		}
		return rows.Err()
	}

	// Season/series boundaries are derived, not provider-flagged: a finale is
	// the highest-numbered episode of its season (TMDB schedules whole seasons
	// ahead, so this holds for scheduled seasons — a half-announced season can
	// briefly mark its newest known episode), and a series finale is the
	// finale of an ended show's last season.
	if err := collect(`
		SELECT e.id, e.air_date, mi.library_id, c.media_type::text, c.id, c.slug, c.title,
		       s.season_number, e.episode_number, e.title, e.is_special,
		       (NOT e.is_special AND e.episode_number = (
		          SELECT max(e2.episode_number) FROM tv_episodes e2
		          WHERE e2.season_id = e.season_id AND NOT e2.is_special
		       )),
		       COALESCE(s.season_number = (
		          SELECT max(s2.season_number) FROM tv_seasons s2
		          WHERE s2.series_id = sr.id AND s2.season_number > 0
		       ) AND sr.status IN ('ended', 'canceled'), false),
		       EXISTS (
		         SELECT 1 FROM library_file_links lfl
		         JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
		         WHERE lfl.tv_episode_id = e.id
		       ),
		       mi.monitored
		FROM tv_episodes e
		JOIN tv_seasons s ON s.id = e.season_id
		JOIN tv_series sr ON sr.id = s.series_id
		JOIN media_items mi ON mi.id = sr.media_item_id
		JOIN media_item_cards c ON c.id = mi.id
		WHERE e.air_date BETWEEN $1 AND $2
		  AND (cardinality($3::bigint[]) = 0 OR mi.library_id = ANY($3))
		  AND (NOT $4 OR mi.monitored)`,
		"episode", func(rows pgx.Rows, date *pgtype.Date, ev *CalendarEventView) error {
			var episodeID int64
			var lastOfSeason, endedLastSeason bool
			if err := rows.Scan(&episodeID, date, &ev.LibraryID, &ev.MediaType, &ev.MediaItemID, &ev.Slug, &ev.Title,
				&ev.Season, &ev.Episode, &ev.EpisodeName, &ev.Special,
				&lastOfSeason, &endedLastSeason,
				&ev.HasFile, &ev.Monitored); err != nil {
				return err
			}
			ev.ID = fmt.Sprintf("ep:%d", episodeID)
			switch {
			case ev.Special:
			case ev.Episode == 1 && ev.Season == 1:
				ev.Milestone = "series_premiere"
			case ev.Episode == 1:
				ev.Milestone = "season_premiere"
			case lastOfSeason && endedLastSeason:
				ev.Milestone = "series_finale"
			case lastOfSeason:
				ev.Milestone = "season_finale"
			}
			return nil
		}); err != nil {
		return nil, err
	}

	if err := collect(`
		SELECT m.release_date, mi.library_id, c.media_type::text, c.id, c.slug, c.title,
		       EXISTS (
		         SELECT 1 FROM library_file_links lfl
		         JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
		         WHERE lfl.media_item_id = mi.id AND lfl.relation_type IN ('primary', 'part')
		       ),
		       mi.monitored
		FROM movies m
		JOIN media_items mi ON mi.id = m.media_item_id
		JOIN media_item_cards c ON c.id = mi.id
		WHERE m.release_date BETWEEN $1 AND $2
		  AND (cardinality($3::bigint[]) = 0 OR mi.library_id = ANY($3))
		  AND (NOT $4 OR mi.monitored)`,
		"movie", func(rows pgx.Rows, date *pgtype.Date, ev *CalendarEventView) error {
			if err := rows.Scan(date, &ev.LibraryID, &ev.MediaType, &ev.MediaItemID, &ev.Slug, &ev.Title,
				&ev.HasFile, &ev.Monitored); err != nil {
				return err
			}
			ev.ID = fmt.Sprintf("movie:%d", ev.MediaItemID)
			return nil
		}); err != nil {
		return nil, err
	}

	// Albums: has_file means the linked local copy is COMPLETE — a half-ripped
	// album on release day should read as missing, matching the library lens.
	if err := collect(`
		SELECT d.release_date, mi.library_id, c.media_type::text, c.id, c.slug, c.title,
		       d.title, d.album_type, d.secondary_types, d.id, d.album_id,
		       COALESCE(ts.have > 0 AND ts.have >= ts.total, false),
		       mi.monitored
		FROM artist_discography d
		JOIN artists ar ON ar.id = d.artist_id
		JOIN media_items mi ON mi.id = ar.media_item_id
		JOIN media_item_cards c ON c.id = mi.id
		LEFT JOIN LATERAL (
		  SELECT count(DISTINCT t.id)::int AS total,
		         count(DISTINCT t.id) FILTER (WHERE lf.id IS NOT NULL)::int AS have
		  FROM tracks t
		  LEFT JOIN track_files tf ON tf.track_id = t.id
		  LEFT JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
		  WHERE t.album_id = d.album_id
		) ts ON d.album_id IS NOT NULL
		WHERE d.release_date BETWEEN $1 AND $2
		  AND (cardinality($3::bigint[]) = 0 OR mi.library_id = ANY($3))
		  AND (NOT $4 OR mi.monitored)`,
		"album", func(rows pgx.Rows, date *pgtype.Date, ev *CalendarEventView) error {
			var secondary []string
			var discoID int64
			var albumID pgtype.Int8
			if err := rows.Scan(date, &ev.LibraryID, &ev.MediaType, &ev.MediaItemID, &ev.Slug, &ev.Title,
				&ev.AlbumTitle, &ev.AlbumType, &secondary, &discoID, &albumID,
				&ev.HasFile, &ev.Monitored); err != nil {
				return err
			}
			ev.AlbumType = effectiveReleaseType(ev.AlbumType, secondary, ev.AlbumTitle)
			if albumID.Valid {
				ev.AlbumRef = fmt.Sprintf("%d", albumID.Int64)
			} else {
				ev.AlbumRef = fmt.Sprintf("d%d", discoID)
			}
			// The DISCOGRAPHY row is the event's identity — always unique even
			// if legacy data double-links one local album, and stable across a
			// release getting linked (AlbumRef changes, the id doesn't).
			ev.ID = fmt.Sprintf("album:d%d", discoID)
			return nil
		}); err != nil {
		return nil, err
	}

	if err := collect(`
		SELECT b.publish_date, mi.library_id, c.media_type::text, c.id, c.slug, c.title,
		       EXISTS (
		         SELECT 1 FROM library_files lf
		         WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		       ),
		       mi.monitored
		FROM books b
		JOIN media_items mi ON mi.id = b.media_item_id
		JOIN media_item_cards c ON c.id = mi.id
		WHERE b.publish_date BETWEEN $1 AND $2
		  AND (cardinality($3::bigint[]) = 0 OR mi.library_id = ANY($3))
		  AND (NOT $4 OR mi.monitored)`,
		"book", func(rows pgx.Rows, date *pgtype.Date, ev *CalendarEventView) error {
			if err := rows.Scan(date, &ev.LibraryID, &ev.MediaType, &ev.MediaItemID, &ev.Slug, &ev.Title,
				&ev.HasFile, &ev.Monitored); err != nil {
				return err
			}
			ev.ID = fmt.Sprintf("book:%d", ev.MediaItemID)
			return nil
		}); err != nil {
		return nil, err
	}

	// Date → title → item → season → episode. The item id tie-break matters:
	// consumers collapse same-day episode runs assuming one item's rows are
	// CONTIGUOUS, and two same-titled series (Ghosts US/UK) would otherwise
	// interleave by season number.
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date < events[j].Date
		}
		if events[i].Title != events[j].Title {
			return events[i].Title < events[j].Title
		}
		if events[i].MediaItemID != events[j].MediaItemID {
			return events[i].MediaItemID < events[j].MediaItemID
		}
		if events[i].Season != events[j].Season {
			return events[i].Season < events[j].Season
		}
		return events[i].Episode < events[j].Episode
	})
	return events, nil
}
