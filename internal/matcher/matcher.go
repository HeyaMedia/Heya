package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/rs/zerolog/log"
)

// debouncedEnrichWindow is the trailing-edge debounce delay applied to
// child-content additions (new albums/tracks under an artist, new
// seasons/episodes under a series). 30s covers a downloader dropping
// a box-set without firing one heya.media fetch per release.
const debouncedEnrichWindow = 30 * time.Second

// enrichStatusComplete mirrors worker.enrichStatusComplete. Lives here
// so the matcher can compare without importing the worker package.
const enrichStatusComplete = "complete"

type MatchInfo struct {
	ProviderName string
	ProviderID   string
	IsNew        bool
	// ArtistID is set when a music match creates or links to an artist. The
	// MetadataMatchWorker uses it to enqueue a RefreshMusicArtist job for
	// post-match enrichment.
	ArtistID int64
}

// ProbeFunc runs ffprobe against a local or SMB path and returns parsed media
// info. It is injected (rather than imported) so the matcher can read embedded
// audio tags on demand without depending on the worker package — which imports
// the matcher, so a direct import would cycle. Wired to worker.ProbeFile in
// service.App; nil in tests, where music fusion transparently falls back to
// path/NFO only.
type ProbeFunc func(ctx context.Context, path string) (*mediaprobe.MediaInfo, error)

type Matcher struct {
	db    *pgxpool.Pool
	q     *sqlc.Queries
	heya  *heyamedia.HeyaProvider
	opts  MatchOptions
	probe ProbeFunc
}

func New(db *pgxpool.Pool, opts MatchOptions, heya *heyamedia.HeyaProvider, probe ProbeFunc) *Matcher {
	return &Matcher{
		db:    db,
		q:     sqlc.New(db),
		heya:  heya,
		opts:  opts,
		probe: probe,
	}
}

// WithTx returns a copy of the matcher whose queries run inside tx, so a caller
// can make a multi-step rebuild (delete + re-store, as in re-identify) atomic.
// Every persistence helper goes through m.q, so swapping it is sufficient. The
// one exception is the music-merge path, which opens its own pool transaction
// via m.db — don't call it through a tx-scoped matcher.
func (m *Matcher) WithTx(tx pgx.Tx) *Matcher {
	c := *m
	c.q = m.q.WithTx(tx)
	return &c
}

func (m *Matcher) MatchLibrary(ctx context.Context, libraryID int64, mediaType sqlc.MediaType) (MatchResult, error) {
	var result MatchResult

	if mediaType == sqlc.MediaTypeMusic {
		return m.matchMusicLibrary(ctx, libraryID)
	}

	files, err := m.q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     10000,
		Offset:    0,
	})
	if err != nil {
		return result, fmt.Errorf("listing pending files: %w", err)
	}

	for _, file := range files {
		_, err := m.matchFile(ctx, file, mediaType, libraryID)
		if err != nil {
			log.Error().Err(err).Str("path", file.Path).Msg("match error")
			m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
				ID:           file.ID,
				Status:       sqlc.FileStatusError,
				ErrorMessage: err.Error(),
			})
			result.Errors++
			continue
		}

		updated, _ := m.q.GetLibraryFileByID(ctx, file.ID)
		if updated.Status == sqlc.FileStatusMatched {
			result.Matched++
		} else {
			result.Unmatched++
		}
	}

	return result, nil
}

func (m *Matcher) MatchSingleFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) (MatchInfo, error) {
	if mediaType == sqlc.MediaTypeMusic {
		return m.matchMusicSingleFile(ctx, file, libraryID)
	}
	return m.matchFile(ctx, file, mediaType, libraryID)
}

