package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

// MediaItemView wraps a media item with its availability status.
type MediaItemView struct {
	sqlc.MediaItem
	Available bool `json:"available"`
}

// UnmatchedFile wraps a library file with its match candidates.
type UnmatchedFile struct {
	File       sqlc.LibraryFile      `json:"file"`
	Candidates []sqlc.MatchCandidate `json:"candidates"`
}

// EpisodeFileEntry describes a single episode file mapping.
type EpisodeFileEntry struct {
	FileID int64 `json:"file_id"`
	Size   int64 `json:"size"`
}

// ListMedia returns media items of the given type with availability flags.
func (a *App) ListMedia(ctx context.Context, mediaType sqlc.MediaType, limit, offset int32) ([]MediaItemView, error) {
	q := sqlc.New(a.db)

	items, err := q.ListMediaItemsByType(ctx, sqlc.ListMediaItemsByTypeParams{
		MediaType: mediaType,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, fmt.Errorf("listing media items: %w", err)
	}

	unavailableIDs, _ := q.ListUnavailableMediaItemIDs(ctx, mediaType)
	unavailable := make(map[int64]bool, len(unavailableIDs))
	for _, id := range unavailableIDs {
		unavailable[id] = true
	}

	views := make([]MediaItemView, len(items))
	for i, item := range items {
		views[i] = MediaItemView{
			MediaItem: item,
			Available: !unavailable[item.ID],
		}
	}

	return views, nil
}

// GetMediaItem resolves a media item by numeric ID or slug string.
func (a *App) GetMediaItem(ctx context.Context, idOrSlug string) (sqlc.MediaItem, error) {
	q := sqlc.New(a.db)

	if id, err := strconv.ParseInt(idOrSlug, 10, 64); err == nil {
		return q.GetMediaItemByID(ctx, id)
	}
	return q.GetMediaItemBySlug(ctx, idOrSlug)
}

// GetMediaDetail fetches a media item plus all type-specific data, cast, crew,
// keywords, videos, certifications, recommendations, production companies,
// assets, extras, external ratings, and episode files.
func (a *App) GetMediaDetail(ctx context.Context, idOrSlug string) (map[string]any, error) {
	q := sqlc.New(a.db)

	item, err := a.GetMediaItem(ctx, idOrSlug)
	if err != nil {
		return nil, fmt.Errorf("media item not found: %w", err)
	}

	hasFiles := false
	var mediaFiles []map[string]any
	if files, filesErr := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: item.ID, Valid: true}); filesErr == nil && len(files) > 0 {
		hasFiles = true
		for _, f := range files {
			mediaFiles = append(mediaFiles, map[string]any{
				"id":   f.ID,
				"size": f.Size,
			})
		}
	}

	result := map[string]any{"media_item": item, "available": hasFiles, "files": mediaFiles}

	// Type-specific data
	switch item.MediaType {
	case sqlc.MediaTypeMovie:
		movie, movieErr := q.GetMovieByMediaItemID(ctx, item.ID)
		if movieErr == nil {
			result["movie"] = movie
			if movie.CollectionID.Valid {
				col, colErr := q.GetCollectionByID(ctx, movie.CollectionID.Int64)
				if colErr == nil {
					result["collection"] = col
				}
			}
		}
	case sqlc.MediaTypeTv:
		series, seriesErr := q.GetTVSeriesByMediaItemID(ctx, item.ID)
		if seriesErr == nil {
			result["tv_series"] = series
			seasons, _ := q.ListTVSeasonsBySeries(ctx, series.ID)

			availableSeasons := map[int]bool{}
			if epFiles, err := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: item.ID, Valid: true}); err == nil {
				for _, f := range epFiles {
					if len(f.ParseResult) == 0 {
						continue
					}
					var pr struct {
						Parsed struct {
							Release struct {
								Seasons []int `json:"seasons"`
							} `json:"release"`
						} `json:"parsed"`
					}
					if json.Unmarshal(f.ParseResult, &pr) == nil {
						for _, s := range pr.Parsed.Release.Seasons {
							availableSeasons[s] = true
						}
					}
				}
			}

			type episodeView struct {
				sqlc.TvEpisode
				PreferredTitle    string `json:"preferred_title,omitempty"`
				PreferredOverview string `json:"preferred_overview,omitempty"`
			}
			type seasonWithEpisodes struct {
				sqlc.TvSeason
				Episodes []episodeView `json:"episodes"`
			}

			lib, _ := q.GetLibraryByID(ctx, item.LibraryID)
			libSettings := metadata.ParseSettings(lib.Settings)
			prefLang := libSettings.PreferredLanguage

			var enriched []seasonWithEpisodes
			for _, s := range seasons {
				if len(availableSeasons) > 0 && !availableSeasons[int(s.SeasonNumber)] {
					continue
				}
				eps, _ := q.ListTVEpisodesBySeason(ctx, s.ID)
				var views []episodeView
				for _, ep := range eps {
					ev := episodeView{TvEpisode: ep}
					if prefLang != "" {
						if t, err := q.GetEpisodeTitleByLanguage(ctx, sqlc.GetEpisodeTitleByLanguageParams{EpisodeID: ep.ID, Language: prefLang}); err == nil {
							ev.PreferredTitle = t.Title
						} else if prefLang != "en" {
							if t, err := q.GetEpisodeTitleByLanguage(ctx, sqlc.GetEpisodeTitleByLanguageParams{EpisodeID: ep.ID, Language: "en"}); err == nil {
								ev.PreferredTitle = t.Title
							}
						}
						if o, err := q.GetEpisodeOverviewByLanguage(ctx, sqlc.GetEpisodeOverviewByLanguageParams{EpisodeID: ep.ID, Language: prefLang}); err == nil {
							ev.PreferredOverview = o.Overview
						} else if prefLang != "en" {
							if o, err := q.GetEpisodeOverviewByLanguage(ctx, sqlc.GetEpisodeOverviewByLanguageParams{EpisodeID: ep.ID, Language: "en"}); err == nil {
								ev.PreferredOverview = o.Overview
							}
						}
					}
					views = append(views, ev)
				}
				enriched = append(enriched, seasonWithEpisodes{TvSeason: s, Episodes: views})
			}
			result["seasons"] = enriched
		}
	case sqlc.MediaTypeMusic:
		artist, artistErr := q.GetArtistByMediaItemID(ctx, item.ID)
		if artistErr == nil {
			result["artist"] = artist
			albums, _ := q.ListAlbumsByArtist(ctx, artist.ID)
			result["albums"] = albums
		}
	case sqlc.MediaTypeBook:
		book, bookErr := q.GetBookByMediaItemID(ctx, item.ID)
		if bookErr == nil {
			result["book"] = book
			if book.AuthorID.Valid {
				author, _ := q.GetAuthorByID(ctx, book.AuthorID.Int64)
				result["author"] = author
			}
		}
	}

	// Cast & crew
	if cast, castErr := q.ListMediaCastSlim(ctx, item.ID); castErr == nil && len(cast) > 0 {
		result["cast"] = cast
	}
	if crew, crewErr := q.ListMediaCrewSlim(ctx, item.ID); crewErr == nil && len(crew) > 0 {
		result["crew"] = crew
	}

	// Keywords
	if keywords, kwErr := q.ListMediaKeywords(ctx, item.ID); kwErr == nil && len(keywords) > 0 {
		result["keywords"] = keywords
	}

	// Videos
	if videos, vidErr := q.ListMediaVideos(ctx, item.ID); vidErr == nil && len(videos) > 0 {
		result["videos"] = videos
	}

	// Certifications
	if certs, certErr := q.ListMediaCertifications(ctx, item.ID); certErr == nil && len(certs) > 0 {
		result["certifications"] = certs
	}

	// Recommendations
	if recs, recErr := q.ListMediaRecommendationsWithLibrary(ctx, item.ID); recErr == nil && len(recs) > 0 {
		result["recommendations"] = recs
	}

	// Production companies
	if companies, compErr := q.ListMediaProductionCompanies(ctx, item.ID); compErr == nil && len(companies) > 0 {
		result["production_companies"] = companies
	}

	// Assets
	if assets, assetErr := q.ListMediaAssets(ctx, item.ID); assetErr == nil && len(assets) > 0 {
		result["assets"] = assets
	}

	// Extras
	if extras, extErr := q.ListMediaExtras(ctx, item.ID); extErr == nil && len(extras) > 0 {
		result["extras"] = extras
	}

	// Titles (multi-language)
	if titles, err := q.ListMediaTitles(ctx, item.ID); err == nil && len(titles) > 0 {
		result["titles"] = titles
	}

	// Overviews (multi-language)
	if overviews, err := q.ListMediaOverviews(ctx, item.ID); err == nil && len(overviews) > 0 {
		result["overviews"] = overviews
	}

	// External ratings
	if ratings, ratErr := q.ListExternalRatings(ctx, item.ID); ratErr == nil && len(ratings) > 0 {
		result["external_ratings"] = ratings
	}

	// Episode file map (TV only)
	if item.MediaType == sqlc.MediaTypeTv {
		if epFiles, epErr := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: item.ID, Valid: true}); epErr == nil && len(epFiles) > 0 {
			episodeFileMap := BuildEpisodeFileMap(epFiles)
			if len(episodeFileMap) > 0 {
				result["episode_files"] = episodeFileMap
			}
		}
	}

	lib, libErr := q.GetLibraryByID(ctx, item.LibraryID)
	if libErr == nil {
		settings := metadata.ParseSettings(lib.Settings)
		lang := settings.PreferredLanguage
		country := settings.PreferredCountry

		if lang != "" {
			if t, err := q.GetMediaTitleByLanguage(ctx, sqlc.GetMediaTitleByLanguageParams{MediaItemID: item.ID, Language: lang}); err == nil {
				result["preferred_title"] = t.Title
			} else if lang != "en" {
				if t, err := q.GetMediaTitleByLanguage(ctx, sqlc.GetMediaTitleByLanguageParams{MediaItemID: item.ID, Language: "en"}); err == nil {
					result["preferred_title"] = t.Title
				}
			}

			if o, err := q.GetMediaOverview(ctx, sqlc.GetMediaOverviewParams{MediaItemID: item.ID, Language: lang}); err == nil {
				result["preferred_overview"] = o.Overview
			} else if lang != "en" {
				if o, err := q.GetMediaOverview(ctx, sqlc.GetMediaOverviewParams{MediaItemID: item.ID, Language: "en"}); err == nil {
					result["preferred_overview"] = o.Overview
				}
			}
		}

		if country != "" {
			if c, err := q.GetMediaCertification(ctx, sqlc.GetMediaCertificationParams{MediaItemID: item.ID, Country: country}); err == nil {
				result["preferred_certification"] = c.Certification
			} else if country != "US" {
				if c, err := q.GetMediaCertification(ctx, sqlc.GetMediaCertificationParams{MediaItemID: item.ID, Country: "US"}); err == nil {
					result["preferred_certification"] = c.Certification
				}
			}
		}
	}

	return result, nil
}

