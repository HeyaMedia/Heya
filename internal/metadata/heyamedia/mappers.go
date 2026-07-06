package heyamedia

import (
	"fmt"
	"strconv"
	"strings"

	gen "github.com/karbowiak/heya/clients/heyamedia"
	"github.com/karbowiak/heya/internal/metadata"
)

// Mappers from the generated heyamediaclient types to the canonical
// metadata.MediaDetail shape consumed by the matcher / worker / UI.
//
// One entry point per upstream kind:
//   - mapMovieDoc(*gen.MovieDocBody)
//   - mapTvDoc(*gen.TVDocBody)
//   - mapArtistDoc(*gen.ArtistDocBody)
//   - mapBookDoc(*gen.BookDocBody)
//   - mapPersonDoc(*gen.PersonDocBody) — returns the legacy
//     HeyaPersonResponse shape so person_worker.go stays untouched.
//
// Behaviour parity is asserted by the golden tests in
// mapdetail_golden_test.go — those decode a captured heya.media response
// twice (once via the legacy heyaItemResponse, once via the generated
// DocBody), runs both mappers, and demands byte-identical JSON. If you
// touch anything in this file, re-run with -update-golden and inspect
// the diff before committing.

// ---------------------------------------------------------------------------
// Movie + TV — both kinds share the typed `gen.Detail` payload.
// ---------------------------------------------------------------------------

func mapMovieDoc(body *gen.MovieDocBody) *metadata.MediaDetail {
	if body == nil {
		return nil
	}
	return mapMovieOrTV(body.Id, body.Kind, body.Title, body.Year, body.Slug, body.Poster, body.Ids, &body.Payload)
}

func mapTvDoc(body *gen.TVDocBody) *metadata.MediaDetail {
	if body == nil {
		return nil
	}
	return mapMovieOrTV(body.Id, body.Kind, body.Title, body.Year, body.Slug, body.Poster, body.Ids, &body.Payload)
}

func mapMovieOrTV(id, kind, title string, year *int64, slug string, poster *string, ids gen.ExternalIDsDTO, pay *gen.Detail) *metadata.MediaDetail {
	_ = id // not currently surfaced into MediaDetail; reserved for diagnostics.

	posterURL := strPtr(poster)
	backdropURL := ""
	if pay.Artwork != nil {
		// Top-level poster wins; fall back to the first poster in the
		// artwork array. Backdrops always come from artwork.
		if posterURL == "" && pay.Artwork.Posters != nil && len(*pay.Artwork.Posters) > 0 {
			posterURL = (*pay.Artwork.Posters)[0].Url
		}
		if pay.Artwork.Backdrops != nil && len(*pay.Artwork.Backdrops) > 0 {
			backdropURL = (*pay.Artwork.Backdrops)[0].Url
		}
	}

	extIDs := mergeExternalIDs(ids, pay.ExternalIds)

	var rating float64
	if pay.Ratings != nil && len(*pay.Ratings) > 0 {
		rating = (*pay.Ratings)[0].Value
	}

	yearStr := ""
	if year != nil && *year > 0 {
		yearStr = strconv.FormatInt(*year, 10)
	}

	sortTitle := strings.ToLower(coalesce(strPtr(pay.SortTitle), title))
	overview := strPtr(pay.Overview)
	firstAirDate := strPtr(pay.FirstAirDate)

	detail := &metadata.MediaDetail{
		Title:         title,
		SortTitle:     sortTitle,
		Year:          yearStr,
		Description:   overview,
		Titles:        mapLocalizedTitles(pay.Titles),
		Overviews:     mapStr(pay.Overviews),
		PosterURL:     posterURL,
		BackdropURL:   backdropURL,
		ExternalIDs:   extIDs,
		Genres:        strs(pay.Genres),
		Rating:        rating,
		ProviderKind:  kind,
		HeyaSlug:      slug,
		OriginalTitle: strPtr(pay.OriginalTitle),

		// Movie fields.
		Collection:          mapCollection(pay.Collection),
		RuntimeMinutes:      intPtr64AsInt(pay.Runtime),
		Tagline:             strPtr(pay.Tagline),
		ReleaseDate:         firstAirDate, // mirrors the legacy mapper — movies use first_air_date as release_date.
		OriginalLanguage:    strPtr(pay.OriginalLanguage),
		Budget:              intPtr64(pay.Budget),
		Revenue:             intPtr64(pay.Revenue),
		Popularity:          floatPtr(pay.Popularity),
		ProductionCompanies: mapStudios(pay.Studios),
		Cast:                mapCast(pay.Cast),
		Crew:                mapCrew(pay.Crew),
		Keywords:            mapKeywords(pay.Keywords),
		Videos:              mapVideos(pay.Videos),
		Certifications:      mapContentRatings(pay.ContentRatings),
		Recommendations:     mapRecommendations(pay.Recommendations),
		Homepage:            strPtr(pay.Homepage),
		SpokenLanguages:     strs(pay.SpokenLanguages),
		OriginCountry:       strs(pay.OriginCountry),

		// TV fields.
		Status:       strPtr(pay.Status),
		FirstAirDate: firstAirDate,
		LastAirDate:  strPtr(pay.LastAirDate),
		Networks:     mapNamedRefs(pay.Networks),
		CreatedBy:    mapCreators(pay.CreatedBy),
	}

	if pay.Seasons != nil {
		detail.Seasons = mapSeasons(*pay.Seasons)
		detail.NumberOfSeasons = len(*pay.Seasons)
		detail.NumberOfEpisodes = countEpisodesGen(*pay.Seasons)
	}

	return detail
}