func (m *Matcher) matchFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) (MatchInfo, error) {
	parsed, nfoIDs := parseFileResult(file.ParseResult)

	kind := MediaTypeToKind(mediaType)

	if nfoIDs != nil && (nfoIDs.TMDBID != "" || nfoIDs.IMDBID != "" || nfoIDs.MBID != "" || nfoIDs.AniDBID != "" || nfoIDs.MALID != "") {
		if info, matched := m.tryNFOLookup(ctx, file, kind, libraryID, nfoIDs); matched {
			return info, nil
		}
		log.Debug().Int64("file_id", file.ID).Msg("NFO lookup failed, falling back to title search")
	}

	query := buildSearchQuery(parsed, kind)

	if fetchOpts := m.fetchOptsForLibrary(ctx, libraryID); fetchOpts != nil {
		query.Language = fetchOpts.Language
		query.Country = fetchOpts.Country
	}

	if query.Title == "" && query.ISBN == "" {
		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusUnmatched,
			ErrorMessage: "no parseable title",
		})
		return MatchInfo{}, nil
	}

	allResults, searchErr := m.heya.Search(ctx, kind, query)
	if searchErr != nil {
		log.Warn().Err(searchErr).Msg("search failed")
		allResults = nil
	}
	for i := range allResults {
		allResults[i].Confidence = scoreBestTitle(query.Title, allResults[i], query.Year)
	}

	if len(allResults) == 0 {
		// Materialize a local entity ONLY when the search genuinely returned
		// nothing. A search ERROR (transient / network) must stay a retryable
		// unmatched row — materializing would permanently mark the file matched
		// and mask a fetchable remote match behind a low-quality local stub.
		if searchErr == nil {
			// No remote signal at all — fold into an existing matched item of the
			// same identity if one exists (heya.media-outage recovery).
			if info, ok := m.materializeLocal(ctx, file, parsed, nfoIDs, kind, mediaType, libraryID, true); ok {
				return info, nil
			}
		}
		msg := "no provider results"
		if searchErr != nil {
			msg = "search error: " + searchErr.Error()
		}
		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusUnmatched,
			ErrorMessage: msg,
		})
		return MatchInfo{}, nil
	}

	sortByConfidence(allResults)

	if len(allResults) > m.opts.MaxCandidates {
		allResults = allResults[:m.opts.MaxCandidates]
	}

	top := allResults[0]
	clearGap := hasClearGap(allResults, query.Title)

	threshold := autoMatchThresholdFor(top, m.opts.AutoMatchThreshold)
	if top.Confidence >= threshold && clearGap {
		log.Info().
			Str("query", query.Title).
			Str("matched", top.Title).
			Str("year", top.Year).
			Float64("score", top.Confidence).
			Float64("threshold", threshold).
			Bool("enriched", top.Enriched).
			Msg("auto-match")
		return m.autoMatch(ctx, file, top, kind, libraryID)
	}

	log.Info().
		Str("query", query.Title).
		Str("top_match", top.Title).
		Str("year", top.Year).
		Float64("score", top.Confidence).
		Float64("threshold", threshold).
		Bool("enriched", top.Enriched).
		Bool("clear_gap", clearGap).
		Int("candidates", len(allResults)).
		Msg("match rejected — needs manual review")

	m.storeCandidates(ctx, file.ID, allResults)
	// Ambiguous match: candidates are stored for manual review. Materialize a
	// stub so the file is visible, but restrict the fold to local stubs — never
	// silently attach onto a published item a coincidental title+year matches.
	if info, ok := m.materializeLocal(ctx, file, parsed, nfoIDs, kind, mediaType, libraryID, false); ok {
		return info, nil
	}
	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:           file.ID,
		Status:       sqlc.FileStatusUnmatched,
		ErrorMessage: fmt.Sprintf("%d candidates, top confidence %.2f", len(allResults), top.Confidence),
	})
	return MatchInfo{}, nil
}

// linkExisting points a library file at an already-present media item and
// marks it matched — the shared tail of every materializeLocal dedup hit.
func (m *Matcher) linkExisting(ctx context.Context, fileID, mediaItemID int64) (MatchInfo, bool) {
	_ = m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          fileID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})
	return MatchInfo{IsNew: false}, true
}

