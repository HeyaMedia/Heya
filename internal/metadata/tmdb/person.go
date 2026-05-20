package tmdb

import (
	"context"
	"fmt"
	"net/url"
)

type PersonDetail struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Biography    string   `json:"biography"`
	Birthday     string   `json:"birthday"`
	Deathday     string   `json:"deathday"`
	PlaceOfBirth string   `json:"place_of_birth"`
	AlsoKnownAs  []string `json:"also_known_as"`
	Gender       int      `json:"gender"`
	ProfilePath  string   `json:"profile_path"`
	Homepage     string   `json:"homepage"`
	Popularity   float64  `json:"popularity"`
	ImdbID       string   `json:"imdb_id"`
	ExternalIDs  struct {
		ImdbID      string `json:"imdb_id"`
		WikidataID  string `json:"wikidata_id"`
		FacebookID  string `json:"facebook_id"`
		InstagramID string `json:"instagram_id"`
		TwitterID   string `json:"twitter_id"`
	} `json:"external_ids"`
}

func (p *Provider) GetPersonDetail(ctx context.Context, tmdbID int) (*PersonDetail, error) {
	var d PersonDetail
	params := url.Values{
		"append_to_response": {"external_ids"},
	}
	if err := p.get(ctx, fmt.Sprintf("/person/%d", tmdbID), params, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
