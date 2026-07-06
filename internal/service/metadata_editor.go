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
	"github.com/karbowiak/heya/internal/eventhub"
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
	// Music-only (artist row). Title doubles as the artist name.
	SortName       *string `json:"sort_name"`
	Disambiguation *string `json:"disambiguation"`
	Biography      *string `json:"biography"`
}

// UpdateEpisodeReq holds the fields that can be patched on an episode.
type UpdateEpisodeReq struct {
	Title          *string `json:"title"`
	Overview       *string `json:"overview"`
	RuntimeMinutes *int32  `json:"runtime_minutes"`
	AirDate        *string `json:"air_date"`
}

// updateMediaItemParamsFrom spells every UpdateMediaItem field from the
// current row. Callers override the one or two fields they're changing —
// UpdateMediaItem is a full-row write, so any field not copied here would be
// silently blanked. (Mirrors the builder in internal/worker.)
func updateMediaItemParamsFrom(item sqlc.MediaItem) sqlc.UpdateMediaItemParams {
	return sqlc.UpdateMediaItemParams{
		ID:               item.ID,
		Title:            item.Title,
		SortTitle:        item.SortTitle,
		Year:             item.Year,
		Description:      item.Description,
		PosterPath:       item.PosterPath,
		BackdropPath:     item.BackdropPath,
		ExternalIds:      item.ExternalIds,
		Tagline:          item.Tagline,
		OriginalTitle:    item.OriginalTitle,
		OriginalLanguage: item.OriginalLanguage,
		Status:           item.Status,
		ProviderKind:     item.ProviderKind,
		HeyaSlug:         item.HeyaSlug,
	}
}

