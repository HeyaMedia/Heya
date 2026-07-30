package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/decision"
	"github.com/karbowiak/heya/internal/manager/torznab"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/parser/video"
	"github.com/rs/zerolog/log"
)

// rssAdvisoryLockKey guards against overlapping RSS runs (worker ticker vs
// manual API/CLI trigger) via a Postgres advisory lock.
const rssAdvisoryLockKey int64 = 0x48455941_5253_5301 // "HEYA RSS"

const (
	rssPageSize = 100
	rssMaxPages = 10 // 1000-release watermark-gap backstop, arr-style
)

// ManagerRSSRunView summarizes one RSS sweep for the CLI/API.
type ManagerRSSRunView struct {
	RunID      int64                   `json:"run_id"`
	Status     string                  `json:"status"`
	Partial    bool                    `json:"partial"`
	Truncated  bool                    `json:"truncated"`
	Fetched    int                     `json:"fetched"`
	Fresh      int                     `json:"fresh"`
	Matched    int                     `json:"matched"`
	Evaluated  int                     `json:"evaluated"`
	WouldGrabs int                     `json:"would_grabs"`
	Indexers   []ManagerRunIndexerView `json:"indexers"`
}

// rssTargetRef is one monitored item in the identity index.
type rssTargetRef struct {
	ItemID    int64
	MediaType string // movie | tv | anime
	Monitored bool
}

type rssIdentityIndex struct {
	byTitle map[string][]rssTargetRef
	byID    map[string]rssTargetRef // "tvdbid:123" / "imdbid:tt..." / "tmdbid:9"
}

// RunManagerRSS sweeps every enabled usenet indexer's recent releases for
// the movie and tv domains, records everything seen (the anti-survivor-bias
// ledger), matches against monitored items, and evaluates wanted targets —
// dry-run: decisions record what WOULD have been grabbed.
func (a *App) RunManagerRSS(ctx context.Context, source string) (*ManagerRSSRunView, error) {
	// One RSS run at a time, across processes.
	lockConn, err := a.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer lockConn.Release()
	var locked bool
	if err := lockConn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, rssAdvisoryLockKey).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, fmt.Errorf("an RSS run is already in progress")
	}
	defer func() {
		_, _ = lockConn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, rssAdvisoryLockKey)
	}()

	q := sqlc.New(a.db)
	index, domains, err := a.buildRSSIdentityIndex(ctx)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("nothing is monitored — the RSS sweep has no targets")
	}

	scope, _ := json.Marshal(map[string]any{"domains": domains})
	run, err := q.CreateManagerRun(ctx, sqlc.CreateManagerRunParams{Kind: "rss", Source: source, Scope: scope})
	if err != nil {
		return nil, fmt.Errorf("creating rss run: %w", err)
	}

	view := &ManagerRSSRunView{RunID: run.ID}
	indexers, err := q.ListManagerIndexers(ctx)
	if err != nil {
		return nil, err
	}

	var anyOK, anyFailed, anyTruncated bool
	freshSightings := make([]sqlc.ManagerReleaseSighting, 0)
	releasesByID := map[int64]sqlc.ManagerRelease{}
	first := true
	for _, row := range indexers {
		if row.Kind == IndexerKindProwlarr {
			continue
		}
		for _, domain := range domains {
			// Politeness gap: the sweeps hit the same Prowlarr host
			// back-to-back and its rate limiter 429s tight bursts.
			if !first && row.Enabled && row.Protocol != "torrent" {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
			first = false
			fetch := a.sweepIndexerDomain(ctx, q, run.ID, row, domain, &freshSightings, releasesByID)
			switch fetch.Status {
			case "ok":
				anyOK = true
			case "failed":
				anyFailed = true
			case "truncated":
				anyOK = true
				anyTruncated = true
			}
			view.Fetched += fetch.Fetched
			view.Indexers = append(view.Indexers, fetch)
		}
	}
	view.Fresh = len(freshSightings)

	// Evaluate fresh sightings: identity → wantedness → engine, grouped so
	// one target's releases from this sweep compete against each other.
	matched, evaluated, grabs, err := a.evaluateRSSSightings(ctx, q, run.ID, index, freshSightings, releasesByID)
	if err != nil {
		return nil, err
	}
	view.Matched, view.Evaluated, view.WouldGrabs = matched, evaluated, grabs

	status := "completed"
	if anyFailed && !anyOK {
		status = "failed"
	}
	view.Status = status
	view.Partial = anyFailed && anyOK
	view.Truncated = anyTruncated
	stats, _ := json.Marshal(map[string]any{
		"fetched": view.Fetched, "fresh": view.Fresh, "matched": view.Matched,
		"evaluated": view.Evaluated, "would_grabs": view.WouldGrabs,
	})
	if _, err := q.FinishManagerRun(ctx, sqlc.FinishManagerRunParams{
		ID: run.ID, Status: status, Partial: view.Partial, Truncated: anyTruncated,
		Stats: stats, Errors: []byte("[]"),
	}); err != nil {
		return nil, err
	}

	// Maintenance piggybacks the sweep while the advisory lock is held.
	a.pruneManagerLedger(ctx, q)

	a.notifyManagerChanged(ctx, "runs")
	return view, nil
}

