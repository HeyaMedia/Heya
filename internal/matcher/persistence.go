package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/rs/zerolog/log"
)

func (m *Matcher) createOrLinkMediaItem(ctx context.Context, detail *metadata.MediaDetail, kind metadata.MediaKind, libraryID int64, filePath string) (int64, bool, error) {
	extJSON, _ := json.Marshal(detail.ExternalIDs)

	// Only link by external IDs when we actually HAVE some. `external_ids @> '{}'`
	// matches every row, so an empty-ID stub (NFO-less / filename-only local)
	// would otherwise link onto an arbitrary existing media_item. Empty-ID
	// dedup is handled by local_identity_key (Phase 1), not containment.
	hasExternalIDs := len(detail.ExternalIDs) > 0 && len(extJSON) > 0 &&
		string(extJSON) != "{}" && string(extJSON) != "null"

	if hasExternalIDs {
		existing, err := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
			LibraryID: libraryID,
			ExtFilter: extJSON,
		})
		if err == nil {
			log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item")
			return existing.ID, false, nil
		}
	}

	mediaType := kindToMediaType(kind)
	sortTitle := strings.ToLower(detail.Title)

	item, err := m.q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:        libraryID,
		MediaType:        mediaType,
		Title:            detail.Title,
		SortTitle:        sortTitle,
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       detail.PosterURL,
		BackdropPath:     detail.BackdropURL,
		ExternalIds:      extJSON,
		Tagline:          detail.Tagline,
		OriginalTitle:    detail.OriginalTitle,
		OriginalLanguage: detail.OriginalLanguage,
		Status:           detail.Status,
		ProviderKind:     detail.ProviderKind,
		HeyaSlug:         detail.HeyaSlug,
	})
	if err != nil {
		if hasExternalIDs {
			existing, retryErr := m.q.GetMediaItemByExternalID(ctx, sqlc.GetMediaItemByExternalIDParams{
				LibraryID: libraryID,
				ExtFilter: extJSON,
			})
			if retryErr == nil {
				log.Debug().Int64("id", existing.ID).Str("title", existing.Title).Msg("linked to existing media item (race resolved)")
				return existing.ID, false, nil
			}
		}
		return 0, false, fmt.Errorf("creating media item: %w", err)
	}

	itemSlug := slug.GenerateUnique(ctx, detail.Title, detail.Year, item.ID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			r, err := m.q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
			if err != nil {
				return false, err
			}
			return r, nil
		})
	m.q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: itemSlug})

	_ = m.q.MarkMatched(ctx, item.ID)

	return item.ID, true, nil
}

// createTypeSpecificRow inserts the type-specific row (movies / tv_series /
// books) for a media item. Called by the enrich path (after a successful
// GetDetail) — not by the match step, which writes only the media_items
// stub. Music is intentionally absent: the music enrich path goes through
// matcher.RefreshMusicArtist (album + track upsert from the artist payload)
// instead of a MediaDetail → CreateMusic shape, so this function no-ops for
// music. If a future feature needs a music candidate write here, route it
// through RefreshMusicArtist instead of resurrecting a separate path.
func (m *Matcher) createTypeSpecificRow(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail, filePath string) error {
	switch kind {
	case metadata.KindMovie:
		return m.createMovie(ctx, mediaItemID, detail)
	case metadata.KindTV:
		return m.createTVSeries(ctx, mediaItemID, detail)
	case metadata.KindBook:
		return m.createBook(ctx, mediaItemID, detail, filePath)
	}
	return nil
}