// EnrichedMovieView holds an enriched movie with resolution and availability info.
type EnrichedMovieView struct {
	ID               int64    `json:"id"`
	LibraryID        int64    `json:"library_id"`
	MediaType        string   `json:"media_type"`
	Title            string   `json:"title"`
	SortTitle        string   `json:"sort_title"`
	Slug             string   `json:"slug"`
	Year             string   `json:"year"`
	Description      string   `json:"description"`
	PosterPath       string   `json:"poster_path"`
	BackdropPath     string   `json:"backdrop_path"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	Available        bool     `json:"available"`
	Genres           []string `json:"genres"`
	Rating           float64  `json:"rating"`
	RuntimeMinutes   int32    `json:"runtime_minutes"`
	OriginalLanguage string   `json:"original_language"`
	ReleaseDate      string   `json:"release_date,omitempty"`
	CollectionID     *int64   `json:"collection_id,omitempty"`
	Resolution       string   `json:"resolution,omitempty"`
}

// EnrichedTVView holds an enriched TV series with resolution and availability info.
type EnrichedTVView struct {
	ID               int64    `json:"id"`
	LibraryID        int64    `json:"library_id"`
	MediaType        string   `json:"media_type"`
	Title            string   `json:"title"`
	SortTitle        string   `json:"sort_title"`
	Slug             string   `json:"slug"`
	Year             string   `json:"year"`
	Description      string   `json:"description"`
	PosterPath       string   `json:"poster_path"`
	BackdropPath     string   `json:"backdrop_path"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	Available        bool     `json:"available"`
	Genres           []string `json:"genres"`
	Rating           float64  `json:"rating"`
	Status           string   `json:"status"`
	OriginalLanguage string   `json:"original_language"`
	FirstAirDate     string   `json:"first_air_date,omitempty"`
	LastAirDate      string   `json:"last_air_date,omitempty"`
	NumberOfSeasons  int32    `json:"number_of_seasons"`
	NumberOfEpisodes int32    `json:"number_of_episodes"`
	Resolution       string   `json:"resolution,omitempty"`
}

