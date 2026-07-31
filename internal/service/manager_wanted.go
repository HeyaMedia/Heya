package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/decision"
)

// ManagerWantedRow is one unit the pipeline still owes: a missing movie, a
// missing episode, a missing album, a below-cutoff file, or a configuration
// problem.
type ManagerWantedRow struct {
	Kind        string `json:"kind"` // movie | episode | album
	MediaItemID int64  `json:"media_item_id"`
	LibraryID   int64  `json:"library_id"`
	Title       string `json:"title"`
	Year        int    `json:"year,omitempty"`
	Season      *int   `json:"season,omitempty"`
	Episode     *int   `json:"episode,omitempty"`
	EpisodeName string `json:"episode_name,omitempty"`
	EpisodeID   *int64 `json:"episode_id,omitempty"`
	// Music: the catalog release the library is missing.
	AlbumTitle    string `json:"album_title,omitempty"`
	DiscographyID int64  `json:"discography_id,omitempty"`
	MusicTargetID int64  `json:"music_target_id,omitempty" doc:"manager_music_targets id — feeds the music search"`
	AirDate       string `json:"air_date,omitempty"`
	ProfileName   string `json:"profile_name,omitempty"`
	// Cutoff tab: what's on disk and why it falls short.
	CurrentQuality string `json:"current_quality,omitempty"`
	CurrentScore   int32  `json:"current_score,omitempty"`
	Shortfall      string `json:"shortfall,omitempty"`
	Uncertain      bool   `json:"uncertain,omitempty"`
	// Problems tab.
	Problem string `json:"problem,omitempty"`
	// Last decision covering this unit, if any — how the pipeline last saw
	// it (rss sweep vs interactive search) and what it concluded.
	LastDecisionAt      *time.Time `json:"last_decision_at,omitempty"`
	LastDecisionVerdict string     `json:"last_decision_verdict,omitempty"`
	LastDecisionKind    string     `json:"last_decision_kind,omitempty"`
	LastDecisionRunID   int64      `json:"last_decision_run_id,omitempty"`
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

// ManagerWanted computes the wanted surface. Missing covers movies,
// episodes, and albums (catalog releases of monitored artists); cutoff-unmet
// parses on-disk basenames under each item's profile (movies fully; TV
// cutoff analysis lands with a denormalized parse pass — recorded as a
// documented v1 gap).
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
// Movies and episodes come from one SQL union; missing albums resolve their
// release-type bucket through the same effectiveReleaseType the entity page
// uses (secondary types + title hints), so the two surfaces agree on what
// counts as a real album. The merged set sorts and paginates in Go — the
// candidate pools are personal-library sized.
func (a *App) wantedMissing(ctx context.Context, p ManagerWantedParams) (ManagerWantedPage, error) {
	page := ManagerWantedPage{Rows: []ManagerWantedRow{}}
	libs := libraryFilterArgs(p.Libraries)

	var all []datedWantedRow

	rows, err := a.db.Query(ctx, `
		SELECT 'movie'::text AS kind, mi.id AS item_id, mi.library_id, c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0) AS year,
		       NULL::int AS season, NULL::int AS episode, ''::text AS episode_name,
		       NULL::bigint AS episode_id,
		       COALESCE(m.release_date::text, '') AS air_date,
		       COALESCE(qp.name, '') AS profile_name,
		       COALESCE(m.release_date, '1900-01-01'::date) AS sort_date
		FROM media_items mi
		JOIN media_item_cards c ON c.id = mi.id
		LEFT JOIN movies m ON m.media_item_id = mi.id
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
			WHERE lfl.tv_episode_id = e.id AND lf.deleted_at IS NULL)`,
		libs,
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
		all = append(all, datedWantedRow{row, sortDate})
	}
	if err := rows.Err(); err != nil {
		return page, err
	}

	albums, err := a.wantedMissingAlbums(ctx, libs)
	if err != nil {
		return page, err
	}
	all = append(all, albums...)

	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].date.Equal(all[j].date) {
			return all[i].date.After(all[j].date)
		}
		if all[i].row.Title != all[j].row.Title {
			return all[i].row.Title < all[j].row.Title
		}
		si, sj := intOr(all[i].row.Season, -1), intOr(all[j].row.Season, -1)
		if si != sj {
			return si < sj
		}
		return intOr(all[i].row.Episode, -1) < intOr(all[j].row.Episode, -1)
	})

	page.Total = int64(len(all))
	start := min((p.Page-1)*p.PerPage, len(all))
	end := min(start+p.PerPage, len(all))
	for _, d := range all[start:end] {
		page.Rows = append(page.Rows, d.row)
	}
	a.attachLastDecisions(ctx, page.Rows)
	return page, nil
}

func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

// datedWantedRow pairs a row with its sort key for the in-Go merge.
type datedWantedRow struct {
	row  ManagerWantedRow
	date time.Time
}