// mapCollection converts heya.media's collection block (franchise membership,
// e.g. "Bad Boys Collection") into the matcher-facing CollectionDetail. The
// matcher find-or-creates a franchise row keyed on Name and points the movie's
// collection_id at it — see (*Matcher).linkCollection. Returns nil when the
// payload carries no named collection so the matcher skips linking.
func mapCollection(c *gen.Collection) *metadata.CollectionDetail {
	if c == nil || c.Name == "" {
		return nil
	}
	ext := map[string]string{}
	if c.Ids.Tmdb != 0 {
		ext["tmdb"] = strconv.FormatInt(c.Ids.Tmdb, 10)
	}
	poster := ""
	if c.Posters != nil && len(*c.Posters) > 0 {
		poster = (*c.Posters)[0].Url
	}
	backdrop := ""
	if c.Backdrops != nil && len(*c.Backdrops) > 0 {
		backdrop = (*c.Backdrops)[0].Url
	}
	var parts []metadata.CollectionPart
	if c.Parts != nil {
		parts = make([]metadata.CollectionPart, 0, len(*c.Parts))
		for _, p := range *c.Parts {
			parts = append(parts, metadata.CollectionPart{
				Title:       p.Title,
				Year:        intPtr64AsInt(p.Year),
				TmdbID:      intPtr64(p.TmdbId),
				PosterPath:  strPtr(p.PosterPath),
				VoteAverage: floatPtr(p.VoteAverage),
			})
		}
	}
	return &metadata.CollectionDetail{
		ExternalIDs:  ext,
		Name:         c.Name,
		Overview:     strPtr(c.Overview),
		PosterPath:   poster,
		BackdropPath: backdrop,
		Parts:        parts,
	}
}

// ---------------------------------------------------------------------------
// Artist
// ---------------------------------------------------------------------------

