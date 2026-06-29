package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// UpdateMediaMetadataReq holds the fields that can be patched on a media item.
type UpdateMediaMetadataReq struct {
	Title            *string           `json:"title"`
	SortTitle        *string           `json:"sort_title"`
	Year             *string           `json:"year"`
	Description      *string           `json:"description"`
	ExternalIDs      map[string]string `json:"external_ids"`
	Tagline          *string           `json:"tagline"`
	Genres           []string          `json:"genres"`
	ReleaseDate      *string           `json:"release_date"`
	OriginalTitle    *string           `json:"original_title"`
	OriginalLanguage *string           `json:"original_language"`
	RuntimeMinutes   *int32            `json:"runtime_minutes"`
	Status           *string           `json:"status"`
	FirstAirDate     *string           `json:"first_air_date"`
	LastAirDate      *string           `json:"last_air_date"`
	OriginalName     *string           `json:"original_name"`
}

// UpdateEpisodeReq holds the fields that can be patched on an episode.
type UpdateEpisodeReq struct {
	Title          *string `json:"title"`
	Overview       *string `json:"overview"`
	RuntimeMinutes *int32  `json:"runtime_minutes"`
	AirDate        *string `json:"air_date"`
}

// ListLibraryMedia returns media items belonging to a library with optional search.
func (a *App) ListLibraryMedia(ctx context.Context, libraryID int64, limit, offset int32, query string) ([]sqlc.MediaItem, error) {
	q := sqlc.New(a.db)
	return q.SearchMediaItemsByLibrary(ctx, sqlc.SearchMediaItemsByLibraryParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
		Column4:   query,
	})
}