// sweepIndexerDomain pages one indexer's recent releases for one domain
// until the durable cursor watermark, a non-full page, or the page cap.
func (a *App) sweepIndexerDomain(
	ctx context.Context,
	q *sqlc.Queries,
	runID int64,
	row sqlc.ManagerIndexer,
	domain string,
	freshSightings *[]sqlc.ManagerReleaseSighting,
	releasesByID map[int64]sqlc.ManagerRelease,
) ManagerRunIndexerView {
	view := ManagerRunIndexerView{Indexer: row.Name, Domain: domain, Status: "ok"}
	recordRunIndexer := func(pages int, err string, started time.Time) pgtype.Int8 {
		view.DurationMs = time.Since(started).Milliseconds()
		view.Error = err
		runIdx, dbErr := q.CreateManagerRunIndexer(ctx, sqlc.CreateManagerRunIndexerParams{
			RunID: runID, IndexerID: pgtype.Int8{Int64: row.ID, Valid: true},
			IndexerName: row.Name, Domain: domain, Status: view.Status,
			PagesFetched: int32(pages), Fetched: int32(view.Fetched),
			DurationMs: view.DurationMs, Error: err,
		})
		if dbErr != nil {
			return pgtype.Int8{}
		}
		return pgtype.Int8{Int64: runIdx.ID, Valid: true}
	}

	switch {
	case !row.Enabled:
		view.Status = "skipped_disabled"
		recordRunIndexer(0, "", time.Now())
		return view
	case row.Protocol == "torrent":
		view.Status = "skipped_unsupported_protocol"
		recordRunIndexer(0, "", time.Now())
		return view
	}

	cursor, cursorErr := q.GetManagerRSSCursor(ctx, sqlc.GetManagerRSSCursorParams{
		IndexerID: row.ID, Domain: domain,
	})
	haveCursor := cursorErr == nil

	client := torznab.New(row.BaseUrl, row.ApiKey)
	cats := domainDefaultCats(domain)
	if len(row.Categories) > 0 {
		cats = make([]int, 0, len(row.Categories))
		for _, c := range row.Categories {
			cats = append(cats, int(c))
		}
	}

	started := time.Now()
	var (
		newestKey     string
		newestPublish pgtype.Timestamptz
		pages         int
		sawWatermark  bool
	)
	runIdxID := pgtype.Int8{}
	// The run_indexer row is created after the sweep (aggregates); requests
	// reference it, so requests are buffered.
	type bufferedRequest struct {
		params   map[string]string
		offset   int
		results  int
		duration time.Duration
		err      string
	}
	var requests []bufferedRequest

	for page := 0; page < rssMaxPages; page++ {
		query := torznab.Query{Type: "search", Cats: cats, Limit: rssPageSize, Offset: page * rssPageSize}
		params := map[string]string{
			"t": "search", "cats": intsCSV(cats),
			"limit": strconv.Itoa(rssPageSize), "offset": strconv.Itoa(page * rssPageSize),
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		reqStart := time.Now()
		releases, err := client.Search(cctx, query)
		cancel()
		elapsed := time.Since(reqStart)
		req := bufferedRequest{params: params, offset: page * rssPageSize, results: len(releases), duration: elapsed}
		if err != nil {
			req.err = err.Error()
			requests = append(requests, req)
			view.Status = "failed"
			runIdxID = recordRunIndexer(pages, err.Error(), started)
			break
		}
		requests = append(requests, req)
		pages++
		view.Fetched += len(releases)

		for i, rel := range releases {
			releaseKey := rel.GUID
			if releaseKey == "" {
				releaseKey = "fp:" + releaseFingerprint(rel.Title, rel.Size)
			}
			if page == 0 && i == 0 {
				newestKey = releaseKey
				if !rel.PublishDate.IsZero() {
					newestPublish = pgtype.Timestamptz{Time: rel.PublishDate, Valid: true}
				}
			}
			if haveCursor && cursor.LastReleaseKey != "" && releaseKey == cursor.LastReleaseKey {
				sawWatermark = true
				break
			}
			releaseRow, sighting, fresh, err := a.ingestRSSRelease(ctx, q, runID, row, domain, rel, releaseKey)
			if err != nil {
				continue
			}
			releasesByID[releaseRow.ID] = releaseRow
			if fresh {
				*freshSightings = append(*freshSightings, sighting)
			}
		}
		if sawWatermark || len(releases) < rssPageSize {
			break
		}
		if page == rssMaxPages-1 {
			view.Status = "truncated"
		}
	}

	if view.Status != "failed" {
		if !runIdxID.Valid {
			runIdxID = recordRunIndexer(pages, "", started)
		}
		// Cursor advances only after a durable sweep of this indexer+domain.
		if newestKey != "" {
			_ = q.UpsertManagerRSSCursor(ctx, sqlc.UpsertManagerRSSCursorParams{
				IndexerID: row.ID, Domain: domain,
				LastReleaseKey: newestKey, LastPublishDate: newestPublish,
				LastRunID: pgtype.Int8{Int64: runID, Valid: true},
			})
		}
	}
	for ordinal, req := range requests {
		params, _ := json.Marshal(req.params)
		if runIdxID.Valid {
			_, _ = q.CreateManagerRunRequest(ctx, sqlc.CreateManagerRunRequestParams{
				RunIndexerID: runIdxID.Int64, Ordinal: int32(ordinal), Params: params,
				PageOffset: int32(req.offset), Results: int32(req.results),
				DurationMs: req.duration.Milliseconds(), Error: req.err,
			})
		}
	}
	return view
}

// ingestRSSRelease upserts the durable release row and creates a pending
// sighting when this release is new for this indexer+domain (fresh = the
// first time we've ever seen it; re-sightings only bump aggregates).
func (a *App) ingestRSSRelease(
	ctx context.Context,
	q *sqlc.Queries,
	runID int64,
	row sqlc.ManagerIndexer,
	domain string,
	rel torznab.Release,
	releaseKey string,
) (sqlc.ManagerRelease, sqlc.ManagerReleaseSighting, bool, error) {
	rawAttrs, _ := json.Marshal(rel.RawAttrs)
	var publishDate pgtype.Timestamptz
	if !rel.PublishDate.IsZero() {
		publishDate = pgtype.Timestamptz{Time: rel.PublishDate, Valid: true}
	}
	var guid pgtype.Text
	if rel.GUID != "" {
		guid = pgtype.Text{String: rel.GUID, Valid: true}
	}
	releaseRow, err := q.UpsertManagerRelease(ctx, sqlc.UpsertManagerReleaseParams{
		IndexerID: pgtype.Int8{Int64: row.ID, Valid: true}, IndexerName: row.Name,
		Domain: domain, ReleaseKey: releaseKey,
		UiFingerprint: releaseFingerprint(rel.Title, rel.Size),
		Guid:          guid, Title: rel.Title, SizeBytes: max64(rel.Size, 0),
		PublishDate: publishDate, PublishDateRaw: rel.PublishDateRaw,
		Categories: int32Slice(rel.Categories), RawAttrs: rawAttrs,
		InfoUrl: stripQueryString(rel.InfoURL),
	})
	if err != nil {
		return sqlc.ManagerRelease{}, sqlc.ManagerReleaseSighting{}, false, err
	}
	fresh := releaseRow.TimesSeen == 1
	if !fresh {
		return releaseRow, sqlc.ManagerReleaseSighting{}, false, nil
	}
	sighting, err := q.CreateManagerReleaseSighting(ctx, sqlc.CreateManagerReleaseSightingParams{
		ReleaseID: releaseRow.ID, RunID: pgtype.Int8{Int64: runID, Valid: true},
		Status: "pending",
	})
	if err != nil {
		return releaseRow, sqlc.ManagerReleaseSighting{}, false, err
	}
	return releaseRow, sighting, true, nil
}

// buildRSSIdentityIndex loads every monitored movie/tv item's normalized
// titles, aliases, and provider ids. Domains with zero monitored targets
// aren't swept at all.
func (a *App) buildRSSIdentityIndex(ctx context.Context) (*rssIdentityIndex, []string, error) {
	return a.buildIdentityIndex(ctx, true)
}

// buildIdentityIndex is the shared identity map. RSS sweeps index monitored
// items only (they define wantedness); the queue recognizer indexes the
// WHOLE library — a foreign download should be recognized even when the
// matched item isn't monitored.
func (a *App) buildIdentityIndex(ctx context.Context, monitoredOnly bool) (*rssIdentityIndex, []string, error) {
	index := &rssIdentityIndex{byTitle: map[string][]rssTargetRef{}, byID: map[string]rssTargetRef{}}
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.media_type, mi.monitored, c.title,
		       COALESCE(array_agg(DISTINCT mt.title) FILTER (WHERE mt.title IS NOT NULL), '{}'),
		       COALESCE(array_agg(DISTINCT ei.provider || ':' || ei.external_id) FILTER (WHERE ei.provider IS NOT NULL), '{}')
		FROM media_items mi
		JOIN media_item_cards c ON c.id = mi.id
		LEFT JOIN media_titles mt ON mt.media_item_id = mi.id
		LEFT JOIN media_item_external_ids ei ON ei.media_item_id = mi.id
		WHERE (mi.monitored OR NOT $1) AND mi.media_type IN ('movie','tv','anime')
		GROUP BY mi.id, mi.media_type, mi.monitored, c.title`, monitoredOnly)
	if err != nil {
		return nil, nil, fmt.Errorf("building identity index: %w", err)
	}
	defer rows.Close()

	domains := map[string]bool{}
	for rows.Next() {
		var (
			ref     rssTargetRef
			title   string
			aliases []string
			ids     []string
		)
		if err := rows.Scan(&ref.ItemID, &ref.MediaType, &ref.Monitored, &title, &aliases, &ids); err != nil {
			return nil, nil, err
		}
		if ref.MediaType == "movie" {
			domains["movie"] = true
		} else {
			domains["tv"] = true
		}
		for _, t := range append(aliases, title) {
			if n := matcher.NormalizeTitle(t); n != "" {
				index.byTitle[n] = append(index.byTitle[n], ref)
			}
		}
		for _, id := range ids {
			parts := strings.SplitN(id, ":", 2)
			if len(parts) != 2 || parts[1] == "" {
				continue
			}
			switch strings.ToLower(parts[0]) {
			case "tvdb":
				index.byID["tvdbid:"+parts[1]] = ref
			case "imdb":
				index.byID["imdbid:"+strings.TrimPrefix(parts[1], "tt")] = ref
			case "tmdb":
				index.byID["tmdbid:"+parts[1]] = ref
			}
		}
	}
	out := make([]string, 0, 2)
	for _, d := range []string{"movie", "tv"} {
		if domains[d] {
			out = append(out, d)
		}
	}
	return index, out, rows.Err()
}

// resolveRSSIdentity matches one release against the index: indexer id
// attrs win; otherwise the parsed title must map to exactly one target.
func resolveRSSIdentity(index *rssIdentityIndex, domain, title string, attrs map[string]string) (rssTargetRef, string) {
	for key, prefix := range map[string]string{"tvdbid": "tvdbid:", "imdbid": "imdbid:", "tmdbid": "tmdbid:"} {
		if v := attrs[key]; v != "" && v != "0" {
			if key == "imdbid" {
				v = strings.TrimPrefix(v, "tt")
			}
			if ref, ok := index.byID[prefix+v]; ok {
				return ref, ""
			}
		}
	}
	parsed := ""
	if domain == "tv" {
		parsed = video.FilenameParseShow(title).Title
	} else {
		parsed = video.FilenameParseMovie(title).Title
	}
	normalized := matcher.NormalizeTitle(parsed)
	if normalized == "" {
		return rssTargetRef{}, "unmatched"
	}
	refs := index.byTitle[normalized]
	switch {
	case len(refs) == 1:
		return refs[0], ""
	case len(refs) > 1:
		// Same normalized title on multiple monitored items: never guess.
		first := refs[0]
		for _, ref := range refs[1:] {
			if ref.ItemID != first.ItemID {
				return rssTargetRef{}, "ambiguous"
			}
		}
		return first, ""
	default:
		return rssTargetRef{}, "unmatched"
	}
}

// evaluateRSSSightings resolves identity for every fresh sighting, groups
// matches per target scope, and runs the engine for wanted targets.
func (a *App) evaluateRSSSightings(
	ctx context.Context,
	q *sqlc.Queries,
	runID int64,
	index *rssIdentityIndex,
	sightings []sqlc.ManagerReleaseSighting,
	releasesByID map[int64]sqlc.ManagerRelease,
) (matched, evaluated, grabs int, err error) {
	type groupKey struct {
		itemID int64
		season int // -1 for movies
	}
	groups := map[groupKey][]sqlc.ManagerRelease{}
	groupSightings := map[groupKey][]sqlc.ManagerReleaseSighting{}

	markSighting := func(s sqlc.ManagerReleaseSighting, status, errText string, match *rssTargetRef, decisionID int64) {
		var matchedDoc []byte
		if match != nil {
			matchedDoc, _ = json.Marshal(map[string]any{"media_item_id": match.ItemID, "media_type": match.MediaType})
		}
		var decID pgtype.Int8
		if decisionID != 0 {
			decID = pgtype.Int8{Int64: decisionID, Valid: true}
		}
		_ = q.UpdateManagerReleaseSighting(ctx, sqlc.UpdateManagerReleaseSightingParams{
			ID: s.ID, Status: status, Error: errText, Matched: matchedDoc, DecisionID: decID,
		})
	}

	for _, sighting := range sightings {
		release, ok := releasesByID[sighting.ReleaseID]
		if !ok {
			markSighting(sighting, "failed", "release row missing from sweep cache", nil, 0)
			continue
		}
		attrs := attrsMapFromRaw(release.RawAttrs)
		ref, failure := resolveRSSIdentity(index, release.Domain, release.Title, attrs)
		if failure != "" {
			markSighting(sighting, failure, "", nil, 0)
			continue
		}
		matched++
		key := groupKey{itemID: ref.ItemID, season: -1}
		if release.Domain == "tv" {
			show := video.FilenameParseShow(release.Title)
			if len(show.Seasons) > 0 {
				key.season = show.Seasons[0]
			} else {
				// Absolute-numbered or unparseable season: evaluate against
				// season -2 marker → resolved per-episode via absolute map
				// during target build; v1 groups them per item.
				key.season = -2
			}
		}
		groups[key] = append(groups[key], release)
		groupSightings[key] = append(groupSightings[key], sighting)
	}

	for key, releases := range groups {
		var (
			target decision.Target
			meta   searchTargetMeta
			err    error
		)
		if key.season == -1 {
			target, meta, err = a.buildMovieTarget(ctx, key.itemID)
		} else {
			season := key.season
			scope := ManagerSearchScope{}
			if season >= 0 {
				scope.Season = &season
			} else {
				// Whole-series scope for absolute-numbered releases.
				zero := 1
				scope.Season = &zero
			}
			var mediaType string
			_ = a.db.QueryRow(ctx, `SELECT media_type FROM media_items WHERE id = $1`, key.itemID).Scan(&mediaType)
			target, meta, err = a.buildTVTarget(ctx, key.itemID, mediaType == "anime", scope)
		}
		if err != nil {
			for _, s := range groupSightings[key] {
				markSighting(s, "failed", err.Error(), nil, 0)
			}
			continue
		}
		var policyHash string
		if meta.ProfileID != 0 {
			profile, hash, perr := a.buildDecisionPolicy(ctx, q, meta.ProfileID)
			if perr == nil && profile.Domain == meta.Domain {
				target.Profile = profile
				policyHash = hash
				decision.ResolveUnits(&target)
			}
		}

		// Wantedness: any unit missing a file, or below either cutoff.
		if !targetIsWanted(target) {
			ref := rssTargetRef{ItemID: key.itemID}
			for _, s := range groupSightings[key] {
				markSighting(s, "unwanted", "", &ref, 0)
			}
			continue
		}

		candidates := make([]decision.Candidate, 0, len(releases))
		releaseIDs := map[int]pgtype.Int8{}
		for i, release := range releases {
			releaseIDs[i] = pgtype.Int8{Int64: release.ID, Valid: true}
			var publish time.Time
			if release.PublishDate.Valid {
				publish = release.PublishDate.Time
			}
			priority := int32(25)
			candidates = append(candidates, decision.Candidate{
				Index: i, Title: release.Title, SizeBytes: release.SizeBytes,
				PublishDate: publish,
				IndexerID:   release.IndexerID.Int64, IndexerName: release.IndexerName,
				IndexerPriority: priority,
				Categories:      intSlice(release.Categories),
				IDHints:         attrsMapFromRaw(release.RawAttrs),
			})
		}

		result := decision.Evaluate(target, candidates)
		evaluated += len(candidates)

		tx, terr := a.db.Begin(ctx)
		if terr != nil {
			return matched, evaluated, grabs, terr
		}
		qtx := sqlc.New(tx)
		chosenTitle, verdict, perr := a.persistEvaluationTx(ctx, qtx, runID, target, meta, policyHash, result, releaseIDs)
		if perr != nil {
			_ = tx.Rollback(ctx)
			for _, s := range groupSightings[key] {
				markSighting(s, "failed", perr.Error(), nil, 0)
			}
			continue
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return matched, evaluated, grabs, cerr
		}
		_ = chosenTitle
		if verdict == decision.VerdictWouldGrab {
			grabs++
		}
		ref := rssTargetRef{ItemID: key.itemID}
		for _, s := range groupSightings[key] {
			markSighting(s, "evaluated", "", &ref, 0)
		}
	}
	return matched, evaluated, grabs, nil
}

// targetIsWanted reports whether any unit is missing, or sits below either
// cutoff under the resolved profile.
func targetIsWanted(target decision.Target) bool {
	if target.Profile == nil {
		return false
	}
	cutoffPos, cutoffFound := target.Profile.CutoffPosition()
	for _, unit := range target.Units {
		if !unit.Monitored || !unit.Released {
			continue
		}
		if len(unit.Existing) == 0 {
			return true
		}
		for _, file := range unit.Existing {
			if !file.PositionFound {
				// A known quality above the profile's cutoff (canonically)
				// is satisfied, not wanted; only unknown quality is wanted.
				if meets, ok := decision.QualityMeetsCutoffCanonically(target.Domain, target.Profile, file.Quality); ok && meets {
					continue
				}
				return true
			}
			if cutoffFound && file.Position > cutoffPos {
				return true
			}
			if file.FormatScore < target.Profile.CutoffFormatScore {
				return true
			}
		}
	}
	return false
}

// pruneManagerLedger applies the time-symmetric retention policy.
func (a *App) pruneManagerLedger(ctx context.Context, q *sqlc.Queries) {
	_, _ = q.PruneManagerRuns(ctx, pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -90), Valid: true})
	_, _ = q.PruneManagerReleases(ctx, pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -30), Valid: true})
}

// attrsMapFromRaw flattens the persisted ordered attr list back to a map.
func attrsMapFromRaw(raw []byte) map[string]string {
	var attrs []torznab.Attr
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		out[strings.ToLower(attr.Name)] = attr.Value
	}
	return out
}

func intSlice(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// StartManagerRSSLoop runs the RSS sweep on an interval — the worker
// process's background loop. The interval comes from
// HEYA_MANAGER_RSS_INTERVAL_MINUTES (default 15; 0 disables).
func (a *App) StartManagerRSSLoop(ctx context.Context, intervalMinutes int) {
	if intervalMinutes == 0 {
		intervalMinutes = 15
	}
	if intervalMinutes < 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := a.RunManagerRSS(ctx, "scheduled"); err != nil {
					log.Warn().Err(err).Msg("manager rss sweep failed")
				}
			}
		}
	}()
}
