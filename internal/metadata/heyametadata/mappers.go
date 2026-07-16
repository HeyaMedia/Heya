package heyametadata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/rs/zerolog/log"
)

func (p *HeyaProvider) mapDocument(ctx context.Context, body []byte) (*metadata.MediaDetail, error) {
	var header canonicalHeader
	if err := json.Unmarshal(body, &header); err != nil {
		return nil, fmt.Errorf("decode canonical document header: %w", err)
	}
	var (
		detail *metadata.MediaDetail
		err    error
	)
	switch header.Kind {
	case "movie":
		var document movieDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail = p.mapMovie(document)
	case "tv_show", "anime":
		var document episodicDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail = p.mapEpisodic(document)
	case "artist":
		var document artistDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail, err = p.mapArtist(ctx, document)
		if err != nil {
			return nil, err
		}
	case "release_group":
		var document releaseGroupDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail = p.mapReleaseGroup(document)
	case "release":
		var document releaseDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail = p.mapRelease(document)
	case "book_work", "book_edition", "manga_volume", "manga_edition", "comic_volume", "comic_edition":
		var document bookDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		detail = p.mapBook(document)
	default:
		return nil, fmt.Errorf("heyametadata: unsupported canonical kind %q", header.Kind)
	}
	return detail, nil
}

func commonDetail(header canonicalHeader) *metadata.MediaDetail {
	return &metadata.MediaDetail{
		CanonicalID: header.ID, CanonicalKind: header.Kind,
		SchemaVersion: header.SchemaVersion, ProjectionVersion: header.ProjectionVersion,
		ProviderKind: "heya", HeyaSlug: header.ID,
		ExternalIDs: flattenExternalIDs(header.ExternalIDs),
	}
}

func (p *HeyaProvider) mapMovie(document movieDocument) *metadata.MediaDetail {
	detail := commonDetail(document.canonicalHeader)
	detail.Title = document.Display.Title
	detail.OriginalTitle = document.Display.OriginalTitle
	detail.SortTitle = document.Display.Title
	detail.Year = yearString(document.Display.Year)
	detail.PosterURL = p.client.ImageURL(document.Display.ImageID)
	detail.Titles = mapLocalizedTitles(document.Data.Titles)
	detail.Description, detail.Overviews = localizedOverview(document.Data.Overviews)
	detail.Genres = document.Data.Classification.Genres
	detail.OriginalLanguage = document.Data.Classification.OriginalLanguage
	detail.SpokenLanguages = document.Data.Classification.SpokenLanguages
	detail.OriginCountry = document.Data.Classification.Countries
	detail.MovieStatus = document.Data.Release.NormalizedStatus
	detail.Status = document.Data.Release.NormalizedStatus
	for _, event := range document.Data.Release.ReleaseEvents {
		if detail.ReleaseDate == "" || event.Type == "theatrical" || event.Type == "premiere" {
			detail.ReleaseDate = event.Date
		}
		if event.Certification != "" {
			detail.Certifications = append(detail.Certifications, metadata.CertificationDetail{Country: event.Country, Certification: event.Certification, ReleaseDate: event.Date, Source: "heyametadata"})
		}
	}
	if len(document.Data.Taglines) > 0 {
		detail.Tagline = document.Data.Taglines[0].Value
	}
	if document.Data.Measurements.RuntimeMinutes != nil {
		detail.RuntimeMinutes = *document.Data.Measurements.RuntimeMinutes
	}
	if document.Data.Measurements.Budget != nil {
		detail.Budget = document.Data.Measurements.Budget.Amount
	}
	if document.Data.Measurements.Revenue != nil {
		detail.Revenue = document.Data.Measurements.Revenue.Amount
	}
	if document.Data.Measurements.Popularity != nil {
		detail.Popularity = *document.Data.Measurements.Popularity
	}
	detail.Rating, detail.VoteCount = preferredRating(document.Data.Ratings)
	detail.Artwork = p.mapImages(document.Data.Images)
	detail.BackdropURL = firstArtworkURL(detail.Artwork, "backdrop")
	for _, studio := range document.Data.Studios {
		detail.ProductionCompanies = append(detail.ProductionCompanies, metadata.ProductionCompanyDetail{
			ExternalIDs: map[string]string{"provider_id": studio.ProviderID}, Name: studio.Name,
			LogoPath: p.client.ImageURL(studio.LogoImageID), OriginCountry: studio.Country,
		})
	}
	detail.Cast, detail.Crew = p.mapCredits(document.Data.Credits)
	for _, keyword := range document.Data.Classification.Keywords {
		detail.Keywords = append(detail.Keywords, metadata.KeywordDetail{Name: keyword})
	}
	for _, video := range document.Data.Videos {
		detail.Videos = append(detail.Videos, metadata.VideoDetail{ProviderKey: video.Key, Name: video.Name, Site: video.Host, Key: video.Key, Type: video.Type, Language: video.Language, Official: video.Official, PublishedAt: video.PublishedAt})
	}
	for _, link := range document.Data.Links {
		if strings.EqualFold(link.Kind, "homepage") {
			detail.Homepage = link.Value
			break
		}
	}
	for _, recommendation := range document.Data.Recommendations {
		external := map[string]string{recommendation.Provider: recommendation.ProviderTargetID}
		if recommendation.EntityID != "" {
			external["heyametadata"] = recommendation.EntityID
		}
		detail.Recommendations = append(detail.Recommendations, metadata.RecommendationDetail{
			CanonicalID: recommendation.EntityID, ExternalIDs: external,
			Title: recommendation.Title, PosterPath: p.client.ImageURL(recommendation.ImageID), MediaType: "movie", ProviderScore: recommendation.ProviderScore,
		})
	}
	if collection := document.Data.Collection; collection != nil {
		mapped := &metadata.CollectionDetail{ExternalIDs: map[string]string{"provider_id": collection.ProviderID}, Name: collection.Name, Overview: collection.Overview}
		for _, image := range collection.Images {
			switch image.Class {
			case "poster":
				if mapped.PosterPath == "" {
					mapped.PosterPath = p.client.ImageURL(image.ID)
				}
			case "backdrop":
				if mapped.BackdropPath == "" {
					mapped.BackdropPath = p.client.ImageURL(image.ID)
				}
			}
		}
		for _, member := range collection.Members {
			part := metadata.CollectionPart{Title: member.Title, Year: member.Year, PosterPath: p.client.ImageURL(member.ImageID)}
			part.TmdbID, _ = strconv.ParseInt(member.ProviderID, 10, 64)
			mapped.Parts = append(mapped.Parts, part)
		}
		detail.Collection = mapped
	}
	return detail
}