// UpdateMediaMetadata patches a media item and its type-specific record.
func (a *App) UpdateMediaMetadata(ctx context.Context, mediaItemID int64, req UpdateMediaMetadataReq) error {
	return a.withTx(ctx, func(q *sqlc.Queries) error {

		item, err := q.GetMediaItemByID(ctx, mediaItemID)
		if err != nil {
			return fmt.Errorf("media item not found: %w", err)
		}

		title := item.Title
		if req.Title != nil {
			title = *req.Title
		}
		sortTitle := item.SortTitle
		if req.SortTitle != nil {
			sortTitle = *req.SortTitle
		}
		year := item.Year
		if req.Year != nil {
			year = *req.Year
		}
		desc := item.Description
		if req.Description != nil {
			desc = *req.Description
		}
		externalIDs := item.ExternalIds
		if req.ExternalIDs != nil {
			b, _ := json.Marshal(req.ExternalIDs)
			externalIDs = b
		}
		tagline := item.Tagline
		if req.Tagline != nil {
			tagline = *req.Tagline
		}
		origTitle := item.OriginalTitle
		if req.OriginalTitle != nil {
			origTitle = *req.OriginalTitle
		}
		origLang := item.OriginalLanguage
		if req.OriginalLanguage != nil {
			origLang = *req.OriginalLanguage
		}
		status := item.Status
		if req.Status != nil {
			status = *req.Status
		}

		_, err = q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
			ID:               mediaItemID,
			Title:            title,
			SortTitle:        sortTitle,
			Year:             year,
			Description:      desc,
			PosterPath:       item.PosterPath,
			BackdropPath:     item.BackdropPath,
			ExternalIds:      externalIDs,
			Tagline:          tagline,
			OriginalTitle:    origTitle,
			OriginalLanguage: origLang,
			Status:           status,
			ProviderKind:     item.ProviderKind,
			HeyaSlug:         item.HeyaSlug,
		})
		if err != nil {
			return fmt.Errorf("updating media item: %w", err)
		}

		switch item.MediaType {
		case sqlc.MediaTypeMovie:
			movie, mErr := q.GetMovieByMediaItemID(ctx, mediaItemID)
			if mErr == nil {
				tagline := movie.Tagline
				if req.Tagline != nil {
					tagline = *req.Tagline
				}
				genres := movie.Genres
				if req.Genres != nil {
					genres = req.Genres
				}
				releaseDate := movie.ReleaseDate
				if req.ReleaseDate != nil {
					releaseDate = pgDateFromStr(*req.ReleaseDate)
				}
				origTitle := movie.OriginalTitle
				if req.OriginalTitle != nil {
					origTitle = *req.OriginalTitle
				}
				origLang := movie.OriginalLanguage
				if req.OriginalLanguage != nil {
					origLang = *req.OriginalLanguage
				}
				runtime := movie.RuntimeMinutes
				if req.RuntimeMinutes != nil {
					runtime = *req.RuntimeMinutes
				}
				if _, err := q.UpdateMovie(ctx, sqlc.UpdateMovieParams{
					ID:               movie.ID,
					RuntimeMinutes:   runtime,
					Tagline:          tagline,
					Genres:           genres,
					Rating:           movie.Rating,
					ReleaseDate:      releaseDate,
					OriginalTitle:    origTitle,
					OriginalLanguage: origLang,
					Budget:           movie.Budget,
					Revenue:          movie.Revenue,
					Popularity:       movie.Popularity,
					SpokenLanguages:  movie.SpokenLanguages,
					OriginCountry:    movie.OriginCountry,
				}); err != nil {
					return fmt.Errorf("updating movie metadata: %w", err)
				}
			}
		case sqlc.MediaTypeTv:
			series, sErr := q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
			if sErr == nil {
				status := series.Status
				if req.Status != nil {
					status = *req.Status
				}
				genres := series.Genres
				if req.Genres != nil {
					genres = req.Genres
				}
				firstAir := series.FirstAirDate
				if req.FirstAirDate != nil {
					firstAir = pgDateFromStr(*req.FirstAirDate)
				}
				lastAir := series.LastAirDate
				if req.LastAirDate != nil {
					lastAir = pgDateFromStr(*req.LastAirDate)
				}
				origName := series.OriginalName
				if req.OriginalName != nil {
					origName = *req.OriginalName
				}
				origLang := series.OriginalLanguage
				if req.OriginalLanguage != nil {
					origLang = *req.OriginalLanguage
				}
				if _, err := q.UpdateTVSeries(ctx, sqlc.UpdateTVSeriesParams{
					ID:               series.ID,
					Status:           status,
					Genres:           genres,
					Rating:           series.Rating,
					FirstAirDate:     firstAir,
					LastAirDate:      lastAir,
					OriginalName:     origName,
					OriginalLanguage: origLang,
					NumberOfSeasons:  series.NumberOfSeasons,
					NumberOfEpisodes: series.NumberOfEpisodes,
					Popularity:       series.Popularity,
					SpokenLanguages:  series.SpokenLanguages,
					OriginCountry:    series.OriginCountry,
				}); err != nil {
					return fmt.Errorf("updating tv metadata: %w", err)
				}
			}
		}

		return nil
	})
}

// UpdateEpisode patches a TV episode record.
func (a *App) UpdateEpisode(ctx context.Context, episodeID int64, req UpdateEpisodeReq) (sqlc.TvEpisode, error) {
	q := sqlc.New(a.db)

	ep, err := q.GetTVEpisodeByID(ctx, episodeID)
	if err != nil {
		return sqlc.TvEpisode{}, fmt.Errorf("episode not found: %w", err)
	}

	title := ep.Title
	if req.Title != nil {
		title = *req.Title
	}
	overview := ep.Overview
	if req.Overview != nil {
		overview = *req.Overview
	}
	runtime := ep.RuntimeMinutes
	if req.RuntimeMinutes != nil {
		runtime = *req.RuntimeMinutes
	}
	airDate := ep.AirDate
	if req.AirDate != nil {
		airDate = pgDateFromStr(*req.AirDate)
	}

	return q.UpdateTVEpisode(ctx, sqlc.UpdateTVEpisodeParams{
		ID:             episodeID,
		Title:          title,
		Overview:       overview,
		StillPath:      ep.StillPath,
		RuntimeMinutes: runtime,
		AirDate:        airDate,
		Rating:         ep.Rating,
		AbsoluteNumber: ep.AbsoluteNumber,
		IsSpecial:      ep.IsSpecial,
		EpisodeType:    ep.EpisodeType,
		ExternalIds:    ep.ExternalIds,
		Source:         ep.Source,
	})
}

