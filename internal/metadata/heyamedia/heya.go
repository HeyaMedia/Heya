package heyamedia

import (
	"context"
	"encoding/json"
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

// BuildLookupID returns a heya provider ID of the form
// "heya:<apiKind>:<provider>:<value>" — the canonical key for the v0.3.0
// /api/v1/{kind}/{id} endpoint. Picks the highest-priority external ID that
// the heya.media server accepts for the given kind. Returns "" when no
// usable identifier is available.
//
// heya.media no longer exposes a slug-fetch endpoint, so the heya_slug we
// keep in our DB is for our own URL routing only — it's not a refetch key.
func BuildLookupID(kind metadata.MediaKind, externalIDs map[string]string) string {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return ""
	}
	for _, key := range providerOrderForKind(apiKind) {
		if v := externalIDs[key]; v != "" {
			return "heya:" + apiKind + ":" + canonicalProviderKey(key) + ":" + v
		}
	}
	return ""
}

// providerOrderForKind lists the external-ID providers heya.media accepts for
// a given api kind, in our preferred priority. Order matches the v0.3.0
// /{kind}/{id} doc: artist (mbid/apple/discogs/deezer), movie (tmdb/imdb),
// tv (tmdb/imdb/tvdb + anime IDs), person (tmdb/imdb), book (ol_work_id).
func providerOrderForKind(apiKind string) []string {
	switch apiKind {
	case "artist":
		return []string{"mbid", "musicbrainz", "apple", "discogs", "deezer"}
	case "movie":
		return []string{"tmdb", "imdb"}
	case "tv":
		return []string{"tmdb", "tvdb", "imdb", "anidb", "mal", "tvmaze", "tvrage"}
	case "person":
		return []string{"tmdb", "imdb"}
	case "book":
		return []string{"ol_work_id", "openlibrary"}
	}
	return nil
}

func canonicalProviderKey(k string) string {
	switch k {
	case "musicbrainz":
		return "mbid"
	case "openlibrary":
		return "ol_work_id"
	}
	return k
}

// parseProviderID splits a "heya:<apiKind>:<provider>:<value>" string into
// its three components. Returns ok=false if the input isn't in that shape.
func parseProviderID(providerID string) (apiKind, provider, value string, ok bool) {
	rest := strings.TrimPrefix(providerID, "heya:")
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
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

	hits, err := p.searchHits(ctx, apiKind, query.Title, query.Year, query.Artist, 20)
	if err != nil {
		return nil, err
	}

	results := make([]metadata.SearchResult, 0, len(hits))
	for _, h := range hits {
		providerID := buildProviderIDFromHit(apiKind, h)
		if providerID == "" {
			// Server returned a hit with no usable id field — skip.
			continue
		}
		confidence := 0.7
		if h.Enriched {
			confidence = 0.95
		}
		year := ""
		if h.Year > 0 {
			year = strconv.Itoa(h.Year)
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   providerID,
			ProviderName: "heya",
			Title:        h.Name,
			Year:         year,
			Description:  h.Snippet,
			PosterURL:    h.Image,
			Confidence:   confidence,
			ExternalIDs:  map[string]string(h.ExternalIDs),
			AltTitles:    h.AltTitles,
			HeyaSlug:     h.Slug,
			Enriched:     h.Enriched,
		})
	}
	return results, nil
}

