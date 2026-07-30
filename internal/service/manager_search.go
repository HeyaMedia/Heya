package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/decision"
	"github.com/karbowiak/heya/internal/manager/formats"
	"github.com/karbowiak/heya/internal/manager/sabnzbd"
	"github.com/karbowiak/heya/internal/manager/torznab"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/parser/video"
)

// ── Views ────────────────────────────────────────────────────────────────

type ManagerRejectionView struct {
	Code    string         `json:"code"`
	Stage   string         `json:"stage"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

type ManagerSearchCandidateView struct {
	Title           string                 `json:"title"`
	Indexer         string                 `json:"indexer"`
	SizeBytes       int64                  `json:"size_bytes"`
	PublishDate     *time.Time             `json:"publish_date,omitempty"`
	Quality         string                 `json:"quality,omitempty"`
	FormatScore     int32                  `json:"format_score"`
	FormatBreakdown []decision.FormatHit   `json:"format_breakdown,omitempty"`
	Languages       []string               `json:"languages,omitempty"`
	Rejections      []ManagerRejectionView `json:"rejections"`
	Acceptable      bool                   `json:"acceptable"`
	SelectionRank   int                    `json:"selection_rank,omitempty"`
	Chosen          bool                   `json:"chosen"`
}

type ManagerRunIndexerView struct {
	Indexer    string `json:"indexer"`
	Domain     string `json:"domain"`
	Status     string `json:"status"`
	Fetched    int    `json:"fetched"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type ManagerSearchRunView struct {
	RunID       int64                        `json:"run_id"`
	Status      string                       `json:"status"`
	Partial     bool                         `json:"partial"`
	Verdict     string                       `json:"verdict"`
	ChosenTitle string                       `json:"chosen_title,omitempty"`
	Target      string                       `json:"target"`
	Profile     string                       `json:"profile"`
	Indexers    []ManagerRunIndexerView      `json:"indexers"`
	Candidates  []ManagerSearchCandidateView `json:"candidates"`
}

// ── Policy snapshot ──────────────────────────────────────────────────────

// policySnapshotDoc is the full evaluation policy, serialized canonically
// (encoding/json sorts map keys) and content-addressed by sha256. One row
// backs every decision made under identical policy.
type policySnapshotDoc struct {
	Profile struct {
		ID                int64                `json:"id"`
		Name              string               `json:"name"`
		Domain            string               `json:"domain"`
		Items             []ManagerQualityItem `json:"items"`
		Cutoff            string               `json:"cutoff"`
		UpgradesEnabled   bool                 `json:"upgrades_enabled"`
		MinFormatScore    int32                `json:"min_format_score"`
		CutoffFormatScore int32                `json:"cutoff_format_score"`
		MinUpgradeScore   int32                `json:"min_upgrade_score"`
		Language          string               `json:"language"`
		FormatScores      map[string]int32     `json:"format_scores"`
	} `json:"profile"`
	Formats            []formats.Format `json:"formats"`
	PreferProperRepack bool             `json:"prefer_proper_repack"`
	SizeDefsVersion    int              `json:"size_defs_version"`
	EvaluatorVersion   int              `json:"evaluator_version"`
	ParserVersion      int              `json:"parser_version"`
}

// buildDecisionPolicy loads a profile + its domain's custom formats into the
// engine's policy model and persists the content-addressed snapshot.
func (a *App) buildDecisionPolicy(ctx context.Context, q *sqlc.Queries, profileID int64) (*decision.Profile, string, error) {
	row, err := q.GetManagerQualityProfile(ctx, profileID)
	if err != nil {
		return nil, "", fmt.Errorf("loading quality profile %d: %w", profileID, err)
	}
	var items []ManagerQualityItem
	if err := json.Unmarshal(row.Items, &items); err != nil {
		return nil, "", fmt.Errorf("profile %d ladder: %w", profileID, err)
	}
	var scores []ManagerFormatScore
	_ = json.Unmarshal(row.FormatScores, &scores)

	profile := &decision.Profile{
		ID: row.ID, Name: row.Name, Domain: row.Domain,
		Cutoff:            row.Cutoff,
		UpgradesEnabled:   row.UpgradesEnabled,
		MinFormatScore:    row.MinFormatScore,
		CutoffFormatScore: row.CutoffFormatScore,
		MinUpgradeScore:   row.MinUpgradeScore,
		Language:          row.Language,
		// Not yet a profile column; arr default behavior.
		PreferProperRepack: true,
		FormatScores:       map[int64]int32{},
	}
	for _, item := range items {
		profile.Items = append(profile.Items, decision.LadderItem{
			Quality: item.Quality, Group: item.Group, Qualities: item.Qualities, Allowed: item.Allowed,
		})
	}
	for _, s := range scores {
		profile.FormatScores[s.FormatID] = s.Score
	}
	if row.Domain == "movie" || row.Domain == "tv" {
		profile.SizeDefs = decision.VideoSizeDefs
	}

	formatRows, err := q.ListManagerCustomFormats(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("loading custom formats: %w", err)
	}
	for _, fr := range formatRows {
		if fr.Domain != row.Domain {
			continue
		}
		var specs []formats.CustomFormatSpec
		if err := json.Unmarshal(fr.Specifications, &specs); err != nil {
			continue
		}
		profile.Formats = append(profile.Formats, formats.Format{ID: fr.ID, Name: fr.Name, Specs: specs})
	}
	sort.Slice(profile.Formats, func(i, j int) bool { return profile.Formats[i].ID < profile.Formats[j].ID })

	doc := policySnapshotDoc{
		Formats:            profile.Formats,
		PreferProperRepack: profile.PreferProperRepack,
		SizeDefsVersion:    decision.SizeDefsVersion,
		EvaluatorVersion:   decision.EvaluatorVersion,
		ParserVersion:      decision.ParserVersion,
	}
	doc.Profile.ID = profile.ID
	doc.Profile.Name = profile.Name
	doc.Profile.Domain = profile.Domain
	doc.Profile.Items = items
	doc.Profile.Cutoff = profile.Cutoff
	doc.Profile.UpgradesEnabled = profile.UpgradesEnabled
	doc.Profile.MinFormatScore = profile.MinFormatScore
	doc.Profile.CutoffFormatScore = profile.CutoffFormatScore
	doc.Profile.MinUpgradeScore = profile.MinUpgradeScore
	doc.Profile.Language = profile.Language
	doc.Profile.FormatScores = map[string]int32{}
	for id, score := range profile.FormatScores {
		doc.Profile.FormatScores[fmt.Sprintf("%d", id)] = score
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling policy snapshot: %w", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if err := q.InsertManagerPolicySnapshot(ctx, sqlc.InsertManagerPolicySnapshotParams{
		PolicyHash: hash, Snapshot: raw,
	}); err != nil {
		return nil, "", fmt.Errorf("persisting policy snapshot: %w", err)
	}
	return profile, hash, nil
}