// IdentifySearchResult wraps the search results from all providers.
type IdentifySearchResult struct {
	Results []metadata.SearchResult `json:"results"`
}

// IdentifySearch queries metadata providers for potential matches.
func (a *App) IdentifySearch(ctx context.Context, mediaItemID int64, query, year string, kind metadata.MediaKind) (IdentifySearchResult, error) {
	q := sqlc.New(a.db)

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return IdentifySearchResult{}, fmt.Errorf("media item not found: %w", err)
	}

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return IdentifySearchResult{}, fmt.Errorf("library not found: %w", err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	if query == "" {
		query = item.Title
	}

	if kind == "" {
		switch item.MediaType {
		case sqlc.MediaTypeMovie:
			kind = metadata.KindMovie
		case sqlc.MediaTypeTv:
			kind = metadata.KindTV
		case sqlc.MediaTypeMusic:
			kind = metadata.KindMusic
		case sqlc.MediaTypeBook:
			kind = metadata.KindBook
		}
	}

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	// If the query looks like a URL/shortcode that points to a specific provider
	// item, resolve it directly to a single result instead of doing a text search.
	if providerName, providerID, ok := parseIdentifyURL(query, kind); ok {
		if res, err := a.resolveIdentifyURL(ctx, providerName, providerID, fetchOpts); err == nil {
			return IdentifySearchResult{Results: []metadata.SearchResult{res}}, nil
		} else {
			log.Debug().Err(err).Str("provider", providerName).Str("provider_id", providerID).Msg("identify URL lookup failed")
		}
	}

	sq := metadata.SearchQuery{
		Title:    query,
		Year:     year,
		Language: settings.PreferredLanguage,
		Country:  settings.PreferredCountry,
	}

	results, err := a.heya.Search(ctx, kind, sq)
	if err != nil {
		log.Debug().Err(err).Msg("identify search failed")
		results = nil
	}

	return IdentifySearchResult{Results: results}, nil
}

// resolveIdentifyURL fetches a single search-result-shaped item using the
// heya provider. Used when the user pastes a direct URL into the identify dialog.
func (a *App) resolveIdentifyURL(ctx context.Context, providerName, providerID string, opts *metadata.FetchOptions) (metadata.SearchResult, error) {
	if providerName != "heya" {
		return metadata.SearchResult{}, fmt.Errorf("unknown provider: %s", providerName)
	}
	detail, err := a.heya.GetDetail(ctx, providerID, opts)
	if err != nil {
		return metadata.SearchResult{}, err
	}
	return metadata.SearchResult{
		ProviderID:   providerID,
		ProviderName: providerName,
		Title:        detail.Title,
		Year:         detail.Year,
		Description:  detail.Description,
		PosterURL:    detail.PosterURL,
		Confidence:   1.0,
	}, nil
}