// searchHits is the raw access path to /api/v1/search — returns the server's
// SearchHit rows verbatim. Used by Search() and SearchArtistBest().
func (p *HeyaProvider) searchHits(ctx context.Context, apiKind, query, year, artist string, limit int) ([]SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("heya: empty search query")
	}
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{
		"type":  {apiKind},
		"q":     {query},
		"limit": {strconv.Itoa(limit)},
	}
	if year != "" {
		params.Set("year", year)
	}
	if artist != "" {
		params.Set("artist", artist)
	}
	var resp searchResponse
	if err := p.client.get(ctx, "/api/v1/search", params, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// buildProviderIDFromHit converts a SearchHit into our internal providerID
// format. The hit's `id` is already "<provider>:<value>"; we prefix it with
// "heya:<apiKind>:" so GetDetail can round-trip it back to /{kind}/{id}.
func buildProviderIDFromHit(apiKind string, h SearchHit) string {
	if h.ID == "" {
		return ""
	}
	return "heya:" + apiKind + ":" + h.ID
}

// ---------------------------------------------------------------------------
// GetDetail
// ---------------------------------------------------------------------------

func (p *HeyaProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	apiKind, provider, value, ok := parseProviderID(providerID)
	if !ok {
		return nil, fmt.Errorf("heya: invalid provider id %q (expected heya:<kind>:<provider>:<value>)", providerID)
	}
	resp, err := p.fetchKindID(ctx, apiKind, provider+":"+value)
	if err != nil {
		return nil, err
	}
	return p.mapDetail(resp), nil
}

// ---------------------------------------------------------------------------
// FetchArtwork
// ---------------------------------------------------------------------------

func (p *HeyaProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string, opts *metadata.FetchOptions) ([]metadata.ArtworkResult, error) {
	resp, err := p.lookupByExternalIDs(ctx, kind, externalIDs)
	if err != nil {
		return nil, nil // gracefully return empty on lookup failure
	}
	return p.mapArtwork(&resp.Payload), nil
}

// ---------------------------------------------------------------------------
// FetchRatings
// ---------------------------------------------------------------------------

func (p *HeyaProvider) FetchRatings(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*metadata.RatingsData, error) {
	resp, err := p.lookupByExternalIDs(ctx, kind, externalIDs)
	if err != nil {
		return nil, nil
	}
	return p.mapRatings(&resp.Payload), nil
}

// ---------------------------------------------------------------------------
// LookupByNFO
// ---------------------------------------------------------------------------

func (p *HeyaProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil, "", fmt.Errorf("heya: unsupported kind %s", kind)
	}

	// Try each ID source in priority order. The new /{kind}/{id} endpoint
	// returns the full enriched doc directly (no separate lookup hop).
	lookups := []struct {
		provider string
		value    string
	}{
		{"tmdb", ids.TMDBID},
		{"tvdb", ids.TVDBID},
		{"imdb", ids.IMDBID},
		{"mbid", ids.MBID},
	}

	for _, l := range lookups {
		if l.value == "" {
			continue
		}
		id := l.provider + ":" + l.value
		resp, err := p.fetchKindID(ctx, apiKind, id)
		if err != nil {
			continue
		}
		detail := p.mapDetail(resp)
		return detail, "heya:" + apiKind + ":" + id, nil
	}

	return nil, "", fmt.Errorf("heya: no matching item for NFO IDs")
}

// ---------------------------------------------------------------------------
// SearchHit (raw search row)
// ---------------------------------------------------------------------------

// SearchHit is one row from /api/v1/search (v0.3.0). The endpoint merges
// warm-cache matches with on-demand provider lookups and returns canonical
// "<provider>:<value>" keys in `id` — feed `id` directly back to
// /api/v1/{type}/{id} (or to BuildLookupID-style wrappers) to enrich.
//
// enriched=true means the row is already in heya.media's warm DB and `slug`
// is populated; the fetch returns instantly with no upstream calls.
type SearchHit struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Year        int      `json:"year,omitempty"`
	Country     string   `json:"country,omitempty"`
	Image       string   `json:"image,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Sources     []string `json:"sources"`
	ExternalIDs flexIDs  `json:"external_ids,omitempty"`
	// AltTitles is the flat, deduped union of every locale variant,
	// romanization, native-script form, and alias HeyaMedia could pull
	// from the upstream providers. Used by the matcher to score scanned
	// filenames against all known title forms, not just the primary
	// English one — fixes romaji-vs-English anime mismatches and similar.
	AltTitles []string `json:"alt_titles,omitempty"`
	Score     float64  `json:"score"`
	Enriched  bool     `json:"enriched"`
	Slug      string   `json:"slug,omitempty"`
}

// flexIDs decodes a JSON object whose values may be strings, numbers, or
// booleans into a map[string]string. HeyaMedia's revamped search returns
// numeric IDs (e.g. `"tmdb": 1429`) while older / detail responses still
// use string form — flexIDs coerces both into the canonical string we
// store internally.
//
// Operates as a regular map for read access — callers do `m["tmdb"]`
// like always. Only the JSON decode path is special.
type flexIDs map[string]string

func (f *flexIDs) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := flexValueAsString(v)
		if !ok || s == "" {
			continue
		}
		out[k] = s
	}
	*f = out
	return nil
}

// flexValueAsString reads a json.RawMessage that may hold a string, number,
// or boolean and returns its canonical string form. Objects, arrays, and
// null are skipped (returns ok=false).
func flexValueAsString(v json.RawMessage) (string, bool) {
	v = []byte(strings.TrimSpace(string(v)))
	if len(v) == 0 || string(v) == "null" {
		return "", false
	}
	switch v[0] {
	case '"':
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return "", false
		}
		return s, true
	case '{', '[':
		return "", false
	default:
		// Number or bool — strip wrapping whitespace, store raw text.
		return strings.Trim(string(v), `"`), true
	}
}