// materializeLocal creates a visible entity from local signal (filename / NFO)
// when remote matching produced nothing confident — so the file isn't left
// invisible. The entity is flagged enrichment_status='local'; the enrich worker
// upgrades it in place if it carries a provider id. Returns ok=false (writing
// nothing) when there isn't even a local title to show. Music is not handled
// here (it goes through matchMusicSingleFile).
//
// foldIntoMatched controls the dedup reach. When true (remote search returned
// nothing at all), the file may fold into an existing enriched/matched item of
// the same identity — the recovery that keeps a heya.media outage on a new
// episode from forking the series. When false (search returned candidates but
// none was a confident, unambiguous winner), the fold is restricted to local
// stubs, so a coincidental title+year collision can't silently attach a file
// onto a published item that manual review should have adjudicated.
func (m *Matcher) materializeLocal(ctx context.Context, file sqlc.LibraryFile, parsed parser.ParsedStorageEntry, nfoIDs *metadata.NFOIDs, kind metadata.MediaKind, mediaType sqlc.MediaType, libraryID int64, foldIntoMatched bool) (MatchInfo, bool) {
	title, year := "", ""
	if parsed.Release != nil {
		title = parsed.Release.Title
		year = parsed.Release.Year
	}
	if nfoIDs != nil && nfoIDs.Title != "" {
		title = nfoIDs.Title
		if nfoIDs.Year != "" {
			year = nfoIDs.Year
		}
	}
	if strings.TrimSpace(title) == "" {
		return MatchInfo{}, false
	}

	// Dedup: link this file to the existing entity of the same natural identity
	// (normalized title|year|media_type) instead of spawning a duplicate. How far
	// the fold reaches is gated by foldIntoMatched (see the doc comment).
	if existing, err := m.q.FindMediaItemByIdentity(ctx, sqlc.FindMediaItemByIdentityParams{
		LibraryID:      libraryID,
		MediaType:      mediaType,
		Year:           year,
		Title:          title,
		IncludeMatched: foldIntoMatched,
	}); err == nil {
		if existing.EnrichmentStatus != "local" {
			log.Info().Int64("file_id", file.ID).Int64("media_id", existing.ID).
				Str("title", title).Str("existing_status", existing.EnrichmentStatus).
				Msg("materialize local: linked file to existing matched item instead of forking a duplicate")
			// Folding a new episode's file onto a complete series won't create
			// its tv_episodes row — the enrich worker's idempotency gate skips
			// complete parents. Schedule the same trailing-edge forced re-enrich
			// autoMatch uses for new episodes so the sweeper pulls the new
			// season/episode. No-ops unless the parent is already complete.
			if kind == metadata.KindTV {
				m.maybeDebounceEnrich(ctx, existing.ID, "matcher.tv.local-fold")
			}
		}
		return m.linkExisting(ctx, file.ID, existing.ID)
	}

	extIDs := map[string]string{}
	providerKind := "local"
	if nfoIDs != nil {
		if nfoIDs.TMDBID != "" {
			extIDs["tmdb"] = nfoIDs.TMDBID
		}
		if nfoIDs.IMDBID != "" {
			extIDs["imdb"] = nfoIDs.IMDBID
		}
		if nfoIDs.TVDBID != "" {
			extIDs["tvdb"] = nfoIDs.TVDBID
		}
		if len(extIDs) > 0 {
			providerKind = "nfo"
		}
	}

	stub := &metadata.MediaDetail{
		Title:        title,
		SortTitle:    strings.ToLower(title),
		Year:         year,
		ExternalIDs:  extIDs,
		ProviderKind: providerKind,
	}

	mediaItemID, isNew, err := m.createOrLinkMediaItem(ctx, stub, kind, libraryID, file.Path)
	if err != nil {
		log.Error().Err(err).Int64("file_id", file.ID).Msg("materialize local: create media item failed")
		return MatchInfo{}, false
	}

	// Flag local; record title/year as locally-sourced (enrich may refresh them
	// — they're not user-locked). Re-scan dedup keys on natural identity, so
	// there's no stored key to stamp here.
	if err := m.q.MarkMediaItemLocal(ctx, sqlc.MarkMediaItemLocalParams{
		ID:              mediaItemID,
		MatchConfidence: 0,
	}); err != nil {
		log.Warn().Err(err).Int64("id", mediaItemID).Msg("materialize local: mark local failed")
	}
	prov := FieldProvenance{}.Set("title", ProvLocal).Set("year", ProvLocal)
	_ = m.q.SetMediaItemFieldProvenance(ctx, sqlc.SetMediaItemFieldProvenanceParams{ID: mediaItemID, FieldProvenance: prov.Marshal()})

	// Materialize the type-specific stub so the item is visible via the library's
	// INNER JOIN. The stub detail carries no seasons, so for TV this writes only
	// the series row; episodes arrive from enrich.
	if err := m.createTypeSpecificRow(ctx, mediaItemID, kind, stub, file.Path); err != nil {
		log.Warn().Err(err).Int64("id", mediaItemID).Msg("materialize local: type-specific row failed")
	}

	_ = m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          file.ID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})

	log.Info().Int64("file_id", file.ID).Int64("media_id", mediaItemID).
		Str("title", title).Str("provider_kind", providerKind).Msg("materialized local entity")
	return MatchInfo{IsNew: isNew}, true
}