func (p *HeyaProvider) mapEpisodic(document episodicDocument) *metadata.MediaDetail {
	detail := commonDetail(document.canonicalHeader)
	detail.Title = document.Display.Title
	detail.OriginalTitle = document.Display.OriginalTitle
	detail.OriginalName = document.Display.OriginalTitle
	detail.SortTitle = document.Display.Title
	detail.Year = yearString(document.Display.Year)
	detail.PosterURL = p.client.ImageURL(document.Display.ImageID)
	detail.Titles = mapLocalizedTitles(document.Data.Titles)
	detail.Description = firstNonEmpty(document.Data.Overview, firstLocalized(document.Data.Overviews))
	_, detail.Overviews = localizedOverview(document.Data.Overviews)
	detail.Status = document.Data.Classification.Status
	detail.FirstAirDate = document.Data.Lifecycle.StartDate
	detail.LastAirDate = document.Data.Lifecycle.EndDate
	detail.OriginalLanguage = document.Data.Classification.Language
	detail.OriginCountry = document.Data.Classification.Countries
	detail.Genres = document.Data.Classification.Genres
	detail.RuntimeMinutes = document.Data.RuntimeMinutes
	detail.NumberOfEpisodes = document.Data.EpisodeCount
	detail.NumberOfSeasons = document.Data.SeasonCount
	detail.Rating, detail.VoteCount = preferredRating(document.Data.Ratings)
	detail.Artwork = p.mapImages(document.Data.Images)
	detail.BackdropURL = firstArtworkURL(detail.Artwork, "backdrop")
	for _, network := range document.Data.Networks {
		detail.Networks = append(detail.Networks, metadata.NetworkDetail{CanonicalID: network.EntityID, ExternalIDs: flattenExternalIDs(network.ExternalIDs), Name: network.Name, LogoPath: p.client.ImageURL(network.LogoImageID), Country: network.Country})
	}
	detail.Cast, detail.Crew = p.mapCredits(document.Data.Credits)
	for _, crew := range detail.Crew {
		if strings.EqualFold(crew.Job, "creator") || strings.EqualFold(crew.Job, "created by") {
			detail.CreatedBy = append(detail.CreatedBy, metadata.CreatorDetail{CanonicalID: crew.CanonicalID, ExternalIDs: crew.ExternalIDs, Name: crew.Name})
		}
	}
	for _, keyword := range document.Data.Keywords {
		detail.Keywords = append(detail.Keywords, metadata.KeywordDetail{Name: keyword})
	}
	for _, video := range document.Data.Videos {
		detail.Videos = append(detail.Videos, metadata.VideoDetail{ProviderKey: video.Key, Name: video.Name, Site: video.Provider, Key: video.Key, Type: video.Type, Language: video.Language, Official: video.Official})
	}
	for _, cert := range document.Data.Certifications {
		detail.Certifications = append(detail.Certifications, metadata.CertificationDetail{Country: cert.Country, Certification: cert.Rating, ReleaseType: cert.Order, Source: cert.System})
	}
	for _, rec := range document.Data.Recommendations {
		external := flattenExternalIDs(rec.ExternalIDs)
		if external == nil {
			external = map[string]string{}
		}
		if rec.EntityID != "" {
			external["heyametadata"] = rec.EntityID
		}
		detail.Recommendations = append(detail.Recommendations, metadata.RecommendationDetail{CanonicalID: rec.EntityID, ExternalIDs: external, Title: rec.Title, PosterPath: p.client.ImageURL(rec.ImageID), MediaType: legacyKind(document.Kind), ProviderScore: rec.ProviderScore, ReleaseDate: rec.FirstAirDate})
	}

	episodesBySeason := map[string][]episodicEpisode{}
	for _, episode := range document.Data.Episodes {
		episodesBySeason[episode.SeasonID] = append(episodesBySeason[episode.SeasonID], episode)
	}
	for _, season := range document.Data.Seasons {
		mapped := metadata.SeasonDetail{CanonicalID: season.ID, Number: season.Number, Title: firstNonEmpty(season.Name, firstLocalized(season.Titles)), Overview: firstLocalized(season.Overviews), AirDate: season.PremiereDate, EndDate: season.EndDate, Status: season.Status, AiredEpisodes: season.AiredEpisodeCount, PosterURL: p.imageByClass(season.Images, "poster")}
		for _, id := range season.ExternalIDs {
			value, _ := strconv.Atoi(id.Value)
			switch id.Provider {
			case "tmdb":
				mapped.TmdbSeasonID = value
			case "tvdb":
				mapped.TvdbSeasonID = value
			case "anidb":
				mapped.AnidbID = value
			}
		}
		for _, episode := range episodesBySeason[season.ID] {
			mapped.Episodes = append(mapped.Episodes, p.mapEpisode(episode, season.Number, document.Kind))
		}
		sort.SliceStable(mapped.Episodes, func(i, j int) bool { return mapped.Episodes[i].Number < mapped.Episodes[j].Number })
		detail.Seasons = append(detail.Seasons, mapped)
	}
	return detail
}

