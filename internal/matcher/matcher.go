package matcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/rs/zerolog/log"
)

type Matcher struct {
	db         *pgxpool.Pool
	q          *sqlc.Queries
	providers  []metadata.Provider
	downloader *images.Downloader
	opts       MatchOptions
}

func New(db *pgxpool.Pool, dl *images.Downloader, opts MatchOptions, providers ...metadata.Provider) *Matcher {
	return &Matcher{
		db:         db,
		q:          sqlc.New(db),
		providers:  providers,
		downloader: dl,
		opts:       opts,
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
		err := m.matchFile(ctx, file, mediaType, libraryID)
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

func (m *Matcher) MatchSingleFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) error {
	return m.matchFile(ctx, file, mediaType, libraryID)
}

func (m *Matcher) matchFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) error {
	parsed, nfoIDs := parseFileResult(file.ParseResult)

	kind := mediaTypeToKind(mediaType)

	if nfoIDs != nil && (nfoIDs.TMDBID != "" || nfoIDs.IMDBID != "" || nfoIDs.MBID != "") {
		if matched := m.tryNFOLookup(ctx, file, kind, libraryID, nfoIDs); matched {
			return nil
		}
		log.Debug().Int64("file_id", file.ID).Msg("NFO lookup failed, falling back to title search")
	}

	query := buildSearchQuery(parsed, kind)

	if query.Title == "" && query.ISBN == "" {
		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusUnmatched,
			ErrorMessage: "no parseable title",
		})
		return nil
	}

	var allResults []metadata.SearchResult
	for _, p := range m.providers {
		if !p.Supports(kind) {
			continue
		}

		results, err := p.Search(ctx, kind, query)
		if err != nil {
			log.Warn().Err(err).Str("provider", p.Name()).Msg("search failed")
			continue
		}

		for i := range results {
			results[i].Confidence = ScoreConfidence(query.Title, results[i].Title, query.Year, results[i].Year)
		}
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusUnmatched,
			ErrorMessage: "no provider results",
		})
		return nil
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
	return nil
}

func (m *Matcher) autoMatch(ctx context.Context, file sqlc.LibraryFile, result metadata.SearchResult, kind metadata.MediaKind, libraryID int64) error {
	provider := m.findProvider(result.ProviderName)
	if provider == nil {
		return fmt.Errorf("provider %q not found", result.ProviderName)
	}

	detail, err := provider.GetDetail(ctx, result.ProviderID)
	if err != nil {
		return fmt.Errorf("getting detail: %w", err)
	}

	mediaItemID, err := m.createOrLinkMediaItem(ctx, detail, kind, libraryID, file.Path)
	if err != nil {
		return fmt.Errorf("creating media item: %w", err)
	}

	m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:          file.ID,
		Status:      sqlc.FileStatusMatched,
		MediaItemID: pgInt8(mediaItemID),
	})

	return nil
}

func (m *Matcher) ResolveMatch(ctx context.Context, libraryFileID int64, candidateID int64) error {
	candidate, err := m.q.GetMatchCandidateByID(ctx, candidateID)
	if err != nil {
		return fmt.Errorf("getting candidate: %w", err)
	}

	provider := m.findProvider(candidate.ProviderName)
	if provider == nil {
		return fmt.Errorf("provider %q not found", candidate.ProviderName)
	}

	detail, err := provider.GetDetail(ctx, candidate.ProviderID)
	if err != nil {
		return fmt.Errorf("getting detail: %w", err)
	}

	file, err := m.q.GetLibraryFileByID(ctx, libraryFileID)
	if err != nil {
		return fmt.Errorf("getting library file: %w", err)
	}

	kind := metadata.MediaKind(mediaTypeFromProvider(candidate.ProviderName))
	mediaItemID, err := m.createOrLinkMediaItem(ctx, detail, kind, file.LibraryID, file.Path)
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

func (m *Matcher) tryNFOLookup(ctx context.Context, file sqlc.LibraryFile, kind metadata.MediaKind, libraryID int64, ids *metadata.NFOIDs) bool {
	for _, p := range m.providers {
		if !p.Supports(kind) {
			continue
		}

		dlp, ok := p.(metadata.DirectLookupProvider)
		if !ok {
			continue
		}

		detail, providerID, err := dlp.LookupByNFO(ctx, kind, *ids)
		if err != nil {
			log.Debug().Err(err).Str("provider", p.Name()).Msg("NFO lookup failed")
			continue
		}

		log.Info().
			Str("provider", p.Name()).
			Str("provider_id", providerID).
			Str("title", detail.Title).
			Int64("file_id", file.ID).
			Msg("matched via NFO direct lookup")

		mediaItemID, err := m.createOrLinkMediaItem(ctx, detail, kind, libraryID, file.Path)
		if err != nil {
			log.Error().Err(err).Msg("failed to create media item from NFO lookup")
			continue
		}

		m.q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:          file.ID,
			Status:      sqlc.FileStatusMatched,
			MediaItemID: pgInt8(mediaItemID),
		})
		return true
	}
	return false
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

func (m *Matcher) findProvider(name string) metadata.Provider {
	for _, p := range m.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
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

func mediaTypeToKind(mt sqlc.MediaType) metadata.MediaKind {
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

func mediaTypeFromProvider(providerName string) string {
	switch providerName {
	case "tmdb":
		return "movie"
	case "musicbrainz":
		return "music"
	case "openlibrary":
		return "book"
	default:
		return "movie"
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