// parseIdentifyURL inspects a user-pasted string and returns the heya provider
// ID it refers to, if recognized. The hint kind is used when the input itself
// doesn't disambiguate movie vs. tv (e.g. IMDb URLs).
//
// Supported inputs:
//   - heya.media shortcodes/URLs: heya_<kind>:<provider>:<value>   (kind from prefix)
//   - heya providerID passthrough: heya:<kind>:<provider>:<value>  (used as-is)
//   - TMDB URLs:                   https://www.themoviedb.org/{movie,tv}/<id>[-<name>]
//   - IMDb URLs:                   https://www.imdb.com/title/tt<id>/  (uses hint kind)
//   - TheTVDB URLs:                https://thetvdb.com/{series,movies}/<id>
func parseIdentifyURL(input string, hint metadata.MediaKind) (provider, providerID string, ok bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", false
	}

	hintAPI := heyaAPIKind(hint)

	var host string
	var pathSegments []string

	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", "", false
		}
		host = strings.ToLower(u.Host)
		pathSegments = splitPath(u.Path)
	} else {
		// Treat the whole token as a single path segment (for shortcode inputs).
		pathSegments = []string{s}
	}

	for _, seg := range pathSegments {
		// heya.media URL/shortcode: heya_<kind>:<provider>:<value>
		if strings.HasPrefix(seg, "heya_") {
			rest := strings.TrimPrefix(seg, "heya_")
			parts := strings.SplitN(rest, ":", 3)
			if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
				return "heya", "heya:" + parts[0] + ":" + parts[1] + ":" + parts[2], true
			}
		}
		// Pre-built heya provider ID: heya:<kind>:<provider>:<value>
		if strings.HasPrefix(seg, "heya:") {
			rest := strings.TrimPrefix(seg, "heya:")
			parts := strings.SplitN(rest, ":", 3)
			if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
				return "heya", seg, true
			}
		}
	}

	if strings.Contains(host, "themoviedb.org") && len(pathSegments) >= 2 {
		tmdbKind := pathSegments[0]
		if tmdbKind == "tv" || tmdbKind == "movie" {
			idPart := pathSegments[1]
			if dash := strings.Index(idPart, "-"); dash > 0 {
				idPart = idPart[:dash]
			}
			if _, err := strconv.Atoi(idPart); err == nil {
				return "heya", "heya:" + tmdbKind + ":tmdb:" + idPart, true
			}
		}
	}

	if strings.Contains(host, "imdb.com") && hintAPI != "" {
		for i, seg := range pathSegments {
			if seg == "title" && i+1 < len(pathSegments) {
				ttID := pathSegments[i+1]
				if strings.HasPrefix(ttID, "tt") {
					return "heya", "heya:" + hintAPI + ":imdb:" + ttID, true
				}
			}
		}
	}

	if strings.Contains(host, "thetvdb.com") {
		for i, seg := range pathSegments {
			if i+1 >= len(pathSegments) {
				continue
			}
			idPart := pathSegments[i+1]
			n, err := strconv.Atoi(idPart)
			if err != nil || n <= 0 {
				continue
			}
			switch seg {
			case "series":
				return "heya", "heya:tv:tvdb:" + idPart, true
			case "movies":
				return "heya", "heya:movie:tvdb:" + idPart, true
			}
		}
	}

	return "", "", false
}

// heyaAPIKind maps an internal MediaKind to the api kind segment heya.media
// uses in /{kind}/{id} (music → artist, others pass through).
func heyaAPIKind(k metadata.MediaKind) string {
	switch k {
	case metadata.KindMovie:
		return "movie"
	case metadata.KindTV:
		return "tv"
	case metadata.KindMusic:
		return "artist"
	case metadata.KindBook:
		return "book"
	}
	return ""
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// ApplyIdentify fetches metadata from a specific provider and applies it to the media item.
func (a *App) ApplyIdentify(ctx context.Context, mediaItemID int64, providerName, providerID string) error {
	q := sqlc.New(a.db)

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("media item not found: %w", err)
	}

	if providerName != "heya" {
		return fmt.Errorf("unknown provider: %s", providerName)
	}

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return fmt.Errorf("library not found: %w", err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	detail, err := a.heya.GetDetail(ctx, providerID, fetchOpts)
	if err != nil {
		return fmt.Errorf("metadata fetch failed: %w", err)
	}

	externalIDs, _ := json.Marshal(detail.ExternalIDs)
	q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
		ID:               mediaItemID,
		Title:            detail.Title,
		SortTitle:        strings.ToLower(detail.Title),
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       item.PosterPath,
		BackdropPath:     item.BackdropPath,
		ExternalIds:      externalIDs,
		Tagline:          item.Tagline,
		OriginalTitle:    item.OriginalTitle,
		OriginalLanguage: item.OriginalLanguage,
		Status:           item.Status,
		ProviderKind:     item.ProviderKind,
		HeyaSlug:         item.HeyaSlug,
	})

	q.DeleteMediaCastByItem(ctx, mediaItemID)
	q.DeleteMediaCrewByItem(ctx, mediaItemID)
	q.DeleteMediaKeywordsByItem(ctx, mediaItemID)
	q.DeleteMediaVideosByItem(ctx, mediaItemID)
	q.DeleteMediaCertificationsByItem(ctx, mediaItemID)
	q.DeleteMediaRecommendationsByItem(ctx, mediaItemID)
	q.DeleteMediaProductionCompaniesByItem(ctx, mediaItemID)

	var kind metadata.MediaKind
	switch item.MediaType {
	case sqlc.MediaTypeMovie:
		kind = metadata.KindMovie
	case sqlc.MediaTypeTv:
		kind = metadata.KindTV
	default:
		kind = metadata.MediaKind(item.MediaType)
	}

	a.matcher.StoreEntityMetadata(ctx, mediaItemID, kind, detail)
	a.matcher.StoreRichMetadata(ctx, mediaItemID, detail)

	tx, txErr := a.db.Begin(ctx)
	if txErr == nil {
		_, _ = a.river.InsertTx(ctx, tx, worker.FetchArtworkArgs{
			MediaItemID: mediaItemID,
			MediaType:   string(item.MediaType),
		}, nil)
		_ = tx.Commit(ctx)
	}

	return nil
}