// hasClearGap reports whether the top-ranked candidate is an unambiguous enough
// winner to auto-apply without manual review. results must be non-empty and
// already sorted by descending confidence; queryTitle is the search title.
//
// A candidate wins the gap when it is alone, or clearly ahead (>0.10) of the
// next candidate with a DIFFERENT normalized title, OR when it is the sole exact
// normalized-title match to the query. The last clause is load-bearing: a
// companion that merely shares a title prefix — "Enter the House of the Dragon",
// "…Podcast: House of the Dragon" — scores close on fuzzy similarity and would
// otherwise veto the real series via the 0.10 gap, forking a fresh local entity
// for every new episode. Two genuine same-title hits still read as ambiguous.
func hasClearGap(results []metadata.SearchResult, queryTitle string) bool {
	if len(results) == 1 {
		return true
	}
	top := results[0]
	secondDiff := -1
	for i := 1; i < len(results); i++ {
		if NormalizeTitle(results[i].Title) != NormalizeTitle(top.Title) {
			secondDiff = i
			break
		}
	}
	if secondDiff == -1 || (top.Confidence-results[secondDiff].Confidence) > 0.10 {
		return true
	}
	if NormalizeTitle(top.Title) == NormalizeTitle(queryTitle) {
		exact := 0
		for _, r := range results {
			if NormalizeTitle(r.Title) == NormalizeTitle(queryTitle) {
				exact++
			}
		}
		return exact == 1
	}
	return false
}

// scoreBestTitle scores the query against the result's primary Title plus
// every entry in AltTitles and returns the best match. HeyaMedia's
// alt_titles[] carries all known locale variants, romanizations, and
// aliases for a hit — running ScoreConfidence over the union lets a
// filename like "Shingeki no Kyojin" score against the Japanese form
// even when HeyaMedia's primary "name" is "Attack on Titan".
//
// Year disambiguation transfers naturally: ScoreConfidence's year bonus
// only uses the result's year (same for every alt-title comparison), so
// the best title-similarity score gets the same year boost.
func scoreBestTitle(queryTitle string, r metadata.SearchResult, queryYear string) float64 {
	best := ScoreConfidence(queryTitle, r.Title, queryYear, r.Year)
	for _, alt := range r.AltTitles {
		if alt == "" {
			continue
		}
		if s := ScoreConfidence(queryTitle, alt, queryYear, r.Year); s > best {
			best = s
		}
	}
	return best
}

// autoMatchThresholdFor adjusts the base auto-match threshold based on
// signals the search hit carried back from heya.media. An "enriched" hit
// means heya has the detail warm-cached and has cross-confirmed the entry
// against multiple sources — we can accept a slightly lower title-similarity
// score in that case because heya's own ranker has already done some of the
// disambiguation work. A non-enriched hit comes from cold upstream provider
// data and gets the full title-match scrutiny.
func autoMatchThresholdFor(top metadata.SearchResult, base float64) float64 {
	if top.Enriched {
		const enrichedBoost = 0.10
		t := base - enrichedBoost
		if t < 0.6 {
			t = 0.6 // floor — title still has to be in the right ballpark
		}
		return t
	}
	return base
}

func (m *Matcher) fetchOptsForLibrary(ctx context.Context, libraryID int64) *metadata.FetchOptions {
	lib, err := m.q.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return nil
	}
	s := metadata.ParseSettings(lib.Settings)
	if s.PreferredLanguage == "" && s.PreferredCountry == "" {
		return nil
	}
	return &metadata.FetchOptions{Language: s.PreferredLanguage, Country: s.PreferredCountry}
}

