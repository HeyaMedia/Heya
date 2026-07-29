package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Manager library lens: the managed view over a library's media_items —
// monitored flags, quality profiles, and per-item completeness (what exists
// on disk vs what the catalog says should exist). Same catalog as the server,
// different room. Everything here is read-shaped for the /manager/library/{id}
// page plus one bulk mutation (monitored / profile assignment).
//
// The completeness unit differs per domain:
//   movie  → the movie itself (a primary/part-linked live file)
//   tv     → aired, non-special episodes with a live linked file
//   music  → catalog tracks with a live track_file (media_item = artist)
//   book   → the book itself (any live file)
// GroupCount carries the domain's container count (seasons / albums).

type ManagerLibraryItemView struct {
	ID               int64  `json:"id"`
	Slug             string `json:"slug"`
	Title            string `json:"title"`
	Year             string `json:"year"`
	MediaType        string `json:"media_type"`
	Status           string `json:"status"`
	Monitored        bool   `json:"monitored"`
	QualityProfileID *int64 `json:"quality_profile_id,omitempty"`
	AddedAt          string `json:"added_at"`
	SizeOnDisk       int64  `json:"size_on_disk"`
	FileCount        int32  `json:"file_count"`
	HaveCount        int32  `json:"have_count"`
	TotalCount       int32  `json:"total_count"`
	MissingCount     int32  `json:"missing_count"`
	GroupCount       int32  `json:"group_count" doc:"Seasons for TV, albums for music, 0 elsewhere"`
}

type ManagerLibraryStatsView struct {
	Items        int      `json:"items"`
	Monitored    int      `json:"monitored"`
	ItemsMissing int      `json:"items_missing" doc:"Items with at least one missing unit"`
	SizeOnDisk   int64    `json:"size_on_disk"`
	UnitsHave    int64    `json:"units_have"`
	UnitsTotal   int64    `json:"units_total"`
	Files        int64    `json:"files"`
	Statuses     []string `json:"statuses" doc:"Distinct metadata statuses present, for the filter dropdown"`
}

type ManagerLibraryRef struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Domain    string `json:"domain" doc:"Quality-profile domain this library maps to (movie|tv|music|book)"`
}

type ManagerLibraryItemsPage struct {
	Library ManagerLibraryRef        `json:"library"`
	Stats   ManagerLibraryStatsView  `json:"stats"`
	Items   []ManagerLibraryItemView `json:"items"`
	Total   int                      `json:"total" doc:"Rows matching the current filters, across all pages"`
	Page    int                      `json:"page"`
	PerPage int                      `json:"per_page"`
}

type ManagerLibraryItemsParams struct {
	Search    string
	Monitored string // "", "monitored", "unmonitored"
	FileState string // "", "missing", "complete"
	Status    string // metadata status, e.g. "returning_series"
	Profile   string // "", "none", or a profile id
	Sort      string // title|year|added|size|missing|units|progress|status
	Dir       string // asc|desc
	Page      int
	PerPage   int
}

// managerProfileDomain maps a library media_type to the quality-profile
// domain vocabulary. Anime is a TV-shaped library with its own profiles by
// convention, so it shares the tv ladder.
func managerProfileDomain(mediaType string) string {
	switch mediaType {
	case "tv", "anime":
		return "tv"
	default:
		return mediaType
	}
}