// HeightToResolution converts a pixel height to a display resolution label.
func HeightToResolution(h int32) string {
	switch {
	case h >= 2160:
		return "4k"
	case h >= 1080:
		return "1080p"
	case h >= 720:
		return "720p"
	case h > 0:
		return "sd"
	default:
		return ""
	}
}

// ListEnrichedMovies returns enriched movie views with resolution and availability.
func (a *App) ListEnrichedMovies(ctx context.Context, limit, offset int32) ([]EnrichedMovieView, error) {
	q := sqlc.New(a.db)

	movies, err := q.ListEnrichedMovies(ctx, sqlc.ListEnrichedMoviesParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(movies))
	for i, m := range movies {
		ids[i] = m.ID
	}

	resMap := buildResolutionMap(ctx, q, ids)
	unavailMap := buildUnavailableMap(ctx, q, sqlc.MediaTypeMovie)

	views := make([]EnrichedMovieView, len(movies))
	for i, m := range movies {
		v := EnrichedMovieView{
			ID:               m.ID,
			LibraryID:        m.LibraryID,
			MediaType:        string(m.MediaType),
			Title:            m.Title,
			SortTitle:        m.SortTitle,
			Slug:             m.Slug,
			Year:             m.Year,
			Description:      m.Description,
			PosterPath:       m.PosterPath,
			BackdropPath:     m.BackdropPath,
			Available:        !unavailMap[m.ID],
			Genres:           m.Genres,
			RuntimeMinutes:   m.RuntimeMinutes,
			OriginalLanguage: m.OriginalLanguage,
			Resolution:       resMap[m.ID],
		}
		if m.CreatedAt.Valid {
			v.CreatedAt = m.CreatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if m.UpdatedAt.Valid {
			v.UpdatedAt = m.UpdatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if f, err := m.Rating.Float64Value(); err == nil && f.Valid {
			v.Rating = f.Float64
		}
		if m.ReleaseDate.Valid {
			v.ReleaseDate = m.ReleaseDate.Time.Format("2006-01-02")
		}
		if m.CollectionID.Valid {
			cid := m.CollectionID.Int64
			v.CollectionID = &cid
		}
		views[i] = v
	}
	return views, nil
}

// ListEnrichedTVSeries returns enriched TV series views with resolution and availability.
func (a *App) ListEnrichedTVSeries(ctx context.Context, limit, offset int32) ([]EnrichedTVView, error) {
	q := sqlc.New(a.db)

	series, err := q.ListEnrichedTVSeries(ctx, sqlc.ListEnrichedTVSeriesParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(series))
	for i, s := range series {
		ids[i] = s.ID
	}

	resMap := buildResolutionMap(ctx, q, ids)
	unavailMap := buildUnavailableMap(ctx, q, sqlc.MediaTypeTv)

	views := make([]EnrichedTVView, len(series))
	for i, s := range series {
		v := EnrichedTVView{
			ID:               s.ID,
			LibraryID:        s.LibraryID,
			MediaType:        string(s.MediaType),
			Title:            s.Title,
			SortTitle:        s.SortTitle,
			Slug:             s.Slug,
			Year:             s.Year,
			Description:      s.Description,
			PosterPath:       s.PosterPath,
			BackdropPath:     s.BackdropPath,
			Available:        !unavailMap[s.ID],
			Genres:           s.Genres,
			Status:           s.Status,
			OriginalLanguage: s.OriginalLanguage,
			NumberOfSeasons:  s.NumberOfSeasons,
			NumberOfEpisodes: s.NumberOfEpisodes,
			Resolution:       resMap[s.ID],
		}
		if s.CreatedAt.Valid {
			v.CreatedAt = s.CreatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if s.UpdatedAt.Valid {
			v.UpdatedAt = s.UpdatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if f, err := s.Rating.Float64Value(); err == nil && f.Valid {
			v.Rating = f.Float64
		}
		if s.FirstAirDate.Valid {
			v.FirstAirDate = s.FirstAirDate.Time.Format("2006-01-02")
		}
		if s.LastAirDate.Valid {
			v.LastAirDate = s.LastAirDate.Time.Format("2006-01-02")
		}
		views[i] = v
	}
	return views, nil
}

func buildResolutionMap(ctx context.Context, q *sqlc.Queries, ids []int64) map[int64]string {
	resMap := make(map[int64]string)
	if len(ids) == 0 {
		return resMap
	}
	rows, err := q.ListMediaResolutions(ctx, ids)
	if err != nil {
		return resMap
	}
	for _, row := range rows {
		if row.MediaItemID.Valid {
			resMap[row.MediaItemID.Int64] = HeightToResolution(row.MaxHeight)
		}
	}
	return resMap
}

func buildUnavailableMap(ctx context.Context, q *sqlc.Queries, mt sqlc.MediaType) map[int64]bool {
	unavailMap := make(map[int64]bool)
	unavailIDs, _ := q.ListUnavailableMediaItemIDs(ctx, mt)
	for _, id := range unavailIDs {
		unavailMap[id] = true
	}
	return unavailMap
}

// GetMediaImagePath resolves the local file path for a media item's image.
// Returns the path and true if found, or empty string and false otherwise.
func (a *App) GetMediaImagePath(ctx context.Context, mediaItemID int64, imageType string, sortOrder int, label string) (string, bool) {
	q := sqlc.New(a.db)

	assets, err := q.ListMediaAssets(ctx, mediaItemID)
	if err == nil && len(assets) > 0 {
		if label != "" {
			for _, asset := range assets {
				if asset.Label == label && asset.LocalPath != "" {
					return asset.LocalPath, true
				}
			}
		}
		if sortOrder >= 0 {
			for _, asset := range assets {
				if string(asset.AssetType) == imageType && int(asset.SortOrder) == sortOrder && asset.LocalPath != "" {
					return asset.LocalPath, true
				}
			}
		}
		for _, asset := range assets {
			if string(asset.AssetType) == imageType && asset.LocalPath != "" {
				return asset.LocalPath, true
			}
		}
	}

	if imageType == "poster" || imageType == "backdrop" {
		item, err := q.GetMediaItemByID(ctx, mediaItemID)
		if err != nil {
			return "", false
		}
		var imgPath string
		if imageType == "poster" {
			imgPath = item.PosterPath
		} else {
			imgPath = item.BackdropPath
		}
		if imgPath == "" || strings.HasPrefix(imgPath, "http") {
			return "", false
		}
		return imgPath, true
	}

	return "", false
}

// GetPersonImagePath resolves the local file path for a person's profile image.
func (a *App) GetPersonImagePath(ctx context.Context, personID int64) (string, bool) {
	q := sqlc.New(a.db)
	person, err := q.GetPersonByID(ctx, personID)
	if err != nil || person.ProfilePath == "" || strings.HasPrefix(person.ProfilePath, "http") {
		return "", false
	}
	return person.ProfilePath, true
}

// GetStudioLogoName resolves the production company name for logo lookup.
func (a *App) GetStudioLogoName(ctx context.Context, studioID int64) (string, bool) {
	q := sqlc.New(a.db)
	company, err := q.GetProductionCompanyByID(ctx, studioID)
	if err != nil {
		return "", false
	}
	return company.Name, true
}

// GetPerson fetches a person by ID or slug, along with cast and crew credits.
func (a *App) GetPerson(ctx context.Context, idOrSlug string) (map[string]any, error) {
	q := sqlc.New(a.db)

	var person sqlc.Person
	var err error
	if id, parseErr := strconv.ParseInt(idOrSlug, 10, 64); parseErr == nil {
		person, err = q.GetPersonByID(ctx, id)
	} else {
		person, err = q.GetPersonBySlug(ctx, idOrSlug)
	}
	if err != nil {
		return nil, fmt.Errorf("person not found: %w", err)
	}

	result := map[string]any{"person": person}

	if castCredits, castErr := q.ListPersonCastCredits(ctx, person.ID); castErr == nil && len(castCredits) > 0 {
		result["cast_credits"] = castCredits
	}

	if crewCredits, crewErr := q.ListPersonCrewCredits(ctx, person.ID); crewErr == nil && len(crewCredits) > 0 {
		result["crew_credits"] = crewCredits
	}

	if bios, err := q.ListPersonBiographies(ctx, person.ID); err == nil && len(bios) > 0 {
		result["biographies"] = bios
	}

	if profiles, err := q.ListPersonProfiles(ctx, person.ID); err == nil && len(profiles) > 0 {
		result["profiles"] = profiles
	}

	return result, nil
}

// ListUnmatched returns unmatched library files with their match candidates.
func (a *App) ListUnmatched(ctx context.Context, libraryID int64) ([]UnmatchedFile, error) {
	q := sqlc.New(a.db)

	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusUnmatched,
		Limit:     100,
		Offset:    0,
	})
	if err != nil {
		return nil, fmt.Errorf("listing unmatched files: %w", err)
	}

	var result []UnmatchedFile
	for _, f := range files {
		candidates, _ := q.ListMatchCandidatesByFile(ctx, f.ID)
		result = append(result, UnmatchedFile{File: f, Candidates: candidates})
	}

	return result, nil
}

// BuildEpisodeFileMap parses library file parse results to build a map
// from "s{season}e{episode}" keys to file entries.
func BuildEpisodeFileMap(files []sqlc.ListEpisodeFilesRow) map[string]EpisodeFileEntry {
	type parseResult struct {
		Parsed struct {
			Release struct {
				Seasons  []int `json:"seasons"`
				Episodes []int `json:"episodes"`
			} `json:"release"`
		} `json:"parsed"`
	}

	result := make(map[string]EpisodeFileEntry)
	for _, f := range files {
		if len(f.ParseResult) == 0 {
			continue
		}
		var pr parseResult
		if err := json.Unmarshal(f.ParseResult, &pr); err != nil {
			continue
		}
		for _, s := range pr.Parsed.Release.Seasons {
			for _, e := range pr.Parsed.Release.Episodes {
				key := fmt.Sprintf("s%de%d", s, e)
				result[key] = EpisodeFileEntry{FileID: f.ID, Size: f.Size}
			}
		}
	}
	return result
}