func (m *Matcher) autoMatch(ctx context.Context, file sqlc.LibraryFile, result metadata.SearchResult, kind metadata.MediaKind, libraryID int64) (MatchInfo, error) {
	stub := stubDetailFromSearch(result)
	mediaItemID, isNew, err := m.createOrLinkMediaItem(ctx, stub, kind, libraryID, file.Path)
	if err != nil {
		return MatchInfo{}, fmt.Errorf("creating media item: %w", err)
	}

	info := MatchInfo{
		ProviderName: result.ProviderName,
		ProviderID:   result.ProviderID,
		IsNew:        isNew,
	}

	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          file.ID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})

	// Trailing-edge debounce. For media types that grow children over
	// time (TV: new seasons/episodes), an existing-and-complete parent
	// otherwise leaves the new content unenriched — the enrich worker's
	// idempotency gate skips re-fetches when status='complete'. The
	// sweeper picks this row up after the debounce window and runs a
	// forced enrich, which pulls fresh upstream data including any new
	// seasons/episodes. See [internal/worker/debounce_sweep_worker.go].
	if !isNew && kind == metadata.KindTV {
		m.maybeDebounceEnrich(ctx, mediaItemID, "matcher.tv")
		// Resolve this file's absolute number immediately if the series is
		// already enriched, so it doesn't wait on the debounced re-enrich.
		// Cheap no-op for non-absolute files and not-yet-enriched series.
		if err := reconcileAbsoluteFile(ctx, m.q, mediaItemID, file.ID, file.ParseResult); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("reconcile absolute episode failed")
		}
	}

	return info, nil
}

// maybeDebounceEnrich upserts a debounced_enriches row when the
// media_item is in enrichment_status='complete'. No-op otherwise (the
// initial enrich is still pending and will fire via the IsNew=true
// path). Errors are logged but never abort the caller's match.
func (m *Matcher) maybeDebounceEnrich(ctx context.Context, mediaItemID int64, requestedBy string) {
	mi, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		log.Debug().Err(err).Int64("media_item_id", mediaItemID).Msg("debounce: media_item lookup failed")
		return
	}
	if mi.EnrichmentStatus != enrichStatusComplete {
		return
	}
	fireAt := pgtype.Timestamptz{Time: time.Now().Add(debouncedEnrichWindow), Valid: true}
	if err := m.q.UpsertDebouncedEnrich(ctx, sqlc.UpsertDebouncedEnrichParams{
		MediaItemID: mediaItemID,
		FireAt:      fireAt,
		RequestedBy: requestedBy,
	}); err != nil {
		log.Warn().Err(err).Int64("media_item_id", mediaItemID).Msg("upsert debounced_enrich failed")
	}
}

// stubDetailFromSearch projects a heya.media search hit into the minimum
// MediaDetail the match step needs to persist a media_items stub. Fields
// not present in the search response (cast, crew, runtime, seasons, etc.)
// stay zero — the enrich worker fills them in once GetDetail is called.
func stubDetailFromSearch(r metadata.SearchResult) *metadata.MediaDetail {
	return &metadata.MediaDetail{
		Title:       r.Title,
		SortTitle:   strings.ToLower(r.Title),
		Year:        r.Year,
		Description: r.Description,
		PosterURL:   r.PosterURL,
		ExternalIDs: r.ExternalIDs,
		HeyaSlug:    r.HeyaSlug,
	}
}

// stubDetailFromNFO builds a media_items stub from a sidecar NFO. NFOs
// reliably carry title + year + provider external IDs (TMDB/IMDB/TVDB/MBID),
// which is enough for the match step. The enrich worker resolves the rest
// from the provider via the external IDs.
func stubDetailFromNFO(ids metadata.NFOIDs) *metadata.MediaDetail {
	extIDs := map[string]string{}
	if ids.TMDBID != "" {
		extIDs["tmdb"] = ids.TMDBID
	}
	if ids.IMDBID != "" {
		extIDs["imdb"] = ids.IMDBID
	}
	if ids.TVDBID != "" {
		extIDs["tvdb"] = ids.TVDBID
	}
	if ids.MBID != "" {
		extIDs["mbid"] = ids.MBID
	}
	if ids.AniDBID != "" {
		extIDs["anidb"] = ids.AniDBID
	}
	if ids.MALID != "" {
		extIDs["mal"] = ids.MALID
	}
	return &metadata.MediaDetail{
		Title:       ids.Title,
		SortTitle:   strings.ToLower(ids.Title),
		Year:        ids.Year,
		ExternalIDs: extIDs,
	}
}

