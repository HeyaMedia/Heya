package heyametadata

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	gen "github.com/karbowiak/heya/clients/heyametadata"
)

// These compatibility DTOs isolate the existing person read-model writer
// from the V2 transport. They can disappear once that worker writes directly
// from PersonDocument and PersonCredit.
type HeyaPersonResponse struct {
	ID                string
	Kind              string
	Title             string
	Year              int
	Slug              string
	Poster            string
	SchemaVersion     int
	ProjectionVersion int64
	IDs               HeyaIDs
	Payload           HeyaPersonPayload
}

type HeyaIDs struct {
	IMDB                                   string
	TMDB, TVDB, AniDB, TVMaze, TVRage, MAL int
	MBID, OLWorkID                         string
}

type HeyaPersonPayload struct {
	Name, SortName, KnownForDepartment, Gender, Slug, Birthday, BirthPlace, Deathday, Biography, Homepage string
	BirthYear                                                                                             int
	AlsoKnownAs                                                                                           []string
	Biographies                                                                                           map[string]string
	Profiles                                                                                              []HeyaArtworkItem
	ExternalIDs                                                                                           map[string]string
	Popularity                                                                                            float64
	Cast, Crew, KnownForTitles                                                                            []HeyaCredit
}

type HeyaCredit struct {
	Title                                  string
	Year                                   int
	Character, Job, Department, Kind, Slug string
	TmdbID, TvdbID                         int
	ImdbID, PosterURL                      string
	EpisodeCount, Order                    int
	Source                                 string
}

type HeyaArtworkItem struct {
	URL, Source, Aspect string
	Width, Height       int
	Score               float64
	Likes               int
}

func GetPersonByEntityFromHeya(ctx context.Context, client *Client, entityID string, credentials ...ProviderCredentials) (*HeyaPersonResponse, error) {
	if client == nil {
		return nil, fmt.Errorf("heyametadata: nil client")
	}
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid canonical person ID %q: %w", entityID, err)
	}
	detailResponse, err := client.gen.PersonDetailWithResponse(ctx, id, &gen.PersonDetailParams{}, client.credentialEditor(firstCredentials(credentials)))
	if err != nil {
		return nil, fmt.Errorf("read canonical person %s: %w", id, err)
	}
	if detailResponse.StatusCode() != http.StatusOK || detailResponse.JSON200 == nil {
		return nil, responseError("read canonical person", detailResponse.StatusCode(), detailResponse.Body)
	}
	page, err := client.personCredits(ctx, id.String(), credentials...)
	if err != nil {
		return nil, fmt.Errorf("read canonical person credits %s: %w", id, err)
	}
	return client.mapPerson(ctx, detailResponse.JSON200, page, credentials...)
}

func (c *Client) personCredits(ctx context.Context, entityID string, credentials ...ProviderCredentials) (*gen.PersonCreditsOutputBody, error) {
	id, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid canonical person ID %q: %w", entityID, err)
	}
	const pageSize = int64(250)
	combined := &gen.PersonCreditsOutputBody{}
	all := make([]gen.PersonCredit, 0)
	for offset := int64(0); ; {
		limit := pageSize
		response, err := c.gen.PersonCreditsWithResponse(ctx, id, &gen.PersonCreditsParams{Limit: &limit, Offset: &offset}, c.credentialEditor(firstCredentials(credentials)))
		if err != nil {
			return nil, fmt.Errorf("read canonical person credits %s: %w", id, err)
		}
		if response.StatusCode() != http.StatusOK || response.JSON200 == nil {
			return nil, responseError("read canonical person credits", response.StatusCode(), response.Body)
		}
		page := response.JSON200
		if offset == 0 {
			combined.Schema = page.Schema
			combined.Limit = page.Limit
			combined.Offset = page.Offset
			combined.Person = page.Person
		}
		combined.Total = page.Total
		pageCredits := slicePersonCredits(page.Credits)
		all = append(all, pageCredits...)
		combined.Credits = &all
		if int64(len(all)) >= page.Total {
			return combined, nil
		}
		if len(pageCredits) == 0 {
			return nil, fmt.Errorf("read canonical person credits: page at offset %d returned no credits before total %d", offset, page.Total)
		}
		offset += int64(len(pageCredits))
	}
}

func slicePersonCredits(value *[]gen.PersonCredit) []gen.PersonCredit {
	if value == nil {
		return nil
	}
	return *value
}

