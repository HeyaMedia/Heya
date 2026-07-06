package matcher

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Absolute-numbered anime files ("Series - 24 - Title.mkv") parse with an
// absolute episode and no season — see parser.SceneReleaseParse.AbsoluteEpisodes.
// The absolute number only resolves to a real season/episode once the series is
// enriched (tv_episodes.absolute_number populated). Rather than teach every
// file<->episode join about absolute numbering, we resolve once, at enrichment
// (and on the per-file match path), and write the real season/episode back into
// the file's parse_result so it looks like a normal SxxExx file everywhere.

type absSeasonEpisode struct {
	season  int
	episode int
}

// releaseArrays is the slice of a file's parse_result we read and rewrite.
type releaseArrays struct {
	Parsed struct {
		Release struct {
			Seasons          []int `json:"seasons"`
			Episodes         []int `json:"episodes"`
			AbsoluteEpisodes []int `json:"absoluteEpisodes"`
		} `json:"release"`
	} `json:"parsed"`
}

// loadAbsoluteMap builds absolute-number -> real (season, episode) for a series
// from the enriched catalog, excluding specials (season 0 / is_special) so an
// absolute file can't resolve onto a special that carries a stray absolute
// number. Empty when the series has no absolute numbering yet (not enriched).
func loadAbsoluteMap(ctx context.Context, q *sqlc.Queries, seriesMediaItemID int64) (map[int]absSeasonEpisode, error) {
	rows, err := q.ListEpisodeAbsoluteMap(ctx, seriesMediaItemID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	m := make(map[int]absSeasonEpisode, len(rows))
	for _, r := range rows {
		if r.SeasonNumber <= 0 || r.AbsoluteNumber <= 0 {
			continue
		}
		m[int(r.AbsoluteNumber)] = absSeasonEpisode{season: int(r.SeasonNumber), episode: int(r.EpisodeNumber)}
	}
	return m, nil
}

// resolveAbsolute maps a file's absolute episode numbers to the concrete
// (seasons, episodes) arrays to store. Seasons are unique-and-sorted; episodes
// preserve order. Returns (nil, nil) when nothing resolves (unknown absolutes).
func resolveAbsolute(absEps []int, absMap map[int]absSeasonEpisode) (seasons, episodes []int) {
	seen := map[int]bool{}
	for _, abs := range absEps {
		se, ok := absMap[abs]
		if !ok {
			continue
		}
		if !seen[se.season] {
			seen[se.season] = true
			seasons = append(seasons, se.season)
		}
		episodes = append(episodes, se.episode)
	}
	if len(episodes) == 0 {
		return nil, nil
	}
	sort.Ints(seasons)
	return seasons, episodes
}

// storeResolved rewrites one file's parse_result with resolved season/episode
// arrays, unless they already match (idempotent — avoids a write on every
// re-enrich / re-match). Returns whether a write happened.
func storeResolved(ctx context.Context, q *sqlc.Queries, fileID int64, cur releaseArrays, absMap map[int]absSeasonEpisode) (bool, error) {
	rel := cur.Parsed.Release
	if len(rel.AbsoluteEpisodes) == 0 {
		return false, nil // not an absolute file
	}
	seasons, episodes := resolveAbsolute(rel.AbsoluteEpisodes, absMap)
	if len(episodes) == 0 {
		return false, nil // nothing resolvable yet
	}
	if intsEqual(rel.Seasons, seasons) && intsEqual(rel.Episodes, episodes) {
		return false, nil // already reconciled
	}
	seasonsJSON, err := json.Marshal(seasons)
	if err != nil {
		return false, err
	}
	episodesJSON, err := json.Marshal(episodes)
	if err != nil {
		return false, err
	}
	if err := q.SetLibraryFileResolvedEpisodes(ctx, sqlc.SetLibraryFileResolvedEpisodesParams{
		ID:       fileID,
		Seasons:  seasonsJSON,
		Episodes: episodesJSON,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// ReconcileAbsoluteEpisodes resolves every absolute-numbered file of a series
// and writes real season/episode arrays into their parse_result. Called after
// a series is enriched (structure persisted) so the whole back catalog lines up
// at once. A no-op when the series has no absolute-numbered files or isn't
// enriched yet.
func ReconcileAbsoluteEpisodes(ctx context.Context, q *sqlc.Queries, seriesMediaItemID int64) (int, error) {
	absMap, err := loadAbsoluteMap(ctx, q, seriesMediaItemID)
	if err != nil || len(absMap) == 0 {
		return 0, err
	}
	files, err := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: seriesMediaItemID, Valid: true})
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, f := range files {
		if len(f.ParseResult) == 0 {
			continue
		}
		var cur releaseArrays
		if err := json.Unmarshal(f.ParseResult, &cur); err != nil {
			continue
		}
		wrote, err := storeResolved(ctx, q, f.ID, cur, absMap)
		if err != nil {
			return updated, err
		}
		if wrote {
			updated++
		}
	}
	return updated, nil
}

// reconcileAbsoluteFile resolves a single just-matched file against the series'
// (possibly-not-yet-enriched) catalog. Cheap fast-path: files with no absolute
// episodes return before touching the DB, so non-anime scans pay nothing.
func reconcileAbsoluteFile(ctx context.Context, q *sqlc.Queries, seriesMediaItemID, fileID int64, parseResult []byte) error {
	if len(parseResult) == 0 {
		return nil
	}
	var cur releaseArrays
	if err := json.Unmarshal(parseResult, &cur); err != nil {
		return nil
	}
	if len(cur.Parsed.Release.AbsoluteEpisodes) == 0 {
		return nil // not an absolute file — no catalog fetch
	}
	absMap, err := loadAbsoluteMap(ctx, q, seriesMediaItemID)
	if err != nil || len(absMap) == 0 {
		return err // not enriched yet; the enrich hook will reconcile later
	}
	_, err = storeResolved(ctx, q, fileID, cur, absMap)
	return err
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