func (m *Matcher) ResolveMatch(ctx context.Context, libraryFileID int64, candidateID int64) error {
	candidate, err := m.q.GetMatchCandidateByID(ctx, candidateID)
	if err != nil {
		return fmt.Errorf("getting candidate: %w", err)
	}

	file, err := m.q.GetLibraryFileByID(ctx, libraryFileID)
	if err != nil {
		return fmt.Errorf("getting library file: %w", err)
	}

	opts := m.fetchOptsForLibrary(ctx, file.LibraryID)
	detail, err := m.heya.GetDetail(ctx, candidate.ProviderID, opts)
	if err != nil {
		return fmt.Errorf("getting detail: %w", err)
	}

	lib, err := m.q.GetLibraryByID(ctx, file.LibraryID)
	if err != nil {
		return fmt.Errorf("getting library: %w", err)
	}
	kind := MediaTypeToKind(lib.MediaType)
	mediaItemID, _, err := m.createOrLinkMediaItem(ctx, detail, kind, file.LibraryID, file.Path)
	if err != nil {
		return fmt.Errorf("creating media item: %w", err)
	}

	// User explicitly picked this candidate — we already paid for GetDetail
	// above, so fill in the type-specific + rich rows now rather than
	// queueing a separate enrich job.
	_ = m.createTypeSpecificRow(ctx, mediaItemID, kind, detail, file.Path)
	m.storeRichMetadata(ctx, mediaItemID, detail)

	m.q.ChooseMatchCandidate(ctx, sqlc.ChooseMatchCandidateParams{
		ChosenID:      candidateID,
		LibraryFileID: libraryFileID,
	})

	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          file.ID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})

	// The episode catalog was just built inline — reconcile the whole series'
	// absolute-numbered files onto their real season/episode.
	if kind == metadata.KindTV {
		if _, err := ReconcileAbsoluteEpisodes(ctx, m.q, mediaItemID); err != nil {
			log.Warn().Err(err).Int64("item_id", mediaItemID).Msg("reconcile absolute episodes failed")
		}
	}

	return nil
}

type parsedFileResult struct {
	Parsed parser.ParsedStorageEntry `json:"parsed"`
	NFO    *nfoData                  `json:"nfo,omitempty"`
}

type nfoData struct {
	TMDBID string `json:"TMDBID"`
	IMDBID string `json:"IMDBID"`
	TVDBID string `json:"TVDBID"`
	MBID   string `json:"MBID"`
	Title  string `json:"Title"`
	Year   string `json:"Year"`
}

func parseFileResult(data []byte) (parser.ParsedStorageEntry, *metadata.NFOIDs) {
	var wrapper parsedFileResult
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Parsed.InputPath != "" {
		var ids *metadata.NFOIDs
		if wrapper.NFO != nil && (wrapper.NFO.TMDBID != "" || wrapper.NFO.IMDBID != "" || wrapper.NFO.MBID != "") {
			ids = &metadata.NFOIDs{
				TMDBID: wrapper.NFO.TMDBID,
				IMDBID: wrapper.NFO.IMDBID,
				TVDBID: wrapper.NFO.TVDBID,
				MBID:   wrapper.NFO.MBID,
				Title:  wrapper.NFO.Title,
				Year:   wrapper.NFO.Year,
			}
		}
		return wrapper.Parsed, mergeFilenameIDs(ids, wrapper.Parsed.Release)
	}

	var parsed parser.ParsedStorageEntry
	json.Unmarshal(data, &parsed)
	return parsed, mergeFilenameIDs(nil, parsed.Release)
}