// createMovie writes the movies row from a remote detail. Get-or-create-or-fill:
//   - no row yet (normal first enrich, or a freshly materialized local stub) →
//     INSERT the full detail.
//   - row exists (local stub being upgraded by enrich, a re-identify, or a
//     forced refresh) → UPDATE with remote values, EXCEPT fields the user has
//     edited (field_provenance == "user"), which are preserved (edits win).
//
// Non-forced re-enrich never reaches here: the enrich worker gates the base
// component on BaseEnrichedAt. Rich metadata (cast/crew/…) is written by the
// caller's separate StoreRichMetadata, not here.
func (m *Matcher) createMovie(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	existing, gerr := m.q.GetMovieByMediaItemID(ctx, mediaItemID)
	switch {
	case errors.Is(gerr, pgx.ErrNoRows):
		_, err := m.q.CreateMovie(ctx, sqlc.CreateMovieParams{
			MediaItemID:      mediaItemID,
			RuntimeMinutes:   int32(d.RuntimeMinutes),
			Tagline:          d.Tagline,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			ReleaseDate:      pgDateFromString(d.ReleaseDate),
			OriginalTitle:    d.OriginalTitle,
			OriginalLanguage: d.OriginalLanguage,
			Budget:           d.Budget,
			Revenue:          d.Revenue,
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	case gerr == nil:
		locked := m.lockedFields(ctx, mediaItemID)
		p := sqlc.UpdateMovieParams{
			ID:               existing.ID,
			RuntimeMinutes:   int32(d.RuntimeMinutes),
			Tagline:          d.Tagline,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			ReleaseDate:      pgDateFromString(d.ReleaseDate),
			OriginalTitle:    d.OriginalTitle,
			OriginalLanguage: d.OriginalLanguage,
			Budget:           d.Budget,
			Revenue:          d.Revenue,
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		}
		// Remote wins EXCEPT any field the user has edited (provenance == "user").
		if locked["genres"] {
			p.Genres = existing.Genres
		}
		if locked["runtime_minutes"] {
			p.RuntimeMinutes = existing.RuntimeMinutes
		}
		if locked["tagline"] {
			p.Tagline = existing.Tagline
		}
		if locked["rating"] {
			p.Rating = existing.Rating
		}
		if locked["release_date"] {
			p.ReleaseDate = existing.ReleaseDate
		}
		if locked["original_title"] {
			p.OriginalTitle = existing.OriginalTitle
		}
		if locked["original_language"] {
			p.OriginalLanguage = existing.OriginalLanguage
		}
		if locked["budget"] {
			p.Budget = existing.Budget
		}
		if locked["revenue"] {
			p.Revenue = existing.Revenue
		}
		if locked["popularity"] {
			p.Popularity = existing.Popularity
		}
		if locked["spoken_languages"] {
			p.SpokenLanguages = existing.SpokenLanguages
		}
		if locked["origin_country"] {
			p.OriginCountry = existing.OriginCountry
		}
		if _, err := m.q.UpdateMovie(ctx, p); err != nil {
			return err
		}
	default:
		return gerr
	}
	return nil
}

// lockedFields returns the set of base/type-specific field names a user has
// manually edited (field_provenance == "user") on the media_item — the enrich
// writers must not overwrite these.
func (m *Matcher) lockedFields(ctx context.Context, mediaItemID int64) map[string]bool {
	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return nil
	}
	fp := ParseFieldProvenance(item.FieldProvenance)
	if len(fp) == 0 {
		return nil
	}
	locked := make(map[string]bool, len(fp))
	for field, src := range fp {
		if src == ProvUser {
			locked[field] = true
		}
	}
	return locked
}

func (m *Matcher) storeRichMetadata(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) {
	seenCast := map[string]bool{}
	for _, c := range d.Cast {
		dedup := c.Name + "|" + c.Character
		if seenCast[dedup] {
			continue
		}
		seenCast[dedup] = true

		person := m.findOrCreatePerson(ctx, c.Name, c.ExternalIDs, c.Gender, c.ProfilePath, c.Popularity, c.Profiles)
		if person.ID == 0 {
			continue
		}
		m.q.CreateMediaCast(ctx, sqlc.CreateMediaCastParams{
			MediaItemID:  mediaItemID,
			PersonID:     person.ID,
			Character:    c.Character,
			DisplayOrder: int32(c.Order),
			Gender:       int32(c.Gender),
			Source:       c.Source,
		})
	}

	seenCrew := map[string]bool{}
	for _, c := range d.Crew {
		dedup := c.Name + "|" + c.Job
		if seenCrew[dedup] {
			continue
		}
		seenCrew[dedup] = true

		person := m.findOrCreatePerson(ctx, c.Name, c.ExternalIDs, c.Gender, c.ProfilePath, 0, c.Profiles)
		if person.ID == 0 {
			continue
		}
		m.q.CreateMediaCrew(ctx, sqlc.CreateMediaCrewParams{
			MediaItemID: mediaItemID,
			PersonID:    person.ID,
			Job:         c.Job,
			Department:  c.Department,
			Gender:      int32(c.Gender),
			Source:      c.Source,
		})
	}

	seenKeywords := map[string]bool{}
	for _, k := range d.Keywords {
		if seenKeywords[k.Name] {
			continue
		}
		seenKeywords[k.Name] = true

		kw := m.findOrCreateKeyword(ctx, k.Name, k.ExternalIDs)
		if kw.ID == 0 {
			continue
		}
		m.q.LinkMediaKeyword(ctx, sqlc.LinkMediaKeywordParams{
			MediaItemID: mediaItemID,
			KeywordID:   kw.ID,
		})
	}

	seenCompanies := map[string]bool{}
	for _, pc := range d.ProductionCompanies {
		if seenCompanies[pc.Name] {
			continue
		}
		seenCompanies[pc.Name] = true

		co := m.findOrCreateCompany(ctx, pc.Name, pc.ExternalIDs, pc.LogoPath, pc.OriginCountry)
		if co.ID == 0 {
			continue
		}
		m.q.LinkMediaProductionCompany(ctx, sqlc.LinkMediaProductionCompanyParams{
			MediaItemID: mediaItemID,
			CompanyID:   co.ID,
		})
	}

	for _, v := range d.Videos {
		m.q.CreateMediaVideo(ctx, sqlc.CreateMediaVideoParams{
			MediaItemID: mediaItemID,
			ProviderKey: v.ProviderKey,
			Name:        v.Name,
			Site:        v.Site,
			VideoKey:    v.Key,
			VideoType:   v.Type,
			Language:    v.Language,
			Official:    v.Official,
		})
	}

	for _, c := range d.Certifications {
		m.q.CreateMediaCertification(ctx, sqlc.CreateMediaCertificationParams{
			MediaItemID:   mediaItemID,
			Country:       c.Country,
			Certification: c.Certification,
			ReleaseDate:   pgDateFromString(c.ReleaseDate),
			ReleaseType:   int32(c.ReleaseType),
			Source:        c.Source,
		})
	}

	for _, r := range d.Recommendations {
		m.q.CreateMediaRecommendation(ctx, sqlc.CreateMediaRecommendationParams{
			MediaItemID: mediaItemID,
			ExternalIds: mustJSON(r.ExternalIDs),
			Title:       r.Title,
			PosterPath:  r.PosterPath,
			MediaType:   r.MediaType,
			VoteAverage: numericFromFloat(r.VoteAverage),
			ReleaseDate: r.ReleaseDate,
		})
	}

	if d.Collection != nil && d.Collection.Name != "" && m.shouldAutoCollect(ctx, mediaItemID) {
		m.linkCollection(ctx, mediaItemID, d.Collection)
	}

	if d.ExternalIDs["wikidata"] != "" || d.ExternalIDs["facebook"] != "" || d.ExternalIDs["instagram"] != "" || d.ExternalIDs["twitter"] != "" || d.Homepage != "" {
		item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
		if err == nil {
			m.q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
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
			})
		}
	}

	for _, t := range d.Titles {
		m.q.CreateMediaTitle(ctx, sqlc.CreateMediaTitleParams{
			MediaItemID: mediaItemID,
			Title:       t.Title,
			Language:    t.Language,
			Country:     t.Country,
			TitleType:   t.TitleType,
			Source:      t.Source,
		})
	}

	for lang, text := range d.Overviews {
		m.q.CreateMediaOverview(ctx, sqlc.CreateMediaOverviewParams{
			MediaItemID: mediaItemID,
			Language:    lang,
			Overview:    text,
		})
	}

	log.Info().Int64("media_id", mediaItemID).
		Int("cast", len(d.Cast)).
		Int("crew", len(d.Crew)).
		Int("keywords", len(d.Keywords)).
		Int("videos", len(d.Videos)).
		Int("recs", len(d.Recommendations)).
		Int("titles", len(d.Titles)).
		Int("overviews", len(d.Overviews)).
		Msg("stored rich metadata")
}