// hasSource reports whether one of the hit's sources matches s (case-insensitive).
func (h *SearchHit) hasSource(s string) bool {
	for _, src := range h.Sources {
		if strings.EqualFold(src, s) {
			return true
		}
	}
	return false
}

type searchResponse struct {
	Type    string      `json:"type"`
	Query   string      `json:"query"`
	Results []SearchHit `json:"results"`
}

// SearchArtistBest searches for an artist by name and picks the best hit to
// fetch. heya.media sorts the merged result list with enriched warm-cache
// rows first, then by score within source tier, so hits[0] is usually right.
// We still bias toward MusicBrainz on cold lookups so we land an MBID in our
// DB (cross-reference key) instead of an apple/deezer id we can fetch but
// not cross-reference later.
func (p *HeyaProvider) SearchArtistBest(ctx context.Context, name string) (*SearchHit, error) {
	hits, err := p.searchHits(ctx, "artist", name, "", "", 20)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}

	if hits[0].Enriched {
		return &hits[0], nil
	}
	for i := range hits {
		if hits[i].hasSource("musicbrainz") {
			return &hits[i], nil
		}
	}
	return &hits[0], nil
}

// ---------------------------------------------------------------------------
// Similar
// ---------------------------------------------------------------------------

// SimilarHit is one row from /api/v1/similar/artist or /similar/track.
// Score is normalized 0..1 within each source (Last.fm vs ListenBrainz),
// so cross-source comparison is approximate at best.
type SimilarHit struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Artist string  `json:"artist,omitempty"` // only on track similars
	MBID   string  `json:"mbid,omitempty"`
	Score  float64 `json:"score"`
	Source string  `json:"source"` // lastfm | listenbrainz
	Image  string  `json:"image,omitempty"`
	URL    string  `json:"url,omitempty"`
}

type similarResponse struct {
	Results []SimilarHit `json:"results"`
}

