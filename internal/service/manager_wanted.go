package service

import (
	"context"
	"fmt"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/decision"
)

// ManagerWantedRow is one unit the pipeline still owes: a missing movie, a
// missing episode, a below-cutoff file, or a configuration problem.
type ManagerWantedRow struct {
	Kind        string `json:"kind"` // movie | episode
	MediaItemID int64  `json:"media_item_id"`
	LibraryID   int64  `json:"library_id"`
	Title       string `json:"title"`
	Year        int    `json:"year,omitempty"`
	Season      *int   `json:"season,omitempty"`
	Episode     *int   `json:"episode,omitempty"`
	EpisodeName string `json:"episode_name,omitempty"`
	EpisodeID   *int64 `json:"episode_id,omitempty"`
	AirDate     string `json:"air_date,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
	// Cutoff tab: what's on disk and why it falls short.
	CurrentQuality string `json:"current_quality,omitempty"`
	CurrentScore   int32  `json:"current_score,omitempty"`
	Shortfall      string `json:"shortfall,omitempty"`
	Uncertain      bool   `json:"uncertain,omitempty"`
	// Problems tab.
	Problem string `json:"problem,omitempty"`
	// Last decision for this unit, if any.
	LastDecisionAt      *time.Time `json:"last_decision_at,omitempty"`
	LastDecisionVerdict string     `json:"last_decision_verdict,omitempty"`
}

type ManagerWantedPage struct {
	Rows  []ManagerWantedRow `json:"rows"`
	Total int64              `json:"total"`
}

type ManagerWantedParams struct {
	Tab       string // missing | cutoff | problems
	Libraries []int64
	Page      int
	PerPage   int
}

// ManagerWanted computes the wanted surface. Missing and problems are pure
// SQL; cutoff-unmet parses on-disk basenames under each item's profile
// (movies fully; TV cutoff analysis lands with a denormalized parse pass —
// recorded as a documented v1 gap).
func (a *App) ManagerWanted(ctx context.Context, p ManagerWantedParams) (ManagerWantedPage, error) {
	if p.PerPage <= 0 || p.PerPage > 200 {
		p.PerPage = 50
	}
	if p.Page < 1 {
		p.Page = 1
	}
	switch p.Tab {
	case "", "missing":
		return a.wantedMissing(ctx, p)
	case "cutoff":
		return a.wantedCutoff(ctx, p)
	case "problems":
		return a.wantedProblems(ctx, p)
	default:
		return ManagerWantedPage{}, fmt.Errorf("unknown wanted tab %q", p.Tab)
	}
}

func libraryFilterArgs(libraries []int64) []int64 {
	if libraries == nil {
		return []int64{}
	}
	return libraries
}

// wantedMissing: monitored + released/aired units with nothing on disk.
// Movies and episodes union under one keyset-ish pagination (offset-based;
// the sets are small enough and filters bound them).
func (a *App) wantedMissing(ctx context.Context, p ManagerWantedParams) (ManagerWantedPage, error) {
	page := ManagerWantedPage{Rows: []ManagerWantedRow{}}
	libs := libraryFilterArgs(p.Libraries)
	offset := (p.Page - 1) * p.PerPage

	err := a.db.QueryRow(ctx, `
		WITH missing_movies AS (
			SELECT mi.id FROM media_items mi
			JOIN movies m ON m.media_item_id = mi.id
			WHERE mi.monitored AND mi.media_type = 'movie'
			  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
			  AND (m.release_date IS NULL OR m.release_date <= CURRENT_DATE)
			  AND NOT EXISTS (
				SELECT 1 FROM library_file_links lfl
				JOIN library_files lf ON lf.id = lfl.library_file_id
				WHERE lfl.media_item_id = mi.id AND lfl.relation_type IN ('primary','part')
				  AND lf.deleted_at IS NULL)
		), missing_episodes AS (
			SELECT e.id FROM tv_episodes e
			JOIN tv_seasons s ON s.id = e.season_id
			JOIN tv_series ser ON ser.id = s.series_id
			JOIN media_items mi ON mi.id = ser.media_item_id
			WHERE mi.monitored AND s.monitored AND e.monitored
			  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
			  AND e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE
			  AND NOT EXISTS (
				SELECT 1 FROM library_file_links lfl
				JOIN library_files lf ON lf.id = lfl.library_file_id
				WHERE lfl.tv_episode_id = e.id AND lf.deleted_at IS NULL)
		)
		SELECT (SELECT count(*) FROM missing_movies) + (SELECT count(*) FROM missing_episodes)`,
		libs,
	).Scan(&page.Total)
	if err != nil {
		return page, fmt.Errorf("counting missing: %w", err)
	}

	rows, err := a.db.Query(ctx, `
		SELECT * FROM (
			SELECT 'movie'::text AS kind, mi.id AS item_id, mi.library_id, c.title,
			       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0) AS year,
			       NULL::int AS season, NULL::int AS episode, ''::text AS episode_name,
			       NULL::bigint AS episode_id,
			       COALESCE(m.release_date::text, '') AS air_date,
			       COALESCE(qp.name, '') AS profile_name,
			       COALESCE(m.release_date, '1900-01-01'::date) AS sort_date
			FROM media_items mi
			JOIN media_item_cards c ON c.id = mi.id
			JOIN movies m ON m.media_item_id = mi.id
			LEFT JOIN manager_quality_profiles qp ON qp.id = mi.quality_profile_id
			WHERE mi.monitored AND mi.media_type = 'movie'
			  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
			  AND (m.release_date IS NULL OR m.release_date <= CURRENT_DATE)
			  AND NOT EXISTS (
				SELECT 1 FROM library_file_links lfl
				JOIN library_files lf ON lf.id = lfl.library_file_id
				WHERE lfl.media_item_id = mi.id AND lfl.relation_type IN ('primary','part')
				  AND lf.deleted_at IS NULL)
			UNION ALL
			SELECT 'episode', mi.id, mi.library_id, c.title,
			       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0),
			       s.season_number, e.episode_number, e.title, e.id,
			       COALESCE(e.air_date::text, ''),
			       COALESCE(qp.name, ''),
			       COALESCE(e.air_date, '1900-01-01'::date)
			FROM tv_episodes e
			JOIN tv_seasons s ON s.id = e.season_id
			JOIN tv_series ser ON ser.id = s.series_id
			JOIN media_items mi ON mi.id = ser.media_item_id
			JOIN media_item_cards c ON c.id = mi.id
			LEFT JOIN manager_quality_profiles qp ON qp.id = mi.quality_profile_id
			WHERE mi.monitored AND s.monitored AND e.monitored
			  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
			  AND e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE
			  AND NOT EXISTS (
				SELECT 1 FROM library_file_links lfl
				JOIN library_files lf ON lf.id = lfl.library_file_id
				WHERE lfl.tv_episode_id = e.id AND lf.deleted_at IS NULL)
		) wanted
		ORDER BY sort_date DESC, title, season NULLS FIRST, episode NULLS FIRST
		LIMIT $2 OFFSET $3`,
		libs, p.PerPage, offset,
	)
	if err != nil {
		return page, fmt.Errorf("listing missing: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			row      ManagerWantedRow
			season   *int
			episode  *int
			epID     *int64
			sortDate time.Time
		)
		if err := rows.Scan(&row.Kind, &row.MediaItemID, &row.LibraryID, &row.Title, &row.Year,
			&season, &episode, &row.EpisodeName, &epID, &row.AirDate, &row.ProfileName, &sortDate); err != nil {
			return page, err
		}
		row.Season, row.Episode, row.EpisodeID = season, episode, epID
		page.Rows = append(page.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	a.attachLastDecisions(ctx, page.Rows)
	return page, nil
}

// wantedCutoff: on-disk movies whose best file sits below the profile
// cutoff (quality or format score). Parse-based, mirroring the engine's
// existing-file semantics; uncertain provenance is surfaced, not asserted.
func (a *App) wantedCutoff(ctx context.Context, p ManagerWantedParams) (ManagerWantedPage, error) {
	page := ManagerWantedPage{Rows: []ManagerWantedRow{}}
	libs := libraryFilterArgs(p.Libraries)

	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.library_id, c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0),
		       mi.quality_profile_id, qp.name
		FROM media_items mi
		JOIN media_item_cards c ON c.id = mi.id
		JOIN manager_quality_profiles qp ON qp.id = mi.quality_profile_id
		WHERE mi.monitored AND mi.media_type = 'movie' AND mi.quality_profile_id IS NOT NULL
		  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
		  AND EXISTS (
			SELECT 1 FROM library_file_links lfl
			JOIN library_files lf ON lf.id = lfl.library_file_id
			WHERE lfl.media_item_id = mi.id AND lfl.relation_type IN ('primary','part')
			  AND lf.deleted_at IS NULL)
		ORDER BY c.title`,
		libs,
	)
	if err != nil {
		return page, fmt.Errorf("listing cutoff candidates: %w", err)
	}
	type movieRef struct {
		id, libraryID, profileID int64
		title, profileName       string
		year                     int
	}
	var refs []movieRef
	for rows.Next() {
		var ref movieRef
		var profileID int64
		if err := rows.Scan(&ref.id, &ref.libraryID, &ref.title, &ref.year, &profileID, &ref.profileName); err != nil {
			rows.Close()
			return page, err
		}
		ref.profileID = profileID
		refs = append(refs, ref)
	}
	rows.Close()

	q := sqlc.New(a.db)
	profiles := map[int64]*decision.Profile{}
	for _, ref := range refs {
		profile, ok := profiles[ref.profileID]
		if !ok {
			var err error
			profile, _, err = a.buildDecisionPolicy(ctx, q, ref.profileID)
			if err != nil {
				continue
			}
			profiles[ref.profileID] = profile
		}
		if profile.Domain != "movie" {
			continue
		}
		files, err := a.movieExistingFiles(ctx, ref.id)
		if err != nil || len(files) == 0 {
			continue
		}
		target := decision.Target{Domain: "movie", Profile: profile,
			Units: []decision.Unit{{Key: "cutoff-check", Existing: files}}}
		decision.ResolveUnits(&target)
		best := bestExisting(target.Units[0].Existing)
		if best == nil {
			continue
		}
		cutoffPos, cutoffFound := profile.CutoffPosition()
		shortfall := ""
		switch {
		case best.Uncertain:
			shortfall = "current quality uncertain (inferred from a mute filename)"
		case !best.PositionFound:
			if meets, ok := decision.QualityMeetsCutoffCanonically("movie", profile, best.Quality); ok && meets {
				continue // above the want (e.g. a 2160p disc under a 1080p profile) — satisfied
			}
			qualityLabel := best.Quality
			if qualityLabel == "" {
				qualityLabel = "unknown"
			}
			shortfall = fmt.Sprintf("current quality %q is below profile %q (no ladder slot)", qualityLabel, profile.Name)
		case cutoffFound && best.Position > cutoffPos:
			shortfall = fmt.Sprintf("below quality cutoff %s", profile.Cutoff)
		case best.FormatScore < profile.CutoffFormatScore:
			shortfall = fmt.Sprintf("format score %d below cutoff %d", best.FormatScore, profile.CutoffFormatScore)
		default:
			continue // cutoff met — not wanted
		}
		page.Rows = append(page.Rows, ManagerWantedRow{
			Kind: "movie", MediaItemID: ref.id, LibraryID: ref.libraryID,
			Title: ref.title, Year: ref.year, ProfileName: ref.profileName,
			CurrentQuality: best.Quality, CurrentScore: best.FormatScore,
			Shortfall: shortfall, Uncertain: best.Uncertain,
		})
	}
	page.Total = int64(len(page.Rows))
	start := (p.Page - 1) * p.PerPage
	if start > len(page.Rows) {
		start = len(page.Rows)
	}
	end := start + p.PerPage
	if end > len(page.Rows) {
		end = len(page.Rows)
	}
	page.Rows = page.Rows[start:end]
	a.attachLastDecisions(ctx, page.Rows)
	return page, nil
}