func (m *Matcher) findOrCreatePerson(ctx context.Context, name string, externalIDs map[string]string, gender int, profilePath string, popularity float64, profiles []metadata.ProfileImage) sqlc.Person {
	var person sqlc.Person
	for k, v := range externalIDs {
		if v == "" {
			continue
		}
		probe := mustJSON(map[string]string{k: v})
		if existing, err := m.q.FindPersonByExternalID(ctx, probe); err == nil {
			person = existing
			break
		}
	}

	if person.ID == 0 {
		created, err := m.q.CreatePerson(ctx, sqlc.CreatePersonParams{
			ExternalIds: mustJSON(externalIDs),
			Name:        name,
			AlsoKnownAs: []string{},
			Gender:      int32(gender),
			ProfilePath: profilePath,
			Popularity:  numericFromFloat(popularity),
		})
		if err != nil {
			log.Debug().Err(err).Str("name", name).Msg("failed to create person")
			return sqlc.Person{}
		}
		person = created
	}

	for i, p := range profiles {
		if p.URL == "" {
			continue
		}
		m.q.CreatePersonProfile(ctx, sqlc.CreatePersonProfileParams{
			PersonID:  person.ID,
			Url:       p.URL,
			Source:    p.Source,
			Aspect:    fallbackAspect(p.Aspect),
			Width:     int32(p.Width),
			Height:    int32(p.Height),
			Score:     numericFromFloat(p.Score),
			SortOrder: int32(i),
		})
	}

	return person
}