// SimilarArtists fetches similar-artist suggestions. Prefers MBID lookup
// (more reliable cross-provider match); falls back to name search.
func (p *HeyaProvider) SimilarArtists(ctx context.Context, mbid, name string) ([]SimilarHit, error) {
	params := url.Values{}
	if mbid != "" {
		params.Set("mbid", mbid)
	} else if name != "" {
		params.Set("name", name)
	} else {
		return nil, fmt.Errorf("heya: similar artists needs mbid or name")
	}
	var resp similarResponse
	if err := p.client.get(ctx, "/api/v1/similar/artist", params, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// ---------------------------------------------------------------------------
// FetchByKindID (path-style, v0.3.0)
// ---------------------------------------------------------------------------

// FetchByKindID hits `GET /api/v1/{kind}/{id}` and returns the full enriched
// doc plus the providerID to store. `id` is the `<provider>:<value>` string
// /search returns. Triggers inline enrichment on cache miss (1–60s typical,
// up to 5 min for popular music artists).
//
// Valid (kind, provider) pairs per the v0.3.0 OpenAPI:
//   - artist: mbid, apple, discogs, deezer
//   - movie:  tmdb, imdb
//   - tv:     tmdb, imdb, tvdb
//   - person: tmdb, imdb
//   - book:   ol_work_id (501 on miss — enrichment not implemented yet)
//
// Bad provider for the kind → 400 from the server.
func (p *HeyaProvider) FetchByKindID(ctx context.Context, kind, id string) (*metadata.MediaDetail, string, error) {
	if kind == "" || id == "" {
		return nil, "", fmt.Errorf("heya: empty kind or id")
	}
	resp, err := p.fetchKindID(ctx, kind, id)
	if err != nil {
		return nil, "", err
	}
	return p.mapDetail(resp), "heya:" + kind + ":" + id, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// fetchKindID GETs /api/v1/{kind}/{id} where id is "<provider>:<value>".
// The embedded colon in id is left as-is — net/url's PathEscape doesn't
// touch colons, and heya.media splits on the rightmost slash to recover id.
func (p *HeyaProvider) fetchKindID(ctx context.Context, apiKind, id string) (*heyaItemResponse, error) {
	if apiKind == "" || id == "" {
		return nil, fmt.Errorf("heya: empty kind or id")
	}
	path := "/api/v1/" + url.PathEscape(apiKind) + "/" + url.PathEscape(id)
	var resp heyaItemResponse
	if err := p.client.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// lookupByExternalIDs picks the best external ID for `kind` and fetches the
// item via /api/v1/{kind}/{provider}:{value}.
func (p *HeyaProvider) lookupByExternalIDs(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*heyaItemResponse, error) {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil, fmt.Errorf("heya: unsupported kind %s", kind)
	}
	for _, key := range providerOrderForKind(apiKind) {
		val := externalIDs[key]
		if val == "" {
			continue
		}
		id := canonicalProviderKey(key) + ":" + val
		resp, err := p.fetchKindID(ctx, apiKind, id)
		if err != nil {
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("heya: no matching external ID for kind %s", apiKind)
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
	if resp.IDs.Discogs != 0 {
		extIDs["discogs"] = strconv.FormatInt(resp.IDs.Discogs, 10)
	}
	if resp.IDs.Deezer != 0 {
		extIDs["deezer"] = strconv.FormatInt(resp.IDs.Deezer, 10)
	}
	if resp.IDs.Apple != 0 {
		extIDs["apple"] = strconv.FormatInt(resp.IDs.Apple, 10)
	}
	// Merge payload external_ids (wikidata, spotify, facebook, etc.)
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

	if resp.Kind == "artist" {
		artistName := coalesce(pay.Name, pay.Title)
		detail.Title = artistName
		detail.SortTitle = strings.ToLower(coalesce(pay.SortName, artistName))
		detail.Description = pay.Profile
		detail.ArtistName = artistName
		detail.ArtistBio = pay.Profile
		detail.ArtistSortName = pay.SortName
		detail.ArtistDisambiguation = pay.Disambiguation
		detail.ArtistNativeName = pay.NativeName
		detail.ArtistNativeLanguage = pay.NativeLanguage
		detail.ArtistCountry = pay.Country
		detail.ArtistType = pay.Type
		detail.ArtistGender = pay.Gender
		detail.ArtistBeginDate = coalesce(pay.Begin, pay.Birthday)
		detail.ArtistBeginYear = pay.BeginYear
		detail.ArtistBirthplace = pay.Birthplace
		if len(pay.Tags) > 0 {
			detail.Genres = mergeStrings(detail.Genres, pay.Tags)
		}
		detail.Albums = mapHeyaAlbums(pay.Albums)
	}

	return detail
}

func mapHeyaAlbums(albums []heyaAlbumEntry) []metadata.AlbumEntry {
	if len(albums) == 0 {
		return nil
	}
	out := make([]metadata.AlbumEntry, 0, len(albums))
	for _, a := range albums {
		coverURL := ""
		if len(a.Artwork) > 0 {
			coverURL = a.Artwork[0].URL
		}
		out = append(out, metadata.AlbumEntry{
			Title:       a.Title,
			Type:        a.Type,
			ReleaseDate: a.ReleaseDate,
			Year:        a.Year,
			Label:       a.Label,
			CatalogNo:   a.CatalogNo,
			Country:     a.Country,
			Barcode:     a.Barcode,
			ISRCs:       a.ISRCs,
			ExternalIDs: a.ExternalIDs,
			TrackCount:  a.TrackCount,
			Popularity:  a.Popularity,
			CoverURL:    coverURL,
			Tracks:      mapHeyaAlbumTracks(a.Tracks),
		})
	}
	return out
}

func mapHeyaAlbumTracks(tracks []heyaAlbumTrackEntry) []metadata.TrackDetail {
	if len(tracks) == 0 {
		return nil
	}
	out := make([]metadata.TrackDetail, 0, len(tracks))
	for _, t := range tracks {
		disc := t.Disc
		if disc == 0 {
			disc = 1
		}
		out = append(out, metadata.TrackDetail{
			DiscNumber:    disc,
			TrackNumber:   t.Position,
			Title:         t.Title,
			Duration:      t.Duration,
			ISRC:          t.ISRC,
			RecordingMBID: t.RecordingMBID,
			PreviewURL:    t.Preview,
		})
	}
	return out
}

func mergeStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
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
		// heya.media indexes artists (with embedded discography) as the music
		// entry point; album-level search isn't available yet.
		return "artist"
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

	// Music providers (numeric in top-level ids, string in payload.external_ids).
	Discogs int64 `json:"discogs"`
	Deezer  int64 `json:"deezer"`
	Apple   int64 `json:"apple"`
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
	ExternalIDs     flexIDs           `json:"external_ids"`
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

	// Music (artist payload).
	Name           string            `json:"name"`
	SortName       string            `json:"sort_name"`
	NativeName     string            `json:"native_name"`
	NativeLanguage string            `json:"native_language"`
	Disambiguation string            `json:"disambiguation"`
	Type           string            `json:"type"`
	Gender         string            `json:"gender"`
	Country        string            `json:"country"`
	Area           string            `json:"area"`
	BeginArea      string            `json:"begin_area"`
	Birthplace     string            `json:"birthplace"`
	Begin          string            `json:"begin"`
	BeginYear      int               `json:"begin_year"`
	Birthday       string            `json:"birthday"`
	Profile        string            `json:"profile"`
	Tags           []string          `json:"tags"`
	AlbumCount     int               `json:"album_count"`
	URLs           []heyaArtistURL   `json:"urls"`
	WikipediaLinks map[string]string `json:"wikipedia_links"`
	Albums         []heyaAlbumEntry  `json:"albums"`
	EnrichedAt     string            `json:"enriched_at"`
}

type heyaArtistURL struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type heyaAlbumEntry struct {
	Title       string                `json:"title"`
	Type        string                `json:"type"`
	ReleaseDate string                `json:"release_date"`
	Year        int                   `json:"year"`
	Label       string                `json:"label"`
	CatalogNo   string                `json:"catalog_no"`
	Country     string                `json:"country"`
	ExternalIDs flexIDs               `json:"external_ids"`
	Barcode     string                `json:"barcode"`
	ISRCs       []string              `json:"isrcs"`
	Artwork     []heyaArtworkEntry    `json:"artwork"`
	TrackCount  int                   `json:"track_count"`
	Popularity  float64               `json:"popularity"`
	Tracks      []heyaAlbumTrackEntry `json:"tracks"`
	Source      string                `json:"source"`
}

type heyaAlbumTrackEntry struct {
	Disc          int     `json:"disc"`
	Position      int     `json:"position"`
	Title         string  `json:"title"`
	Duration      int     `json:"duration"` // seconds
	ISRC          string  `json:"isrc"`
	RecordingMBID string  `json:"recording_mbid"`
	ExternalIDs   flexIDs `json:"external_ids"`
	Preview       string  `json:"preview"`
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
	ExternalIDs flexIDs           `json:"external_ids"`
	Popularity  float64           `json:"popularity"`
	Source      string            `json:"source"`
}

type heyaCrewEntry struct {
	Name        string            `json:"name"`
	Job         string            `json:"job"`
	Department  string            `json:"department"`
	Gender      string            `json:"gender"`
	ProfileURLs []HeyaArtworkItem `json:"profile_urls"`
	ExternalIDs flexIDs           `json:"external_ids"`
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
	ExternalIDs flexIDs `json:"external_ids"`
	Title       string  `json:"title"`
	PosterPath  string  `json:"poster_path"`
	MediaType   string  `json:"media_type"`
	VoteAverage float64 `json:"vote_average"`
	ReleaseDate string  `json:"release_date"`
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
	ExternalIDs        flexIDs           `json:"external_ids"`
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
	path := fmt.Sprintf("/api/v1/person/tmdb:%d", tmdbID)
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