// ── Movie target ─────────────────────────────────────────────────────────

type movieTargetMeta struct {
	LibraryID int64
	Title     string
	Year      int
	ProfileID int64
}

// buildMovieTarget assembles the full decision target for one movie:
// identity (title + aliases + provider ids), existing files with parsed
// quality + provenance, and live queue coverage.
func (a *App) buildMovieTarget(ctx context.Context, itemID int64) (decision.Target, movieTargetMeta, error) {
	var (
		target decision.Target
		meta   movieTargetMeta
	)
	var (
		title, originalTitle, originalLanguage string
		year, runtime                          int
		profileID                              pgtype.Int8
		monitored                              bool
		released                               pgtype.Date
	)
	err := a.db.QueryRow(ctx, `
		SELECT c.title,
		       COALESCE(CASE WHEN c.year ~ '^\d{4}' THEN left(c.year, 4)::int END, 0),
		       mi.monitored, mi.quality_profile_id,
		       COALESCE(m.original_title, ''), COALESCE(m.original_language, ''),
		       COALESCE(m.runtime_minutes, 0), m.release_date, c.library_id
		FROM media_item_cards c
		JOIN media_items mi ON mi.id = c.id
		LEFT JOIN movies m ON m.media_item_id = c.id
		WHERE c.id = $1 AND c.media_type = 'movie'`,
		itemID,
	).Scan(&title, &year, &monitored, &profileID, &originalTitle, &originalLanguage, &runtime, &released, &meta.LibraryID)
	if err != nil {
		return target, meta, fmt.Errorf("loading movie %d: %w", itemID, err)
	}
	meta.Title, meta.Year = title, year
	if profileID.Valid {
		meta.ProfileID = profileID.Int64
	}

	target.Domain = "movie"
	target.MediaItemID = itemID
	target.Year = year
	target.OriginalLanguage = strings.ToLower(originalLanguage)
	target.RuntimeMinutes = runtime
	target.IDs = map[string]string{}

	titles := map[string]bool{}
	addTitle := func(s string) {
		if n := matcher.NormalizeTitle(s); n != "" && !titles[n] {
			titles[n] = true
			target.NormalizedTitles = append(target.NormalizedTitles, n)
		}
	}
	addTitle(title)
	addTitle(originalTitle)
	aliasRows, err := a.db.Query(ctx, `SELECT title FROM media_titles WHERE media_item_id = $1`, itemID)
	if err == nil {
		for aliasRows.Next() {
			var alias string
			if aliasRows.Scan(&alias) == nil {
				addTitle(alias)
			}
		}
		aliasRows.Close()
	}

	idRows, err := a.db.Query(ctx, `SELECT provider, external_id FROM media_item_external_ids WHERE media_item_id = $1`, itemID)
	if err == nil {
		for idRows.Next() {
			var provider, id string
			if idRows.Scan(&provider, &id) == nil {
				switch strings.ToLower(provider) {
				case "imdb":
					target.IDs["imdbid"] = id
				case "tmdb":
					target.IDs["tmdbid"] = id
				case "tvdb":
					target.IDs["tvdbid"] = id
				}
			}
		}
		idRows.Close()
	}

	unit := decision.Unit{
		Key:       fmt.Sprintf("movie:%d", itemID),
		Monitored: monitored,
		Released:  !released.Valid || !released.Time.After(time.Now()),
	}
	unit.Existing, err = a.movieExistingFiles(ctx, itemID)
	if err != nil {
		return target, meta, err
	}
	unit.Queued = a.queueCoverage(ctx, target.NormalizedTitles, year)
	target.Units = []decision.Unit{unit}
	return target, meta, nil
}

