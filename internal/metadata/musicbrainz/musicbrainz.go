package musicbrainz

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/karbowiak/kura/internal/metadata"
)

const (
	baseURL     = "https://musicbrainz.org/ws/2"
	coverArtURL = "https://coverartarchive.org"
)

type Provider struct {
	client *metadata.RateLimitedClient
}

func NewProvider() *Provider {
	client := metadata.NewRateLimitedClient(1.0, 1, "Kura/1.0 (https://github.com/karbowiak/kura)")
	return &Provider{client: client}
}

func (p *Provider) Name() string { return "musicbrainz" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMusic
}

func (p *Provider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs) (*metadata.MediaDetail, string, error) {
	if ids.MBID == "" {
		return nil, "", fmt.Errorf("no MusicBrainz ID available")
	}
	providerID := "musicbrainz:" + ids.MBID
	detail, err := p.GetDetail(ctx, providerID)
	if err != nil {
		return nil, "", err
	}
	return detail, providerID, nil
}

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	var q string
	if query.Artist != "" && query.Album != "" {
		q = fmt.Sprintf("releasegroup:%s AND artist:%s", query.Album, query.Artist)
	} else if query.Title != "" {
		q = query.Title
	} else {
		return nil, nil
	}

	params := url.Values{
		"query": {q},
		"fmt":   {"json"},
		"limit": {"10"},
	}

	u := baseURL + "/release-group/?" + params.Encode()
	var resp searchResponse
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for _, rg := range resp.ReleaseGroups {
		year := ""
		if len(rg.FirstRelease) >= 4 {
			year = rg.FirstRelease[:4]
		}

		artist := ""
		if len(rg.ArtistCredit) > 0 {
			artist = rg.ArtistCredit[0].Artist.Name
		}

		title := rg.Title
		if artist != "" {
			title = artist + " - " + rg.Title
		}

		results = append(results, metadata.SearchResult{
			ProviderID:   "musicbrainz:" + rg.ID,
			ProviderName: "musicbrainz",
			Title:        title,
			Year:         year,
			Description:  rg.PrimaryType,
			RawData:      rg,
		})
	}

	return results, nil
}

func (p *Provider) GetDetail(ctx context.Context, providerID string) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid provider ID: %s", providerID)
	}
	mbid := parts[1]

	params := url.Values{
		"inc": {"artists+releases+genres+tags"},
		"fmt": {"json"},
	}
	u := baseURL + "/release-group/" + mbid + "?" + params.Encode()

	var rg releaseGroupDetail
	if err := p.client.GetJSON(ctx, u, &rg); err != nil {
		return nil, err
	}

	year := ""
	if len(rg.FirstRelease) >= 4 {
		year = rg.FirstRelease[:4]
	}

	artistName := ""
	artistMBID := ""
	if len(rg.ArtistCredit) > 0 {
		artistName = rg.ArtistCredit[0].Artist.Name
		artistMBID = rg.ArtistCredit[0].Artist.ID
	}

	genres := extractNames(rg.Genres)
	tags := extractNames(rg.Tags)

	detail := &metadata.MediaDetail{
		Title:      rg.Title,
		SortTitle:  strings.ToLower(rg.Title),
		Year:       year,
		AlbumTitle: rg.Title,
		AlbumType:  rg.PrimaryType,
		ArtistName: artistName,
		Genres:     genres,
		Tags:       tags,
		ExternalIDs: map[string]string{
			"musicbrainz":        mbid,
			"musicbrainz_artist": artistMBID,
		},
	}

	if len(rg.Releases) > 0 {
		rel := pickBestRelease(rg.Releases)
		detail.Country = rel.Country
		detail.Barcode = rel.Barcode
		if len(rel.LabelInfo) > 0 {
			detail.Label = rel.LabelInfo[0].Label.Name
		}
		if rel.Date != "" {
			detail.PublishDate = rel.Date
		}

		tracks, totalDiscs := p.fetchTracks(ctx, rel.ID)
		detail.Tracks = tracks
		detail.TotalDiscs = totalDiscs
	}

	detail.CoverURL = p.fetchCoverArt(ctx, mbid)

	return detail, nil
}

func (p *Provider) fetchTracks(ctx context.Context, releaseID string) ([]metadata.TrackDetail, int) {
	params := url.Values{
		"inc": {"recordings"},
		"fmt": {"json"},
	}
	u := baseURL + "/release/" + releaseID + "?" + params.Encode()

	var rel releaseDetail
	if err := p.client.GetJSON(ctx, u, &rel); err != nil {
		return nil, 0
	}

	var tracks []metadata.TrackDetail
	maxDisc := 0
	for _, media := range rel.Media {
		if media.Position > maxDisc {
			maxDisc = media.Position
		}
		for _, t := range media.Tracks {
			dur := t.Length
			if dur == 0 {
				dur = t.Recording.Length
			}
			tracks = append(tracks, metadata.TrackDetail{
				DiscNumber:  media.Position,
				TrackNumber: t.Position,
				Title:       t.Title,
				DurationMs:  dur,
			})
		}
	}

	return tracks, maxDisc
}

func (p *Provider) fetchCoverArt(ctx context.Context, releaseGroupID string) string {
	u := coverArtURL + "/release-group/" + releaseGroupID
	var resp coverArtResponse
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return ""
	}

	for _, img := range resp.Images {
		if img.Front {
			if img.Thumbnails.Large != "" {
				return img.Thumbnails.Large
			}
			return img.Image
		}
	}
	if len(resp.Images) > 0 {
		return resp.Images[0].Image
	}
	return ""
}

func pickBestRelease(releases []releaseRef) releaseRef {
	for _, r := range releases {
		if r.Status == "Official" {
			return r
		}
	}
	return releases[0]
}

func extractNames(tags []genreTag) []string {
	var names []string
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}