func (p *HeyaProvider) mapEpisode(episode episodicEpisode, fallbackSeason int, kind string) metadata.EpisodeDetail {
	number, absolute, provider := 0, 0, ""
	bestPriority := 999
	numbers := make([]metadata.EpisodeNumber, 0, len(episode.Numbers))
	for _, value := range episode.Numbers {
		numbers = append(numbers, metadata.EpisodeNumber{
			Scheme:   value.Scheme,
			Season:   value.Season,
			Number:   value.Number,
			Provider: value.Provider,
		})
		if value.Scheme == "absolute" {
			absolute = int(value.Number)
			continue
		}
		priority := 10
		if value.Scheme == "aired" {
			priority = 0
		}
		if kind == "anime" && value.Scheme == "anidb" {
			priority = 1
		}
		if kind == "tv_show" && value.Scheme == "tvmaze" {
			priority = 1
		}
		if value.Season == fallbackSeason && priority < bestPriority {
			number = int(value.Number)
			provider = firstNonEmpty(value.Provider, value.Scheme)
			bestPriority = priority
		}
	}
	if number == 0 && len(episode.Numbers) > 0 {
		number = int(episode.Numbers[0].Number)
		provider = firstNonEmpty(episode.Numbers[0].Provider, episode.Numbers[0].Scheme)
	}
	ratingValue, votes := preferredRating(episode.Ratings)
	result := metadata.EpisodeDetail{CanonicalID: episode.ID, Number: number, Numbers: numbers, Title: firstNonEmpty(firstLocalized(episode.Titles), "Episode "+strconv.Itoa(number)), Titles: mapLocalizedTitles(episode.Titles), Overview: firstNonEmpty(firstLocalized(episode.Overviews), episode.Summary), RuntimeMinutes: episode.RuntimeMinutes, AirDate: episode.AirDate, Rating: ratingValue, VoteCount: votes, AbsoluteNumber: absolute, IsSpecial: episode.IsSpecial, EpisodeType: episodeTypeNumber(episode.EpisodeType), StillURL: p.imageByClass(episode.Images, "still"), Source: provider}
	_, result.Overviews = localizedOverview(episode.Overviews)
	for _, id := range episode.ExternalIDs {
		value, _ := strconv.Atoi(id.Value)
		switch id.Provider {
		case "tmdb":
			result.TmdbID = value
		case "tvdb":
			result.TvdbID = value
		}
	}
	return result
}

