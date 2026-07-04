package heyamedia

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	gen "github.com/karbowiak/heya/clients/heyamedia"
	"github.com/karbowiak/heya/internal/metadata"
)

// HeyaProvider is the sole metadata provider — it pulls fully-enriched,
// CDN-resolved payloads from the heya.media aggregator API which fronts
// TMDB / TVDB / AniDB / fanart.tv / OMDB / MusicBrainz / OpenLibrary
// upstream. One provider, four kinds (movie / tv / music / book).
//
// All HTTP I/O goes through the generated client in clients/heyamedia.
// The mapper functions in mappers.go translate the generated wire shape
// into the canonical metadata.MediaDetail consumed by the matcher and
// workers. Behaviour parity vs the previous hand-rolled implementation
// is asserted by the golden tests in mapdetail_golden_test.go.
type HeyaProvider struct {
	client *Client
}

func NewHeyaProvider(c *Client) *HeyaProvider {
	return &HeyaProvider{client: c}
}

// Name returns the canonical provider name used in stored provider_name
// columns.
func (p *HeyaProvider) Name() string { return "heya" }

// ---------------------------------------------------------------------------
// Provider-ID conventions
// ---------------------------------------------------------------------------

// BuildLookupIDs returns every usable provider-id heya.media would
// accept for the given media item, in fallback priority order. Slug
// goes first when present (most stable, doesn't depend on any one
// upstream populating an external_id), then MBID / TMDB / etc. per
// providerOrderForKind.
//
// Used by the enrich worker to walk the chain on 404 — heya.media's
// slug lookups return 404 with no on-demand enrichment available, so
// we have to fall back to a provider-keyed lookup that *can* trigger
// upstream enrichment.
func BuildLookupIDs(kind metadata.MediaKind, externalIDs map[string]string, heyaSlug string) []string {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil
	}
	var ids []string
	if heyaSlug != "" {
		ids = append(ids, "heya:"+apiKind+":slug:"+heyaSlug)
	}
	for _, key := range providerOrderForKind(apiKind) {
		if v := externalIDs[key]; v != "" {
			ids = append(ids, "heya:"+apiKind+":"+canonicalProviderKey(key)+":"+v)
		}
	}
	return ids
}

// BuildLookupID returns a heya provider ID of the form
// "heya:<apiKind>:<provider>:<value>" — the canonical key for the
// /api/v1/{kind}/{id} endpoint. Picks the highest-priority identifier
// available: heya_slug first (most stable, doesn't depend on any single
// upstream provider populating an external_id), then the per-provider
// keys (mbid, tmdb, …) in apiKind-specific priority. Returns "" when
// no usable identifier is available.
//
// heya.media accepts a bare slug as the path id (e.g. /api/v1/artist/
// nogizaka46-2011), so for slug lookups we transparently strip the
// "slug:" prefix in fetchKindIDDetail before calling the API.
func BuildLookupID(kind metadata.MediaKind, externalIDs map[string]string, heyaSlug string) string {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return ""
	}
	if heyaSlug != "" {
		return "heya:" + apiKind + ":slug:" + heyaSlug
	}
	for _, key := range providerOrderForKind(apiKind) {
		if v := externalIDs[key]; v != "" {
			return "heya:" + apiKind + ":" + canonicalProviderKey(key) + ":" + v
		}
	}
	return ""
}

// providerOrderForKind lists the external-ID providers heya.media accepts
// for a given api kind, in our preferred priority. Order matches the
// v0.3.x /{kind}/{id} doc.
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