func mapArtistDoc(body *gen.ArtistDocBody) *metadata.MediaDetail {
	if body == nil {
		return nil
	}
	pay := &body.Payload

	yearStr := ""
	if body.Year != nil && *body.Year > 0 {
		yearStr = strconv.FormatInt(*body.Year, 10)
	}

	// Heya.media's artist payload doesn't expose an artwork tree — every
	// image (band photo, fanart, hand-picked promo) goes through the flat
	// payload.images list. Capture the first one as the primary poster so
	// the matcher's RefreshArtistResult.PosterURL stays populated for
	// downstream gap-fill.
	posterURL := strPtr(body.Poster)
	if posterURL == "" && pay.Images != nil && len(*pay.Images) > 0 {
		posterURL = (*pay.Images)[0].Url
	}

	extIDs := mergeExternalIDs(body.Ids, pay.ExternalIds)

	artistName := pay.Name
	sortTitle := strings.ToLower(coalesce(strPtr(pay.SortName), artistName))

	// Genres column: curated genres list + folksonomy tags, union of both.
	// The legacy mapper did this same merge so the matcher only has to
	// look at detail.Genres to surface the full label set.
	genres := strs(pay.Genres)
	if pay.Tags != nil {
		genres = mergeStrings(genres, *pay.Tags)
	}

	// Popularity: heya.media types the artist field as *int64 while the
	// movie/TV branch's analogue is *float64. The legacy mapper read it
	// as a float and stored it twice — once on the cross-kind float field
	// and once truncated onto ArtistPopularity. Mirror that so detail.
	// Popularity stays non-zero for artists (used in /api/discover
	// ranking, among other places).
	artistPop := intPtr64(pay.Popularity)

	detail := &metadata.MediaDetail{
		Title:        artistName,
		SortTitle:    sortTitle,
		Year:         yearStr,
		Description:  strPtr(pay.Profile),
		PosterURL:    posterURL,
		ExternalIDs:  extIDs,
		Genres:       genres,
		Popularity:   float64(artistPop),
		ProviderKind: body.Kind,
		HeyaSlug:     body.Slug,

		// Artist-specific.
		ArtistName:           artistName,
		ArtistBio:            strPtr(pay.Profile),
		ArtistSortName:       strPtr(pay.SortName),
		ArtistDisambiguation: strPtr(pay.Disambiguation),
		ArtistNativeName:     strPtr(pay.NativeName),
		ArtistNativeLanguage: strPtr(pay.NativeLanguage),
		ArtistCountry:        strPtr(pay.Country),
		ArtistType:           strPtr(pay.Type),
		ArtistGender:         strPtr(pay.Gender),
		ArtistBeginDate:      coalesce(strPtr(pay.Begin), strPtr(pay.Birthday)),
		ArtistBeginYear:      intPtr64AsInt(pay.BeginYear),
		ArtistBirthplace:     strPtr(pay.Birthplace),
		ArtistAliases:        strs(pay.Aliases),
		ArtistAnnotation:     strPtr(pay.Annotation),
		ArtistEndDate:        strPtr(pay.End),
		ArtistEnded:          boolPtr(pay.Ended),
		ArtistDeathday:       strPtr(pay.Deathday),
		ArtistListeners:      intPtr64(pay.Listeners),
		ArtistPlaycount:      intPtr64(pay.Playcount),
		// Popularity for movies/TV is a *float64; for artists it's *int64.
		// The artists.popularity column is int — store the upstream value
		// directly (no truncation needed, it's already integer).
		ArtistPopularity: int(artistPop),
		ArtistTags:       strs(pay.Tags),
		ArtistWikipedia:  mapStr(pay.WikipediaLinks),
		ArtistProfiles:   mapStr(pay.Profiles),
		ArtistImages:     mapArtworkItems(pay.Images, ""),
		ArtistURLs:       mapArtistURLs(pay.Urls),
		ArtistGroups:     mapArtistRelations(pay.Groups),
		ArtistMembers:    mapArtistRelations(pay.Members),
		ArtistTopTracks:  mapTopTracks(pay.TopTracks),
	}

	if pay.SimilarArtists != nil {
		detail.ArtistSimilarArtists = mapSimilarArtists(*pay.SimilarArtists)
	}

	detail.Albums = mapAlbums(pay.Albums)

	return detail
}

// ---------------------------------------------------------------------------
// Book (untyped payload; minimal envelope mapping)
// ---------------------------------------------------------------------------

func mapBookDoc(body *gen.BookDocBody) *metadata.MediaDetail {
	if body == nil {
		return nil
	}
	yearStr := ""
	if body.Year != nil && *body.Year > 0 {
		yearStr = strconv.FormatInt(*body.Year, 10)
	}
	return &metadata.MediaDetail{
		Title:        body.Title,
		SortTitle:    strings.ToLower(body.Title),
		Year:         yearStr,
		PosterURL:    strPtr(body.Poster),
		ExternalIDs:  mergeExternalIDs(body.Ids, nil),
		ProviderKind: body.Kind,
		HeyaSlug:     body.Slug,
	}
}

// ---------------------------------------------------------------------------
// Person — returns the legacy HeyaPersonResponse shape so person_worker.go
// keeps consuming `resp.Slug` / `resp.Payload.Name` / etc unchanged.
// ---------------------------------------------------------------------------

func mapPersonDoc(body *gen.PersonDocBody) *HeyaPersonResponse {
	if body == nil {
		return nil
	}
	out := &HeyaPersonResponse{
		ID:     body.Id,
		Kind:   body.Kind,
		Title:  body.Title,
		Slug:   body.Slug,
		Poster: strPtr(body.Poster),
		IDs:    flattenIDs(body.Ids),
	}
	if body.Year != nil {
		out.Year = int(*body.Year)
	}
	pay := &body.Payload
	out.Payload = HeyaPersonPayload{
		Name:               pay.Name,
		SortName:           strPtr(pay.SortName),
		AlsoKnownAs:        strs(pay.AlsoKnownAs),
		KnownForDepartment: strPtr(pay.KnownForDepartment),
		Gender:             strPtr(pay.Gender),
		Slug:               pay.Slug,
		Birthday:           strPtr(pay.Birthday),
		BirthYear:          intPtr64AsInt(pay.BirthYear),
		BirthPlace:         strPtr(pay.BirthPlace),
		Deathday:           strPtr(pay.Deathday),
		Biography:          strPtr(pay.Biography),
		Biographies:        mapStr(pay.Biographies),
		Profiles:           mapArtworkItemsLegacy(pay.Profiles),
		ExternalIDs:        mapStr(pay.ExternalIds),
		Popularity:         floatPtr(pay.Popularity),
		Homepage:           strPtr(pay.Homepage),
		Cast:               mapCredits(pay.Cast),
		Crew:               mapCredits(pay.Crew),
		KnownForTitles:     mapCredits(pay.KnownForTitles),
	}
	return out
}