// DeleteMediaAsset removes a media asset and its file from disk, updating poster/backdrop if needed.
func (a *App) DeleteMediaAsset(ctx context.Context, mediaItemID, assetID int64) error {
	q := sqlc.New(a.db)

	asset, err := q.GetMediaAssetByID(ctx, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %w", err)
	}
	if asset.MediaItemID != mediaItemID {
		return fmt.Errorf("asset does not belong to this media item")
	}

	if asset.LocalPath != "" {
		fullPath := filepath.Join(a.config.DataDir.Value, "images", asset.LocalPath)
		os.Remove(fullPath)
	}

	q.DeleteMediaAsset(ctx, assetID)

	if asset.SortOrder == 0 {
		assetType := string(asset.AssetType)
		if assetType == "poster" || assetType == "backdrop" {
			remaining, _ := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
				MediaItemID: mediaItemID,
				AssetType:   asset.AssetType,
			})
			newPath := ""
			if len(remaining) > 0 {
				newPath = remaining[0].LocalPath
			}
			if assetType == "poster" {
				q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{ID: mediaItemID, PosterPath: newPath})
			} else {
				q.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{ID: mediaItemID, BackdropPath: newPath})
			}
		}
	}

	return nil
}

// SetPrimaryAsset promotes a media asset to the primary position for its type.
func (a *App) SetPrimaryAsset(ctx context.Context, mediaItemID, assetID int64) error {
	q := sqlc.New(a.db)

	asset, err := q.GetMediaAssetByID(ctx, assetID)
	if err != nil {
		return fmt.Errorf("asset not found: %w", err)
	}
	if asset.MediaItemID != mediaItemID {
		return fmt.Errorf("asset does not belong to this media item")
	}

	q.ShiftAssetSortOrders(ctx, sqlc.ShiftAssetSortOrdersParams{
		MediaItemID: mediaItemID,
		Column2:     asset.AssetType,
	})
	q.SetAssetSortOrder(ctx, sqlc.SetAssetSortOrderParams{ID: assetID, SortOrder: 0})

	assetType := string(asset.AssetType)
	if assetType == "poster" {
		q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{ID: mediaItemID, PosterPath: asset.LocalPath})
	} else if assetType == "backdrop" {
		q.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{ID: mediaItemID, BackdropPath: asset.LocalPath})
	}

	return nil
}