func episodeTypeNumber(value string) int {
	switch strings.ToLower(value) {
	case "special":
		return 2
	case "ova", "oad":
		return 3
	default:
		return 1
	}
}

func (p *HeyaProvider) mapArtist(ctx context.Context, document artistDocument) (*metadata.MediaDetail, error) {
	detail := commonDetail(document.canonicalHeader)
	detail.Title = document.Display.Name
	detail.ArtistName = document.Display.Name
	detail.SortTitle = document.Display.Name
	detail.ArtistDisambiguation = document.Display.Disambiguation
	detail.PosterURL = p.client.ImageURL(document.Display.ImageID)
	detail.ArtistType = document.Data.Classification.ArtistType
	detail.ArtistGender = document.Data.Classification.Gender
	for _, name := range document.Data.Names {
		if name.Value != document.Display.Name {
			detail.ArtistAliases = append(detail.ArtistAliases, name.Value)
		}
		if name.Primary && detail.ArtistSortName == "" {
			detail.ArtistSortName = name.SortValue
		}
		if name.Type == "native" {
			detail.ArtistNativeName, detail.ArtistNativeLanguage = name.Value, name.Language
		}
	}
	// TheAudioDB ships up to 12 language variants and upstream order is not
	// language-sorted — prefer English (or untagged) over positional first.
	for _, bio := range document.Data.Biographies {
		if detail.ArtistBio == "" {
			detail.ArtistBio = bio.Value
		}
		if bio.Language == "" || strings.HasPrefix(strings.ToLower(bio.Language), "en") {
			detail.ArtistBio = bio.Value
			break
		}
	}
	if len(document.Data.Annotations) > 0 {
		detail.ArtistAnnotation = document.Data.Annotations[0].Value
	}
	for _, area := range document.Data.Areas {
		if area.Role == "begin" {
			detail.ArtistBirthplace = area.Name
		}
		if detail.ArtistCountry == "" && len(area.ISOCodes) > 0 {
			detail.ArtistCountry = area.ISOCodes[0]
		}
	}
	for _, date := range document.Data.Lifecycle.Dates {
		switch date.Type {
		case "begin", "birth":
			detail.ArtistBeginDate = date.Value
			if len(date.Value) >= 4 {
				detail.ArtistBeginYear, _ = strconv.Atoi(date.Value[:4])
			}
		case "end", "death":
			detail.ArtistEndDate, detail.ArtistDeathday = date.Value, date.Value
		}
	}
	if document.Data.Lifecycle.Ended != nil {
		detail.ArtistEnded = *document.Data.Lifecycle.Ended
	}
	// Multiple providers repeat the same genre/tag with different casing
	// ("rock" / "Rock") — dedupe case-insensitively, keeping first-seen
	// casing and upstream order (weight-ranked within each provider).
	seenGenres := map[string]struct{}{}
	for _, genre := range document.Data.Genres {
		key := strings.ToLower(strings.TrimSpace(genre.Name))
		if _, dup := seenGenres[key]; dup || key == "" {
			continue
		}
		seenGenres[key] = struct{}{}
		detail.Genres = append(detail.Genres, genre.Name)
	}
	seenTags := map[string]struct{}{}
	for _, tag := range document.Data.Tags {
		key := strings.ToLower(strings.TrimSpace(tag.Name))
		if _, dup := seenTags[key]; dup || key == "" {
			continue
		}
		seenTags[key] = struct{}{}
		detail.ArtistTags = append(detail.ArtistTags, tag.Name)
	}
	for _, link := range document.Data.Links {
		detail.ArtistURLs = append(detail.ArtistURLs, metadata.URLEntry{Type: link.Type, URL: link.URL})
	}
	detail.ArtistImages = p.mapImages(document.Data.Images)
	detail.Artwork = detail.ArtistImages
	for _, metric := range document.Data.Metrics {
		switch strings.ToLower(metric.Name) {
		case "listeners":
			detail.ArtistListeners = int64(metric.Value)
		case "playcount":
			detail.ArtistPlaycount = int64(metric.Value)
		case "popularity":
			// Reported on different scales (tidal 0..1, audiodb 0..100) —
			// int-truncating the fractional scale would zero the field, so
			// keep the largest value seen.
			if v := int(metric.Value); v > detail.ArtistPopularity {
				detail.ArtistPopularity = v
			}
		}
	}
	for _, relation := range document.Data.Relationships {
		entry := metadata.ArtistRelationEntry{Name: relation.TargetName, MBID: relation.TargetID, Begin: relation.BeginDate, End: relation.EndDate, Roles: relation.Attributes}
		if relation.Ended != nil {
			entry.Ended = *relation.Ended
		}
		if relation.Direction == "backward" {
			detail.ArtistGroups = append(detail.ArtistGroups, entry)
		} else {
			detail.ArtistMembers = append(detail.ArtistMembers, entry)
		}
	}
	for provider := range document.Freshness.Providers {
		detail.ArtistMetadataSources = append(detail.ArtistMetadataSources, provider)
	}
	sort.Strings(detail.ArtistMetadataSources)
	for _, item := range document.Data.SimilarArtists {
		entry := metadata.SimilarArtistEntry{Name: item.Name, Match: item.Score, URL: item.URL, Provider: item.Provider}
		if entry.Provider == "" {
			entry.Provider = "lastfm"
		}
		// provider_id is only an MBID for Last.fm rows (deezer/tidal send
		// their own numeric ids) — and Last.fm omits it for artists it
		// can't resolve, so shape-check before trusting it.
		if entry.Provider == "lastfm" && looksLikeUUID(item.ProviderID) {
			entry.MBID = item.ProviderID
		}
		detail.ArtistSimilarArtists = append(detail.ArtistSimilarArtists, entry)
	}
	for _, video := range document.Data.MusicVideos {
		key := youtubeVideoKey(video.URL)
		if key == "" {
			continue
		}
		detail.Videos = append(detail.Videos, metadata.VideoDetail{
			ProviderKey: video.ProviderVideoID,
			Name:        video.TrackTitle,
			Site:        "YouTube",
			Key:         key,
			Type:        "music_video",
			Official:    true,
		})
	}
	if tracks, err := p.client.TopTracks(ctx, document.ID, p.credentials); err != nil {
		// Loaded stays false so the writer preserves the last known local
		// ranking. Don't swallow the error silently — an always-failing
		// fetch looks identical to "artist has no top tracks" otherwise.
		log.Warn().Err(err).Str("entity_id", document.ID).Msg("heyametadata: artist top-tracks fetch failed; keeping previous ranking")
	} else {
		detail.ArtistTopTracksLoaded = true
		for _, track := range tracks {
			recordingEntityID := ""
			if track.RecordingEntityId != nil {
				recordingEntityID = track.RecordingEntityId.String()
			}
			mapped := metadata.TopTrackEntry{Rank: int(track.Rank), Provider: track.Provider, Title: track.Title, RecordingEntityID: recordingEntityID, Playcount: int64Value(track.Playcount), Listeners: int64Value(track.Listeners), URL: stringValue(track.Url)}
			if track.ExternalIds != nil {
				for _, id := range *track.ExternalIds {
					if id.Provider == "musicbrainz" {
						mapped.MBID = id.Value
					}
				}
			}
			detail.ArtistTopTracks = append(detail.ArtistTopTracks, mapped)
		}
	}
	albums, err := p.artistDiscography(ctx, document.ID)
	if err != nil {
		return nil, fmt.Errorf("read canonical artist discography %s: %w", document.ID, err)
	}
	detail.Albums = albums
	return detail, nil
}