// movieExistingFiles snapshots the live files with parsed quality and
// honest provenance: a basename with an explicit source token is
// 'parsed_name'; a bare-resolution name whose WEB-DL was invented by the
// parser fallback is 'inferred' (uncertain); resolution-only knowledge from
// media info is 'media_info' (uncertain).
func (a *App) movieExistingFiles(ctx context.Context, itemID int64) ([]decision.ExistingFile, error) {
	rows, err := a.db.Query(ctx, `
		SELECT lf.id, lf.path, COALESCE(lf.video_height, 0)
		FROM library_file_links lfl
		JOIN library_files lf ON lf.id = lfl.library_file_id
		WHERE lfl.media_item_id = $1 AND lfl.relation_type IN ('primary','part')
		  AND lf.deleted_at IS NULL`, itemID)
	if err != nil {
		return nil, fmt.Errorf("loading movie files: %w", err)
	}
	defer rows.Close()

	var files []decision.ExistingFile
	for rows.Next() {
		var (
			fileID      int64
			path        string
			videoHeight int
		)
		if err := rows.Scan(&fileID, &path, &videoHeight); err != nil {
			return nil, err
		}
		files = append(files, existingFileSnapshot(fileID, filepath.Base(path), videoHeight))
	}
	return files, rows.Err()
}

func existingFileSnapshot(fileID int64, basename string, videoHeight int) decision.ExistingFile {
	attrs := formats.ParseVideoRelease(basename, 0, false)
	key := formats.QualityKey(attrs)
	file := decision.ExistingFile{
		FileID: fileID, Basename: basename, Quality: key,
		RevisionVersion: attrs.RevisionVersion,
	}
	explicitSource := len(video.ParseSource(strings.ToLower(basename))) > 0 || attrs.Modifier != "none"
	switch {
	case key != "" && explicitSource:
		file.Provenance = "parsed_name"
	case key != "":
		// The parser invented WEB-DL from a bare resolution; the quality is
		// a guess, not a fact.
		file.Provenance = "inferred"
		file.Uncertain = true
	case videoHeight > 0:
		file.Quality = heightFallbackQuality(videoHeight)
		file.Provenance = "media_info"
		file.Uncertain = true
	default:
		file.Provenance = "inferred"
		file.Uncertain = true
	}
	return file
}

// heightFallbackQuality is the resolution-only guess used when only media
// info knows anything — width-first labeling is handled upstream at scan
// time; here the stored height band picks the nearest ladder resolution.
func heightFallbackQuality(height int) string {
	switch {
	case height >= 1600:
		return "webdl-2160p"
	case height >= 800:
		return "webdl-1080p"
	case height >= 600:
		return "webdl-720p"
	case height > 0:
		return "webdl-480p"
	default:
		return ""
	}
}