func (c *Client) mapPerson(ctx context.Context, document *gen.PersonDocument, page *gen.PersonCreditsOutputBody, credentials ...ProviderCredentials) (*HeyaPersonResponse, error) {
	entityID := document.Id.String()
	result := &HeyaPersonResponse{ID: entityID, Kind: document.Kind, Title: document.Display.Title, Slug: entityID, SchemaVersion: int(document.SchemaVersion), ProjectionVersion: document.ProjectionVersion, IDs: HeyaIDs{}}
	result.Payload = HeyaPersonPayload{Name: document.Display.Title, SortName: document.Display.Title, Slug: entityID, ExternalIDs: map[string]string{}, Biographies: map[string]string{}}
	if document.ExternalIds != nil {
		for _, id := range *document.ExternalIds {
			result.Payload.ExternalIDs[id.Provider] = id.Value
			switch id.Provider {
			case "tmdb":
				result.IDs.TMDB, _ = strconv.Atoi(id.Value)
			case "tvdb":
				result.IDs.TVDB, _ = strconv.Atoi(id.Value)
			case "imdb":
				result.IDs.IMDB = id.Value
			}
		}
	}
	data := document.Data
	result.Payload.AlsoKnownAs = sliceValue(data.Names)
	result.Payload.KnownForDepartment = stringValue(data.KnownForDepartment)
	result.Payload.Gender = strings.ToLower(stringValue(data.Gender))
	result.Payload.Birthday = stringValue(data.BirthDate)
	result.Payload.Deathday = stringValue(data.DeathDate)
	result.Payload.BirthPlace = stringValue(data.PlaceOfBirth)
	result.Payload.Biography = stringValue(data.Biography)
	result.Payload.Homepage = stringValue(data.Homepage)
	if data.Popularity != nil {
		result.Payload.Popularity = *data.Popularity
	}
	if data.Biographies != nil {
		result.Payload.Biographies = *data.Biographies
	}
	if len(result.Payload.Birthday) >= 4 {
		result.Year, _ = strconv.Atoi(result.Payload.Birthday[:4])
		result.Payload.BirthYear = result.Year
	}
	if document.Display.ImageId != nil {
		result.Poster = c.ImageURL(*document.Display.ImageId)
	}
	images, err := c.Images(ctx, entityID, "", "", credentials...)
	if err != nil {
		return nil, fmt.Errorf("read canonical person images: %w", err)
	}
	if images.Results != nil {
		var selectedProfile *HeyaArtworkItem
		for _, image := range *images.Results {
			if image.Class != "profile" {
				continue
			}
			mapped := HeyaArtworkItem{URL: c.ImageURL(image.Id), Source: image.Provider, Width: int64PtrInt(image.Width), Height: int64PtrInt(image.Height), Score: float64Ptr(image.ProviderScore)}
			if image.Id == images.Selections["profile"] || image.Selected {
				result.Poster = mapped.URL
				selectedProfile = &mapped
				continue
			}
			result.Payload.Profiles = append(result.Payload.Profiles, mapped)
		}
		if selectedProfile != nil {
			result.Payload.Profiles = append([]HeyaArtworkItem{*selectedProfile}, result.Payload.Profiles...)
		}
	}
	if len(result.Payload.Profiles) == 0 && result.Poster != "" {
		result.Payload.Profiles = []HeyaArtworkItem{{URL: result.Poster, Source: "heyametadata"}}
	}
	if page != nil && page.Credits != nil {
		for _, credit := range *page.Credits {
			mapped := mapPersonCredit(c, credit)
			if credit.CreditType == "cast" {
				result.Payload.Cast = append(result.Payload.Cast, mapped)
			} else {
				result.Payload.Crew = append(result.Payload.Crew, mapped)
			}
			if len(result.Payload.KnownForTitles) < 20 {
				result.Payload.KnownForTitles = append(result.Payload.KnownForTitles, mapped)
			}
		}
	}
	return result, nil
}

func mapPersonCredit(c *Client, credit gen.PersonCredit) HeyaCredit {
	result := HeyaCredit{Title: credit.Title, Year: int64PtrInt(credit.Year), Character: stringValue(credit.Character), Job: stringValue(credit.Job), Department: stringValue(credit.Department), Kind: legacyKind(credit.Kind), Order: int64PtrInt(credit.Order), Source: credit.Provider}
	if credit.EntityId != nil {
		result.Slug = credit.EntityId.String()
	}
	if credit.ImageId != nil {
		result.PosterURL = c.ImageURL(credit.ImageId.String())
	}
	if credit.ProviderTargetId != nil {
		value := *credit.ProviderTargetId
		switch credit.Provider {
		case "tmdb":
			result.TmdbID, _ = strconv.Atoi(value)
		case "tvdb":
			result.TvdbID, _ = strconv.Atoi(value)
		case "imdb":
			result.ImdbID = value
		}
	}
	return result
}

func int64PtrInt(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}
func float64Ptr(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
