package heyamedia

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

// HeyaProvider is the sole metadata provider: it pulls a fully-enriched,
// CDN-resolved payload from the Heya metadata API. It covers all media kinds
// (movie, tv, music, book) by aggregating upstream sources (TMDB, TVDB, AniDB,
// fanart, OMDB, MusicBrainz, OpenLibrary, etc.) server-side.
type HeyaProvider struct {
	client *Client
}

func NewHeyaProvider(c *Client) *HeyaProvider {
	return &HeyaProvider{client: c}
}

// BuildLookupID returns a heya provider ID for a media item given its slug and
// external IDs map. Prefers the heya slug if present; otherwise falls back to
// the highest-priority external ID supported by the heya lookup endpoint.
// Returns the empty string when no usable identifier is available.
func BuildLookupID(heyaSlug string, externalIDs map[string]string) string {
	if heyaSlug != "" {
		return "heya:" + heyaSlug
	}
	for _, key := range []string{"tmdb", "imdb", "tvdb", "anidb", "mal", "tvmaze", "tvrage", "mbid", "musicbrainz", "ol_work_id", "openlibrary"} {
		if v := externalIDs[key]; v != "" {
			return "heya:" + normalizeLookupKey(key) + ":" + v
		}
	}
	return ""
}

func normalizeLookupKey(k string) string {
	switch k {
	case "musicbrainz":
		return "mbid"
	case "openlibrary":
		return "ol_work_id"
	}
	return k
}

// Name returns the canonical provider name used in stored provider_name fields.
func (p *HeyaProvider) Name() string { return "heya" }

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func (p *HeyaProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil, fmt.Errorf("heya: unsupported kind %s", kind)
	}

	params := url.Values{
		"query": {query.Title},
		"kind":  {apiKind},
		"limit": {"20"},
	}
	if query.Year != "" {
		params.Set("year", query.Year)
	}

	var resp heyaSearchResponse
	if err := p.client.get(ctx, "/api/v1/search", params, &resp); err != nil {
		return nil, err
	}

	results := make([]metadata.SearchResult, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		confidence := 0.7
		if h.Enriched {
			confidence = 0.95
		}
		year := ""
		if h.Year > 0 {
			year = strconv.Itoa(h.Year)
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   "heya:" + h.Slug,
			ProviderName: "heya",
			Title:        h.Title,
			Year:         year,
			PosterURL:    h.Poster,
			Confidence:   confidence,
		})
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// GetDetail
// ---------------------------------------------------------------------------

func (p *HeyaProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	resp, err := p.fetchByProviderID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return p.mapDetail(resp), nil
}

// ---------------------------------------------------------------------------
// FetchArtwork
// ---------------------------------------------------------------------------

func (p *HeyaProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string, opts *metadata.FetchOptions) ([]metadata.ArtworkResult, error) {
	resp, err := p.lookupByExternalIDs(ctx, externalIDs)
	if err != nil {
		return nil, nil // gracefully return empty on lookup failure
	}
	return p.mapArtwork(&resp.Payload), nil
}

// ---------------------------------------------------------------------------
// FetchRatings
// ---------------------------------------------------------------------------

func (p *HeyaProvider) FetchRatings(ctx context.Context, externalIDs map[string]string) (*metadata.RatingsData, error) {
	resp, err := p.lookupByExternalIDs(ctx, externalIDs)
	if err != nil {
		return nil, nil
	}
	return p.mapRatings(&resp.Payload), nil
}

// ---------------------------------------------------------------------------
// LookupByNFO
// ---------------------------------------------------------------------------