func fallbackAspect(a string) string {
	if a == "" {
		return "profile"
	}
	return a
}

func (m *Matcher) findOrCreateKeyword(ctx context.Context, name string, externalIDs map[string]string) sqlc.Keyword {
	if existing, err := m.q.FindKeywordByName(ctx, name); err == nil {
		return existing
	}
	kw, err := m.q.CreateKeyword(ctx, sqlc.CreateKeywordParams{
		ExternalIds: mustJSON(externalIDs),
		Name:        name,
	})
	if err != nil {
		return sqlc.Keyword{}
	}
	return kw
}

func (m *Matcher) findOrCreateCompany(ctx context.Context, name string, externalIDs map[string]string, logoPath, country string) sqlc.ProductionCompany {
	for k, v := range externalIDs {
		if v == "" {
			continue
		}
		if existing, err := m.q.FindProductionCompanyByExternalID(ctx, mustJSON(map[string]string{k: v})); err == nil {
			return existing
		}
	}
	if existing, err := m.q.FindProductionCompanyByName(ctx, name); err == nil {
		return existing
	}
	co, err := m.q.CreateProductionCompany(ctx, sqlc.CreateProductionCompanyParams{
		ExternalIds:   mustJSON(externalIDs),
		Name:          name,
		LogoPath:      logoPath,
		OriginCountry: country,
	})
	if err != nil {
		return sqlc.ProductionCompany{}
	}
	return co
}

// linkCollection find-or-creates the collection (franchise) row and points
// the movie's collection_id at it. Collections carry no unique constraint on
// name, so a blind insert per enrich would pile up duplicate rows — an
// existing row is refreshed with the latest artwork/overview instead.
func (m *Matcher) linkCollection(ctx context.Context, mediaItemID int64, c *metadata.CollectionDetail) {
	col, err := m.q.FindCollectionByName(ctx, c.Name)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		col, err = m.q.CreateCollection(ctx, sqlc.CreateCollectionParams{
			ExternalIds:  mustJSON(c.ExternalIDs),
			Name:         c.Name,
			Overview:     c.Overview,
			PosterPath:   c.PosterPath,
			BackdropPath: c.BackdropPath,
		})
		if err != nil {
			log.Debug().Err(err).Str("collection", c.Name).Msg("failed to create collection")
			return
		}
	case err != nil:
		log.Debug().Err(err).Str("collection", c.Name).Msg("failed to look up collection")
		return
	default:
		if err := m.q.UpdateCollection(ctx, sqlc.UpdateCollectionParams{
			ID:           col.ID,
			ExternalIds:  mustJSON(c.ExternalIDs),
			Overview:     c.Overview,
			PosterPath:   c.PosterPath,
			BackdropPath: c.BackdropPath,
		}); err != nil {
			log.Debug().Err(err).Str("collection", c.Name).Msg("failed to refresh collection")
		}
	}
	if err := m.q.SetMovieCollection(ctx, sqlc.SetMovieCollectionParams{
		MediaItemID:  mediaItemID,
		CollectionID: pgtype.Int8{Int64: col.ID, Valid: true},
	}); err != nil {
		log.Debug().Err(err).Int64("media_id", mediaItemID).Str("collection", c.Name).Msg("failed to link movie to collection")
	}
}

func (m *Matcher) shouldAutoCollect(ctx context.Context, mediaItemID int64) bool {
	item, err := m.q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return true
	}
	lib, err := m.q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return true
	}
	settings := metadata.ParseSettings(lib.Settings)
	if settings.IsEmpty() {
		return true
	}
	return settings.AutoCollections
}