// managerLibraryRowsCTE returns the WITH-clause body producing the `rows`
// relation every listing/stat query selects from. One shape per domain; the
// only bind parameter is $1 = library id. Every shape emits the same columns.
func managerLibraryRowsCTE(mediaType string) (string, error) {
	// All live bytes attributed to the item, regardless of relation (extras
	// included — like the arrs, size-on-disk answers "what does this item
	// cost me", not "how big is the primary file").
	const sizes = `
sizes AS (
  SELECT media_item_id, sum(size)::bigint AS size_on_disk, count(*)::int AS file_count
  FROM library_files
  WHERE library_id = $1 AND deleted_at IS NULL AND media_item_id IS NOT NULL
  GROUP BY media_item_id
)`
	const rowsHead = `,
rows AS (
  SELECT c.id, c.slug, c.title,
         COALESCE(NULLIF(lower(c.sort_title), ''), lower(c.title)) AS order_title,
         c.year, c.media_type::text AS media_type, c.status,
         mi.monitored, mi.quality_profile_id, c.created_at AS added_at,
         COALESCE(s.size_on_disk, 0) AS size_on_disk,
         COALESCE(s.file_count, 0) AS file_count,`

	switch mediaType {
	case "movie":
		// Presence means a primary (or multi-part) link — a movie with only
		// trailers/extras on disk is still missing.
		return sizes + `,
prim AS (
  SELECT lfl.media_item_id, count(DISTINCT lfl.library_file_id)::int AS primary_files
  FROM library_file_links lfl
  JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
  WHERE lf.library_id = $1 AND lfl.relation_type IN ('primary', 'part')
  GROUP BY lfl.media_item_id
)` + rowsHead + `
         LEAST(COALESCE(p.primary_files, 0), 1) AS have_count,
         1 AS total_count,
         0 AS group_count
  FROM media_item_cards c
  JOIN media_items mi ON mi.id = c.id
  LEFT JOIN sizes s ON s.media_item_id = c.id
  LEFT JOIN prim p ON p.media_item_id = c.id
  WHERE c.library_id = $1
)`, nil
	case "tv", "anime":
		// Totals count aired, non-special episodes (arr semantics: unaired
		// episodes aren't "missing" yet). Have counts any non-special episode
		// with a live file — early files for unaired episodes can push have
		// past total, so missing clamps at zero downstream.
		return sizes + `,
eps AS (
  SELECT sr.media_item_id,
         count(e.id) FILTER (WHERE NOT e.is_special AND e.air_date IS NOT NULL AND e.air_date <= now()::date)::int AS aired,
         count(DISTINCT s2.id) FILTER (WHERE s2.season_number > 0)::int AS seasons
  FROM tv_series sr
  JOIN media_items mi2 ON mi2.id = sr.media_item_id AND mi2.library_id = $1
  JOIN tv_seasons s2 ON s2.series_id = sr.id
  JOIN tv_episodes e ON e.season_id = s2.id
  GROUP BY sr.media_item_id
),
have AS (
  SELECT lfl.media_item_id, count(DISTINCT lfl.tv_episode_id)::int AS have_eps
  FROM library_file_links lfl
  JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
  JOIN tv_episodes e ON e.id = lfl.tv_episode_id AND NOT e.is_special
  WHERE lf.library_id = $1
  GROUP BY lfl.media_item_id
)` + rowsHead + `
         COALESCE(h.have_eps, 0) AS have_count,
         COALESCE(ep.aired, 0) AS total_count,
         COALESCE(ep.seasons, 0) AS group_count
  FROM media_item_cards c
  JOIN media_items mi ON mi.id = c.id
  LEFT JOIN sizes s ON s.media_item_id = c.id
  LEFT JOIN eps ep ON ep.media_item_id = c.id
  LEFT JOIN have h ON h.media_item_id = c.id
  WHERE c.library_id = $1
)`, nil
	case "music":
		// media_item = artist. The albums table is local-first (catalog-only
		// discography lives in music_catalog_*), so track totals are "tracks
		// this library knows about", and missing = partial albums. One pass
		// over the 400k-track join with FILTER beats separate total/have
		// aggregates by ~2.5x.
		return sizes + `,
cat AS (
  SELECT ar.media_item_id,
         count(DISTINCT t.id)::int AS total_tracks,
         count(DISTINCT al.id)::int AS albums,
         count(DISTINCT t.id) FILTER (WHERE lf.id IS NOT NULL)::int AS have_tracks
  FROM artists ar
  JOIN media_items mi2 ON mi2.id = ar.media_item_id AND mi2.library_id = $1
  JOIN albums al ON al.artist_id = ar.id
  JOIN tracks t ON t.album_id = al.id
  LEFT JOIN track_files tf ON tf.track_id = t.id
  LEFT JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
  GROUP BY ar.media_item_id
)` + rowsHead + `
         COALESCE(ct.have_tracks, 0) AS have_count,
         COALESCE(ct.total_tracks, 0) AS total_count,
         COALESCE(ct.albums, 0) AS group_count
  FROM media_item_cards c
  JOIN media_items mi ON mi.id = c.id
  LEFT JOIN sizes s ON s.media_item_id = c.id
  LEFT JOIN cat ct ON ct.media_item_id = c.id
  WHERE c.library_id = $1
)`, nil
	case "book":
		return sizes + rowsHead + `
         LEAST(COALESCE(s.file_count, 0), 1) AS have_count,
         1 AS total_count,
         0 AS group_count
  FROM media_item_cards c
  JOIN media_items mi ON mi.id = c.id
  LEFT JOIN sizes s ON s.media_item_id = c.id
  WHERE c.library_id = $1
)`, nil
	default:
		return "", fmt.Errorf("manager: unsupported library media type %q", mediaType)
	}
}