// mapCredits translates the generated []gen.Credit into the legacy
// []HeyaCredit shape used by person_worker.go.
func mapCredits(items *[]gen.Credit) []HeyaCredit {
	return mapSlice(items, func(c gen.Credit) HeyaCredit {
		return HeyaCredit{
			Title:        c.Title,
			Year:         intPtr64AsInt(c.Year),
			Character:    strPtr(c.Character),
			Job:          strPtr(c.Job),
			Department:   strPtr(c.Department),
			Kind:         strPtr(c.Kind),
			Slug:         strPtr(c.Slug),
			TmdbID:       intPtr64AsInt(c.TmdbId),
			TvdbID:       intPtr64AsInt(c.TvdbId),
			ImdbID:       strPtr(c.ImdbId),
			PosterURL:    strPtr(c.PosterUrl),
			EpisodeCount: intPtr64AsInt(c.EpisodeCount),
			Order:        intPtr64AsInt(c.Order),
			Source:       strPtr(c.Source),
		}
	})
}

// flattenIDs converts the typed external-ID DTO to the legacy HeyaIDs shape
// (string + int fields) so person_worker.go's existing reads stay valid.
func flattenIDs(ids gen.ExternalIDsDTO) HeyaIDs {
	return HeyaIDs{
		IMDB:     strPtr(ids.Imdb),
		TMDB:     intPtr64AsInt(ids.Tmdb),
		TVDB:     intPtr64AsInt(ids.Tvdb),
		AniDB:    intPtr64AsInt(ids.Anidb),
		TVMaze:   intPtr64AsInt(ids.Tvmaze),
		TVRage:   intPtr64AsInt(ids.Tvrage),
		MAL:      intPtr64AsInt(ids.Mal),
		MBID:     strPtr(ids.Mbid),
		OLWorkID: strPtr(ids.OlWorkId),
	}
}

// mapArtworkItemsLegacy returns the legacy []HeyaArtworkItem shape, kept
// alive solely because person_worker.go reads it. New artwork mappers
// emit metadata.ArtworkResult directly — see mapArtworkItems.
func mapArtworkItemsLegacy(items *[]gen.ArtworkItem) []HeyaArtworkItem {
	return mapSlice(items, func(it gen.ArtworkItem) HeyaArtworkItem {
		return HeyaArtworkItem{
			URL:    it.Url,
			Source: it.Source,
			Aspect: strPtr(it.Aspect),
			Width:  intPtr64AsInt(it.Width),
			Height: intPtr64AsInt(it.Height),
			Score:  floatPtr(it.Score),
			Likes:  intPtr64AsInt(it.Likes),
		}
	})
}

// ---------------------------------------------------------------------------
// Shared sub-mappers
// ---------------------------------------------------------------------------

// mapSlice converts a generated *[]S into []D via fn, handling the
// nil/empty/allocate/loop shell every element mapper repeats. Returns nil for
// nil or empty input — the golden tests pin the resulting JSON.
func mapSlice[S, D any](in *[]S, fn func(S) D) []D {
	if in == nil || len(*in) == 0 {
		return nil
	}
	out := make([]D, 0, len(*in))
	for _, s := range *in {
		out = append(out, fn(s))
	}
	return out
}

