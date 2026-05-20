package anidb

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/rs/zerolog/log"
)

const apiURL = "http://api.anidb.net:9001/httpapi"

type Provider struct {
	client     *metadata.RateLimitedClient
	clientName string
	titles     *TitleCache
}

func NewProvider(clientName, dataDir string) *Provider {
	// 0.2 req/sec = 1 request every 5 seconds — well within AniDB's limits
	client := metadata.NewRateLimitedClient(0.2, 1, "Heya/1.0")
	tc := NewTitleCache(dataDir)

	go func() {
		if err := tc.EnsureLoaded(); err != nil {
			log.Warn().Err(err).Msg("anidb title cache initial load failed")
		}
	}()

	return &Provider{
		client:     client,
		clientName: clientName,
		titles:     tc,
	}
}

func (p *Provider) Name() string { return "anidb" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindTV || kind == metadata.KindMovie
}

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if err := p.titles.EnsureLoaded(); err != nil {
		return nil, fmt.Errorf("anidb title cache: %w", err)
	}

	matches := p.titles.Search(query.Title, 10)
	if len(matches) == 0 {
		return nil, nil
	}

	var results []metadata.SearchResult
	for _, m := range matches {
		results = append(results, metadata.SearchResult{
			ProviderID:   "anidb:" + strconv.Itoa(m.AID),
			ProviderName: "anidb",
			Title:        m.Title,
			Confidence:   m.Score,
		})
	}
	return results, nil
}

func (p *Provider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid anidb provider ID: %s", providerID)
	}
	aid := parts[1]

	anime, err := p.fetchAnime(ctx, aid)
	if err != nil {
		return nil, err
	}

	return p.convertAnime(anime, aid), nil
}

func (p *Provider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	// AniDB IDs aren't typically in NFOs, but some tools use them
	// Check if any of the IDs map — for now we don't have a mapping service
	return nil, "", fmt.Errorf("anidb: no direct ID available in NFO")
}

func (p *Provider) fetchAnime(ctx context.Context, aid string) (*animeResponse, error) {
	u := fmt.Sprintf("%s?request=anime&client=%s&clientver=1&protover=1&aid=%s",
		apiURL, p.clientName, aid)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("anidb fetch aid=%s: %w", aid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("anidb aid=%s: HTTP %d: %s", aid, resp.StatusCode, string(body))
	}

	var anime animeResponse
	if err := xml.NewDecoder(resp.Body).Decode(&anime); err != nil {
		return nil, fmt.Errorf("anidb decode aid=%s: %w", aid, err)
	}

	return &anime, nil
}

func (p *Provider) convertAnime(a *animeResponse, aid string) *metadata.MediaDetail {
	mainTitle := pickTitle(a.Titles, "main", "")
	enTitle := pickTitle(a.Titles, "official", "en")
	if enTitle == "" {
		enTitle = mainTitle
	}

	year := ""
	if len(a.StartDate) >= 4 {
		year = a.StartDate[:4]
	}

	var genres []string
	for _, t := range a.Tags {
		if t.Weight >= 200 && len(genres) < 10 {
			genres = append(genres, t.Name)
		}
	}

	rating := a.Ratings.Permanent.Value
	if rating == 0 {
		rating = a.Ratings.Temporary.Value
	}

	posterURL := ""
	if a.Picture != "" {
		posterURL = imageBaseURL + a.Picture
	}

	cast := p.convertCharacters(a.Characters)

	isMovie := strings.EqualFold(a.Type, "Movie")

	detail := &metadata.MediaDetail{
		Title:        enTitle,
		SortTitle:    strings.ToLower(enTitle),
		Year:         year,
		Description:  cleanDescription(a.Description),
		PosterURL:    posterURL,
		ExternalIDs:  map[string]string{"anidb": aid},
		Genres:       genres,
		Rating:       rating,
		OriginalName: mainTitle,
		Cast:         cast,
	}

	if isMovie {
		detail.RuntimeMinutes = totalRuntime(a.Episodes)
		detail.ReleaseDate = a.StartDate
	} else {
		detail.FirstAirDate = a.StartDate
		detail.LastAirDate = a.EndDate
		detail.Status = animeStatus(a.EndDate)
		detail.NumberOfEpisodes = a.EpisodeCount

		seasons := p.buildSeasons(a.Episodes)
		detail.Seasons = seasons
		detail.NumberOfSeasons = len(seasons)
	}

	return detail
}

func (p *Provider) convertCharacters(chars []animeChar) []metadata.CastMember {
	var cast []metadata.CastMember
	for _, c := range chars {
		if c.Seiyuu == nil {
			continue
		}
		profilePath := ""
		if c.Seiyuu.Picture != "" {
			profilePath = imageBaseURL + c.Seiyuu.Picture
		}
		cast = append(cast, metadata.CastMember{
			Name:        c.Seiyuu.Name,
			Character:   c.Name,
			ProfilePath: profilePath,
			Order:       len(cast),
		})
		if len(cast) >= 30 {
			break
		}
	}
	return cast
}

func (p *Provider) buildSeasons(episodes []animeEpisode) []metadata.SeasonDetail {
	var regular []metadata.EpisodeDetail
	for _, ep := range episodes {
		if ep.EpNo.Type != epTypeRegular {
			continue
		}
		num, _ := strconv.Atoi(ep.EpNo.Value)
		if num == 0 {
			continue
		}
		epRating := 0.0
		if ep.Rating != nil {
			epRating = ep.Rating.Value
		}
		regular = append(regular, metadata.EpisodeDetail{
			Number:         num,
			Title:          pickEpTitle(ep.Titles, "en"),
			Overview:       "",
			RuntimeMinutes: ep.Length,
			AirDate:        ep.AirDate,
			Rating:         epRating,
		})
	}

	if len(regular) == 0 {
		return nil
	}

	return []metadata.SeasonDetail{
		{
			Number:   1,
			Title:    "Season 1",
			Episodes: regular,
		},
	}
}

func pickTitle(titles []animeTitle, titleType, lang string) string {
	for _, t := range titles {
		if t.Type == titleType && (lang == "" || t.Lang == lang) {
			return t.Value
		}
	}
	if lang != "" {
		for _, t := range titles {
			if t.Type == titleType {
				return t.Value
			}
		}
	}
	if len(titles) > 0 {
		return titles[0].Value
	}
	return ""
}

func pickEpTitle(titles []animeEpTitle, lang string) string {
	for _, t := range titles {
		if t.Lang == lang {
			return t.Value
		}
	}
	if len(titles) > 0 {
		return titles[0].Value
	}
	return ""
}

func totalRuntime(episodes []animeEpisode) int {
	total := 0
	for _, ep := range episodes {
		if ep.EpNo.Type == epTypeRegular {
			total += ep.Length
		}
	}
	return total
}

func animeStatus(endDate string) string {
	if endDate == "" {
		return "Continuing"
	}
	return "Ended"
}

func cleanDescription(s string) string {
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")

	var result strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			result.WriteRune(r)
		}
	}

	lines := strings.Split(result.String(), "\n")
	var cleaned []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "Source:") && !strings.HasPrefix(l, "Note:") {
			cleaned = append(cleaned, l)
		}
	}
	return strings.Join(cleaned, "\n\n")
}