// mergeFilenameIDs folds provider IDs embedded in the filename/path into the
// NFO-derived ID set. NFO IDs win (they're explicit, curated metadata); the
// filename only fills providers the NFO didn't supply. Returns ids unchanged
// when the release carries no embedded IDs.
func mergeFilenameIDs(ids *metadata.NFOIDs, rel *parser.SceneReleaseParse) *metadata.NFOIDs {
	if rel == nil || (rel.ImdbID == "" && rel.TmdbID == "" && rel.TvdbID == "" && rel.AnidbID == "" && rel.MalID == "") {
		return ids
	}
	if ids == nil {
		ids = &metadata.NFOIDs{}
	}
	if ids.IMDBID == "" {
		ids.IMDBID = rel.ImdbID
	}
	if ids.TMDBID == "" {
		ids.TMDBID = rel.TmdbID
	}
	if ids.TVDBID == "" {
		ids.TVDBID = rel.TvdbID
	}
	if ids.AniDBID == "" {
		ids.AniDBID = rel.AnidbID
	}
	if ids.MALID == "" {
		ids.MALID = rel.MalID
	}
	// Carry the filename's title/year too: the new-item strong-ID path
	// (tryNFOLookup → stubDetailFromNFO) needs a title to write the stub —
	// without it, a filename-ID-only item would bail to a fuzzy title search
	// instead of an authoritative direct-ID match.
	if ids.Title == "" {
		ids.Title = rel.Title
	}
	if ids.Year == "" {
		ids.Year = rel.Year
	}
	return ids
}

func (m *Matcher) tryNFOLookup(ctx context.Context, file sqlc.LibraryFile, kind metadata.MediaKind, libraryID int64, ids *metadata.NFOIDs) (MatchInfo, bool) {
	if info, linked := m.tryLinkExistingByNFO(ctx, file, libraryID, ids); linked {
		return info, true
	}

	// New item path. Skip heya.LookupByNFO (which fetches full detail
	// inline) and write a stub straight from the NFO. The enrich worker
	// will resolve the rest via the external IDs we just stored.
	providerID := pickProviderIDFromNFO(kind, ids)
	if providerID == "" {
		log.Debug().Int64("file_id", file.ID).Msg("NFO had no usable IDs for stub match")
		return MatchInfo{}, false
	}

	stub := stubDetailFromNFO(*ids)
	if stub.Title == "" {
		// Without a title we can't render the row meaningfully — fall
		// back to the title-search path.
		log.Debug().Int64("file_id", file.ID).Msg("NFO missing title, falling back to search")
		return MatchInfo{}, false
	}

	mediaItemID, isNew, err := m.createOrLinkMediaItem(ctx, stub, kind, libraryID, file.Path)
	if err != nil {
		log.Error().Err(err).Msg("failed to create media item from NFO stub")
		return MatchInfo{}, false
	}

	log.Info().
		Str("provider_id", providerID).
		Str("title", stub.Title).
		Int64("file_id", file.ID).
		Msg("matched via NFO stub")

	info := MatchInfo{
		ProviderName: m.heya.Name(),
		ProviderID:   providerID,
		IsNew:        isNew,
	}

	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          file.ID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})
	return info, true
}

// pickProviderIDFromNFO selects the best provider ID from an NFO sidecar
// for the enrich step's heya.media GetDetail call. Preference order mirrors
// what heyamedia.LookupByNFO would try.
func pickProviderIDFromNFO(kind metadata.MediaKind, ids *metadata.NFOIDs) string {
	switch kind {
	case metadata.KindTV:
		if ids.TVDBID != "" {
			return "tvdb:" + ids.TVDBID
		}
		if ids.TMDBID != "" {
			return "tmdb:" + ids.TMDBID
		}
		if ids.IMDBID != "" {
			return "imdb:" + ids.IMDBID
		}
		if ids.AniDBID != "" {
			return "anidb:" + ids.AniDBID
		}
		if ids.MALID != "" {
			return "mal:" + ids.MALID
		}
	case metadata.KindMovie:
		if ids.TMDBID != "" {
			return "tmdb:" + ids.TMDBID
		}
		if ids.IMDBID != "" {
			return "imdb:" + ids.IMDBID
		}
	case metadata.KindMusic:
		if ids.MBID != "" {
			return "mbid:" + ids.MBID
		}
	}
	return ""
}

