package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

type Matcher struct {
	db   *pgxpool.Pool
	q    *sqlc.Queries
	heya *heyamedia.HeyaProvider
	opts MatchOptions
}

func New(db *pgxpool.Pool, opts MatchOptions, heya *heyamedia.HeyaProvider) *Matcher {
	return &Matcher{
		db:   db,
		q:    sqlc.New(db),
		heya: heya,
		opts: opts,
	}
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

	if nfoIDs != nil && (nfoIDs.TMDBID != "" || nfoIDs.IMDBID != "" || nfoIDs.MBID != "") {
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

	allResults, err := m.heya.Search(ctx, kind, query)
	if err != nil {
		log.Warn().Err(err).Msg("search failed")
		allResults = nil
	}
	for i := range allResults {
		allResults[i].Confidence = scoreBestTitle(query.Title, allResults[i], query.Year)
	}

	if len(allResults) == 0 {
		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusUnmatched,
			ErrorMessage: "no provider results",
		})
		return MatchInfo{}, nil
	}

	sortByConfidence(allResults)

	if len(allResults) > m.opts.MaxCandidates {
		allResults = allResults[:m.opts.MaxCandidates]
	}

	top := allResults[0]
	clearGap := len(allResults) == 1
	if !clearGap {
		secondDiff := -1
		for i := 1; i < len(allResults); i++ {
			if NormalizeTitle(allResults[i].Title) != NormalizeTitle(top.Title) {
				secondDiff = i
				break
			}
		}
		clearGap = secondDiff == -1 || (top.Confidence-allResults[secondDiff].Confidence) > 0.10
	}

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
	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:           file.ID,
		Status:       sqlc.FileStatusUnmatched,
		ErrorMessage: fmt.Sprintf("%d candidates, top confidence %.2f", len(allResults), top.Confidence),
	})
	return MatchInfo{}, nil
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
		return wrapper.Parsed, ids
	}

	var parsed parser.ParsedStorageEntry
	json.Unmarshal(data, &parsed)
	return parsed, nil
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

	for _, extIDs := range candidates {
		extJSON, _ := json.Marshal(extIDs)
		existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
			LibraryID: libraryID,
			Column2:   extJSON,
		})
		if err != nil {
			continue
		}

		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:          file.ID,
			Status:      sqlc.FileStatusMatched,
			MediaItemID: pgInt8(existing.ID),
		})

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