// emitMediaUpdated broadcasts a media.updated event over the hub, nil-guarded
// like every other emit site (see watch.go's UpdateWatchProgress). Centralized
// here so the several silent-mutation call sites in this file share one
// payload construction instead of repeating it.
func (a *App) emitMediaUpdated(mediaItemID, libraryID int64, title, mediaType string) {
	if a.hub != nil {
		a.hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: mediaItemID,
			LibraryID:   libraryID,
			Title:       title,
			MediaType:   mediaType,
		})
	}
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
	var libraryID int64
	var title, mediaType string

	err := a.withTx(ctx, func(q *sqlc.Queries) error {

		item, err := q.GetMediaItemByID(ctx, mediaItemID)
		if err != nil {
			return fmt.Errorf("media item not found: %w", err)
		}

		p := updateMediaItemParamsFrom(item)
		if req.Title != nil {
			p.Title = *req.Title
		}
		if req.SortTitle != nil {
			p.SortTitle = *req.SortTitle
		}
		if req.Year != nil {
			p.Year = *req.Year
		}
		if req.Description != nil {
			p.Description = *req.Description
		}
		if req.ExternalIDs != nil {
			b, _ := json.Marshal(req.ExternalIDs)
			p.ExternalIds = b
		}
		if req.Tagline != nil {
			p.Tagline = *req.Tagline
		}
		if req.OriginalTitle != nil {
			p.OriginalTitle = *req.OriginalTitle
		}
		if req.OriginalLanguage != nil {
			p.OriginalLanguage = *req.OriginalLanguage
		}
		if req.Status != nil {
			p.Status = *req.Status
		}

		libraryID = item.LibraryID
		title = p.Title
		mediaType = string(item.MediaType)

		if _, err := q.UpdateMediaItem(ctx, p); err != nil {
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
		case sqlc.MediaTypeMusic:
			artist, aErr := q.GetArtistByMediaItemID(ctx, mediaItemID)
			if aErr == nil {
				name := artist.Name
				if req.Title != nil && *req.Title != "" {
					name = *req.Title
				}
				sortName := artist.SortName
				if req.SortName != nil {
					sortName = *req.SortName
				}
				disambig := artist.Disambiguation
				if req.Disambiguation != nil {
					disambig = *req.Disambiguation
				}
				bio := artist.Biography
				if req.Biography != nil {
					bio = *req.Biography
				}
				if _, err := q.UpdateArtist(ctx, sqlc.UpdateArtistParams{
					ID:             artist.ID,
					MusicbrainzID:  artist.MusicbrainzID,
					Name:           name,
					SortName:       sortName,
					Disambiguation: disambig,
					Biography:      bio,
				}); err != nil {
					return fmt.Errorf("updating artist metadata: %w", err)
				}
			}
		}

		// Stamp 'user' provenance for the fields the user actually set, so a later
		// enrich / forced refresh / re-identify fills around them (matcher's
		// provenance-gated writers) instead of clobbering the edit.
		var edited []string
		if req.Title != nil {
			edited = append(edited, "title")
		}
		if req.Year != nil {
			edited = append(edited, "year")
		}
		if req.Description != nil {
			edited = append(edited, "description")
		}
		if req.Tagline != nil {
			edited = append(edited, "tagline")
		}
		if req.OriginalTitle != nil {
			edited = append(edited, "original_title")
		}
		if req.OriginalLanguage != nil {
			edited = append(edited, "original_language")
		}
		if req.OriginalName != nil {
			edited = append(edited, "original_name")
		}
		if req.Status != nil {
			edited = append(edited, "status")
		}
		if req.ExternalIDs != nil {
			edited = append(edited, "external_ids")
		}
		if req.Genres != nil {
			edited = append(edited, "genres")
		}
		if req.RuntimeMinutes != nil {
			edited = append(edited, "runtime_minutes")
		}
		if req.ReleaseDate != nil {
			edited = append(edited, "release_date")
		}
		if req.FirstAirDate != nil {
			edited = append(edited, "first_air_date")
		}
		if req.LastAirDate != nil {
			edited = append(edited, "last_air_date")
		}
		if req.SortName != nil {
			edited = append(edited, "sort_name")
		}
		if req.Disambiguation != nil {
			edited = append(edited, "disambiguation")
		}
		if req.Biography != nil {
			edited = append(edited, "biography")
		}
		if len(edited) > 0 {
			if err := q.SetMediaItemFieldProvenance(ctx, sqlc.SetMediaItemFieldProvenanceParams{
				ID:              mediaItemID,
				FieldProvenance: stampUserProvenance(item.FieldProvenance, edited...),
			}); err != nil {
				return fmt.Errorf("stamping field provenance: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	a.emitMediaUpdated(mediaItemID, libraryID, title, mediaType)
	return nil
}

// stampUserProvenance merges the given field names into the existing
// field_provenance map as "user" (manual edits), returning the JSON blob. A
// never-nil "{}" floor keeps the jsonb column a valid object.
func stampUserProvenance(existing []byte, fields ...string) []byte {
	m := map[string]string{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &m)
	}
	if m == nil {
		m = map[string]string{}
	}
	for _, f := range fields {
		m[f] = "user"
	}
	b, err := json.Marshal(m)
	if err != nil || len(b) == 0 {
		return []byte("{}")
	}
	return b
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

	updated, err := q.UpdateTVEpisode(ctx, sqlc.UpdateTVEpisodeParams{
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
	if err != nil {
		return sqlc.TvEpisode{}, err
	}

	// MediaPayload has no episode field, so the update is reported against
	// the parent series (season -> series -> media item) instead.
	if season, sErr := q.GetTVSeasonByID(ctx, ep.SeasonID); sErr == nil {
		if series, serErr := q.GetTVSeriesByID(ctx, season.SeriesID); serErr == nil {
			if item, iErr := q.GetMediaItemByID(ctx, series.MediaItemID); iErr == nil {
				a.emitMediaUpdated(item.ID, item.LibraryID, item.Title, string(item.MediaType))
			}
		}
	}

	return updated, nil
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

	if item.MediaType == sqlc.MediaTypeMusic {
		return a.applyIdentifyMusic(ctx, item, detail)
	}

	var kind metadata.MediaKind
	switch item.MediaType {
	case sqlc.MediaTypeMovie:
		kind = metadata.KindMovie
	case sqlc.MediaTypeTv:
		kind = metadata.KindTV
	default:
		kind = metadata.MediaKind(item.MediaType)
	}

	// Everything below is destructive-then-rebuild (wipe cast/crew/keywords/…,
	// re-store from the fresh detail), so it runs in ONE transaction: a failure
	// mid-rebuild must roll the deletes back, not leave the item stripped. The
	// artwork job is enqueued through the same tx so it only exists if the
	// rebuild committed.
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin re-identify transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txq := q.WithTx(tx)

	externalIDs, _ := json.Marshal(detail.ExternalIDs)
	p := updateMediaItemParamsFrom(item)
	p.Title = detail.Title
	p.SortTitle = strings.ToLower(detail.Title)
	p.Year = detail.Year
	p.Description = detail.Description
	p.ExternalIds = externalIDs
	if _, err := txq.UpdateMediaItem(ctx, p); err != nil {
		return fmt.Errorf("re-identify: update media item: %w", err)
	}

	for _, del := range []struct {
		name string
		fn   func(context.Context, int64) error
	}{
		{"cast", txq.DeleteMediaCastByItem},
		{"crew", txq.DeleteMediaCrewByItem},
		{"keywords", txq.DeleteMediaKeywordsByItem},
		{"videos", txq.DeleteMediaVideosByItem},
		{"certifications", txq.DeleteMediaCertificationsByItem},
		{"recommendations", txq.DeleteMediaRecommendationsByItem},
		{"production companies", txq.DeleteMediaProductionCompaniesByItem},
	} {
		if err := del.fn(ctx, mediaItemID); err != nil {
			return fmt.Errorf("re-identify: clear %s: %w", del.name, err)
		}
	}

	txMatcher := a.matcher.WithTx(tx)
	if err := txMatcher.StoreEntityMetadata(ctx, mediaItemID, kind, detail); err != nil {
		return fmt.Errorf("re-identify: store base metadata: %w", err)
	}
	if err := txMatcher.StoreRichMetadata(ctx, mediaItemID, detail); err != nil {
		return fmt.Errorf("re-identify: store rich metadata: %w", err)
	}

	_, _ = a.river.InsertTx(ctx, tx, worker.FetchArtworkArgs{
		MediaItemID: mediaItemID,
		MediaType:   string(item.MediaType),
	}, nil)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit re-identify: %w", err)
	}

	a.emitMediaUpdated(mediaItemID, item.LibraryID, detail.Title, string(item.MediaType))

	return nil
}

// applyIdentifyMusic re-points a music artist at a different upstream record.
// Unlike the movie/TV path it does NOT rebuild rows inline: it stamps the
// chosen MusicBrainz id on the artist row and enqueues a forced enrich, which
// re-fetches by that MBID and reuses the refresh pipeline's merge machinery
// (findCanonicalSibling folds this row into an existing local artist when the
// new identity is already present — the safe path for chimera repairs, where
// writing media_items.external_ids here would trip idx_media_items_mbid_unique
// instead). Name / bio / albums / top-tracks all adopt on that refresh.
func (a *App) applyIdentifyMusic(ctx context.Context, item sqlc.MediaItem, detail *metadata.MediaDetail) error {
	newMBID := detail.ExternalIDs["mbid"]
	if newMBID == "" {
		return fmt.Errorf("selected match has no MusicBrainz id yet — heya.media could not resolve it; try again once the upstream record is enriched")
	}

	q := sqlc.New(a.db)
	artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("artist for media item %d not found: %w", item.ID, err)
	}

	if _, err := q.UpdateArtist(ctx, sqlc.UpdateArtistParams{
		ID:             artist.ID,
		MusicbrainzID:  newMBID,
		Name:           artist.Name,
		SortName:       artist.SortName,
		Disambiguation: artist.Disambiguation,
		Biography:      artist.Biography,
	}); err != nil {
		return fmt.Errorf("stamp artist mbid: %w", err)
	}

	// An explicit re-identify means the user wants the new record's identity —
	// lift any user-edit locks on the identity fields so the refresh adopts it.
	fp := map[string]string{}
	if len(item.FieldProvenance) > 0 {
		_ = json.Unmarshal(item.FieldProvenance, &fp)
	}
	for _, f := range []string{"title", "sort_name", "disambiguation", "biography"} {
		delete(fp, f)
	}
	blob, _ := json.Marshal(fp)
	if err := q.SetMediaItemFieldProvenance(ctx, sqlc.SetMediaItemFieldProvenanceParams{
		ID:              item.ID,
		FieldProvenance: blob,
	}); err != nil {
		return fmt.Errorf("clear identity provenance: %w", err)
	}

	return worker.EnqueueEnrichForce(ctx, a.river, item.ID, item.MediaType, worker.EnrichSourceForced)
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

	if item, iErr := q.GetMediaItemByID(ctx, mediaItemID); iErr == nil {
		a.emitMediaUpdated(item.ID, item.LibraryID, item.Title, string(item.MediaType))
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

	if item, iErr := q.GetMediaItemByID(ctx, mediaItemID); iErr == nil {
		a.emitMediaUpdated(item.ID, item.LibraryID, item.Title, string(item.MediaType))
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
	case sqlc.MediaTypeMusic:
		// Music media items are artists; the artist payload's flat image
		// pool comes back typed as posters.
		kind = metadata.KindMusic
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

	// No emit here: this only enqueues a DownloadImageArgs job. The asset isn't
	// stored yet, so a media.updated now would trigger a stale refetch. The
	// DownloadImageWorker emits at store-time when the primary poster/backdrop
	// actually lands.

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

	a.emitMediaUpdated(item.ID, item.LibraryID, item.Title, string(item.MediaType))

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