func (p *HeyaProvider) artistDiscography(ctx context.Context, artistID string) ([]metadata.AlbumEntry, error) {
	const pageSize = int64(100)
	seen := make(map[string]struct{})
	var result []metadata.AlbumEntry
	for offset := int64(0); ; {
		page, err := p.client.Relations(ctx, artistID, "discography", offset, pageSize, p.credentials)
		if err != nil {
			return nil, err
		}
		if page.Relations == nil || len(*page.Relations) == 0 {
			if offset < page.Total {
				return nil, fmt.Errorf("discography page at offset %d returned no relations before total %d", offset, page.Total)
			}
			break
		}
		for _, relation := range *page.Relations {
			if relation.TargetEntityId == nil {
				if album, ok := unresolvedDiscographyAlbum(relation.Metadata); ok {
					result = append(result, album)
				}
				continue
			}
			targetID := relation.TargetEntityId.String()
			if _, duplicate := seen[targetID]; duplicate {
				continue
			}
			seen[targetID] = struct{}{}
			body, err := p.client.Entity(ctx, targetID, "", "", p.credentials)
			if err != nil {
				return nil, fmt.Errorf("read release group %s: %w", targetID, err)
			}
			var group releaseGroupDocument
			if err := json.Unmarshal(body, &group); err != nil {
				return nil, fmt.Errorf("decode release group %s: %w", targetID, err)
			}
			album := p.albumFromReleaseGroup(group)
			edition, err := p.firstIssuedRelease(ctx, group.ID)
			if err != nil {
				return nil, fmt.Errorf("read issued release for release group %s: %w", group.ID, err)
			}
			if edition != nil {
				mergeIssuedRelease(&album, *edition)
			}
			result = append(result, album)
		}
		offset += int64(len(*page.Relations))
		if offset >= page.Total {
			break
		}
	}
	return result, nil
}

