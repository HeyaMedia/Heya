package openlibrary

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

const baseURL = "https://openlibrary.org"

type Provider struct {
	client  *metadata.RateLimitedClient
	BaseURL string
}

func NewProvider() *Provider {
	client := metadata.NewRateLimitedClient(5.0, 5, "Heya/1.0 (https://github.com/karbowiak/heya)")
	return &Provider{client: client, BaseURL: baseURL}
}

func (p *Provider) Name() string { return "openlibrary" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindBook
}

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if query.ISBN != "" {
		return p.searchByISBN(ctx, query.ISBN)
	}
	return p.searchByTitle(ctx, query)
}

func (p *Provider) GetDetail(ctx context.Context, providerID string) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid provider ID: %s", providerID)
	}
	workKey := parts[1]

	var work workDetail
	if err := p.client.GetJSON(ctx, p.BaseURL+workKey+".json", &work); err != nil {
		return nil, fmt.Errorf("fetching work: %w", err)
	}

	description := extractText(work.Description)

	detail := &metadata.MediaDetail{
		Title:       work.Title,
		SortTitle:   strings.ToLower(work.Title),
		Description: description,
		Subjects:    work.Subjects,
		ExternalIDs: map[string]string{"openlibrary": workKey},
	}

	if len(work.Covers) > 0 {
		detail.PosterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", work.Covers[0])
	}

	if len(work.Authors) > 0 {
		authorKey := work.Authors[0].Author.Key
		var author authorDetail
		if err := p.client.GetJSON(ctx, p.BaseURL+authorKey+".json", &author); err == nil {
			detail.AuthorName = author.Name
			detail.AuthorBio = extractText(author.Bio)
			detail.AuthorBirthDate = author.BirthDate
			detail.AuthorDeathDate = author.DeathDate
			detail.ExternalIDs["openlibrary_author"] = authorKey
		}
	}

	var editions editionsResponse
	edURL := p.BaseURL + workKey + "/editions.json?limit=5"
	if err := p.client.GetJSON(ctx, edURL, &editions); err == nil && len(editions.Entries) > 0 {
		ed := pickBestEdition(editions.Entries)
		if len(ed.ISBN13) > 0 {
			detail.ISBN = ed.ISBN13[0]
		} else if len(ed.ISBN10) > 0 {
			detail.ISBN = ed.ISBN10[0]
		}
		detail.PageCount = ed.NumberOfPages
		if len(ed.Publishers) > 0 {
			detail.Publisher = ed.Publishers[0]
		}
		detail.PublishDate = ed.PublishDate
		if len(ed.Languages) > 0 {
			lang := ed.Languages[0].Key
			detail.Language = strings.TrimPrefix(lang, "/languages/")
		}
		if len(ed.Covers) > 0 && detail.PosterURL == "" {
			detail.PosterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", ed.Covers[0])
		}
	}

	if detail.Year == "" && detail.PublishDate != "" {
		for _, part := range strings.Fields(detail.PublishDate) {
			if len(part) == 4 {
				if _, err := strconv.Atoi(part); err == nil {
					detail.Year = part
					break
				}
			}
		}
	}

	return detail, nil
}

func (p *Provider) searchByISBN(ctx context.Context, isbn string) ([]metadata.SearchResult, error) {
	var resp isbnResponse
	u := p.BaseURL + "/isbn/" + isbn + ".json"
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return nil, nil
	}

	workKey := ""
	if len(resp.Works) > 0 {
		workKey = resp.Works[0].Key
	}

	result := metadata.SearchResult{
		ProviderID:   "openlibrary:" + workKey,
		ProviderName: "openlibrary",
		Title:        resp.Title,
		Confidence:   0.99,
	}

	if len(resp.Covers) > 0 {
		result.PosterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", resp.Covers[0])
	}

	return []metadata.SearchResult{result}, nil
}

func (p *Provider) searchByTitle(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{
		"limit": {"10"},
	}
	if query.Title != "" {
		params.Set("title", query.Title)
	}
	if query.Author != "" {
		params.Set("author", query.Author)
	}

	u := p.BaseURL + "/search.json?" + params.Encode()
	var resp searchResponse
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for _, doc := range resp.Docs {
		year := ""
		if doc.FirstPublishYear > 0 {
			year = strconv.Itoa(doc.FirstPublishYear)
		}

		author := ""
		if len(doc.AuthorName) > 0 {
			author = doc.AuthorName[0]
		}

		title := doc.Title
		if author != "" {
			title = author + " - " + doc.Title
		}

		posterURL := ""
		if doc.CoverI > 0 {
			posterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", doc.CoverI)
		}

		results = append(results, metadata.SearchResult{
			ProviderID:   "openlibrary:" + doc.Key,
			ProviderName: "openlibrary",
			Title:        title,
			Year:         year,
			PosterURL:    posterURL,
			RawData:      doc,
		})
	}

	return results, nil
}

func extractText(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]interface{}:
		if text, ok := val["value"].(string); ok {
			return text
		}
	}
	return ""
}

func pickBestEdition(editions []editionEntry) editionEntry {
	for _, ed := range editions {
		if len(ed.ISBN13) > 0 && ed.NumberOfPages > 0 {
			return ed
		}
	}
	for _, ed := range editions {
		if len(ed.ISBN13) > 0 || len(ed.ISBN10) > 0 {
			return ed
		}
	}
	return editions[0]
}