// managerLibrarySorts whitelists ORDER BY expressions — the sort key comes
// off the query string and must never reach SQL as-is.
var managerLibrarySorts = map[string]string{
	"title":    "order_title",
	"year":     "NULLIF(year, '')::int",
	"added":    "added_at",
	"size":     "size_on_disk",
	"missing":  "GREATEST(total_count - have_count, 0)",
	"units":    "total_count",
	"progress": "CASE WHEN total_count > 0 THEN have_count::float / total_count ELSE NULL END",
	"status":   "NULLIF(status, '')",
}

func (a *App) ManagerLibraryItems(ctx context.Context, libraryID int64, p ManagerLibraryItemsParams) (*ManagerLibraryItemsPage, error) {
	lib, err := a.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, ErrManagerNotFound
	}
	mediaType := string(lib.MediaType)
	cte, err := managerLibraryRowsCTE(mediaType)
	if err != nil {
		return nil, err
	}

	where := []string{"TRUE"}
	args := []any{libraryID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q := strings.TrimSpace(p.Search); q != "" {
		ph := arg("%" + q + "%")
		where = append(where, fmt.Sprintf("(title ILIKE %s OR order_title LIKE lower(%s))", ph, ph))
	}
	switch p.Monitored {
	case "monitored":
		where = append(where, "monitored")
	case "unmonitored":
		where = append(where, "NOT monitored")
	}
	switch p.FileState {
	case "missing":
		where = append(where, "GREATEST(total_count - have_count, 0) > 0")
	case "complete":
		where = append(where, "GREATEST(total_count - have_count, 0) = 0 AND have_count > 0")
	}
	if p.Status != "" {
		where = append(where, "status = "+arg(p.Status))
	}
	switch p.Profile {
	case "":
	case "none":
		where = append(where, "quality_profile_id IS NULL")
	default:
		id, err := strconv.ParseInt(p.Profile, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("manager: bad profile filter %q", p.Profile)
		}
		where = append(where, "quality_profile_id = "+arg(id))
	}

	sortExpr, ok := managerLibrarySorts[p.Sort]
	if !ok {
		sortExpr = managerLibrarySorts["title"]
	}
	dir := "ASC"
	nulls := ""
	if p.Dir == "desc" {
		dir = "DESC"
	}
	// Numeric-ish sorts read best with absent values at the end either way.
	if p.Sort == "year" || p.Sort == "progress" || p.Sort == "status" {
		nulls = " NULLS LAST"
	}

	perPage := p.PerPage
	if perPage <= 0 {
		perPage = 60
	}
	if perPage > 500 {
		perPage = 500
	}
	page := p.Page
	if page < 1 {
		page = 1
	}

	// Stats ride along on every row via CROSS JOIN so the expensive rows CTE
	// (referenced twice → materialized once) is evaluated a single time per
	// request instead of once for the page and again for the header tiles.
	query := "WITH " + cte + `,
stats AS (
  SELECT count(*) AS s_items,
         count(*) FILTER (WHERE monitored) AS s_monitored,
         count(*) FILTER (WHERE GREATEST(total_count - have_count, 0) > 0) AS s_missing,
         COALESCE(sum(size_on_disk), 0)::bigint AS s_size,
         COALESCE(sum(have_count), 0)::bigint AS s_have,
         COALESCE(sum(total_count), 0)::bigint AS s_total,
         COALESCE(sum(file_count), 0)::bigint AS s_files,
         COALESCE(array_agg(DISTINCT status ORDER BY status) FILTER (WHERE status <> ''), '{}') AS s_statuses
  FROM rows
)
SELECT f.*, s.* FROM (
  SELECT id, slug, title, year, media_type, status, monitored, quality_profile_id,
         added_at, size_on_disk, file_count, have_count, total_count,
         GREATEST(total_count - have_count, 0) AS missing_count, group_count,
         count(*) OVER () AS total_rows
  FROM rows
  WHERE ` + strings.Join(where, " AND ") + `
  ORDER BY ` + sortExpr + " " + dir + nulls + `, order_title ASC
  LIMIT ` + arg(perPage) + ` OFFSET ` + arg((page-1)*perPage) + `
) f CROSS JOIN stats s`

	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("manager library items: %w", err)
	}
	defer rows.Close()

	items := make([]ManagerLibraryItemView, 0, perPage)
	total := 0
	var stats *ManagerLibraryStatsView
	for rows.Next() {
		var v ManagerLibraryItemView
		var profileID *int64
		var addedAt time.Time
		var s ManagerLibraryStatsView
		if err := rows.Scan(&v.ID, &v.Slug, &v.Title, &v.Year, &v.MediaType, &v.Status,
			&v.Monitored, &profileID, &addedAt, &v.SizeOnDisk, &v.FileCount,
			&v.HaveCount, &v.TotalCount, &v.MissingCount, &v.GroupCount, &total,
			&s.Items, &s.Monitored, &s.ItemsMissing, &s.SizeOnDisk,
			&s.UnitsHave, &s.UnitsTotal, &s.Files, &s.Statuses); err != nil {
			return nil, err
		}
		v.QualityProfileID = profileID
		v.AddedAt = addedAt.UTC().Format(time.RFC3339)
		items = append(items, v)
		stats = &s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Zero matching rows means the CROSS JOIN produced nothing — fetch the
	// (filter-independent) stats on their own so the header tiles still fill.
	if stats == nil {
		stats, err = a.managerLibraryStats(ctx, libraryID, cte)
		if err != nil {
			return nil, err
		}
	}

	return &ManagerLibraryItemsPage{
		Library: ManagerLibraryRef{
			ID:        lib.ID,
			Name:      lib.Name,
			MediaType: mediaType,
			Domain:    managerProfileDomain(mediaType),
		},
		Stats:   *stats,
		Items:   items,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}, nil
}