// parseProviderID splits a "heya:<apiKind>:<provider>:<value>" string
// into its three components. Returns ok=false if the input isn't in that
// shape.
func parseProviderID(providerID string) (apiKind, provider, value string, ok bool) {
	rest := strings.TrimPrefix(providerID, "heya:")
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// heyaKind translates metadata.MediaKind into heya.media's path segment.
func heyaKind(kind metadata.MediaKind) string {
	switch kind {
	case metadata.KindMovie:
		return "movie"
	case metadata.KindTV:
		return "tv"
	case metadata.KindMusic:
		// heya.media indexes artists (with embedded discography) as the
		// music entry point; album-level search goes through SearchAlbums.
		return "artist"
	case metadata.KindBook:
		return "book"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// SearchHit is the legacy shape returned by SearchArtistBest. We keep it
// in the public surface because callers in internal/matcher consume it
// directly; the fields mirror the generated gen.SearchHit but with plain
// (non-pointer) Go types for ergonomic access.
type SearchHit struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Year        int               `json:"year,omitempty"`
	Country     string            `json:"country,omitempty"`
	Image       string            `json:"image,omitempty"`
	Snippet     string            `json:"snippet,omitempty"`
	Sources     []string          `json:"sources"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	// AltTitles is the flat, deduped union of every locale variant the
	// upstream providers know about (kana / romaji / English / etc.).
	// Used by the matcher to score scanned filenames against every form.
	AltTitles []string `json:"alt_titles,omitempty"`
	Score     float64  `json:"score"`
	Enriched  bool     `json:"enriched"`
	Slug      string   `json:"slug,omitempty"`
}

// hasSource reports whether one of the hit's sources matches s
// (case-insensitive). Used by SearchArtistBest to bias toward MusicBrainz
// when no warm-cached hit exists.
func (h *SearchHit) hasSource(s string) bool {
	for _, src := range h.Sources {
		if strings.EqualFold(src, s) {
			return true
		}
	}
	return false
}

// Search executes /api/v1/search and maps the typed response into the
// metadata.SearchResult shape the matcher consumes.
func (p *HeyaProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil, fmt.Errorf("heya: unsupported kind %s", kind)
	}
	hits, err := p.searchHits(ctx, apiKind, query.Title, query.Year, query.Artist, 20)
	if err != nil {
		return nil, err
	}
	return mapHitsToResults(apiKind, hits), nil
}

// SearchAlbums executes /api/v1/search with type=album, optionally scoped to
// an artist name. Hit ids are MusicBrainz release-group ids (mbid:<uuid>), so
// the returned ProviderIDs look like "heya:album:mbid:<uuid>". Used by the
// metadata editor's per-album re-identify.
func (p *HeyaProvider) SearchAlbums(ctx context.Context, title, artist string) ([]metadata.SearchResult, error) {
	hits, err := p.searchHits(ctx, "album", title, "", artist, 20)
	if err != nil {
		return nil, err
	}
	return mapHitsToResults("album", hits), nil
}

func mapHitsToResults(apiKind string, hits []SearchHit) []metadata.SearchResult {
	results := make([]metadata.SearchResult, 0, len(hits))
	for _, h := range hits {
		providerID := "heya:" + apiKind + ":" + h.ID
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
			ExternalIDs:  h.ExternalIDs,
			AltTitles:    h.AltTitles,
			HeyaSlug:     h.Slug,
			Enriched:     h.Enriched,
		})
	}
	return results
}

// searchHits is the shared lookup used by Search() and SearchArtistBest().
func (p *HeyaProvider) searchHits(ctx context.Context, apiKind, query, year, artist string, limit int) ([]SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("heya: empty search query")
	}
	if limit <= 0 {
		limit = 10
	}
	params := &gen.SearchParams{
		Type: gen.SearchParamsType(apiKind),
		Q:    query,
	}
	lim := int64(limit)
	params.Limit = &lim
	if year != "" {
		if y, err := strconv.ParseInt(year, 10, 64); err == nil {
			params.Year = &y
		}
	}
	if artist != "" {
		params.Artist = &artist
	}
	resp, err := p.client.gen.SearchWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("heya search: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, upstreamErr("search", resp.StatusCode(), resp.Body)
	}
	if resp.JSON200.Results == nil {
		return nil, nil
	}
	out := make([]SearchHit, 0, len(*resp.JSON200.Results))
	for _, h := range *resp.JSON200.Results {
		out = append(out, SearchHit{
			ID:          h.Id,
			Kind:        h.Kind,
			Name:        h.Name,
			Year:        intPtr64AsInt(h.Year),
			Country:     strPtr(h.Country),
			Image:       strPtr(h.Image),
			Snippet:     strPtr(h.Snippet),
			Sources:     strs(h.Sources),
			ExternalIDs: mergeExternalIDs(h.ExternalIds, nil),
			AltTitles:   strs(h.AltTitles),
			Score:       h.Score,
			Enriched:    h.Enriched,
			Slug:        strPtr(h.Slug),
		})
	}
	return out, nil
}

// SearchArtistBest searches for an artist by name and picks the best hit
// to fetch. heya.media sorts the merged result list with enriched warm-
// cache rows first, then by score within source tier, so hits[0] is
// usually right. On cold lookups bias toward MusicBrainz so we land an
// MBID in our DB (cross-reference key) rather than an apple/deezer id we
// can fetch but not cross-reference later.
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
// Detail fetches
// ---------------------------------------------------------------------------

// GetDetail resolves a heya providerID ("heya:<kind>:<provider>:<value>")
// into the full enriched MediaDetail.
// GetDetailFallback tries each providerID in order, retrying on 404.
// Caller orders by preference (typically slug → mbid → other providers
// via BuildLookupIDs). The first non-404 result wins — either a
// successful detail or a hard error worth surfacing. Returns the last
// 404 if every ID misses, so the caller's failure message points at a
// real attempted lookup rather than an empty list.
func (p *HeyaProvider) GetDetailFallback(ctx context.Context, providerIDs []string, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	var lastErr error
	for _, id := range providerIDs {
		detail, err := p.GetDetail(ctx, id, opts)
		if err == nil {
			return detail, id, nil
		}
		lastErr = err
		if !IsNotFound(err) {
			return nil, id, err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("heya: no provider ids to try")
	}
	return nil, "", lastErr
}

func (p *HeyaProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	apiKind, provider, value, ok := parseProviderID(providerID)
	if !ok {
		return nil, fmt.Errorf("heya: invalid provider id %q (expected heya:<kind>:<provider>:<value>)", providerID)
	}
	// slug:<slug> goes on the wire as bare <slug> — heya.media accepts
	// /api/v1/<kind>/<slug> directly. Every other provider stays as
	// "<provider>:<value>" so the upstream knows which catalog to query.
	id := provider + ":" + value
	if provider == "slug" {
		id = value
	}
	return p.fetchKindIDDetail(ctx, apiKind, id)
}

// FetchByKindID hits /api/v1/{kind}/{id} where id is "<provider>:<value>".
// Returns the MediaDetail + the heya:<kind>:<id> providerID for storage.
//
// Valid (kind, provider) pairs per the v0.3.x OpenAPI: artist mbid/apple/
// discogs/deezer; movie tmdb/imdb; tv tmdb/imdb/tvdb (+anime IDs); book
// ol_work_id (501 on miss). A bad provider for the kind yields HTTP 400.
func (p *HeyaProvider) FetchByKindID(ctx context.Context, kind, id string) (*metadata.MediaDetail, string, error) {
	if kind == "" || id == "" {
		return nil, "", fmt.Errorf("heya: empty kind or id")
	}
	detail, err := p.fetchKindIDDetail(ctx, kind, id)
	if err != nil {
		return nil, "", err
	}
	return detail, "heya:" + kind + ":" + id, nil
}

// fetchKindIDDetail dispatches to the right typed endpoint and runs the
// matching mapper. All four endpoints (artist / movie / tv / book) share
// this shape so the public entry points stay a one-liner.
func (p *HeyaProvider) fetchKindIDDetail(ctx context.Context, apiKind, id string) (*metadata.MediaDetail, error) {
	switch apiKind {
	case "artist":
		resp, err := p.client.gen.FetchArtistWithResponse(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("heya fetch artist %s: %w", id, err)
		}
		if resp.JSON200 == nil {
			return nil, upstreamErr("artist/"+id, resp.StatusCode(), resp.Body)
		}
		return mapArtistDoc(resp.JSON200), nil
	case "movie":
		resp, err := p.client.gen.FetchMovieWithResponse(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("heya fetch movie %s: %w", id, err)
		}
		if resp.JSON200 == nil {
			return nil, upstreamErr("movie/"+id, resp.StatusCode(), resp.Body)
		}
		return mapMovieDoc(resp.JSON200), nil
	case "tv":
		resp, err := p.client.gen.FetchTvWithResponse(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("heya fetch tv %s: %w", id, err)
		}
		if resp.JSON200 == nil {
			return nil, upstreamErr("tv/"+id, resp.StatusCode(), resp.Body)
		}
		return mapTvDoc(resp.JSON200), nil
	case "book":
		resp, err := p.client.gen.FetchBookWithResponse(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("heya fetch book %s: %w", id, err)
		}
		if resp.JSON200 == nil {
			return nil, upstreamErr("book/"+id, resp.StatusCode(), resp.Body)
		}
		return mapBookDoc(resp.JSON200), nil
	default:
		return nil, fmt.Errorf("heya: unsupported kind %q", apiKind)
	}
}

// LookupByNFO tries each ID source in priority order against the
// /{kind}/{id} endpoint, returning the first successful enriched doc.
func (p *HeyaProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	apiKind := heyaKind(kind)
	if apiKind == "" {
		return nil, "", fmt.Errorf("heya: unsupported kind %s", kind)
	}
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
		detail, err := p.fetchKindIDDetail(ctx, apiKind, id)
		if err != nil {
			continue
		}
		return detail, "heya:" + apiKind + ":" + id, nil
	}
	return nil, "", fmt.Errorf("heya: no matching item for NFO IDs")
}

// FetchArtwork returns every classified artwork entry for the matching
// item. Calls the same /{kind}/{id} endpoint as GetDetail and extracts
// just the artwork tree — saves a round-trip when the caller only cares
// about images. Empty slice on lookup miss (errors are swallowed by
// design — artwork is best-effort).
func (p *HeyaProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string, opts *metadata.FetchOptions) ([]metadata.ArtworkResult, error) {
	art, err := p.fetchArtworkTree(ctx, kind, externalIDs)
	if err != nil || art == nil {
		return nil, nil
	}
	return mapArtwork(art), nil
}

// FetchRatings returns the rating list off the matching item. Best-effort
// in the same way as FetchArtwork.
func (p *HeyaProvider) FetchRatings(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*metadata.RatingsData, error) {
	ratings, err := p.fetchRatingsList(ctx, kind, externalIDs)
	if err != nil || ratings == nil {
		return nil, nil
	}
	return mapRatings(ratings), nil
}

// fetchArtworkTree resolves the doc and returns just the typed Artwork
// node. Movies/TV share Detail.Artwork; artists store their image pool
// flat in ArtistDetail.Images — for the artist case we synthesize a
// minimal Artwork node so the caller can stay uniform.
func (p *HeyaProvider) fetchArtworkTree(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*gen.Artwork, error) {
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
		switch apiKind {
		case "movie":
			resp, err := p.client.gen.FetchMovieWithResponse(ctx, id)
			if err != nil || resp.JSON200 == nil {
				continue
			}
			return resp.JSON200.Payload.Artwork, nil
		case "tv":
			resp, err := p.client.gen.FetchTvWithResponse(ctx, id)
			if err != nil || resp.JSON200 == nil {
				continue
			}
			return resp.JSON200.Payload.Artwork, nil
		case "artist":
			resp, err := p.client.gen.FetchArtistWithResponse(ctx, id)
			if err != nil || resp.JSON200 == nil {
				continue
			}
			// Artist doesn't have a structured Artwork tree; surface the
			// flat Images pool as posters so callers can still pick the
			// best entry.
			return &gen.Artwork{Posters: resp.JSON200.Payload.Images}, nil
		}
	}
	return nil, fmt.Errorf("heya: no matching external ID for artwork lookup (%s)", apiKind)
}

// fetchRatingsList resolves the doc and returns just the typed Ratings
// slice. Artist payloads don't carry ratings; the lookup short-circuits
// to nil in that case.
func (p *HeyaProvider) fetchRatingsList(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) (*[]gen.Rating, error) {
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
		switch apiKind {
		case "movie":
			resp, err := p.client.gen.FetchMovieWithResponse(ctx, id)
			if err != nil || resp.JSON200 == nil {
				continue
			}
			return resp.JSON200.Payload.Ratings, nil
		case "tv":
			resp, err := p.client.gen.FetchTvWithResponse(ctx, id)
			if err != nil || resp.JSON200 == nil {
				continue
			}
			return resp.JSON200.Payload.Ratings, nil
		}
	}
	return nil, fmt.Errorf("heya: no matching external ID for ratings lookup (%s)", apiKind)
}

// ---------------------------------------------------------------------------
// Similar artists
// ---------------------------------------------------------------------------

// SimilarHit is one row from /api/v1/similar/artist or /similar/track.
// Score is normalised 0..1 within each source (Last.fm vs ListenBrainz),
// so cross-source comparison is approximate at best.
type SimilarHit struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Artist string  `json:"artist,omitempty"`
	MBID   string  `json:"mbid,omitempty"`
	Score  float64 `json:"score"`
	Source string  `json:"source"`
	Image  string  `json:"image,omitempty"`
	URL    string  `json:"url,omitempty"`
}

// SimilarArtists fetches similar-artist suggestions. Prefers MBID lookup
// (more reliable cross-provider match); falls back to name search.
func (p *HeyaProvider) SimilarArtists(ctx context.Context, mbid, name string) ([]SimilarHit, error) {
	params := &gen.SimilarArtistParams{}
	switch {
	case mbid != "":
		params.Mbid = &mbid
	case name != "":
		params.Name = &name
	default:
		return nil, fmt.Errorf("heya: similar artists needs mbid or name")
	}
	resp, err := p.client.gen.SimilarArtistWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("heya similar artist: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, upstreamErr("similar/artist", resp.StatusCode(), resp.Body)
	}
	if resp.JSON200.Results == nil {
		return nil, nil
	}
	out := make([]SimilarHit, 0, len(*resp.JSON200.Results))
	for _, c := range *resp.JSON200.Results {
		out = append(out, SimilarHit{
			Kind:   c.Kind,
			Name:   c.Name,
			Artist: strPtr(c.Artist),
			MBID:   strPtr(c.Mbid),
			Score:  c.Score,
			Source: string(c.Source),
			Image:  strPtr(c.Image),
			URL:    strPtr(c.Url),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Person lookup
// ---------------------------------------------------------------------------

// HeyaPersonResponse, HeyaPersonPayload, HeyaIDs, HeyaArtworkItem are
// legacy compat types kept alive because internal/worker/person_worker.go
// consumes them directly. mapPersonDoc populates them from the generated
// PersonDocBody — keep the shape stable until the worker is migrated.

// HeyaPersonResponse is the top-level person doc returned by
// /api/v1/person/{id}.
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
	// External credits — the Heya metadata aggregator surfaces what a
	// person has acted in (`Cast`), crewed on (`Crew`), and a curated
	// highlight set (`KnownForTitles`). These cover titles regardless of
	// whether we have them in the local library; the FE pairs them with
	// the local `media_cast` / `media_crew` joins to render an "in
	// library" vs "known for" view.
	Cast           []HeyaCredit `json:"cast,omitempty"`
	Crew           []HeyaCredit `json:"crew,omitempty"`
	KnownForTitles []HeyaCredit `json:"known_for_titles,omitempty"`
}

// HeyaCredit mirrors clients/heyamedia/openapi.json::Credit. All fields are
// optional upstream — empty values are the empty zero value, not pointers.
type HeyaCredit struct {
	Title        string `json:"title"`
	Year         int    `json:"year,omitempty"`
	Character    string `json:"character,omitempty"`
	Job          string `json:"job,omitempty"`
	Department   string `json:"department,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Slug         string `json:"slug,omitempty"`
	TmdbID       int    `json:"tmdb_id,omitempty"`
	TvdbID       int    `json:"tvdb_id,omitempty"`
	ImdbID       string `json:"imdb_id,omitempty"`
	PosterURL    string `json:"poster_url,omitempty"`
	EpisodeCount int    `json:"episode_count,omitempty"`
	Order        int    `json:"order,omitempty"`
	Source       string `json:"source,omitempty"`
}

// HeyaArtworkItem is the legacy profile-image shape used by the person
// payload. New code should reach for metadata.ProfileImage /
// metadata.ArtworkResult; this stays for person_worker.go compat.
type HeyaArtworkItem struct {
	URL    string  `json:"url"`
	Source string  `json:"source"`
	Aspect string  `json:"aspect"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Score  float64 `json:"score"`
	Likes  int     `json:"likes"`
}

// GetPersonFromHeya is the package-level entry — workers that hold a
// *Client (not a *HeyaProvider) call it directly.
func GetPersonFromHeya(ctx context.Context, c *Client, tmdbID int) (*HeyaPersonResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("heya: nil client")
	}
	id := "tmdb:" + strconv.Itoa(tmdbID)
	resp, err := c.gen.FetchPersonWithResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("heya fetch person %s: %w", id, err)
	}
	if resp.JSON200 == nil {
		return nil, upstreamErr("person/"+id, resp.StatusCode(), resp.Body)
	}
	return mapPersonDoc(resp.JSON200), nil
}

// GetPersonDetail is the HeyaProvider-style wrapper around
// GetPersonFromHeya for callers that already hold a *HeyaProvider.
func (p *HeyaProvider) GetPersonDetail(ctx context.Context, tmdbID int) (*HeyaPersonResponse, error) {
	return GetPersonFromHeya(ctx, p.client, tmdbID)
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// UpstreamError is the typed error returned by failing heya.media calls.
// Callers that want to retry on specific HTTP statuses (e.g. fall back
// from a slug-based lookup to MBID-based on 404) can errors.As() it.
type UpstreamError struct {
	Path    string
	Status  int
	Snippet string
}

func (e *UpstreamError) Error() string {
	if e.Snippet != "" {
		return fmt.Sprintf("heya %s: HTTP %d: %s", e.Path, e.Status, e.Snippet)
	}
	return fmt.Sprintf("heya %s: HTTP %d", e.Path, e.Status)
}

// IsNotFound reports whether err originated as an HTTP 404 from
// heya.media. Used by enrich workers to know when to fall back to
// an alternate lookup ID (slug → MBID, MBID → name search).
func IsNotFound(err error) bool {
	var u *UpstreamError
	if errors.As(err, &u) {
		return u.Status == 404
	}
	return false
}

func upstreamErr(path string, status int, body []byte) error {
	snippet := ""
	if len(body) > 0 {
		max := 256
		if len(body) < max {
			max = len(body)
		}
		snippet = string(body[:max])
	}
	return &UpstreamError{Path: path, Status: status, Snippet: snippet}
}

// Small helpers shared with mappers.go.

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

// copyStringMap clones a map[string]string so the caller can hold onto
// the result without aliasing the upstream payload. Returns nil for the
// empty input — the matcher / persistence layer treats nil and {}
// equivalently.
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

// mergeStrings returns the deduped union of two string slices, preserving
// the order of `a` first.
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