// queueCoverage parses the live SAB queues for releases that already cover
// this target. Best-effort: a dead client contributes nothing.
func (a *App) queueCoverage(ctx context.Context, normalizedTitles []string, year int) []decision.QueuedRelease {
	q := sqlc.New(a.db)
	clients, err := q.ListManagerDownloadClients(ctx)
	if err != nil {
		return nil
	}
	titleSet := map[string]bool{}
	for _, t := range normalizedTitles {
		titleSet[t] = true
	}
	var queued []decision.QueuedRelease
	for _, client := range clients {
		if !client.Enabled || client.Kind != "sabnzbd" {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		sabQueue, err := sabnzbd.New(client.BaseUrl, client.ApiKey).Queue(cctx)
		cancel()
		if err != nil {
			continue
		}
		for _, slot := range sabQueue.Slots {
			parsed := video.FilenameParseMovie(slot.Filename)
			if parsed.Title == "" || !titleSet[matcher.NormalizeTitle(parsed.Title)] {
				continue
			}
			if year > 0 && parsed.Year != "" && parsed.Year != fmt.Sprintf("%d", year) {
				continue
			}
			attrs := formats.ParseVideoRelease(slot.Filename, 0, false)
			queued = append(queued, decision.QueuedRelease{
				Title:           slot.Filename,
				Quality:         formats.QualityKey(attrs),
				RevisionVersion: attrs.RevisionVersion,
			})
		}
	}
	return queued
}

// ── Search orchestration ─────────────────────────────────────────────────

type indexerFetch struct {
	row      sqlc.ManagerIndexer
	releases []torznab.Release
	requests []requestRecord
	status   string
	err      string
	duration time.Duration
}

type requestRecord struct {
	params   map[string]string
	results  int
	duration time.Duration
	err      string
}

const searchConcurrency = 4

// SearchManagerMovie runs the full shadow search for one movie: fan out
// across enabled usenet indexers, evaluate every candidate, persist the run
// as the accountability record, and return the interactive view.
func (a *App) SearchManagerMovie(ctx context.Context, mediaItemID int64, source string) (*ManagerSearchRunView, error) {
	q := sqlc.New(a.db)

	target, meta, err := a.buildMovieTarget(ctx, mediaItemID)
	if err != nil {
		return nil, err
	}

	var policyHash string
	if meta.ProfileID != 0 {
		profile, hash, err := a.buildDecisionPolicy(ctx, q, meta.ProfileID)
		if err != nil {
			return nil, err
		}
		if profile.Domain != "movie" {
			return nil, fmt.Errorf("profile %q is a %s profile, not movie", profile.Name, profile.Domain)
		}
		target.Profile = profile
		policyHash = hash
		// Existing files + queued releases get their ladder position and
		// format score under this profile — the upgrade spec's baseline.
		decision.ResolveUnits(&target)
	}

	scope, _ := json.Marshal(map[string]any{
		"media_item_id": mediaItemID, "title": meta.Title, "year": meta.Year, "domain": "movie",
	})
	run, err := q.CreateManagerRun(ctx, sqlc.CreateManagerRunParams{
		Kind: "interactive", Source: source, Scope: scope,
	})
	if err != nil {
		return nil, fmt.Errorf("creating run: %w", err)
	}

	fetches := a.fanOutMovieSearch(ctx, q, meta, target)

	// Record per-indexer accounting and collect candidates.
	var (
		candidates []decision.Candidate
		candMeta   []indexerFetch
		anyOK      bool
		anyFailed  bool
	)
	indexerViews := make([]ManagerRunIndexerView, 0, len(fetches))
	releaseIDs := map[int]pgtype.Int8{} // candidate index → release row id
	for _, fetch := range fetches {
		runIdx, err := q.CreateManagerRunIndexer(ctx, sqlc.CreateManagerRunIndexerParams{
			RunID: run.ID, IndexerID: pgtype.Int8{Int64: fetch.row.ID, Valid: true},
			IndexerName: fetch.row.Name, Domain: "movie", Status: fetch.status,
			PagesFetched: int32(len(fetch.requests)), Fetched: int32(len(fetch.releases)),
			DurationMs: fetch.duration.Milliseconds(), Error: fetch.err,
		})
		if err != nil {
			return nil, fmt.Errorf("recording run indexer: %w", err)
		}
		var lastRequestID pgtype.Int8
		for ordinal, req := range fetch.requests {
			params, _ := json.Marshal(req.params)
			reqRow, err := q.CreateManagerRunRequest(ctx, sqlc.CreateManagerRunRequestParams{
				RunIndexerID: runIdx.ID, Ordinal: int32(ordinal), Params: params,
				Results: int32(req.results), DurationMs: req.duration.Milliseconds(), Error: req.err,
			})
			if err != nil {
				return nil, fmt.Errorf("recording run request: %w", err)
			}
			lastRequestID = pgtype.Int8{Int64: reqRow.ID, Valid: true}
		}
		switch fetch.status {
		case "ok":
			anyOK = true
		case "failed":
			anyFailed = true
		}
		indexerViews = append(indexerViews, ManagerRunIndexerView{
			Indexer: fetch.row.Name, Domain: "movie", Status: fetch.status,
			Fetched: len(fetch.releases), DurationMs: fetch.duration.Milliseconds(), Error: fetch.err,
		})

		for _, rel := range fetch.releases {
			idx := len(candidates)
			releaseRow, err := a.persistRelease(ctx, q, fetch.row, "movie", rel, run.ID, lastRequestID, policyHash)
			if err != nil {
				return nil, err
			}
			releaseIDs[idx] = pgtype.Int8{Int64: releaseRow, Valid: true}
			candidates = append(candidates, decision.Candidate{
				Index: idx, Title: rel.Title, SizeBytes: rel.Size,
				PublishDate: rel.PublishDate,
				IndexerID:   fetch.row.ID, IndexerName: fetch.row.Name,
				IndexerPriority: fetch.row.Priority,
				Categories:      rel.Categories,
				IDHints:         idHints(rel.Attrs),
			})
		}
		candMeta = append(candMeta, fetch)
	}
	_ = candMeta

	result := decision.Evaluate(target, candidates)

	view, err := a.persistSearchRun(ctx, run, target, meta, policyHash, result, releaseIDs, indexerViews, anyOK, anyFailed)
	if err != nil {
		// The persist transaction rolled back — the run row must not sit in
		// 'running' forever; record the failure on it.
		errDoc, _ := json.Marshal([]string{err.Error()})
		_, _ = q.FinishManagerRun(ctx, sqlc.FinishManagerRunParams{
			ID: run.ID, Status: "failed", Stats: []byte("{}"), Errors: errDoc,
		})
		return nil, err
	}
	a.notifyManagerChanged(ctx, "runs")
	return view, nil
}

// fanOutMovieSearch queries every enabled usenet indexer (bounded
// concurrency, per-indexer timeout). Prowlarr parent rows don't search —
// their materialized torznab children do.
func (a *App) fanOutMovieSearch(ctx context.Context, q *sqlc.Queries, meta movieTargetMeta, target decision.Target) []indexerFetch {
	rows, err := q.ListManagerIndexers(ctx)
	if err != nil {
		return nil
	}
	var searchable []sqlc.ManagerIndexer
	var skipped []indexerFetch
	for _, row := range rows {
		switch {
		case row.Kind == IndexerKindProwlarr:
			continue
		case !row.Enabled:
			skipped = append(skipped, indexerFetch{row: row, status: "skipped_disabled"})
		case row.Protocol == "torrent":
			skipped = append(skipped, indexerFetch{row: row, status: "skipped_unsupported_protocol"})
		default:
			searchable = append(searchable, row)
		}
	}

	results := make([]indexerFetch, len(searchable))
	var wg sync.WaitGroup
	sem := make(chan struct{}, searchConcurrency)
	for i, row := range searchable {
		wg.Add(1)
		go func(i int, row sqlc.ManagerIndexer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = a.searchOneIndexer(ctx, row, meta, target)
		}(i, row)
	}
	wg.Wait()
	return append(results, skipped...)
}

func (a *App) searchOneIndexer(ctx context.Context, row sqlc.ManagerIndexer, meta movieTargetMeta, target decision.Target) indexerFetch {
	fetch := indexerFetch{row: row, status: "ok"}
	client := torznab.New(row.BaseUrl, row.ApiKey)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cats := []int{2000}
	if len(row.Categories) > 0 {
		cats = make([]int, 0, len(row.Categories))
		for _, c := range row.Categories {
			cats = append(cats, int(c))
		}
	}

	// IDs first, q-fallback: an imdb-capable query is precise; the fallback
	// carries the year to keep same-title candidates distinguishable.
	query := torznab.Query{Type: "movie", Cats: cats, Limit: 100}
	params := map[string]string{"t": "movie", "cats": intsCSV(cats)}
	if imdb := target.IDs["imdbid"]; imdb != "" {
		query.ImdbID = imdb
		params["imdbid"] = imdb
	} else {
		query.Type = "search"
		params["t"] = "search"
		query.Q = fmt.Sprintf("%s %d", meta.Title, meta.Year)
		params["q"] = query.Q
	}

	start := time.Now()
	releases, err := client.Search(cctx, query)
	elapsed := time.Since(start)
	record := requestRecord{params: params, results: len(releases), duration: elapsed}
	if err != nil {
		record.err = err.Error()
		fetch.status = "failed"
		fetch.err = err.Error()
	}
	fetch.requests = append(fetch.requests, record)
	fetch.duration = elapsed

	// ID search that returns nothing falls back to a text query — some
	// indexers index sparse id attributes.
	if err == nil && len(releases) == 0 && query.ImdbID != "" {
		fallback := torznab.Query{Type: "search", Q: fmt.Sprintf("%s %d", meta.Title, meta.Year), Cats: cats, Limit: 100}
		fparams := map[string]string{"t": "search", "q": fallback.Q, "cats": intsCSV(cats)}
		start = time.Now()
		fbReleases, fbErr := client.Search(cctx, fallback)
		felapsed := time.Since(start)
		frecord := requestRecord{params: fparams, results: len(fbReleases), duration: felapsed}
		if fbErr != nil {
			frecord.err = fbErr.Error()
		} else {
			releases = fbReleases
		}
		fetch.requests = append(fetch.requests, frecord)
		fetch.duration += felapsed
	}

	fetch.releases = releases
	return fetch
}

// persistRelease upserts the durable release row + a sighting for this run.
func (a *App) persistRelease(ctx context.Context, q *sqlc.Queries, indexer sqlc.ManagerIndexer, domain string, rel torznab.Release, runID int64, requestID pgtype.Int8, policyHash string) (int64, error) {
	releaseKey := rel.GUID
	if releaseKey == "" {
		sum := sha256.Sum256([]byte(rel.Title + "|" + fmt.Sprintf("%d", rel.Size)))
		releaseKey = "fp:" + hex.EncodeToString(sum[:16])
	}
	fingerprint := releaseFingerprint(rel.Title, rel.Size)
	rawAttrs, _ := json.Marshal(rel.RawAttrs)
	var publishDate pgtype.Timestamptz
	if !rel.PublishDate.IsZero() {
		publishDate = pgtype.Timestamptz{Time: rel.PublishDate, Valid: true}
	}
	var guid pgtype.Text
	if rel.GUID != "" {
		guid = pgtype.Text{String: rel.GUID, Valid: true}
	}
	row, err := q.UpsertManagerRelease(ctx, sqlc.UpsertManagerReleaseParams{
		IndexerID: pgtype.Int8{Int64: indexer.ID, Valid: true}, IndexerName: indexer.Name,
		Domain: domain, ReleaseKey: releaseKey, UiFingerprint: fingerprint,
		Guid: guid, Title: rel.Title, SizeBytes: max64(rel.Size, 0),
		PublishDate: publishDate, PublishDateRaw: rel.PublishDateRaw,
		Categories: int32Slice(rel.Categories), RawAttrs: rawAttrs,
		InfoUrl: stripQueryString(rel.InfoURL),
	})
	if err != nil {
		return 0, fmt.Errorf("upserting release %q: %w", rel.Title, err)
	}
	var hash pgtype.Text
	if policyHash != "" {
		hash = pgtype.Text{String: policyHash, Valid: true}
	}
	_, err = q.CreateManagerReleaseSighting(ctx, sqlc.CreateManagerReleaseSightingParams{
		ReleaseID: row.ID, RunID: pgtype.Int8{Int64: runID, Valid: true},
		RunRequestID: requestID, Status: "evaluated", PolicyHash: hash,
	})
	if err != nil {
		return 0, fmt.Errorf("recording sighting: %w", err)
	}
	return row.ID, nil
}

// persistSearchRun writes candidates + decisions + the coverage matrix in
// one transaction (the chosen-row FK is deferred) and builds the view.
func (a *App) persistSearchRun(
	ctx context.Context,
	run sqlc.ManagerRun,
	target decision.Target,
	meta movieTargetMeta,
	policyHash string,
	result decision.Result,
	releaseIDs map[int]pgtype.Int8,
	indexerViews []ManagerRunIndexerView,
	anyOK, anyFailed bool,
) (*ManagerSearchRunView, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback-on-error path

	qtx := sqlc.New(tx)
	candidateRowIDs := make([]int64, len(result.Candidates))
	for i, cand := range result.Candidates {
		parsed, _ := json.Marshal(map[string]any{
			"resolution": cand.Attrs.Resolution, "sources": cand.Attrs.Sources,
			"modifier": cand.Attrs.Modifier, "group": cand.Attrs.Group,
			"languages": cand.Attrs.Languages, "year": cand.Attrs.Year,
			"revision": cand.Attrs.RevisionVersion, "release_type": cand.Attrs.ReleaseType,
			"parser_version": decision.ParserVersion,
		})
		breakdown, _ := json.Marshal(orEmptyHits(cand.FormatBreakdown))
		rejections, _ := json.Marshal(orEmptyRejections(cand.RunRejections))
		var quality pgtype.Text
		if cand.QualityKey != "" {
			quality = pgtype.Text{String: cand.QualityKey, Valid: true}
		}
		var position pgtype.Int4
		if cand.PositionFound {
			position = pgtype.Int4{Int32: int32(cand.Position), Valid: true}
		}
		var publishDate pgtype.Timestamptz
		if !cand.Input.PublishDate.IsZero() {
			publishDate = pgtype.Timestamptz{Time: cand.Input.PublishDate, Valid: true}
		}
		row, err := qtx.CreateManagerCandidate(ctx, sqlc.CreateManagerCandidateParams{
			RunID: run.ID, ReleaseID: releaseIDs[cand.Input.Index],
			IndexerID:   pgtype.Int8{Int64: cand.Input.IndexerID, Valid: cand.Input.IndexerID != 0},
			IndexerName: cand.Input.IndexerName, IndexerPriority: cand.Input.IndexerPriority,
			Title: cand.Input.Title, SizeBytes: max64(cand.Input.SizeBytes, 0),
			PublishDate: publishDate, Categories: int32Slice(cand.Input.Categories),
			Parsed: parsed, Quality: quality, QualityPosition: position,
			FormatScore: cand.FormatScore, FormatBreakdown: breakdown, Rejections: rejections,
		})
		if err != nil {
			return nil, fmt.Errorf("persisting candidate %q: %w", cand.Input.Title, err)
		}
		candidateRowIDs[i] = row.ID
	}

	var (
		chosenTitle string
		verdict     = decision.VerdictNoAcceptableCandidate
	)
	for _, unit := range result.Units {
		verdict = unit.Verdict
		contextDoc, _ := json.Marshal(map[string]any{
			"existing": existingContext(target.Units[0].Existing),
			"queued":   queuedContext(target.Units[0].Queued),
		})
		var profileID pgtype.Int8
		profileName := ""
		if target.Profile != nil {
			profileID = pgtype.Int8{Int64: target.Profile.ID, Valid: true}
			profileName = target.Profile.Name
		}
		var hash pgtype.Text
		if policyHash != "" {
			hash = pgtype.Text{String: policyHash, Valid: true}
		}
		// would_grab rows insert provisionally: the chosen⇔grab CHECK is
		// not deferrable, so the real verdict lands in the same UPDATE
		// that sets the chosen row (still one transaction).
		insertVerdict := unit.Verdict
		if insertVerdict == decision.VerdictWouldGrab {
			insertVerdict = decision.VerdictNoAcceptableCandidate
		}
		decisionRow, err := qtx.CreateManagerDecision(ctx, sqlc.CreateManagerDecisionParams{
			RunID: run.ID, TargetKind: "movie", TargetKey: unit.UnitKey,
			MediaItemID: pgtype.Int8{Int64: target.MediaItemID, Valid: true},
			LibraryID:   meta.LibraryID, Domain: "movie",
			TargetTitle: meta.Title, TargetYear: int32(meta.Year),
			ProfileID: profileID, ProfileName: profileName, PolicyHash: hash,
			EvaluatorVersion: decision.EvaluatorVersion, ParserVersion: decision.ParserVersion,
			Verdict: insertVerdict, Context: contextDoc,
		})
		if err != nil {
			return nil, fmt.Errorf("persisting decision: %w", err)
		}

		var chosenRowID int64
		for i, cand := range result.Candidates {
			eval, ok := cand.PerUnit[unit.UnitKey]
			if !ok {
				continue
			}
			ctVerdict := "rejected"
			var rank pgtype.Int4
			if eval.Acceptable {
				ctVerdict = "acceptable"
				rank = pgtype.Int4{Int32: int32(eval.SelectionRank), Valid: eval.SelectionRank > 0}
			}
			rejections, _ := json.Marshal(orEmptyRejections(eval.Rejections))
			ctRow, err := qtx.CreateManagerCandidateTarget(ctx, sqlc.CreateManagerCandidateTargetParams{
				CandidateID: candidateRowIDs[i], DecisionID: decisionRow.ID, RunID: run.ID,
				Verdict: ctVerdict, Rejections: rejections, SelectionRank: rank,
			})
			if err != nil {
				return nil, fmt.Errorf("persisting candidate target: %w", err)
			}
			if unit.Verdict == decision.VerdictWouldGrab && cand.Input.Index == unit.ChosenCandidate {
				chosenRowID = ctRow.ID
				chosenTitle = cand.Input.Title
			}
		}
		if chosenRowID != 0 {
			if err := qtx.MarkManagerDecisionGrab(ctx, sqlc.MarkManagerDecisionGrabParams{
				ID: decisionRow.ID, ChosenTargetRow: pgtype.Int8{Int64: chosenRowID, Valid: true},
			}); err != nil {
				return nil, fmt.Errorf("setting chosen candidate: %w", err)
			}
		}
	}

	status := "completed"
	partial := anyFailed && anyOK
	if anyFailed && !anyOK {
		status = "failed"
	}
	stats, _ := json.Marshal(map[string]any{
		"candidates": len(result.Candidates), "verdict": verdict,
	})
	if _, err := qtx.FinishManagerRun(ctx, sqlc.FinishManagerRunParams{
		ID: run.ID, Status: status, Partial: partial, Truncated: false,
		Stats: stats, Errors: []byte("[]"),
	}); err != nil {
		return nil, fmt.Errorf("finishing run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	view := &ManagerSearchRunView{
		RunID: run.ID, Status: status, Partial: partial,
		Verdict: verdict, ChosenTitle: chosenTitle,
		Target:   fmt.Sprintf("%s (%d)", meta.Title, meta.Year),
		Indexers: indexerViews,
	}
	if target.Profile != nil {
		view.Profile = target.Profile.Name
	}
	unitKey := ""
	if len(result.Units) > 0 {
		unitKey = result.Units[0].UnitKey
	}
	for _, cand := range result.Candidates {
		cv := ManagerSearchCandidateView{
			Title: cand.Input.Title, Indexer: cand.Input.IndexerName,
			SizeBytes: cand.Input.SizeBytes,
			Quality:   cand.QualityKey, FormatScore: cand.FormatScore,
			FormatBreakdown: cand.FormatBreakdown,
			Languages:       cand.Attrs.Languages,
			Rejections:      rejectionViews(cand.RunRejections),
		}
		if !cand.Input.PublishDate.IsZero() {
			t := cand.Input.PublishDate
			cv.PublishDate = &t
		}
		if eval, ok := cand.PerUnit[unitKey]; ok {
			cv.Acceptable = eval.Acceptable
			cv.SelectionRank = eval.SelectionRank
			cv.Rejections = append(cv.Rejections, rejectionViews(eval.Rejections)...)
			cv.Chosen = len(result.Units) > 0 && result.Units[0].ChosenCandidate == cand.Input.Index && result.Units[0].Verdict == decision.VerdictWouldGrab
		}
		view.Candidates = append(view.Candidates, cv)
	}
	sortCandidateViews(view.Candidates)
	return view, nil
}

// sortCandidateViews: chosen first, then ranked accepted, then rejected by
// title — the interactive-modal order.
func sortCandidateViews(views []ManagerSearchCandidateView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Chosen != b.Chosen {
			return a.Chosen
		}
		if a.Acceptable != b.Acceptable {
			return a.Acceptable
		}
		if a.Acceptable && a.SelectionRank != b.SelectionRank {
			return a.SelectionRank < b.SelectionRank
		}
		return a.Title < b.Title
	})
}