// managerLibraryStats aggregates the whole library (filter-independent — the
// header tiles describe the library, not the current filter).
func (a *App) managerLibraryStats(ctx context.Context, libraryID int64, cte string) (*ManagerLibraryStatsView, error) {
	query := "WITH " + cte + `
SELECT count(*),
       count(*) FILTER (WHERE monitored),
       count(*) FILTER (WHERE GREATEST(total_count - have_count, 0) > 0),
       COALESCE(sum(size_on_disk), 0)::bigint,
       COALESCE(sum(have_count), 0)::bigint,
       COALESCE(sum(total_count), 0)::bigint,
       COALESCE(sum(file_count), 0)::bigint,
       COALESCE(array_agg(DISTINCT status ORDER BY status) FILTER (WHERE status <> ''), '{}')
FROM rows`
	var s ManagerLibraryStatsView
	if err := a.db.QueryRow(ctx, query, libraryID).Scan(
		&s.Items, &s.Monitored, &s.ItemsMissing, &s.SizeOnDisk,
		&s.UnitsHave, &s.UnitsTotal, &s.Files, &s.Statuses); err != nil {
		return nil, fmt.Errorf("manager library stats: %w", err)
	}
	return &s, nil
}

type ManagerMediaBulkInput struct {
	IDs              []int64 `json:"ids" minItems:"1" maxItems:"10000" doc:"media_item ids to update"`
	Monitored        *bool   `json:"monitored,omitempty" doc:"Set monitored state; omit to leave unchanged"`
	QualityProfileID *int64  `json:"quality_profile_id,omitempty" doc:"Assign a quality profile; 0 clears it; omit to leave unchanged"`
}

type ManagerMediaBulkResult struct {
	Updated int64 `json:"updated"`
}

// UpdateManagerMedia applies monitored / quality-profile changes to a set of
// media items. A single toggle is just a one-element bulk call.
func (a *App) UpdateManagerMedia(ctx context.Context, input ManagerMediaBulkInput) (ManagerMediaBulkResult, error) {
	if input.Monitored == nil && input.QualityProfileID == nil {
		return ManagerMediaBulkResult{}, fmt.Errorf("manager: nothing to update")
	}
	if input.QualityProfileID != nil && *input.QualityProfileID != 0 {
		if _, err := sqlc.New(a.db).GetManagerQualityProfile(ctx, *input.QualityProfileID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ManagerMediaBulkResult{}, ErrManagerNotFound
			}
			return ManagerMediaBulkResult{}, err
		}
	}

	sets := []string{}
	args := []any{input.IDs}
	if input.Monitored != nil {
		args = append(args, *input.Monitored)
		sets = append(sets, "monitored = $"+strconv.Itoa(len(args)))
	}
	if input.QualityProfileID != nil {
		if *input.QualityProfileID == 0 {
			sets = append(sets, "quality_profile_id = NULL")
		} else {
			args = append(args, *input.QualityProfileID)
			sets = append(sets, "quality_profile_id = $"+strconv.Itoa(len(args)))
		}
	}
	tag, err := a.db.Exec(ctx,
		"UPDATE media_items SET "+strings.Join(sets, ", ")+", updated_at = now() WHERE id = ANY($1::bigint[])",
		args...)
	if err != nil {
		return ManagerMediaBulkResult{}, fmt.Errorf("manager media update: %w", err)
	}
	a.notifyManagerChanged(ctx, "library")
	return ManagerMediaBulkResult{Updated: tag.RowsAffected()}, nil
}