func (m *Matcher) createTVSeries(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) error {
	series, err := m.upsertTVSeriesRow(ctx, mediaItemID, d)
	if err != nil {
		return err
	}

	m.linkNetworks(ctx, series.ID, d.Networks)
	m.linkCreators(ctx, series.ID, d.CreatedBy)

	for _, sd := range d.Seasons {
		seasonExtIDs := map[string]string{}
		if sd.TmdbSeasonID != 0 {
			seasonExtIDs["tmdb"] = fmt.Sprintf("%d", sd.TmdbSeasonID)
		}
		if sd.TvdbSeasonID != 0 {
			seasonExtIDs["tvdb"] = fmt.Sprintf("%d", sd.TvdbSeasonID)
		}
		if sd.AnidbID != 0 {
			seasonExtIDs["anidb"] = fmt.Sprintf("%d", sd.AnidbID)
		}

		season, err := m.q.CreateTVSeason(ctx, sqlc.CreateTVSeasonParams{
			SeriesID:      series.ID,
			SeasonNumber:  int32(sd.Number),
			Title:         sd.Title,
			Overview:      sd.Overview,
			PosterPath:    sd.PosterURL,
			AirDate:       pgDateFromString(sd.AirDate),
			EndDate:       pgDateFromString(sd.EndDate),
			Status:        sd.Status,
			AiredEpisodes: int32(sd.AiredEpisodes),
			ExternalIds:   mustJSON(seasonExtIDs),
		})
		// DO NOTHING → no row when the season already exists; recover its id so
		// new episodes can still be attached under it.
		if errors.Is(err, pgx.ErrNoRows) {
			season, err = m.q.GetTVSeason(ctx, sqlc.GetTVSeasonParams{SeriesID: series.ID, SeasonNumber: int32(sd.Number)})
		}
		if err != nil {
			log.Warn().Err(err).Int("season", sd.Number).Msg("error creating season")
			continue
		}

		for _, ep := range sd.Episodes {
			epExtIDs := map[string]string{}
			if ep.TmdbID != 0 {
				epExtIDs["tmdb"] = fmt.Sprintf("%d", ep.TmdbID)
			}
			if ep.TvdbID != 0 {
				epExtIDs["tvdb"] = fmt.Sprintf("%d", ep.TvdbID)
			}

			tvEp, err := m.q.CreateTVEpisode(ctx, sqlc.CreateTVEpisodeParams{
				SeasonID:       season.ID,
				EpisodeNumber:  int32(ep.Number),
				Title:          ep.Title,
				Overview:       ep.Overview,
				StillPath:      ep.StillURL,
				RuntimeMinutes: int32(ep.RuntimeMinutes),
				AirDate:        pgDateFromString(ep.AirDate),
				Rating:         numericFromFloat(ep.Rating),
				AbsoluteNumber: int32(ep.AbsoluteNumber),
				IsSpecial:      ep.IsSpecial,
				EpisodeType:    int32(ep.EpisodeType),
				ExternalIds:    mustJSON(epExtIDs),
				Source:         ep.Source,
			})
			// DO NOTHING → episode already exists; preserve it (incl. user edits)
			// and skip its title/overview re-insert below.
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				log.Warn().Err(err).Int("episode", ep.Number).Msg("error creating episode")
				continue
			}
			for _, t := range ep.Titles {
				m.q.CreateEpisodeTitle(ctx, sqlc.CreateEpisodeTitleParams{
					EpisodeID: tvEp.ID,
					Title:     t.Title,
					Language:  t.Language,
					Source:    t.Source,
				})
			}
			for lang, text := range ep.Overviews {
				m.q.CreateEpisodeOverview(ctx, sqlc.CreateEpisodeOverviewParams{
					EpisodeID: tvEp.ID,
					Language:  lang,
					Overview:  text,
				})
			}
		}
	}

	return nil
}