// SearchProviderArtwork queries artwork providers for available images.
func (a *App) SearchProviderArtwork(ctx context.Context, mediaItemID int64, filterType, filterProvider string) ([]metadata.ArtworkResult, error) {
	q := sqlc.New(a.db)

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("media item not found: %w", err)
	}

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		externalIDs = map[string]string{}
	}

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return nil, fmt.Errorf("library not found: %w", err)
	}
	settings := metadata.ParseSettings(lib.Settings)

	var kind metadata.MediaKind
	switch item.MediaType {
	case sqlc.MediaTypeMovie:
		kind = metadata.KindMovie
	case sqlc.MediaTypeTv:
		kind = metadata.KindTV
	default:
		return []metadata.ArtworkResult{}, nil
	}

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	if filterProvider != "" && filterProvider != "heya" {
		return nil, nil
	}

	results, err := a.heya.FetchArtwork(ctx, kind, externalIDs, fetchOpts)
	if err != nil {
		return nil, nil
	}

	if filterType == "" {
		return results, nil
	}
	filtered := results[:0]
	for _, art := range results {
		if art.AssetType == filterType {
			filtered = append(filtered, art)
		}
	}
	return filtered, nil
}

// DownloadAsset queues a background job to download an image asset from a URL.
func (a *App) DownloadAsset(ctx context.Context, mediaItemID int64, url, assetType string) error {
	q := sqlc.New(a.db)

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("media item not found: %w", err)
	}

	sortOrder := 0
	if !worker.SingleAssetTypes[assetType] {
		assetCount, _ := q.CountMediaAssetsByType(ctx, mediaItemID)
		sortOrder = 10
		for _, c := range assetCount {
			if string(c.AssetType) == assetType {
				sortOrder = int(c.Count) + 10
			}
		}
	}

	tx, txErr := a.db.Begin(ctx)
	if txErr != nil {
		return fmt.Errorf("begin tx: %w", txErr)
	}
	_, _ = a.river.InsertTx(ctx, tx, worker.DownloadImageArgs{
		MediaItemID: mediaItemID,
		URL:         url,
		AssetType:   assetType,
		MediaType:   string(item.MediaType),
		SortOrder:   sortOrder,
	}, nil)
	_ = tx.Commit(ctx)

	return nil
}

// UploadMediaAssetResult holds the result of an upload operation.
type UploadMediaAssetResult struct {
	Asset *sqlc.MediaAsset `json:"asset,omitempty"`
	Path  string           `json:"path,omitempty"`
}

// UploadMediaAsset saves an uploaded file to disk and creates a media asset record.
func (a *App) UploadMediaAsset(ctx context.Context, mediaItemID int64, file io.Reader, filename, assetType string) (UploadMediaAssetResult, error) {
	q := sqlc.New(a.db)

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return UploadMediaAssetResult{}, fmt.Errorf("media item not found: %w", err)
	}

	dirName := fmt.Sprintf("%d", mediaItemID)
	if item.Slug != "" {
		dirName = item.Slug
	}

	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".jpg"
	}
	destFilename := fmt.Sprintf("custom_%s%s", assetType, ext)

	dirPath := filepath.Join(a.config.DataDir.Value, "images", string(item.MediaType), dirName)
	os.MkdirAll(dirPath, 0755)

	dst, err := os.Create(filepath.Join(dirPath, destFilename))
	if err != nil {
		return UploadMediaAssetResult{}, fmt.Errorf("failed to save file: %w", err)
	}
	defer dst.Close()

	size, err := io.Copy(dst, file)
	if err != nil {
		return UploadMediaAssetResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	localPath := filepath.Join(string(item.MediaType), dirName, destFilename)

	asset, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: mediaItemID,
		AssetType:   sqlc.AssetType(assetType),
		Source:      "custom",
		LocalPath:   localPath,
		RemoteUrl:   "",
		Label:       "custom",
		SortOrder:   100,
		FileSize:    size,
	})
	if err != nil {
		// Asset creation failed but the file was saved.
		return UploadMediaAssetResult{Path: localPath}, nil
	}

	return UploadMediaAssetResult{Asset: &asset}, nil
}

// pgDateFromStr parses a date string into a pgtype.Date.
func pgDateFromStr(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	var d pgtype.Date
	d.Scan(s)
	return d
}