// wantedProblems: monitored items the pipeline can't act on.
func (a *App) wantedProblems(ctx context.Context, p ManagerWantedParams) (ManagerWantedPage, error) {
	page := ManagerWantedPage{Rows: []ManagerWantedRow{}}
	libs := libraryFilterArgs(p.Libraries)
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.library_id, mi.media_type, c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0)
		FROM media_items mi
		JOIN media_item_cards c ON c.id = mi.id
		WHERE mi.monitored AND mi.quality_profile_id IS NULL
		  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
		ORDER BY c.title
		LIMIT $2 OFFSET $3`,
		libs, p.PerPage, (p.Page-1)*p.PerPage,
	)
	if err != nil {
		return page, fmt.Errorf("listing problems: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row ManagerWantedRow
		var mediaType string
		if err := rows.Scan(&row.MediaItemID, &row.LibraryID, &mediaType, &row.Title, &row.Year); err != nil {
			return page, err
		}
		row.Kind = mediaType
		row.Problem = "monitored but no quality profile assigned — the pipeline cannot evaluate releases for it"
		page.Rows = append(page.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	err = a.db.QueryRow(ctx, `
		SELECT count(*) FROM media_items mi
		WHERE mi.monitored AND mi.quality_profile_id IS NULL
		  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))`, libs).Scan(&page.Total)
	return page, err
}

// attachLastDecisions annotates rows with their latest decision (batch).
func (a *App) attachLastDecisions(ctx context.Context, rows []ManagerWantedRow) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.MediaItemID)
	}
	if len(ids) == 0 {
		return
	}
	dbRows, err := a.db.Query(ctx, `
		SELECT DISTINCT ON (media_item_id) media_item_id, decided_at, verdict
		FROM manager_decisions
		WHERE media_item_id = ANY($1)
		ORDER BY media_item_id, decided_at DESC`, ids)
	if err != nil {
		return
	}
	defer dbRows.Close()
	latest := map[int64]struct {
		at      time.Time
		verdict string
	}{}
	for dbRows.Next() {
		var id int64
		var at time.Time
		var verdict string
		if dbRows.Scan(&id, &at, &verdict) == nil {
			latest[id] = struct {
				at      time.Time
				verdict string
			}{at, verdict}
		}
	}
	for i := range rows {
		if l, ok := latest[rows[i].MediaItemID]; ok {
			at := l.at
			rows[i].LastDecisionAt = &at
			rows[i].LastDecisionVerdict = l.verdict
		}
	}
}

func bestExisting(files []decision.ExistingFile) *decision.ExistingFile {
	var best *decision.ExistingFile
	for i := range files {
		file := &files[i]
		if best == nil {
			best = file
			continue
		}
		if file.PositionFound && (!best.PositionFound || file.Position < best.Position ||
			(file.Position == best.Position && file.FormatScore > best.FormatScore)) {
			best = file
		}
	}
	return best
}