func (p *HeyaProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	// Try each ID source in priority order.
	lookups := []struct {
		param string
		value string
	}{
		{"imdb", ids.IMDBID},
		{"tmdb", ids.TMDBID},
		{"tvdb", ids.TVDBID},
		{"mbid", ids.MBID},
	}

	for _, l := range lookups {
		if l.value == "" {
			continue
		}
		params := url.Values{l.param: {l.value}}
		var resp heyaItemResponse
		if err := p.client.get(ctx, "/api/v1/heya/lookup", params, &resp); err != nil {
			continue
		}
		detail := p.mapDetail(&resp)
		return detail, "heya:" + resp.Slug, nil
	}

	return nil, "", fmt.Errorf("heya: no matching item for NFO IDs")
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// fetchByProviderID parses the providerID and calls the appropriate endpoint.
func (p *HeyaProvider) fetchByProviderID(ctx context.Context, providerID string) (*heyaItemResponse, error) {
	// Strip the "heya:" prefix.
	rest := strings.TrimPrefix(providerID, "heya:")

	// Check for lookup-style IDs: "tmdb:123", "imdb:tt123", "tvdb:456", etc.
	if idx := strings.Index(rest, ":"); idx > 0 {
		idType := rest[:idx]
		idValue := rest[idx+1:]
		switch idType {
		case "tmdb", "imdb", "tvdb", "anidb", "mbid", "ol_work_id", "tvmaze", "tvrage", "mal":
			params := url.Values{idType: {idValue}}
			var resp heyaItemResponse
			if err := p.client.get(ctx, "/api/v1/heya/lookup", params, &resp); err != nil {
				return nil, err
			}
			return &resp, nil
		}
	}

	// Default: treat the rest as a slug.
	var resp heyaItemResponse
	if err := p.client.getJSON(ctx, "/api/v1/heya/"+rest, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// lookupByExternalIDs tries to find an item using the best available ID.
func (p *HeyaProvider) lookupByExternalIDs(ctx context.Context, externalIDs map[string]string) (*heyaItemResponse, error) {
	order := []string{"imdb", "tmdb", "tvdb", "mbid", "ol_work_id"}
	for _, key := range order {
		val, ok := externalIDs[key]
		if !ok || val == "" {
			continue
		}
		params := url.Values{key: {val}}
		var resp heyaItemResponse
		if err := p.client.get(ctx, "/api/v1/heya/lookup", params, &resp); err != nil {
			continue
		}
		return &resp, nil
	}
	return nil, fmt.Errorf("heya: no matching external ID")
}

// mapDetail converts the full Heya item response into a MediaDetail.
func (p *HeyaProvider) mapDetail(resp *heyaItemResponse) *metadata.MediaDetail {
	pay := &resp.Payload

	year := ""
	if pay.Year > 0 {
		year = strconv.Itoa(pay.Year)
	}

	// Poster: prefer top-level poster, fall back to first artwork poster.
	posterURL := resp.Poster
	if posterURL == "" && len(pay.Artwork.Posters) > 0 {
		posterURL = pay.Artwork.Posters[0].URL
	}

	// Backdrop: first artwork backdrop.
	backdropURL := ""
	if len(pay.Artwork.Backdrops) > 0 {
		backdropURL = pay.Artwork.Backdrops[0].URL
	}

	// External IDs: merge top-level IDs and payload external_ids.
	extIDs := make(map[string]string)
	if resp.IDs.IMDB != "" {
		extIDs["imdb"] = resp.IDs.IMDB
	}
	if resp.IDs.TMDB != 0 {
		extIDs["tmdb"] = strconv.Itoa(resp.IDs.TMDB)
	}
	if resp.IDs.TVDB != 0 {
		extIDs["tvdb"] = strconv.Itoa(resp.IDs.TVDB)
	}
	if resp.IDs.AniDB != 0 {
		extIDs["anidb"] = strconv.Itoa(resp.IDs.AniDB)
	}
	if resp.IDs.TVMaze != 0 {
		extIDs["tvmaze"] = strconv.Itoa(resp.IDs.TVMaze)
	}
	if resp.IDs.TVRage != 0 {
		extIDs["tvrage"] = strconv.Itoa(resp.IDs.TVRage)
	}
	if resp.IDs.MAL != 0 {
		extIDs["mal"] = strconv.Itoa(resp.IDs.MAL)
	}
	if resp.IDs.MBID != "" {
		extIDs["mbid"] = resp.IDs.MBID
	}
	if resp.IDs.OLWorkID != "" {
		extIDs["ol_work_id"] = resp.IDs.OLWorkID
	}
	// Merge payload external_ids (wikidata, facebook, etc.)
	for k, v := range pay.ExternalIDs {
		if v != "" && extIDs[k] == "" {
			extIDs[k] = v
		}
	}

	// Genres.
	genres := make([]string, len(pay.Genres))
	for i, g := range pay.Genres {
		genres[i] = g
	}

	// Rating: first entry.
	var rating float64
	if len(pay.Ratings) > 0 {
		rating = pay.Ratings[0].Value
	}

	// Titles.
	titles := make([]metadata.TitleEntry, len(pay.Titles))
	for i, t := range pay.Titles {
		titles[i] = metadata.TitleEntry{
			Title:     t.Title,
			Language:  t.Language,
			Country:   t.Country,
			TitleType: t.Type,
			Source:    t.Source,
		}
	}

	// Cast.
	cast := make([]metadata.CastMember, 0, len(pay.Cast))
	for _, c := range pay.Cast {
		profiles := convertProfileURLs(c.ProfileURLs)
		profilePath := ""
		if len(profiles) > 0 {
			profilePath = profiles[0].URL
		}
		cast = append(cast, metadata.CastMember{
			ExternalIDs: copyStringMap(c.ExternalIDs),
			Name:        c.Name,
			Character:   c.Character,
			Order:       c.Order,
			Gender:      genderStringToInt(c.Gender),
			ProfilePath: profilePath,
			Profiles:    profiles,
			Popularity:  c.Popularity,
			Source:      c.Source,
		})
	}

	// Crew.
	crew := make([]metadata.CrewMember, 0, len(pay.Crew))
	for _, c := range pay.Crew {
		profiles := convertProfileURLs(c.ProfileURLs)
		profilePath := ""
		if len(profiles) > 0 {
			profilePath = profiles[0].URL
		}
		crew = append(crew, metadata.CrewMember{
			ExternalIDs: copyStringMap(c.ExternalIDs),
			Name:        c.Name,
			Job:         c.Job,
			Department:  c.Department,
			Gender:      genderStringToInt(c.Gender),
			ProfilePath: profilePath,
			Profiles:    profiles,
			Source:      c.Source,
		})
	}

	// Keywords.
	keywords := make([]metadata.KeywordDetail, len(pay.Keywords))
	for i, k := range pay.Keywords {
		var kIDs map[string]string
		if k.TmdbID != 0 {
			kIDs = map[string]string{"tmdb": strconv.Itoa(k.TmdbID)}
		}
		keywords[i] = metadata.KeywordDetail{
			ExternalIDs: kIDs,
			Name:        k.Name,
		}
	}

	// Videos.
	videos := make([]metadata.VideoDetail, len(pay.Videos))
	for i, v := range pay.Videos {
		videos[i] = metadata.VideoDetail{
			ProviderKey: v.Source,
			Name:        v.Name,
			Site:        v.Site,
			Key:         v.Key,
			Type:        v.Type,
			Language:    v.Language,
			Official:    v.Official,
			PublishedAt: v.PublishedAt,
		}
	}

	// Certifications.
	certs := make([]metadata.CertificationDetail, len(pay.ContentRatings))
	for i, cr := range pay.ContentRatings {
		certs[i] = metadata.CertificationDetail{
			Country:       cr.Country,
			Certification: cr.Rating,
			Source:        cr.Source,
		}
	}

	// Recommendations.
	recs := make([]metadata.RecommendationDetail, len(pay.Recommendations))
	for i, r := range pay.Recommendations {
		recs[i] = metadata.RecommendationDetail{
			ExternalIDs: copyStringMap(r.ExternalIDs),
			Title:       r.Title,
			PosterPath:  r.PosterPath,
			MediaType:   r.MediaType,
			VoteAverage: r.VoteAverage,
			ReleaseDate: r.ReleaseDate,
		}
	}

	// Production companies (from studios).
	companies := make([]metadata.ProductionCompanyDetail, len(pay.Studios))
	for i, s := range pay.Studios {
		var sIDs map[string]string
		if s.ID != 0 {
			sIDs = map[string]string{s.Source: strconv.Itoa(s.ID)}
		}
		companies[i] = metadata.ProductionCompanyDetail{
			ExternalIDs:   sIDs,
			Name:          s.Name,
			LogoPath:      s.LogoURL,
			OriginCountry: s.Country,
		}
	}

	// Seasons with episodes.
	seasons := make([]metadata.SeasonDetail, 0, len(pay.Seasons))
	for _, s := range pay.Seasons {
		posterURL := ""
		if len(s.PosterURLs) > 0 {
			posterURL = s.PosterURLs[0].URL
		}

		episodes := make([]metadata.EpisodeDetail, 0, len(s.Episodes))
		for _, ep := range s.Episodes {
			stillURL := ""
			if len(ep.StillURLs) > 0 {
				stillURL = ep.StillURLs[0].URL
			}
			var epRating float64
			if len(ep.Ratings) > 0 {
				epRating = ep.Ratings[0].Value
			}
			var epTitles []metadata.TitleEntry
			for _, t := range ep.Titles {
				epTitles = append(epTitles, metadata.TitleEntry{
					Title: t.Title, Language: t.Language, Country: t.Country,
					TitleType: t.Type, Source: t.Source,
				})
			}
			episodes = append(episodes, metadata.EpisodeDetail{
				Number:         ep.Number,
				Title:          ep.Name,
				Titles:         epTitles,
				Overview:       ep.Overview,
				Overviews:      ep.Overviews,
				StillURL:       stillURL,
				RuntimeMinutes: ep.Runtime,
				AirDate:        ep.AirDate,
				Rating:         epRating,
				AbsoluteNumber: ep.AbsoluteNumber,
				IsSpecial:      ep.IsSpecial,
				EpisodeType:    ep.Type,
				TmdbID:         ep.TmdbID,
				TvdbID:         ep.TvdbID,
				Source:         ep.Source,
			})
		}

		seasons = append(seasons, metadata.SeasonDetail{
			Number:        s.Number,
			Title:         s.Name,
			Overview:      s.Overview,
			PosterURL:     posterURL,
			AirDate:       s.AirDate,
			EndDate:       s.EndDate,
			Status:        s.Status,
			AiredEpisodes: s.AiredEpisodes,
			TmdbSeasonID:  s.TmdbSeasonID,
			TvdbSeasonID:  s.TvdbSeasonID,
			AnidbID:       s.AnidbID,
			Episodes:      episodes,
		})
	}

	// Spoken languages.
	spokenLangs := pay.SpokenLanguages

	// Networks.
	networks := make([]metadata.NetworkDetail, 0, len(pay.Networks))
	for _, n := range pay.Networks {
		nd := metadata.NetworkDetail{Name: n.Name}
		if n.ID != 0 {
			nd.ExternalIDs = map[string]string{"tmdb": strconv.Itoa(n.ID)}
		}
		networks = append(networks, nd)
	}

	// Created by.
	createdBy := make([]metadata.CreatorDetail, 0, len(pay.CreatedBy))
	for _, c := range pay.CreatedBy {
		cd := metadata.CreatorDetail{Name: c.Name}
		if c.ID != 0 {
			cd.ExternalIDs = map[string]string{"tmdb": strconv.Itoa(c.ID)}
		}
		createdBy = append(createdBy, cd)
	}

	detail := &metadata.MediaDetail{
		Title:         pay.Title,
		SortTitle:     strings.ToLower(coalesce(pay.SortTitle, pay.Title)),
		Year:          year,
		Description:   pay.Overview,
		Titles:        titles,
		Overviews:     pay.Overviews,
		PosterURL:     posterURL,
		BackdropURL:   backdropURL,
		ExternalIDs:   extIDs,
		Genres:        genres,
		Rating:        rating,
		ProviderKind:  resp.Kind,
		HeyaSlug:      resp.Slug,
		OriginalTitle: pay.OriginalTitle,

		// Movie fields.
		RuntimeMinutes:      pay.Runtime,
		Tagline:             pay.Tagline,
		ReleaseDate:         pay.FirstAirDate, // movies use first_air_date as release_date in enriched data
		OriginalLanguage:    pay.OriginalLanguage,
		Budget:              pay.Budget,
		Revenue:             pay.Revenue,
		Popularity:          pay.Popularity,
		ProductionCompanies: companies,
		Cast:                cast,
		Crew:                crew,
		Keywords:            keywords,
		Videos:              videos,
		Certifications:      certs,
		Recommendations:     recs,
		Homepage:            pay.Homepage,
		SpokenLanguages:     spokenLangs,
		OriginCountry:       pay.OriginCountry,

		// TV fields.
		Status:           pay.Status,
		FirstAirDate:     pay.FirstAirDate,
		LastAirDate:      pay.LastAirDate,
		Networks:         networks,
		CreatedBy:        createdBy,
		NumberOfSeasons:  len(pay.Seasons),
		NumberOfEpisodes: countEpisodes(pay.Seasons),
		Seasons:          seasons,
	}

	return detail
}

// mapArtwork extracts all artwork categories from the payload.
func (p *HeyaProvider) mapArtwork(pay *heyaPayload) []metadata.ArtworkResult {
	var results []metadata.ArtworkResult

	type artworkMapping struct {
		items     []heyaArtworkEntry
		assetType string
	}

	mappings := []artworkMapping{
		{pay.Artwork.Posters, "poster"},
		{pay.Artwork.Backdrops, "backdrop"},
		{pay.Artwork.Logos, "logo"},
		{pay.Artwork.Banners, "banner"},
		{pay.Artwork.Clearart, "clearart"},
		{pay.Artwork.Thumbnails, "thumb"},
	}

	for _, m := range mappings {
		for _, img := range m.items {
			results = append(results, metadata.ArtworkResult{
				URL:       img.URL,
				AssetType: m.assetType,
				Language:  img.Language,
				Source:    img.Source,
				Likes:     img.Likes,
				Score:     img.Score,
				Width:     img.Width,
				Height:    img.Height,
				Aspect:    img.Aspect,
			})
		}
	}

	return results
}

// mapRatings extracts rating data from the payload.
func (p *HeyaProvider) mapRatings(pay *heyaPayload) *metadata.RatingsData {
	if len(pay.Ratings) == 0 {
		return nil
	}

	ratings := make([]metadata.ExternalRating, len(pay.Ratings))
	for i, r := range pay.Ratings {
		ratings[i] = metadata.ExternalRating{
			Source:   r.Source,
			Value:    fmt.Sprintf("%.1f", r.Value),
			Score:    r.Value,
			Votes:    r.Votes,
			RawValue: r.Raw,
		}
	}

	return &metadata.RatingsData{Ratings: ratings}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func heyaKind(kind metadata.MediaKind) string {
	switch kind {
	case metadata.KindMovie:
		return "movie"
	case metadata.KindTV:
		return "tv"
	case metadata.KindMusic:
		return "album"
	case metadata.KindBook:
		return "book"
	default:
		return ""
	}
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func genderStringToInt(g string) int {
	switch g {
	case "female":
		return 1
	case "male":
		return 2
	default:
		return 0
	}
}

func convertProfileURLs(items []HeyaArtworkItem) []metadata.ProfileImage {
	if len(items) == 0 {
		return nil
	}
	out := make([]metadata.ProfileImage, 0, len(items))
	for _, it := range items {
		if it.URL == "" {
			continue
		}
		out = append(out, metadata.ProfileImage{
			URL:    it.URL,
			Source: it.Source,
			Aspect: it.Aspect,
			Width:  it.Width,
			Height: it.Height,
			Score:  it.Score,
			Likes:  it.Likes,
		})
	}
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func countEpisodes(seasons []heyaSeasonEntry) int {
	n := 0
	for _, s := range seasons {
		n += len(s.Episodes)
	}
	return n
}

// ---------------------------------------------------------------------------
// Heya API response types
// ---------------------------------------------------------------------------

type heyaSearchResponse struct {
	Hits           []heyaSearchHit `json:"hits"`
	Query          string          `json:"query"`
	EstimatedTotal int             `json:"estimated_total"`
}

type heyaSearchHit struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Year     int    `json:"year"`
	Slug     string `json:"slug"`
	Poster   string `json:"poster"`
	Enriched bool   `json:"enriched"`
	TmdbID   int    `json:"tmdb_id"`
}

type heyaItemResponse struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Title   string      `json:"title"`
	Year    int         `json:"year"`
	Slug    string      `json:"slug"`
	Poster  string      `json:"poster"`
	IDs     heyaIDs     `json:"ids"`
	Payload heyaPayload `json:"payload"`
}

type heyaIDs struct {
	IMDB     string `json:"imdb"`
	TMDB     int    `json:"tmdb"`
	TVDB     int    `json:"tvdb"`
	AniDB    int    `json:"anidb"`
	TVMaze   int    `json:"tvmaze"`
	TVRage   int    `json:"tvrage"`
	MAL      int    `json:"mal"`
	MBID     string `json:"mbid"`
	OLWorkID string `json:"ol_work_id"`
}

type heyaPayload struct {
	Title           string            `json:"title"`
	OriginalTitle   string            `json:"original_title"`
	SortTitle       string            `json:"sort_title"`
	Tagline         string            `json:"tagline"`
	Year            int               `json:"year"`
	Overview        string            `json:"overview"`
	Overviews       map[string]string `json:"overviews"`
	Titles          []heyaTitleEntry  `json:"titles"`
	Genres          []string          `json:"genres"`
	Keywords        []heyaKeyword     `json:"keywords"`
	ContentRatings  []heyaCR          `json:"content_ratings"`
	Ratings         []heyaRating      `json:"ratings"`
	Cast            []heyaCastEntry   `json:"cast"`
	Crew            []heyaCrewEntry   `json:"crew"`
	Artwork         heyaArtwork       `json:"artwork"`
	Seasons         []heyaSeasonEntry `json:"seasons"`
	Videos          []heyaVideo       `json:"videos"`
	Studios         []heyaStudio      `json:"studios"`
	ExternalIDs     map[string]string `json:"external_ids"`
	Recommendations []heyaRecEntry    `json:"recommendations"`

	// Dates / status.
	FirstAirDate string `json:"first_air_date"`
	LastAirDate  string `json:"last_air_date"`
	Status       string `json:"status"`
	StatusRaw    string `json:"status_raw"`

	// Numeric details.
	Runtime    int     `json:"runtime"`
	Budget     int64   `json:"budget"`
	Revenue    int64   `json:"revenue"`
	Popularity float64 `json:"popularity"`

	// Language / geography.
	OriginCountry    []string `json:"origin_country"`
	OriginalLanguage string   `json:"original_language"`
	SpokenLanguages  []string `json:"spoken_languages"`

	// Misc.
	Homepage  string        `json:"homepage"`
	Networks  []heyaNetwork `json:"networks"`
	CreatedBy []heyaCreator `json:"created_by"`
}

type heyaTitleEntry struct {
	Title    string `json:"title"`
	Language string `json:"language"`
	Country  string `json:"country"`
	Type     string `json:"type"`
	Source   string `json:"source"`
}

type heyaKeyword struct {
	Name   string `json:"name"`
	TmdbID int    `json:"tmdb_id"`
}

type heyaCR struct {
	Country string `json:"country"`
	Rating  string `json:"rating"`
	Source  string `json:"source"`
}

type heyaRating struct {
	Source string  `json:"source"`
	Value  float64 `json:"value"`
	Votes  int     `json:"votes"`
	Raw    string  `json:"raw"`
}

type heyaCastEntry struct {
	Name        string            `json:"name"`
	Character   string            `json:"character"`
	Order       int               `json:"order"`
	Gender      string            `json:"gender"`
	ProfileURLs []HeyaArtworkItem `json:"profile_urls"`
	ExternalIDs map[string]string `json:"external_ids"`
	Popularity  float64           `json:"popularity"`
	Source      string            `json:"source"`
}

type heyaCrewEntry struct {
	Name        string            `json:"name"`
	Job         string            `json:"job"`
	Department  string            `json:"department"`
	Gender      string            `json:"gender"`
	ProfileURLs []HeyaArtworkItem `json:"profile_urls"`
	ExternalIDs map[string]string `json:"external_ids"`
	Source      string            `json:"source"`
}

type heyaArtwork struct {
	Posters    []heyaArtworkEntry `json:"posters"`
	Backdrops  []heyaArtworkEntry `json:"backdrops"`
	Logos      []heyaArtworkEntry `json:"logos"`
	Banners    []heyaArtworkEntry `json:"banners"`
	Clearart   []heyaArtworkEntry `json:"clearart"`
	Thumbnails []heyaArtworkEntry `json:"thumbnails"`
}

type heyaArtworkEntry struct {
	URL      string  `json:"url"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Language string  `json:"language"`
	Score    float64 `json:"score"`
	Likes    int     `json:"likes"`
	Source   string  `json:"source"`
	Aspect   string  `json:"aspect"`
}

type heyaSeasonEntry struct {
	Number        int                `json:"number"`
	Name          string             `json:"name"`
	Overview      string             `json:"overview"`
	AirDate       string             `json:"air_date"`
	EndDate       string             `json:"end_date"`
	EpisodeCount  int                `json:"episode_count"`
	Status        string             `json:"status"`
	AiredEpisodes int                `json:"aired_episodes"`
	PosterURLs    []HeyaArtworkItem  `json:"poster_urls"`
	TmdbSeasonID  int                `json:"tmdb_season_id"`
	TvdbSeasonID  int                `json:"tvdb_season_id"`
	AnidbID       int                `json:"anidb_id"`
	Episodes      []heyaEpisodeEntry `json:"episodes"`
}

type heyaEpisodeEntry struct {
	SeasonNumber   int               `json:"season_number"`
	Number         int               `json:"number"`
	AbsoluteNumber int               `json:"absolute_number"`
	Name           string            `json:"name"`
	Titles         []heyaTitleEntry  `json:"titles"`
	Overview       string            `json:"overview"`
	Overviews      map[string]string `json:"overviews"`
	AirDate        string            `json:"air_date"`
	Runtime        int               `json:"runtime"`
	Type           int               `json:"type"`
	IsSpecial      bool              `json:"is_special"`
	Ratings        []heyaRating      `json:"ratings"`
	TmdbID         int               `json:"tmdb_id"`
	TvdbID         int               `json:"tvdb_id"`
	StillURLs      []HeyaArtworkItem `json:"still_urls"`
	Source         string            `json:"source"`
}

type heyaVideo struct {
	Name        string `json:"name"`
	Site        string `json:"site"`
	Key         string `json:"key"`
	Type        string `json:"type"`
	Language    string `json:"language"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at"`
	Source      string `json:"source"`
}

type heyaStudio struct {
	Name    string `json:"name"`
	ID      int    `json:"id"`
	Source  string `json:"source"`
	Country string `json:"country"`
	LogoURL string `json:"logo_url"`
}

type heyaRecEntry struct {
	ExternalIDs map[string]string `json:"external_ids"`
	Title       string            `json:"title"`
	PosterPath  string            `json:"poster_path"`
	MediaType   string            `json:"media_type"`
	VoteAverage float64           `json:"vote_average"`
	ReleaseDate string            `json:"release_date"`
}

type heyaNetwork struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type heyaCreator struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// ---------------------------------------------------------------------------
// Person lookup types + helper
// ---------------------------------------------------------------------------

// HeyaPersonResponse is the top-level object returned by /api/v1/heya/people/lookup.
type HeyaPersonResponse struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Title   string            `json:"title"`
	Year    int               `json:"year"`
	Slug    string            `json:"slug"`
	Poster  string            `json:"poster"`
	IDs     HeyaIDs           `json:"ids"`
	Payload HeyaPersonPayload `json:"payload"`
}

type HeyaIDs struct {
	IMDB     string `json:"imdb"`
	TMDB     int    `json:"tmdb"`
	TVDB     int    `json:"tvdb"`
	AniDB    int    `json:"anidb"`
	TVMaze   int    `json:"tvmaze"`
	TVRage   int    `json:"tvrage"`
	MAL      int    `json:"mal"`
	MBID     string `json:"mbid"`
	OLWorkID string `json:"ol_work_id"`
}

type HeyaPersonPayload struct {
	Name               string            `json:"name"`
	SortName           string            `json:"sort_name"`
	AlsoKnownAs        []string          `json:"also_known_as"`
	KnownForDepartment string            `json:"known_for_department"`
	Gender             string            `json:"gender"`
	Slug               string            `json:"slug"`
	Birthday           string            `json:"birthday"`
	BirthYear          int               `json:"birth_year"`
	BirthPlace         string            `json:"birth_place"`
	Deathday           string            `json:"deathday,omitempty"`
	Biography          string            `json:"biography"`
	Biographies        map[string]string `json:"biographies"`
	Profiles           []HeyaArtworkItem `json:"profiles"`
	ExternalIDs        map[string]string `json:"external_ids"`
	Popularity         float64           `json:"popularity"`
	Homepage           string            `json:"homepage"`
}

// HeyaArtworkItem represents a single artwork/profile image in the person response.
type HeyaArtworkItem struct {
	URL    string  `json:"url"`
	Source string  `json:"source"`
	Aspect string  `json:"aspect"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Score  float64 `json:"score"`
	Likes  int     `json:"likes"`
}

// GetPersonFromHeya fetches enriched person data from the Heya API by TMDB ID.
// This is a package-level function so workers that hold a *Client (not a
// *HeyaProvider) can call it directly.
func GetPersonFromHeya(ctx context.Context, c *Client, tmdbID int) (*HeyaPersonResponse, error) {
	path := fmt.Sprintf("/api/v1/heya/people/lookup?tmdb=%d", tmdbID)
	var resp HeyaPersonResponse
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetPersonDetail is a method on HeyaProvider that delegates to GetPersonFromHeya.
func (p *HeyaProvider) GetPersonDetail(ctx context.Context, tmdbID int) (*HeyaPersonResponse, error) {
	return GetPersonFromHeya(ctx, p.client, tmdbID)
}