func (p *HeyaProvider) firstIssuedRelease(ctx context.Context, groupID string) (*releaseDocument, error) {
	const pageSize = int64(100)
	for offset := int64(0); ; {
		page, err := p.client.Relations(ctx, groupID, "editions", offset, pageSize, p.credentials)
		if err != nil || page.Relations == nil {
			if err == nil && offset < page.Total {
				return nil, fmt.Errorf("edition page at offset %d returned no relations before total %d", offset, page.Total)
			}
			return nil, err
		}
		for _, relation := range *page.Relations {
			if relation.TargetEntityId != nil {
				targetID := relation.TargetEntityId.String()
				body, readErr := p.client.Release(ctx, targetID, p.credentials)
				if readErr != nil {
					return nil, fmt.Errorf("read issued release %s: %w", targetID, readErr)
				}
				var release releaseDocument
				if err := json.Unmarshal(body, &release); err != nil {
					return nil, fmt.Errorf("decode issued release %s: %w", targetID, err)
				}
				return &release, nil
			}
		}
		if len(*page.Relations) == 0 {
			return nil, nil
		}
		offset += int64(len(*page.Relations))
		if offset >= page.Total {
			return nil, nil
		}
	}
}

func (p *HeyaProvider) mapReleaseGroup(document releaseGroupDocument) *metadata.MediaDetail {
	detail := commonDetail(document.canonicalHeader)
	album := p.albumFromReleaseGroup(document)
	detail.Title, detail.AlbumTitle, detail.Year, detail.AlbumType, detail.CoverURL = album.Title, album.Title, yearString(album.Year), album.Type, album.CoverURL
	detail.ArtistName = document.Display.ArtistCredit
	detail.ExternalIDs, detail.Genres, detail.Tags, detail.Artwork = album.ExternalIDs, album.Genres, album.Tags, []metadata.ArtworkResult{{ImageID: document.Display.ImageID, URL: album.CoverURL, AssetType: "cover", Source: "heyametadata"}}
	detail.Albums = []metadata.AlbumEntry{album}
	return detail
}

// unresolvedDiscographyAlbum retains the provider-transparent facts exposed by
// an unresolved relation. There is deliberately no canonical ID: callers may
// use the title/date/type as matching evidence, but must run release-group
// discovery before navigating to or persisting the release.
func unresolvedDiscographyAlbum(value any) (metadata.AlbumEntry, bool) {
	var relation struct {
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first_release_date"`
		PrimaryType      string `json:"primary_type"`
	}
	body, err := json.Marshal(value)
	if err != nil || json.Unmarshal(body, &relation) != nil {
		return metadata.AlbumEntry{}, false
	}
	relation.Title = strings.TrimSpace(relation.Title)
	if relation.Title == "" {
		return metadata.AlbumEntry{}, false
	}
	year := 0
	if len(relation.FirstReleaseDate) >= 4 {
		year, _ = strconv.Atoi(relation.FirstReleaseDate[:4])
	}
	return metadata.AlbumEntry{
		Title:       relation.Title,
		Type:        strings.ToLower(strings.TrimSpace(relation.PrimaryType)),
		ReleaseDate: relation.FirstReleaseDate,
		Year:        year,
	}, true
}

func (p *HeyaProvider) albumFromReleaseGroup(document releaseGroupDocument) metadata.AlbumEntry {
	album := metadata.AlbumEntry{CanonicalID: document.ID, Title: document.Display.Title, Type: strings.ToLower(document.Data.Classification.PrimaryType), Year: document.Display.Year, CoverURL: p.client.ImageURL(document.Display.ImageID), SecondaryTypes: document.Data.Classification.SecondaryTypes, ExternalIDs: flattenExternalIDs(document.ExternalIDs)}
	for _, date := range document.Data.Dates {
		if album.ReleaseDate == "" || date.Type == "first_release" {
			album.ReleaseDate = date.Value
		}
	}
	for _, genre := range document.Data.Genres {
		album.Genres = append(album.Genres, genre.Name)
	}
	for _, tag := range document.Data.Tags {
		album.Tags = append(album.Tags, tag.Name)
	}
	album.Rating, _ = preferredRating(document.Data.Ratings)
	for _, credit := range document.Data.ArtistCredits {
		album.ArtistCredits = append(album.ArtistCredits, metadata.ArtistCreditEntry{Name: firstNonEmpty(credit.Name, credit.ArtistName), MBID: credit.ArtistID, JoinPhrase: credit.JoinPhrase})
	}
	return album
}

