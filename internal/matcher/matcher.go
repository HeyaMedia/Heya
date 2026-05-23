package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/rs/zerolog/log"
)

type MatchInfo struct {
	ProviderName string
	ProviderID   string
	IsNew        bool
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
		allResults[i].Confidence = ScoreConfidence(query.Title, allResults[i].Title, query.Year, allResults[i].Year)
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

	if top.Confidence >= m.opts.AutoMatchThreshold && clearGap {
		return m.autoMatch(ctx, file, top, kind, libraryID)
	}

	m.storeCandidates(ctx, file.ID, allResults)
	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:           file.ID,
		Status:       sqlc.FileStatusUnmatched,
		ErrorMessage: fmt.Sprintf("%d candidates, top confidence %.2f", len(allResults), top.Confidence),
	})
	return MatchInfo{}, nil
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
	opts := m.fetchOptsForLibrary(ctx, libraryID)
	detail, err := m.heya.GetDetail(ctx, result.ProviderID, opts)
	if err != nil {
		return MatchInfo{}, fmt.Errorf("getting detail: %w", err)
	}

	mediaItemID, isNew, err := m.createOrLinkMediaItem(ctx, detail, kind, libraryID, file.Path)
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

	return info, nil
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

	opts := m.fetchOptsForLibrary(ctx, libraryID)
	detail, providerID, err := m.heya.LookupByNFO(ctx, kind, *ids, opts)
	if err != nil {
		log.Debug().Err(err).Msg("NFO lookup failed")
		return MatchInfo{}, false
	}

	log.Info().
		Str("provider_id", providerID).
		Str("title", detail.Title).
		Int64("file_id", file.ID).
		Msg("matched via NFO provider lookup")

	mediaItemID, isNew, err := m.createOrLinkMediaItem(ctx, detail, kind, libraryID, file.Path)
	if err != nil {
		log.Error().Err(err).Msg("failed to create media item from NFO lookup")
		return MatchInfo{}, false
	}

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