// upsertTVSeriesRow writes the tv_series row from a remote detail, mirroring
// createMovie's get-or-create-or-fill + provenance rules. Returns the row so
// the caller can attach networks/creators/seasons to series.ID.
func (m *Matcher) upsertTVSeriesRow(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail) (sqlc.TvSeries, error) {
	existing, gerr := m.q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	switch {
	case errors.Is(gerr, pgx.ErrNoRows):
		s, err := m.q.CreateTVSeries(ctx, sqlc.CreateTVSeriesParams{
			MediaItemID:      mediaItemID,
			Status:           d.Status,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			FirstAirDate:     pgDateFromString(d.FirstAirDate),
			LastAirDate:      pgDateFromString(d.LastAirDate),
			OriginalName:     d.OriginalName,
			OriginalLanguage: d.OriginalLanguage,
			NumberOfSeasons:  int32(d.NumberOfSeasons),
			NumberOfEpisodes: int32(d.NumberOfEpisodes),
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Concurrent insert won the conflict — fetch the existing row.
			return m.q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
		}
		if err != nil {
			return sqlc.TvSeries{}, fmt.Errorf("creating tv series: %w", err)
		}
		return s, nil
	case gerr == nil:
		locked := m.lockedFields(ctx, mediaItemID)
		p := sqlc.UpdateTVSeriesParams{
			ID:               existing.ID,
			Status:           d.Status,
			Genres:           emptyIfNil(d.Genres),
			Rating:           numericFromFloat(d.Rating),
			FirstAirDate:     pgDateFromString(d.FirstAirDate),
			LastAirDate:      pgDateFromString(d.LastAirDate),
			OriginalName:     d.OriginalName,
			OriginalLanguage: d.OriginalLanguage,
			NumberOfSeasons:  int32(d.NumberOfSeasons),
			NumberOfEpisodes: int32(d.NumberOfEpisodes),
			Popularity:       numericFromFloat(d.Popularity),
			SpokenLanguages:  emptyIfNil(d.SpokenLanguages),
			OriginCountry:    emptyIfNil(d.OriginCountry),
		}
		// Remote wins EXCEPT any user-edited field.
		if locked["genres"] {
			p.Genres = existing.Genres
		}
		if locked["status"] {
			p.Status = existing.Status
		}
		if locked["rating"] {
			p.Rating = existing.Rating
		}
		if locked["first_air_date"] {
			p.FirstAirDate = existing.FirstAirDate
		}
		if locked["last_air_date"] {
			p.LastAirDate = existing.LastAirDate
		}
		if locked["original_name"] {
			p.OriginalName = existing.OriginalName
		}
		if locked["original_language"] {
			p.OriginalLanguage = existing.OriginalLanguage
		}
		if locked["popularity"] {
			p.Popularity = existing.Popularity
		}
		if locked["spoken_languages"] {
			p.SpokenLanguages = existing.SpokenLanguages
		}
		if locked["origin_country"] {
			p.OriginCountry = existing.OriginCountry
		}
		s, err := m.q.UpdateTVSeries(ctx, p)
		if err != nil {
			return sqlc.TvSeries{}, fmt.Errorf("updating tv series: %w", err)
		}
		return s, nil
	default:
		return sqlc.TvSeries{}, gerr
	}
}

func (m *Matcher) linkNetworks(ctx context.Context, seriesID int64, nets []metadata.NetworkDetail) {
	m.q.DeleteNetworksForSeries(ctx, seriesID)
	for i, n := range nets {
		if n.Name == "" {
			continue
		}
		net, err := m.upsertNetwork(ctx, n)
		if err != nil || net.ID == 0 {
			continue
		}
		m.q.AttachNetworkToSeries(ctx, sqlc.AttachNetworkToSeriesParams{
			SeriesID:  seriesID,
			NetworkID: net.ID,
			SortOrder: int32(i),
		})
	}
}

func (m *Matcher) upsertNetwork(ctx context.Context, n metadata.NetworkDetail) (sqlc.Network, error) {
	if len(n.ExternalIDs) > 0 {
		if existing, err := m.q.FindNetworkByExternalID(ctx, mustJSON(n.ExternalIDs)); err == nil {
			return existing, nil
		}
	}
	return m.q.UpsertNetworkByExternalIDs(ctx, sqlc.UpsertNetworkByExternalIDsParams{
		Name:        n.Name,
		ExternalIds: mustJSON(n.ExternalIDs),
		LogoPath:    n.LogoPath,
		Country:     n.Country,
	})
}

func (m *Matcher) linkCreators(ctx context.Context, seriesID int64, creators []metadata.CreatorDetail) {
	m.q.DeleteCreatorsForSeries(ctx, seriesID)
	for i, c := range creators {
		if c.Name == "" {
			continue
		}
		cr, err := m.upsertCreator(ctx, c)
		if err != nil || cr.ID == 0 {
			continue
		}
		m.q.AttachCreatorToSeries(ctx, sqlc.AttachCreatorToSeriesParams{
			SeriesID:  seriesID,
			CreatorID: cr.ID,
			SortOrder: int32(i),
		})
	}
}

func (m *Matcher) upsertCreator(ctx context.Context, c metadata.CreatorDetail) (sqlc.Creator, error) {
	if len(c.ExternalIDs) > 0 {
		if existing, err := m.q.FindCreatorByExternalID(ctx, mustJSON(c.ExternalIDs)); err == nil {
			return existing, nil
		}
	}
	return m.q.UpsertCreatorByExternalIDs(ctx, sqlc.UpsertCreatorByExternalIDsParams{
		Name:        c.Name,
		ExternalIds: mustJSON(c.ExternalIDs),
	})
}

func (m *Matcher) createBook(ctx context.Context, mediaItemID int64, d *metadata.MediaDetail, filePath string) error {
	var authorID pgtype.Int8

	if d.AuthorName != "" {
		existing, err := m.q.GetAuthorByName(ctx, d.AuthorName)
		if err == nil {
			authorID = pgtype.Int8{Int64: existing.ID, Valid: true}
		} else {
			author, err := m.q.CreateAuthor(ctx, sqlc.CreateAuthorParams{
				Name:          d.AuthorName,
				OpenlibraryID: d.ExternalIDs["openlibrary_author"],
				Biography:     d.AuthorBio,
				BirthDate:     d.AuthorBirthDate,
				DeathDate:     d.AuthorDeathDate,
			})
			if err != nil {
				log.Warn().Err(err).Str("author", d.AuthorName).Msg("error creating author")
			} else {
				authorID = pgtype.Int8{Int64: author.ID, Valid: true}
			}
		}
	}

	ext := ""
	if idx := strings.LastIndex(filePath, "."); idx >= 0 {
		ext = filePath[idx+1:]
	}

	_, err := m.q.CreateBook(ctx, sqlc.CreateBookParams{
		MediaItemID:   mediaItemID,
		AuthorID:      authorID,
		Isbn:          d.ISBN,
		OpenlibraryID: d.ExternalIDs["openlibrary"],
		PageCount:     int32(d.PageCount),
		Publisher:     d.Publisher,
		PublishDate:   pgDateFromString(d.PublishDate),
		FilePath:      filePath,
		Subjects:      emptyIfNil(d.Subjects),
		Language:      d.Language,
		SeriesName:    d.SeriesName,
		SeriesNumber:  int32(d.SeriesNum),
		Format:        ext,
		Description:   d.Description,
	})
	return err
}

func kindToMediaType(kind metadata.MediaKind) sqlc.MediaType {
	switch kind {
	case metadata.KindMovie:
		return sqlc.MediaTypeMovie
	case metadata.KindTV:
		return sqlc.MediaTypeTv
	case metadata.KindMusic:
		return sqlc.MediaTypeMusic
	case metadata.KindBook:
		return sqlc.MediaTypeBook
	default:
		return sqlc.MediaTypeMovie
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if b == nil {
		return []byte("{}")
	}
	return b
}

// StoreEntityMetadata persists type-specific metadata (movie, TV, music, book)
// for a media item. Called by the worker pipeline (EnrichMediaItemWorker) and
// the metadata editor after a manual metadata refresh. The match step does
// NOT call this — it writes only the media_items stub via
// createOrLinkMediaItem, and the enrich worker fills in the type-specific
// row here.
func (m *Matcher) StoreEntityMetadata(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail) {
	// pgx.ErrNoRows here is the benign "row already exists" no-op (ON CONFLICT DO
	// NOTHING on a re-enrich) — not a failure. Surface anything else instead of
	// the previous unconditional swallow.
	if err := m.createTypeSpecificRow(ctx, mediaItemID, kind, detail, ""); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Error().Err(err).Int64("media_id", mediaItemID).Str("kind", string(kind)).
			Msg("failed to store type-specific metadata")
	}
}

// StoreRichMetadata persists cast, crew, keywords, production companies, videos,
// certifications, recommendations, and collections for a media item. Called by
// the worker pipeline (EnrichMediaItemWorker) and the metadata editor.
func (m *Matcher) StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail) {
	m.storeRichMetadata(ctx, mediaItemID, detail)
}