func (m *Matcher) tryLinkExistingByNFO(ctx context.Context, file sqlc.LibraryFile, libraryID int64, ids *metadata.NFOIDs) (MatchInfo, bool) {
	candidates := []map[string]string{}

	if ids.TMDBID != "" {
		candidates = append(candidates, map[string]string{"tmdb": ids.TMDBID})
	}
	if ids.IMDBID != "" {
		candidates = append(candidates, map[string]string{"imdb": ids.IMDBID})
	}
	if ids.TVDBID != "" {
		candidates = append(candidates, map[string]string{"tvdb": ids.TVDBID})
	}
	if ids.AniDBID != "" {
		candidates = append(candidates, map[string]string{"anidb": ids.AniDBID})
	}
	if ids.MALID != "" {
		candidates = append(candidates, map[string]string{"mal": ids.MALID})
	}

	for _, extIDs := range candidates {
		extJSON, _ := json.Marshal(extIDs)
		existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
			LibraryID: libraryID,
			ExtFilter: extJSON,
		})
		if err != nil {
			continue
		}

		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:          file.ID,
			Status:      sqlc.FileStatusMatched,
			MediaItemID: pgInt8(existing.ID),
		})

		// Anime linked by anidb to an existing (usually enriched) series:
		// resolve its absolute episode now instead of waiting on a re-enrich.
		if err := reconcileAbsoluteFile(ctx, m.q, existing.ID, file.ID, file.ParseResult); err != nil {
			log.Warn().Err(err).Int64("file_id", file.ID).Msg("reconcile absolute episode failed")
		}

		log.Debug().Int64("file_id", file.ID).Int64("media_id", existing.ID).Str("title", existing.Title).Msg("linked to existing item via NFO IDs")
		return MatchInfo{IsNew: false}, true
	}

	return MatchInfo{}, false
}

func (m *Matcher) storeCandidates(ctx context.Context, fileID int64, results []metadata.SearchResult) {
	m.q.DeleteMatchCandidatesByFile(ctx, fileID)
	for _, r := range results {
		rawJSON, _ := json.Marshal(r.RawData)
		if rawJSON == nil {
			rawJSON = []byte("{}")
		}
		m.q.CreateMatchCandidate(ctx, sqlc.CreateMatchCandidateParams{
			LibraryFileID: fileID,
			ProviderName:  r.ProviderName,
			ProviderID:    r.ProviderID,
			Title:         r.Title,
			Year:          r.Year,
			Description:   truncate(r.Description, 500),
			PosterUrl:     r.PosterURL,
			Confidence:    numericFromFloat(r.Confidence),
			RawData:       rawJSON,
		})
	}
}

func buildSearchQuery(parsed parser.ParsedStorageEntry, kind metadata.MediaKind) metadata.SearchQuery {
	q := metadata.SearchQuery{}

	if parsed.Release != nil {
		q.Title = parsed.Release.Title
		q.Year = parsed.Release.Year
		q.Seasons = parsed.Release.Seasons

		if parsed.Release.ReleaseHash != "" && kind == metadata.KindBook {
			q.ISBN = parsed.Release.ReleaseHash
		}
	}

	if kind == metadata.KindMusic && q.Title != "" {
		parts := strings.SplitN(q.Title, " - ", 2)
		if len(parts) == 2 {
			q.Artist = strings.TrimSpace(parts[0])
			q.Album = strings.TrimSpace(parts[1])
		}
	}

	if kind == metadata.KindBook && q.Title != "" {
		parts := strings.SplitN(q.Title, " - ", 2)
		if len(parts) == 2 {
			q.Author = strings.TrimSpace(parts[0])
			q.Title = strings.TrimSpace(parts[1])
		}
	}

	return q
}

func MediaTypeToKind(mt sqlc.MediaType) metadata.MediaKind {
	switch mt {
	case sqlc.MediaTypeMovie:
		return metadata.KindMovie
	case sqlc.MediaTypeTv:
		return metadata.KindTV
	case sqlc.MediaTypeMusic:
		return metadata.KindMusic
	case sqlc.MediaTypeBook:
		return metadata.KindBook
	default:
		return metadata.KindMovie
	}
}

func sortByConfidence(results []metadata.SearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Confidence > results[j-1].Confidence; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