func mergeIssuedRelease(album *metadata.AlbumEntry, release releaseDocument) {
	album.ReleaseID = release.ID
	if album.ExternalIDs == nil {
		album.ExternalIDs = make(map[string]string)
	}
	for key, value := range flattenExternalIDs(release.ExternalIDs) {
		// Keep the release group's generic MBID on the aggregate album. The
		// issued release remains available under musicbrainz_album, preserving
		// the distinction needed by local tag matching.
		if key != "mbid" {
			album.ExternalIDs[key] = value
		}
	}
	album.ReleaseDate = firstNonEmpty(release.Data.Date, album.ReleaseDate)
	album.Country = release.Data.Country
	album.Barcode = release.Data.Barcode
	if len(release.Data.Labels) > 0 {
		album.Label, album.CatalogNo = release.Data.Labels[0].Name, release.Data.Labels[0].CatalogNumber
	}
	for _, medium := range release.Data.Media {
		for _, track := range medium.Tracks {
			mapped := metadata.TrackDetail{CanonicalID: track.RecordingEntityID, DiscNumber: medium.Position, TrackNumber: track.Sequence, Title: track.Title, Duration: int(track.DurationMS / 1000), RecordingMBID: track.Recording.ProviderID, LyricsAvailable: track.LyricsAvailable, ExternalIDs: map[string]string{track.Recording.Provider: track.Recording.ProviderID}}
			if len(track.Recording.ISRCs) > 0 {
				mapped.ISRC = track.Recording.ISRCs[0]
			}
			for _, credit := range track.ArtistCredits {
				mapped.ArtistCredits = append(mapped.ArtistCredits, metadata.ArtistCreditEntry{Name: firstNonEmpty(credit.Name, credit.ArtistName), MBID: credit.ArtistID, JoinPhrase: credit.JoinPhrase})
			}
			album.Tracks = append(album.Tracks, mapped)
		}
	}
	album.TrackCount = len(album.Tracks)
}

func (p *HeyaProvider) mapRelease(document releaseDocument) *metadata.MediaDetail {
	detail := commonDetail(document.canonicalHeader)
	detail.Title, detail.AlbumTitle, detail.Year, detail.Country, detail.Barcode = document.Display.Title, document.Display.Title, yearString(document.Display.Year), document.Data.Country, document.Data.Barcode
	album := metadata.AlbumEntry{CanonicalID: document.ID, ReleaseID: document.ID, Title: document.Display.Title, Year: document.Display.Year}
	mergeIssuedRelease(&album, document)
	detail.Tracks = album.Tracks
	detail.Label = album.Label
	return detail
}

func (p *HeyaProvider) mapBook(document bookDocument) *metadata.MediaDetail {
	detail := commonDetail(document.canonicalHeader)
	detail.Title, detail.SortTitle, detail.Year, detail.Description = document.Display.Title, document.Display.Title, yearString(firstPositive(document.Display.Year, document.Data.FirstPublishYear)), document.Data.Description
	detail.PosterURL, detail.CoverURL = p.client.ImageURL(document.Display.ImageID), p.client.ImageURL(document.Display.ImageID)
	detail.Subjects, detail.PageCount, detail.PublishDate = document.Data.Subjects, document.Data.PageCount, document.Data.PublishedDate
	if len(document.Data.Authors) > 0 {
		detail.AuthorName = document.Data.Authors[0].Name
		detail.AuthorCanonicalID = document.Data.Authors[0].ID
		detail.AuthorExternalIDs = flattenExternalIDs(document.Data.Authors[0].ExternalIDs)
	}
	if len(document.Data.Languages) > 0 {
		detail.Language = document.Data.Languages[0]
	}
	if len(document.Data.Publishers) > 0 {
		detail.Publisher = document.Data.Publishers[0]
	}
	if len(document.Data.ISBN13) > 0 {
		detail.ISBN = document.Data.ISBN13[0]
	} else if len(document.Data.ISBN10) > 0 {
		detail.ISBN = document.Data.ISBN10[0]
	}
	if len(document.Data.Series) > 0 {
		detail.SeriesName = document.Data.Series[0].Name
		position, _ := strconv.ParseFloat(document.Data.Series[0].Position, 64)
		detail.SeriesNum = int(position)
	}
	detail.Rating, detail.VoteCount = preferredRating(document.Data.Ratings)
	detail.Artwork = p.mapImages(document.Data.Images)
	return detail
}

