package heyamedia

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type OpenLibraryProvider struct {
	client *Client
}

func NewOpenLibraryProvider(c *Client) *OpenLibraryProvider {
	return &OpenLibraryProvider{client: c}
}

func (p *OpenLibraryProvider) Name() string { return "openlibrary" }

func (p *OpenLibraryProvider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindBook
}

func (p *OpenLibraryProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if query.ISBN != "" {
		return p.searchByISBN(ctx, query.ISBN)
	}
	return p.searchByTitle(ctx, query)
}

func (p *OpenLibraryProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid openlibrary provider ID: %s", providerID)
	}
	workID := parts[1]

	var work olWorkDetail
	if err := p.client.getJSON(ctx, "/api/v1/openlib/works/"+workID, &work); err != nil {
		return nil, fmt.Errorf("fetching work: %w", err)
	}

	description := olExtractText(work.Description)

	detail := &metadata.MediaDetail{
		Title:       work.Title,
		SortTitle:   strings.ToLower(work.Title),
		Description: description,
		Subjects:    work.Subjects,
		ExternalIDs: map[string]string{"openlibrary": workID},
	}

	if len(work.Covers) > 0 {
		detail.PosterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", work.Covers[0])
	}

	if len(work.Authors) > 0 {
		authorKey := work.Authors[0].Author.Key
		authorID := strings.TrimPrefix(authorKey, "/authors/")
		var author olAuthorDetail
		if err := p.client.getJSON(ctx, "/api/v1/openlib/authors/"+authorID, &author); err == nil {
			detail.AuthorName = author.Name
			detail.AuthorBio = olExtractText(author.Bio)
			detail.AuthorBirthDate = author.BirthDate
			detail.AuthorDeathDate = author.DeathDate
			detail.ExternalIDs["openlibrary_author"] = authorKey
		}
	}

	params := url.Values{"limit": {"5"}}
	var editions olEditionsResponse
	if err := p.client.get(ctx, "/api/v1/openlib/works/"+workID+"/editions", params, &editions); err == nil && len(editions.Entries) > 0 {
		ed := olPickBestEdition(editions.Entries)
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

func (p *OpenLibraryProvider) searchByISBN(ctx context.Context, isbn string) ([]metadata.SearchResult, error) {
	var resp olISBNResponse
	if err := p.client.getJSON(ctx, "/api/v1/openlib/isbn/"+isbn, &resp); err != nil {
		return nil, nil
	}

	workKey := ""
	if len(resp.Works) > 0 {
		workKey = resp.Works[0].Key
	}
	workID := strings.TrimPrefix(workKey, "/works/")

	result := metadata.SearchResult{
		ProviderID: "openlibrary:" + workID, ProviderName: "openlibrary",
		Title: resp.Title, Confidence: 0.99,
	}
	if len(resp.Covers) > 0 {
		result.PosterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", resp.Covers[0])
	}
	return []metadata.SearchResult{result}, nil
}

func (p *OpenLibraryProvider) searchByTitle(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{"limit": {"10"}}
	if query.Title != "" {
		params.Set("title", query.Title)
	}
	if query.Author != "" {
		params.Set("author", query.Author)
	}

	var resp olSearchResponse
	if err := p.client.get(ctx, "/api/v1/openlib/search", params, &resp); err != nil {
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
		workID := strings.TrimPrefix(doc.Key, "/works/")
		results = append(results, metadata.SearchResult{
			ProviderID: "openlibrary:" + workID, ProviderName: "openlibrary",
			Title: title, Year: year, PosterURL: posterURL, RawData: doc,
		})
	}
	return results, nil
}

func olExtractText(v interface{}) string {
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

func olPickBestEdition(editions []olEditionEntry) olEditionEntry {
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

// --- OpenLibrary response types ---

type olSearchResponse struct {
	Docs []olSearchDoc `json:"docs"`
}

type olSearchDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	CoverI           int      `json:"cover_i"`
}

type olWorkDetail struct {
	Title       string      `json:"title"`
	Description interface{} `json:"description"`
	Covers      []int       `json:"covers"`
	Subjects    []string    `json:"subjects"`
	Authors     []struct {
		Author struct {
			Key string `json:"key"`
		} `json:"author"`
	} `json:"authors"`
}

type olAuthorDetail struct {
	Name      string      `json:"name"`
	Bio       interface{} `json:"bio"`
	BirthDate string      `json:"birth_date"`
	DeathDate string      `json:"death_date"`
}

type olISBNResponse struct {
	Title string `json:"title"`
	Works []struct {
		Key string `json:"key"`
	} `json:"works"`
	Covers []int `json:"covers"`
}

type olEditionsResponse struct {
	Entries []olEditionEntry `json:"entries"`
}

type olEditionEntry struct {
	NumberOfPages int      `json:"number_of_pages"`
	Publishers    []string `json:"publishers"`
	PublishDate   string   `json:"publish_date"`
	ISBN13        []string `json:"isbn_13"`
	ISBN10        []string `json:"isbn_10"`
	Covers        []int    `json:"covers"`
	Languages     []struct {
		Key string `json:"key"`
	} `json:"languages"`
}