// mergeExternalIDs collapses the typed top-level ExternalIDsDTO with the
// payload's free-form map. The legacy mapper used the same precedence:
// the typed top-level values land first, and the payload map only
// supplies keys the top-level didn't already cover.
func mergeExternalIDs(ids gen.ExternalIDsDTO, payloadExt *map[string]string) map[string]string {
	out := map[string]string{}
	if v := strPtr(ids.Imdb); v != "" {
		out["imdb"] = v
	}
	if v := intPtr64(ids.Tmdb); v != 0 {
		out["tmdb"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Tvdb); v != 0 {
		out["tvdb"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Anidb); v != 0 {
		out["anidb"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Tvmaze); v != 0 {
		out["tvmaze"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Tvrage); v != 0 {
		out["tvrage"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Mal); v != 0 {
		out["mal"] = strconv.FormatInt(v, 10)
	}
	if v := strPtr(ids.Mbid); v != "" {
		out["mbid"] = v
	}
	if v := strPtr(ids.OlWorkId); v != "" {
		out["ol_work_id"] = v
	}
	if v := intPtr64(ids.Discogs); v != 0 {
		out["discogs"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Deezer); v != 0 {
		out["deezer"] = strconv.FormatInt(v, 10)
	}
	if v := intPtr64(ids.Apple); v != 0 {
		out["apple"] = strconv.FormatInt(v, 10)
	}
	if payloadExt != nil {
		for k, v := range *payloadExt {
			if v != "" && out[k] == "" {
				out[k] = v
			}
		}
	}
	return out
}

func mapLocalizedTitles(titles *[]gen.LocalizedTitle) []metadata.TitleEntry {
	return mapSlice(titles, func(t gen.LocalizedTitle) metadata.TitleEntry {
		return metadata.TitleEntry{
			Title:     t.Title,
			Language:  strPtr(t.Language),
			Country:   strPtr(t.Country),
			TitleType: strPtr(t.Type),
			Source:    strPtr(t.Source),
		}
	})
}

func mapCast(cast *[]gen.Cast) []metadata.CastMember {
	return mapSlice(cast, func(c gen.Cast) metadata.CastMember {
		profiles := mapProfileItems(c.ProfileUrls)
		profilePath := ""
		if len(profiles) > 0 {
			profilePath = profiles[0].URL
		}
		return metadata.CastMember{
			ExternalIDs: copyStringMap(mapStr(c.ExternalIds)),
			Name:        c.Name,
			Character:   strPtr(c.Character),
			Order:       intPtr64AsInt(c.Order),
			Gender:      genderStringToInt(strPtr(c.Gender)),
			ProfilePath: profilePath,
			Profiles:    profiles,
			Popularity:  floatPtr(c.Popularity),
			Source:      strPtr(c.Source),
		}
	})
}

func mapCrew(crew *[]gen.Crew) []metadata.CrewMember {
	return mapSlice(crew, func(c gen.Crew) metadata.CrewMember {
		profiles := mapProfileItems(c.ProfileUrls)
		profilePath := ""
		if len(profiles) > 0 {
			profilePath = profiles[0].URL
		}
		return metadata.CrewMember{
			ExternalIDs: copyStringMap(mapStr(c.ExternalIds)),
			Name:        c.Name,
			Job:         strPtr(c.Job),
			Department:  strPtr(c.Department),
			Gender:      genderStringToInt(strPtr(c.Gender)),
			ProfilePath: profilePath,
			Profiles:    profiles,
			Source:      strPtr(c.Source),
		}
	})
}

// mapProfileItems handles the cast/crew profile_urls — different return
// shape (metadata.ProfileImage) than the artist images list.
func mapProfileItems(items *[]gen.ArtworkItem) []metadata.ProfileImage {
	if items == nil {
		return nil
	}
	out := make([]metadata.ProfileImage, 0, len(*items))
	for _, it := range *items {
		if it.Url == "" {
			continue
		}
		out = append(out, metadata.ProfileImage{
			URL:    it.Url,
			Source: it.Source,
			Aspect: strPtr(it.Aspect),
			Width:  intPtr64AsInt(it.Width),
			Height: intPtr64AsInt(it.Height),
			Score:  floatPtr(it.Score),
			Likes:  intPtr64AsInt(it.Likes),
		})
	}
	return out
}

// mapArtworkItems is the metadata.ArtworkResult version, used by the
// artist images pool and FetchArtwork.
func mapArtworkItems(items *[]gen.ArtworkItem, assetType string) []metadata.ArtworkResult {
	return mapSlice(items, func(it gen.ArtworkItem) metadata.ArtworkResult {
		return metadata.ArtworkResult{
			URL:       it.Url,
			AssetType: assetType,
			Language:  strPtr(it.Language),
			Source:    it.Source,
			Likes:     intPtr64AsInt(it.Likes),
			Score:     floatPtr(it.Score),
			Width:     intPtr64AsInt(it.Width),
			Height:    intPtr64AsInt(it.Height),
			Aspect:    strPtr(it.Aspect),
		}
	})
}

func mapKeywords(keywords *[]gen.Keyword) []metadata.KeywordDetail {
	return mapSlice(keywords, func(k gen.Keyword) metadata.KeywordDetail {
		var kIDs map[string]string
		if id := intPtr64(k.TmdbId); id != 0 {
			kIDs = map[string]string{"tmdb": strconv.FormatInt(id, 10)}
		}
		return metadata.KeywordDetail{
			ExternalIDs: kIDs,
			Name:        k.Name,
		}
	})
}

func mapVideos(videos *[]gen.Video) []metadata.VideoDetail {
	return mapSlice(videos, func(v gen.Video) metadata.VideoDetail {
		return metadata.VideoDetail{
			ProviderKey: strPtr(v.Source),
			Name:        strPtr(v.Name),
			Site:        strPtr(v.Site),
			Key:         strPtr(v.Key),
			Type:        strPtr(v.Type),
			Language:    strPtr(v.Language),
			Official:    boolPtr(v.Official),
			PublishedAt: strPtr(v.PublishedAt),
		}
	})
}

func mapContentRatings(crs *[]gen.ContentRating) []metadata.CertificationDetail {
	return mapSlice(crs, func(cr gen.ContentRating) metadata.CertificationDetail {
		return metadata.CertificationDetail{
			Country:       strPtr(cr.Country),
			Certification: cr.Rating,
			Source:        strPtr(cr.Source),
		}
	})
}

func mapRecommendations(recs *[]gen.Recommendation) []metadata.RecommendationDetail {
	return mapSlice(recs, func(r gen.Recommendation) metadata.RecommendationDetail {
		// The generated client surfaces TmdbId directly — wrap it back
		// into the external_ids map the matcher persists.
		var ext map[string]string
		if id := intPtr64(r.TmdbId); id != 0 {
			ext = map[string]string{"tmdb": strconv.FormatInt(id, 10)}
		}
		// Year vs release_date: the legacy heyaRecEntry carried
		// ReleaseDate string; the generated model only exposes Year.
		// Format as "YYYY" so the column is non-empty when we have it.
		releaseDate := ""
		if y := intPtr64(r.Year); y > 0 {
			releaseDate = strconv.FormatInt(y, 10)
		}
		return metadata.RecommendationDetail{
			ExternalIDs: ext,
			Title:       r.Title,
			PosterPath:  strPtr(r.PosterPath),
			MediaType:   strPtr(r.MediaType),
			VoteAverage: floatPtr(r.VoteAverage),
			ReleaseDate: releaseDate,
		}
	})
}

func mapStudios(refs *[]gen.NamedRef) []metadata.ProductionCompanyDetail {
	return mapSlice(refs, func(s gen.NamedRef) metadata.ProductionCompanyDetail {
		var sIDs map[string]string
		if id := intPtr64(s.Id); id != 0 {
			sIDs = map[string]string{strPtr(s.Source): strconv.FormatInt(id, 10)}
		}
		return metadata.ProductionCompanyDetail{
			ExternalIDs:   sIDs,
			Name:          s.Name,
			LogoPath:      strPtr(s.LogoUrl),
			OriginCountry: strPtr(s.Country),
		}
	})
}

func mapNamedRefs(refs *[]gen.NamedRef) []metadata.NetworkDetail {
	return mapSlice(refs, func(n gen.NamedRef) metadata.NetworkDetail {
		nd := metadata.NetworkDetail{Name: n.Name}
		if id := intPtr64(n.Id); id != 0 {
			nd.ExternalIDs = map[string]string{"tmdb": strconv.FormatInt(id, 10)}
		}
		return nd
	})
}

func mapCreators(refs *[]gen.NamedRef) []metadata.CreatorDetail {
	if refs == nil {
		return nil
	}
	out := make([]metadata.CreatorDetail, 0, len(*refs))
	for _, c := range *refs {
		cd := metadata.CreatorDetail{Name: c.Name}
		if id := intPtr64(c.Id); id != 0 {
			cd.ExternalIDs = map[string]string{"tmdb": strconv.FormatInt(id, 10)}
		}
		out = append(out, cd)
	}
	return out
}

func mapSeasons(seasons []gen.Season) []metadata.SeasonDetail {
	out := make([]metadata.SeasonDetail, 0, len(seasons))
	for _, s := range seasons {
		posterURL := ""
		if s.PosterUrls != nil && len(*s.PosterUrls) > 0 {
			posterURL = (*s.PosterUrls)[0].Url
		}
		episodes := []metadata.EpisodeDetail{}
		if s.Episodes != nil {
			episodes = mapEpisodes(*s.Episodes)
		}
		out = append(out, metadata.SeasonDetail{
			Number:        intPtr64AsInt(&s.Number),
			Title:         strPtr(s.Name),
			Overview:      strPtr(s.Overview),
			PosterURL:     posterURL,
			AirDate:       strPtr(s.AirDate),
			EndDate:       strPtr(s.EndDate),
			Status:        strPtr(s.Status),
			AiredEpisodes: intPtr64AsInt(s.AiredEpisodes),
			TmdbSeasonID:  intPtr64AsInt(s.TmdbSeasonId),
			TvdbSeasonID:  intPtr64AsInt(s.TvdbSeasonId),
			AnidbID:       intPtr64AsInt(s.AnidbId),
			Episodes:      episodes,
		})
	}
	return out
}

func mapEpisodes(eps []gen.Episode) []metadata.EpisodeDetail {
	out := make([]metadata.EpisodeDetail, 0, len(eps))
	for _, ep := range eps {
		stillURL := ""
		if ep.StillUrls != nil && len(*ep.StillUrls) > 0 {
			stillURL = (*ep.StillUrls)[0].Url
		}
		var epRating float64
		if ep.Ratings != nil && len(*ep.Ratings) > 0 {
			epRating = (*ep.Ratings)[0].Value
		}
		var epTitles []metadata.TitleEntry
		if ep.Titles != nil {
			epTitles = mapLocalizedTitles(ep.Titles)
		}
		out = append(out, metadata.EpisodeDetail{
			Number:         intPtr64AsInt(&ep.Number),
			Title:          strPtr(ep.Name),
			Titles:         epTitles,
			Overview:       strPtr(ep.Overview),
			Overviews:      mapStr(ep.Overviews),
			StillURL:       stillURL,
			RuntimeMinutes: intPtr64AsInt(ep.Runtime),
			AirDate:        strPtr(ep.AirDate),
			Rating:         epRating,
			AbsoluteNumber: intPtr64AsInt(ep.AbsoluteNumber),
			IsSpecial:      boolPtr(ep.IsSpecial),
			EpisodeType:    intPtr64AsInt(ep.Type),
			TmdbID:         intPtr64AsInt(ep.TmdbId),
			TvdbID:         intPtr64AsInt(ep.TvdbId),
			Source:         strPtr(ep.Source),
		})
	}
	return out
}

func mapArtistURLs(urls *[]gen.ArtistURL) []metadata.URLEntry {
	return mapSlice(urls, func(u gen.ArtistURL) metadata.URLEntry {
		return metadata.URLEntry{Type: u.Type, URL: u.Url}
	})
}

func mapArtistRelations(rels *[]gen.ArtistMember) []metadata.ArtistRelationEntry {
	return mapSlice(rels, func(r gen.ArtistMember) metadata.ArtistRelationEntry {
		return metadata.ArtistRelationEntry{
			Name:  r.Name,
			MBID:  strPtr(r.Mbid),
			Slug:  strPtr(r.Slug),
			Begin: strPtr(r.Begin),
			End:   strPtr(r.End),
			Ended: boolPtr(r.Ended),
			Roles: strs(r.Roles),
		}
	})
}

func mapTopTracks(tops *[]gen.TopTrack) []metadata.TopTrackEntry {
	return mapSlice(tops, func(t gen.TopTrack) metadata.TopTrackEntry {
		return metadata.TopTrackEntry{
			Title:     t.Title,
			MBID:      strPtr(t.Mbid),
			Playcount: intPtr64(t.Playcount),
			Listeners: intPtr64(t.Listeners),
			URL:       strPtr(t.Url),
		}
	})
}

func mapSimilarArtists(sims []gen.SimilarArtist) []metadata.SimilarArtistEntry {
	out := make([]metadata.SimilarArtistEntry, 0, len(sims))
	for _, s := range sims {
		out = append(out, metadata.SimilarArtistEntry{
			Name:  s.Name,
			MBID:  strPtr(s.Mbid),
			Match: floatPtr(s.Match),
			URL:   strPtr(s.Url),
		})
	}
	return out
}

func mapAlbums(albums *[]gen.Album) []metadata.AlbumEntry {
	if albums == nil || len(*albums) == 0 {
		return nil
	}
	out := make([]metadata.AlbumEntry, 0, len(*albums))
	for _, a := range *albums {
		coverURL := ""
		if a.Artwork != nil && len(*a.Artwork) > 0 {
			coverURL = (*a.Artwork)[0].Url
		}
		out = append(out, metadata.AlbumEntry{
			Title:          a.Title,
			OriginalTitle:  strPtr(a.OriginalTitle),
			Type:           strPtr(a.Type),
			SecondaryTypes: strs(a.SecondaryTypes),
			ReleaseDate:    strPtr(a.ReleaseDate),
			Year:           intPtr64AsInt(a.Year),
			Label:          strPtr(a.Label),
			CatalogNo:      strPtr(a.CatalogNo),
			Country:        strPtr(a.Country),
			Language:       strPtr(a.Language),
			Barcode:        strPtr(a.Barcode),
			ISRCs:          strs(a.Isrcs),
			ExternalIDs:    mapStr(a.ExternalIds),
			TrackCount:     intPtr64AsInt(a.TrackCount),
			Duration:       intPtr64AsInt(a.Duration),
			Explicit:       boolPtr(a.Explicit),
			Genres:         strs(a.Genres),
			Styles:         strs(a.Styles),
			Tags:           strs(a.Tags),
			Rating:         floatPtr(a.Rating),
			Popularity:     floatPtr64AsFloat(a.Popularity),
			Listeners:      intPtr64(a.Listeners),
			Playcount:      intPtr64(a.Playcount),
			ArtistCredits:  mapArtistCredits(a.Artists),
			CoverURL:       coverURL,
			Tracks:         mapAlbumTracks(a.Tracks),
		})
	}
	return out
}

// floatPtr64AsFloat normalises the artist-album popularity which the spec
// types as *int64 (whole numbers from the upstream rating systems), back
// into the float64 our AlbumEntry exposes. We only get whole numbers, but
// the downstream column is NUMERIC(4,2) so it tolerates either.
func floatPtr64AsFloat(p *int64) float64 {
	if p == nil {
		return 0
	}
	return float64(*p)
}

func mapAlbumTracks(tracks *[]gen.Track) []metadata.TrackDetail {
	if tracks == nil || len(*tracks) == 0 {
		return nil
	}
	out := make([]metadata.TrackDetail, 0, len(*tracks))
	for _, t := range *tracks {
		disc := intPtr64AsInt(t.Disc)
		if disc == 0 {
			disc = 1
		}
		out = append(out, metadata.TrackDetail{
			DiscNumber:    disc,
			TrackNumber:   intPtr64AsInt(t.Position),
			Title:         t.Title,
			Duration:      intPtr64AsInt(t.Duration),
			ISRC:          strPtr(t.Isrc),
			RecordingMBID: strPtr(t.RecordingMbid),
			PreviewURL:    strPtr(t.Preview),
			ExternalIDs:   mapStr(t.ExternalIds),
			Explicit:      boolPtr(t.Explicit),
			ArtistCredits: mapArtistCredits(t.Artists),
		})
	}
	return out
}

func mapArtistCredits(credits *[]gen.ArtistCredit) []metadata.ArtistCreditEntry {
	return mapSlice(credits, func(c gen.ArtistCredit) metadata.ArtistCreditEntry {
		return metadata.ArtistCreditEntry{
			Name:       c.Name,
			MBID:       strPtr(c.Mbid),
			Slug:       strPtr(c.Slug),
			JoinPhrase: strPtr(c.JoinPhrase),
		}
	})
}

// mapArtwork is the FetchArtwork helper — flattens the per-asset-type
// artwork buckets into a single typed []ArtworkResult.
func mapArtwork(art *gen.Artwork) []metadata.ArtworkResult {
	if art == nil {
		return nil
	}
	var results []metadata.ArtworkResult
	results = append(results, mapArtworkItems(art.Posters, "poster")...)
	results = append(results, mapArtworkItems(art.Backdrops, "backdrop")...)
	results = append(results, mapArtworkItems(art.Logos, "logo")...)
	results = append(results, mapArtworkItems(art.Banners, "banner")...)
	results = append(results, mapArtworkItems(art.Clearart, "clearart")...)
	results = append(results, mapArtworkItems(art.Thumbnails, "thumb")...)
	results = append(results, mapArtworkItems(art.DiscArt, "disc")...)
	return results
}

// mapRatings collapses the per-source rating array into the typed
// RatingsData struct.
func mapRatings(ratings *[]gen.Rating) *metadata.RatingsData {
	if ratings == nil || len(*ratings) == 0 {
		return nil
	}
	out := make([]metadata.ExternalRating, 0, len(*ratings))
	for _, r := range *ratings {
		out = append(out, metadata.ExternalRating{
			Source:   r.Source,
			Value:    fmt.Sprintf("%.1f", r.Value),
			Score:    r.Value,
			Votes:    intPtr64AsInt(r.Votes),
			RawValue: strPtr(r.Raw),
		})
	}
	return &metadata.RatingsData{Ratings: out}
}

func countEpisodesGen(seasons []gen.Season) int {
	n := 0
	for _, s := range seasons {
		if s.Episodes != nil {
			n += len(*s.Episodes)
		}
	}
	return n
}