// wantedMissingAlbums: MONITORED music targets (edition groups) of
// monitored artists with no linked local album. Monitoring lives on
// manager_music_targets — albums/EPs default on, singles/live default off,
// and user toggles survive catalog churn. The effectiveReleaseType filter
// still prunes untagged live/remix noise that defaulted monitored.
func (a *App) wantedMissingAlbums(ctx context.Context, libs []int64) ([]datedWantedRow, error) {
	rows, err := a.db.Query(ctx, `
		SELECT t.id, t.title, t.album_type,
		       mi.id, mi.library_id, c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0),
		       COALESCE(qp.name, ''),
		       COALESCE(d.id, 0), COALESCE(d.secondary_types, '{}'),
		       COALESCE(d.release_date::text, NULLIF(t.year, ''), ''),
		       COALESCE(d.release_date,
		                CASE WHEN t.year ~ '^\d{4}' THEN make_date(left(t.year, 4)::int, 1, 1) END,
		                '1900-01-01'::date)
		FROM manager_music_targets t
		JOIN artists ar ON ar.id = t.artist_id
		JOIN media_items mi ON mi.id = ar.media_item_id
		JOIN media_item_cards c ON c.id = mi.id
		LEFT JOIN manager_quality_profiles qp ON qp.id = mi.quality_profile_id
		LEFT JOIN LATERAL (
			SELECT dd.id, dd.secondary_types, dd.release_date
			FROM artist_discography dd
			WHERE dd.artist_id = t.artist_id AND dd.album_type = t.album_type
			  AND COALESCE(NULLIF(dd.edition_key, ''), lower(dd.title)) = t.edition_key
			ORDER BY length(dd.title)
			LIMIT 1
		) d ON true
		WHERE mi.monitored AND mi.media_type = 'music' AND t.monitored
		  AND (cardinality($1::bigint[]) = 0 OR mi.library_id = ANY($1))
		  AND (d.release_date IS NULL OR d.release_date <= CURRENT_DATE)
		  AND NOT EXISTS (
			SELECT 1 FROM artist_discography d2
			WHERE d2.artist_id = t.artist_id AND d2.album_type = t.album_type
			  AND d2.album_id IS NOT NULL
			  AND COALESCE(NULLIF(d2.edition_key, ''), lower(d2.title)) = t.edition_key)`,
		libs,
	)
	if err != nil {
		return nil, fmt.Errorf("listing missing albums: %w", err)
	}
	defer rows.Close()

	var out []datedWantedRow
	for rows.Next() {
		var (
			row       ManagerWantedRow
			albumType string
			secondary []string
			sortDate  time.Time
		)
		row.Kind = "album"
		if err := rows.Scan(&row.MusicTargetID, &row.AlbumTitle, &albumType,
			&row.MediaItemID, &row.LibraryID, &row.Title, &row.Year,
			&row.ProfileName, &row.DiscographyID, &secondary,
			&row.AirDate, &sortDate); err != nil {
			return nil, err
		}
		switch effectiveReleaseType(albumType, secondary, row.AlbumTitle) {
		case "album", "ep":
			out = append(out, datedWantedRow{row, sortDate})
		}
	}
	return out, rows.Err()
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

// attachLastDecisions annotates rows with the latest decision COVERING each
// unit (batch): an episode row only claims a decision that evaluated that
// episode (or its season), never an unrelated unit of the same show. The run
// kind rides along so the UI can say HOW the unit was last seen — an RSS
// sweep versus an explicit search.
func (a *App) attachLastDecisions(ctx context.Context, rows []ManagerWantedRow) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.MediaItemID)
	}
	if len(ids) == 0 {
		return
	}
	dbRows, err := a.db.Query(ctx, `
		SELECT d.media_item_id, d.decided_at, d.verdict, d.target_kind,
		       d.season_number, d.episode_number, d.album_title,
		       COALESCE(d.music_target_id, 0), r.kind, d.run_id
		FROM manager_decisions d
		JOIN manager_runs r ON r.id = d.run_id
		WHERE d.media_item_id = ANY($1)
		ORDER BY d.decided_at DESC
		LIMIT 5000`, ids)
	if err != nil {
		return
	}
	defer dbRows.Close()
	type dec struct {
		at              time.Time
		verdict         string
		targetKind      string
		season, episode *int32
		albumTitle      string
		musicTargetID   int64
		runKind         string
		runID           int64
	}
	byItem := map[int64][]dec{}
	for dbRows.Next() {
		var itemID int64
		var d dec
		if dbRows.Scan(&itemID, &d.at, &d.verdict, &d.targetKind,
			&d.season, &d.episode, &d.albumTitle, &d.musicTargetID, &d.runKind, &d.runID) == nil {
			byItem[itemID] = append(byItem[itemID], d)
		}
	}
	covers := func(row ManagerWantedRow, d dec) bool {
		switch row.Kind {
		case "episode":
			if row.Season == nil {
				return false
			}
			switch d.targetKind {
			case "episode":
				return d.season != nil && d.episode != nil && row.Episode != nil &&
					int(*d.season) == *row.Season && int(*d.episode) == *row.Episode
			case "season":
				return d.season != nil && int(*d.season) == *row.Season
			default:
				return false
			}
		case "album":
			if d.targetKind != "music_release" {
				return false
			}
			if row.MusicTargetID != 0 && d.musicTargetID != 0 {
				return d.musicTargetID == row.MusicTargetID
			}
			return strings.EqualFold(d.albumTitle, row.AlbumTitle)
		default: // movie / whole-item rows: any decision on the item counts
			return true
		}
	}
	for i := range rows {
		for _, d := range byItem[rows[i].MediaItemID] {
			if !covers(rows[i], d) {
				continue
			}
			at := d.at
			rows[i].LastDecisionAt = &at
			rows[i].LastDecisionVerdict = d.verdict
			rows[i].LastDecisionKind = d.runKind
			rows[i].LastDecisionRunID = d.runID
			break
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