// ── Small helpers ────────────────────────────────────────────────────────

func idHints(attrs map[string]string) map[string]string {
	hints := map[string]string{}
	for _, key := range []string{"imdbid", "imdb", "tmdbid", "tvdbid"} {
		if v, ok := attrs[key]; ok && v != "" && v != "0" {
			normalized := key
			if key == "imdb" {
				normalized = "imdbid"
			}
			hints[normalized] = v
		}
	}
	return hints
}

func releaseFingerprint(title string, size int64) string {
	normalized := matcher.NormalizeTitle(title)
	bucket := size / (512 * 1024 * 1024) // half-GB buckets
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", normalized, bucket)))
	return hex.EncodeToString(sum[:12])
}

func stripQueryString(rawURL string) string {
	if idx := strings.IndexByte(rawURL, '?'); idx >= 0 {
		return rawURL[:idx]
	}
	return rawURL
}

func rejectionViews(rejections []decision.Rejection) []ManagerRejectionView {
	views := make([]ManagerRejectionView, 0, len(rejections))
	for _, rej := range rejections {
		views = append(views, ManagerRejectionView{
			Code: string(rej.Code), Stage: rej.Stage, Message: rej.Message, Params: rej.Params,
		})
	}
	return views
}

func existingContext(files []decision.ExistingFile) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]any{
			"file_id": f.FileID, "basename": f.Basename, "quality": f.Quality,
			"revision": f.RevisionVersion, "format_score": f.FormatScore,
			"provenance": f.Provenance, "uncertain": f.Uncertain,
		})
	}
	return out
}

func queuedContext(queued []decision.QueuedRelease) []map[string]any {
	out := make([]map[string]any, 0, len(queued))
	for _, q := range queued {
		out = append(out, map[string]any{"title": q.Title, "quality": q.Quality})
	}
	return out
}

func orEmptyHits(hits []decision.FormatHit) []decision.FormatHit {
	if hits == nil {
		return []decision.FormatHit{}
	}
	return hits
}

func orEmptyRejections(rejections []decision.Rejection) []decision.Rejection {
	if rejections == nil {
		return []decision.Rejection{}
	}
	return rejections
}

func int32Slice(ints []int) []int32 {
	out := make([]int32, len(ints))
	for i, v := range ints {
		out[i] = int32(v)
	}
	return out
}

func intsCSV(ints []int) string {
	parts := make([]string, len(ints))
	for i, v := range ints {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, ",")
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
