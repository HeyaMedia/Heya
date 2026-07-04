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
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
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
// Each item's Title is overlaid with the localized variant matching its
// library's PreferredLanguage when one is configured — so the rails on the
// home page and library views show e.g. "Oshi No Ko" instead of the raw
// canonical title when the library is set to English. Falls back to en,
// then to the raw title.
func (a *App) ListMedia(ctx context.Context, mediaType sqlc.MediaType, limit, offset int32) ([]MediaItemView, error) {
	return a.listMedia(ctx, mediaType, limit, offset, false)
}

// ListMediaRecent is ListMedia ordered newest-first (by created_at) — the
// home "Recently Added" rails. The default alphabetical order only *looked*
// recent while whole libraries arrived in one import burst.
func (a *App) ListMediaRecent(ctx context.Context, mediaType sqlc.MediaType, limit, offset int32) ([]MediaItemView, error) {
	return a.listMedia(ctx, mediaType, limit, offset, true)
}

func (a *App) listMedia(ctx context.Context, mediaType sqlc.MediaType, limit, offset int32, recentFirst bool) ([]MediaItemView, error) {
	q := sqlc.New(a.db)

	var items []sqlc.MediaItem
	var err error
	if recentFirst {
		items, err = q.ListMediaItemsByTypeRecent(ctx, sqlc.ListMediaItemsByTypeRecentParams{
			MediaType: mediaType,
			Limit:     limit,
			Offset:    offset,
		})
	} else {
		items, err = q.ListMediaItemsByType(ctx, sqlc.ListMediaItemsByTypeParams{
			MediaType: mediaType,
			Limit:     limit,
			Offset:    offset,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("listing media items: %w", err)
	}

	unavailableIDs, _ := q.ListUnavailableMediaItemIDs(ctx, mediaType)
	unavailable := make(map[int64]bool, len(unavailableIDs))
	for _, id := range unavailableIDs {
		unavailable[id] = true
	}

	overlay := a.preferredTitleOverlay(ctx, q, items)
	views := make([]MediaItemView, len(items))
	for i, item := range items {
		if t := overlay[item.ID]; t != "" {
			item.Title = t
		}
		views[i] = MediaItemView{
			MediaItem: item,
			Available: !unavailable[item.ID],
		}
	}

	return views, nil
}

// preferredTitleOverlay is the batched form of preferredTitleResolver for
// list pages: two queries per distinct preferred language (wanted language +
// an 'en' fallback for the misses) instead of one per item — the home rails
// paid ~60 sequential round trips per load through the per-item resolver.
// Returns mediaItemID → overlay title; absent keys mean "keep the raw title".
func (a *App) preferredTitleOverlay(ctx context.Context, q *sqlc.Queries, items []sqlc.MediaItem) map[int64]string {
	libLang := map[int64]string{}
	idsByLang := map[string][]int64{}
	for _, it := range items {
		lang, cached := libLang[it.LibraryID]
		if !cached {
			if lib, err := q.GetLibraryByID(ctx, it.LibraryID); err == nil {
				lang = metadata.ParseSettings(lib.Settings).PreferredLanguage
			}
			libLang[it.LibraryID] = lang
		}
		if lang != "" {
			idsByLang[lang] = append(idsByLang[lang], it.ID)
		}
	}

	out := map[int64]string{}
	for lang, ids := range idsByLang {
		if rows, err := q.GetMediaTitlesByLanguageBatch(ctx, sqlc.GetMediaTitlesByLanguageBatchParams{MediaItemIds: ids, Language: lang}); err == nil {
			for _, r := range rows {
				if r.Title != "" {
					out[r.MediaItemID] = r.Title
				}
			}
		}
		if lang == "en" {
			continue
		}
		var missed []int64
		for _, id := range ids {
			if _, ok := out[id]; !ok {
				missed = append(missed, id)
			}
		}
		if len(missed) == 0 {
			continue
		}
		if rows, err := q.GetMediaTitlesByLanguageBatch(ctx, sqlc.GetMediaTitlesByLanguageBatchParams{MediaItemIds: missed, Language: "en"}); err == nil {
			for _, r := range rows {
				if r.Title != "" {
					out[r.MediaItemID] = r.Title
				}
			}
		}
	}
	return out
}

// preferredTitleResolver returns a closure that overlays the library's
// PreferredLanguage title on a (mediaItemID, libraryID) pair, falling back
// to English and then the supplied raw title. Library settings are cached
// for the closure's lifetime so a batch of items (a list page, a rail) only
// hits the libraries table once per distinct library.
func (a *App) preferredTitleResolver(ctx context.Context, q *sqlc.Queries) func(mediaItemID, libraryID int64, fallback string) string {
	libLang := map[int64]string{}
	return func(mediaItemID, libraryID int64, fallback string) string {
		lang, cached := libLang[libraryID]
		if !cached {
			if lib, err := q.GetLibraryByID(ctx, libraryID); err == nil {
				lang = metadata.ParseSettings(lib.Settings).PreferredLanguage
			}
			libLang[libraryID] = lang
		}
		if lang == "" {
			return fallback
		}
		if t, err := q.GetMediaTitleByLanguage(ctx, sqlc.GetMediaTitleByLanguageParams{MediaItemID: mediaItemID, Language: lang}); err == nil && t.Title != "" {
			return t.Title
		}
		if lang != "en" {
			if t, err := q.GetMediaTitleByLanguage(ctx, sqlc.GetMediaTitleByLanguageParams{MediaItemID: mediaItemID, Language: "en"}); err == nil && t.Title != "" {
				return t.Title
			}
		}
		return fallback
	}
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

	// View-promotion: a user is looking at this item right now. If it's
	// not fully enriched yet, jump the queue at priority 1 ahead of any
	// in-flight background enrich. The worker's idempotency gate keeps
	// duplicate enqueues cheap (the second one no-ops fast).
	if item.EnrichmentStatus != "complete" && a.river != nil {
		if err := worker.EnqueueEnrich(ctx, a.river, item.ID, item.MediaType, worker.EnrichSourceView); err != nil {
			log.Debug().Err(err).Int64("item_id", item.ID).Msg("view-promotion enqueue failed")
		}
	}

	// Narrow query on purpose: the response renders only id+size, and the
	// full rows detoast media_info/parse_result jsonb for every file (~30MB
	// and ~750ms for a large music artist).
	hasFiles := false
	var mediaFiles []map[string]any
	if files, filesErr := q.ListLibraryFileSizesByMediaItem(ctx, pgtype.Int8{Int64: item.ID, Valid: true}); filesErr == nil && len(files) > 0 {
		hasFiles = true
		for _, f := range files {
			mediaFiles = append(mediaFiles, map[string]any{
				"id":   f.ID,
				"size": f.Size,
			})
		}
	}

	result := map[string]any{"media_item": item, "available": hasFiles, "files": mediaFiles}

	// TV episode files are consumed twice (available-seasons derivation in
	// the switch below + the episode_files map at the end) — fetch once, the
	// rows carry ~2MB of parse_result jsonb on a long-running series.
	var tvEpisodeFiles []sqlc.ListEpisodeFilesRow

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
				tvEpisodeFiles = epFiles
				availableSeasons = BuildAvailableSeasonSet(epFiles)
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

			// Three whole-series queries instead of one per season plus 2-4
			// per episode — the old shape was ~4000 queries on a
			// 1000-episode series. Preferred-language resolution happens
			// in-memory off the maps.
			allEps, _ := q.ListTVEpisodesBySeries(ctx, series.ID)
			epsBySeason := map[int64][]sqlc.TvEpisode{}
			for _, ep := range allEps {
				epsBySeason[ep.SeasonID] = append(epsBySeason[ep.SeasonID], ep)
			}

			titleByEp := map[int64]map[string]string{}
			overviewByEp := map[int64]map[string]string{}
			if prefLang != "" {
				langs := []string{prefLang}
				if prefLang != "en" {
					langs = append(langs, "en")
				}
				if titles, err := q.ListEpisodeTitlesForSeries(ctx, sqlc.ListEpisodeTitlesForSeriesParams{SeriesID: series.ID, Languages: langs}); err == nil {
					for _, t := range titles {
						if titleByEp[t.EpisodeID] == nil {
							titleByEp[t.EpisodeID] = map[string]string{}
						}
						titleByEp[t.EpisodeID][t.Language] = t.Title
					}
				}
				if overviews, err := q.ListEpisodeOverviewsForSeries(ctx, sqlc.ListEpisodeOverviewsForSeriesParams{SeriesID: series.ID, Languages: langs}); err == nil {
					for _, o := range overviews {
						if overviewByEp[o.EpisodeID] == nil {
							overviewByEp[o.EpisodeID] = map[string]string{}
						}
						overviewByEp[o.EpisodeID][o.Language] = o.Overview
					}
				}
			}
			pick := func(m map[int64]map[string]string, epID int64) string {
				byLang := m[epID]
				if v := byLang[prefLang]; v != "" {
					return v
				}
				return byLang["en"]
			}

			var enriched []seasonWithEpisodes
			for _, s := range seasons {
				if len(availableSeasons) > 0 && !availableSeasons[int(s.SeasonNumber)] {
					continue
				}
				var views []episodeView
				for _, ep := range epsBySeason[s.ID] {
					ev := episodeView{TvEpisode: ep}
					if prefLang != "" {
						ev.PreferredTitle = pick(titleByEp, ep.ID)
						ev.PreferredOverview = pick(overviewByEp, ep.ID)
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
			result["artist"] = BuildArtistView(artist)
			result["albums"] = buildAlbumViews(ctx, q, artist.ID)
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

	// Episode file map (TV only) — reuses the fetch from the TV branch above.
	if item.MediaType == sqlc.MediaTypeTv && len(tvEpisodeFiles) > 0 {
		episodeFileMap := BuildEpisodeFileMap(tvEpisodeFiles)
		if len(episodeFileMap) > 0 {
			result["episode_files"] = episodeFileMap
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

// formatTS renders a timestamptz in the enriched-list API form
// ("2006-01-02T15:04:05Z"), or "" when NULL.
func formatTS(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.Format("2006-01-02T15:04:05Z")
}

// formatDate renders a date column as "2006-01-02", or "" when NULL.
func formatDate(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

// ratingFloat converts a numeric rating to float64, or 0 when NULL/invalid.
func ratingFloat(r pgtype.Numeric) float64 {
	if f, err := r.Float64Value(); err == nil && f.Valid {
		return f.Float64
	}
	return 0
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
			CreatedAt:        formatTS(m.CreatedAt),
			UpdatedAt:        formatTS(m.UpdatedAt),
			Rating:           ratingFloat(m.Rating),
			ReleaseDate:      formatDate(m.ReleaseDate),
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
			CreatedAt:        formatTS(s.CreatedAt),
			UpdatedAt:        formatTS(s.UpdatedAt),
			Rating:           ratingFloat(s.Rating),
			FirstAirDate:     formatDate(s.FirstAirDate),
			LastAirDate:      formatDate(s.LastAirDate),
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

// GetAlbumCover returns the album's cover, distinguishing local files from
// upstream URLs so the HTTP handler can decide between serving bytes
// directly or 302'ing the client. The third return is true when `path` is
// an external URL (heya.media / Deezer / etc.) and false when it's a local
// file path the handler should open + stream.
func (a *App) GetAlbumCover(ctx context.Context, albumID int64) (path string, remote bool, ok bool) {
	q := sqlc.New(a.db)
	album, err := q.GetAlbumByID(ctx, albumID)
	if err != nil || album.CoverPath == "" {
		return "", false, false
	}
	if strings.HasPrefix(album.CoverPath, "http://") || strings.HasPrefix(album.CoverPath, "https://") {
		return album.CoverPath, true, true
	}
	return album.CoverPath, false, true
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

	// External credits (cast/crew/known-for from the upstream metadata
	// aggregator). Split by `kind` so the FE doesn't have to filter, and
	// drop rows where the local library already has the title — those are
	// already represented in cast_credits/crew_credits above and would
	// otherwise show as duplicates in the "Known For" tab. The
	// MatchedMediaItemID column comes from a LEFT JOIN; sqlc infers it
	// as int64 (zero-on-miss) since the SELECT can't see the join is
	// outer, so we check `!= 0` rather than a typed `.Valid`.
	ext, _ := q.ListPersonExternalCredits(ctx, person.ID)

	// Backfill kicker: if we have no external credits AND we have a
	// tmdb_id, queue a PersonFetch in the background. The worker's own
	// short-circuit logic (skip when external creds exist) keeps this
	// from looping — once the worker fills the rows, future visits stop
	// re-enqueueing.
	if len(ext) == 0 && a.river != nil {
		if tmdbID := personTmdbID(person.ExternalIds); tmdbID > 0 {
			_, _ = a.river.Insert(ctx, worker.PersonFetchArgs{PersonID: person.ID, TmdbID: int32(tmdbID)}, nil)
		}
	}

	if len(ext) > 0 {
		var extCast, extCrew, extKnownFor []sqlc.ListPersonExternalCreditsRow
		for _, r := range ext {
			if r.MatchedMediaItemID != 0 {
				continue
			}
			switch r.Kind {
			case "cast":
				extCast = append(extCast, r)
			case "crew":
				extCrew = append(extCrew, r)
			case "known_for":
				extKnownFor = append(extKnownFor, r)
			}
		}
		if len(extCast) > 0 {
			result["external_cast"] = extCast
		}
		if len(extCrew) > 0 {
			result["external_crew"] = extCrew
		}
		if len(extKnownFor) > 0 {
			result["external_known_for"] = extKnownFor
		}
	}

	return result, nil
}

// personTmdbID pulls the upstream TMDB id out of the `people.external_ids`
// JSONB blob. Stored either as a numeric or a string depending on which
// path wrote it; tolerate both. Returns 0 when missing or unparseable.
func personTmdbID(extIDs []byte) int {
	if len(extIDs) == 0 {
		return 0
	}
	var m map[string]any
	if err := json.Unmarshal(extIDs, &m); err != nil {
		return 0
	}
	switch v := m["tmdb"].(type) {
	case string:
		n, _ := strconv.Atoi(v)
		return n
	case float64:
		return int(v)
	}
	return 0
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

// TrackView wraps a track with its physical files, ordered best-first.
type TrackView struct {
	sqlc.Track
	Files []sqlc.TrackFile `json:"files"`
}

// AlbumView wraps an album with its enriched tracks.
type AlbumView struct {
	sqlc.Album
	Tracks []TrackView `json:"tracks"`
}

// buildAlbumViews loads albums for an artist with each album's tracks and
// each track's available files. Files are ordered best-first by the query.
func buildAlbumViews(ctx context.Context, q *sqlc.Queries, artistID int64) []AlbumView {
	albums, err := q.ListAlbumsByArtist(ctx, artistID)
	if err != nil {
		return nil
	}

	// Three whole-artist queries instead of one per album plus one per track
	// (a 50-album / 1000-track artist was ~1050 queries). Both batches come
	// back pre-ordered (tracks by disc/number, files quality-desc within each
	// track), so grouping by id preserves the per-album/per-track order.
	tracks, _ := q.ListTracksByArtist(ctx, artistID)
	files, _ := q.ListTrackFilesByArtist(ctx, artistID)

	filesByTrack := make(map[int64][]sqlc.TrackFile, len(tracks))
	for _, f := range files {
		filesByTrack[f.TrackID] = append(filesByTrack[f.TrackID], f)
	}
	tracksByAlbum := make(map[int64][]TrackView, len(albums))
	for _, t := range tracks {
		files := filesByTrack[t.ID]
		if files == nil {
			files = []sqlc.TrackFile{} // keep `files: []` (not null) for fileless tracks
		}
		tracksByAlbum[t.AlbumID] = append(tracksByAlbum[t.AlbumID], TrackView{Track: t, Files: files})
	}

	views := make([]AlbumView, 0, len(albums))
	for _, album := range albums {
		tv := tracksByAlbum[album.ID]
		if tv == nil {
			tv = []TrackView{} // keep `tracks: []` (not null) for trackless albums
		}
		views = append(views, AlbumView{Album: album, Tracks: tv})
	}
	return views
}

// BuildAvailableSeasonSet parses library file parse results into the set of
// season numbers we hold at least one file for. This is the season-level
// visibility gate: GetMediaDetail hides seasons outside the set (when
// non-empty), and bulk watch actions must skip the same seasons so hidden
// catalog episodes never get marked. Coarser than BuildEpisodeFileMap on
// purpose — a season pack parsed without per-episode numbers still claims
// its season here.
func BuildAvailableSeasonSet(files []sqlc.ListEpisodeFilesRow) map[int]bool {
	set := map[int]bool{}
	for _, f := range files {
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
				set[s] = true
			}
		}
	}
	return set
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