func mapLocalizedTitles(values []localizedText) []metadata.TitleEntry {
	result := make([]metadata.TitleEntry, 0, len(values))
	for _, value := range values {
		result = append(result, metadata.TitleEntry{Title: value.Value, Language: value.Language, Country: value.Country, TitleType: value.Type, Source: "heyametadata"})
	}
	return result
}
func localizedOverview(values []localizedText) (string, map[string]string) {
	mapped := map[string]string{}
	first := ""
	for _, value := range values {
		if first == "" {
			first = value.Value
		}
		key := firstNonEmpty(value.Language, "und")
		if value.Country != "" {
			key += "-" + value.Country
		}
		mapped[key] = value.Value
	}
	if len(mapped) == 0 {
		mapped = nil
	}
	return first, mapped
}
func firstLocalized(values []localizedText) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Value
}

func (p *HeyaProvider) mapImages(values []image) []metadata.ArtworkResult {
	result := make([]metadata.ArtworkResult, 0, len(values))
	for _, value := range values {
		result = append(result, metadata.ArtworkResult{ImageID: value.ID, URL: p.client.ImageURL(value.ID), AssetType: value.Class, Language: value.Language, Source: value.Provider, Score: value.ProviderScore, Width: value.Width, Height: value.Height})
	}
	return result
}
func (p *HeyaProvider) imageByClass(values []image, class string) string {
	for _, value := range values {
		if value.Class == class || (class == "still" && value.Class == "thumbnail") {
			return p.client.ImageURL(value.ID)
		}
	}
	return ""
}
func firstArtworkURL(values []metadata.ArtworkResult, class string) string {
	for _, value := range values {
		if value.AssetType == class {
			return value.URL
		}
	}
	return ""
}

func (p *HeyaProvider) mapCredits(values []credit) ([]metadata.CastMember, []metadata.CrewMember) {
	var cast []metadata.CastMember
	var crew []metadata.CrewMember
	for _, value := range values {
		ids := map[string]string{}
		if value.Provider != "" && value.ProviderPersonID != "" {
			ids[value.Provider] = value.ProviderPersonID
		}
		if value.CreditType == "cast" {
			cast = append(cast, metadata.CastMember{CanonicalID: value.PersonEntityID, ExternalIDs: ids, Name: value.DisplayName, Character: value.Character, Order: value.Order, ProfilePath: p.client.ImageURL(value.ProfileImageID), Source: value.Provider})
		} else {
			crew = append(crew, metadata.CrewMember{CanonicalID: value.PersonEntityID, ExternalIDs: ids, Name: value.DisplayName, Job: value.Job, Department: value.Department, ProfilePath: p.client.ImageURL(value.ProfileImageID), Source: value.Provider})
		}
	}
	return cast, crew
}

func preferredRating(values []rating) (float64, int) {
	if len(values) == 0 {
		return 0, 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value.Votes > best.Votes {
			best = value
		}
	}
	score := best.Value
	if best.ScaleMax > best.ScaleMin {
		score = (best.Value - best.ScaleMin) / (best.ScaleMax - best.ScaleMin) * 10
	}
	return math.Round(score*100) / 100, best.Votes
}
func mapRatings(values []rating) *metadata.RatingsData {
	result := &metadata.RatingsData{}
	for _, value := range values {
		score, _ := preferredRating([]rating{value})
		result.Ratings = append(result.Ratings, metadata.ExternalRating{Source: firstNonEmpty(value.Provider, value.System), Value: fmt.Sprintf("%g", value.Value), Score: score, Votes: value.Votes, RawValue: value.RawValue})
	}
	return result
}
func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// looksLikeUUID is a shape check (8-4-4-4-12 hex), not RFC validation —
// enough to keep Deezer/Tidal numeric artist ids out of MBID columns.
func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
	}
	return true
}

// youtubeVideoKey extracts the watch key from the YouTube link shapes
// TheAudioDB ships (watch?v=, youtu.be/, /embed/, /shorts/). Empty means
// "not a YouTube link" and the video is skipped — media_videos.video_key
// is what the FE builds thumbnails and embeds from.
func youtubeVideoKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if key := parsed.Query().Get("v"); key != "" {
			return key
		}
		for _, prefix := range []string{"/embed/", "/shorts/", "/live/"} {
			if rest, ok := strings.CutPrefix(parsed.Path, prefix); ok {
				if idx := strings.IndexByte(rest, '/'); idx >= 0 {
					rest = rest[:idx]
				}
				return rest
			}
		}
	case "youtu.be":
		return strings.Trim(strings.SplitN(strings.TrimPrefix(parsed.Path, "/"), "/", 2)[0], "/")
	}
	return ""
}
