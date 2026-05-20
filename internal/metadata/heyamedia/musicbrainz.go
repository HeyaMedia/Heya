package heyamedia

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type MusicBrainzProvider struct {
	client *Client
}

func NewMusicBrainzProvider(c *Client) *MusicBrainzProvider {
	return &MusicBrainzProvider{client: c}
}

func (p *MusicBrainzProvider) Name() string { return "musicbrainz" }

func (p *MusicBrainzProvider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMusic
}

func (p *MusicBrainzProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	if ids.MBID == "" {
		return nil, "", fmt.Errorf("no MusicBrainz ID available")
	}
	providerID := "musicbrainz:" + ids.MBID
	detail, err := p.GetDetail(ctx, providerID, opts)
	if err != nil {
		return nil, "", err
	}
	return detail, providerID, nil
}

func (p *MusicBrainzProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
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
		"limit": {"10"},
	}

	var resp mbSearchResponse
	if err := p.client.get(ctx, "/api/v1/musicbrainz/release-group/search", params, &resp); err != nil {
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
			ProviderID: "musicbrainz:" + rg.ID, ProviderName: "musicbrainz",
			Title: title, Year: year, Description: rg.PrimaryType, RawData: rg,
		})
	}
	return results, nil
}

func (p *MusicBrainzProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid musicbrainz provider ID: %s", providerID)
	}
	mbid := parts[1]

	params := url.Values{"inc": {"artists+releases+genres+tags"}}
	var rg mbReleaseGroupDetail
	if err := p.client.get(ctx, "/api/v1/musicbrainz/release-group/"+mbid, params, &rg); err != nil {
		return nil, err
	}

	year := ""
	if len(rg.FirstRelease) >= 4 {
		year = rg.FirstRelease[:4]
	}

	artistName, artistMBID := "", ""
	if len(rg.ArtistCredit) > 0 {
		artistName = rg.ArtistCredit[0].Artist.Name
		artistMBID = rg.ArtistCredit[0].Artist.ID
	}

	genres := mbExtractNames(rg.Genres)
	tags := mbExtractNames(rg.Tags)

	detail := &metadata.MediaDetail{
		Title: rg.Title, SortTitle: strings.ToLower(rg.Title), Year: year,
		AlbumTitle: rg.Title, AlbumType: rg.PrimaryType,
		ArtistName: artistName, Genres: genres, Tags: tags,
		ExternalIDs: map[string]string{"musicbrainz": mbid, "musicbrainz_artist": artistMBID},
	}

	if len(rg.Releases) > 0 {
		rel := mbPickBestRelease(rg.Releases)
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

	if artistMBID != "" {
		detail.ArtistBio = p.fetchArtistAnnotation(ctx, artistMBID)
	}

	return detail, nil
}

func (p *MusicBrainzProvider) fetchArtistAnnotation(ctx context.Context, artistMBID string) string {
	params := url.Values{"inc": {"annotation"}}
	var artist mbArtistDetail
	if err := p.client.get(ctx, "/api/v1/musicbrainz/artist/"+artistMBID, params, &artist); err != nil {
		return ""
	}
	return artist.Annotation
}

func (p *MusicBrainzProvider) fetchTracks(ctx context.Context, releaseID string) ([]metadata.TrackDetail, int) {
	params := url.Values{"inc": {"recordings"}}
	var rel mbReleaseDetail
	if err := p.client.get(ctx, "/api/v1/musicbrainz/release/"+releaseID, params, &rel); err != nil {
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
				DiscNumber: media.Position, TrackNumber: t.Position,
				Title: t.Title, DurationMs: dur,
			})
		}
	}
	return tracks, maxDisc
}

func (p *MusicBrainzProvider) fetchCoverArt(ctx context.Context, releaseGroupID string) string {
	var resp mbCoverArtResponse
	if err := p.client.getJSON(ctx, "/api/v1/musicbrainz/cover-art/"+releaseGroupID, &resp); err != nil {
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

func mbPickBestRelease(releases []mbReleaseRef) mbReleaseRef {
	for _, r := range releases {
		if r.Status == "Official" {
			return r
		}
	}
	return releases[0]
}

func mbExtractNames(tags []mbGenreTag) []string {
	var names []string
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}

// --- MusicBrainz response types ---

type mbSearchResponse struct {
	ReleaseGroups []mbReleaseGroupResult `json:"release-groups"`
}

type mbReleaseGroupResult struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	PrimaryType  string           `json:"primary-type"`
	FirstRelease string           `json:"first-release-date"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
	Score        int              `json:"score"`
}

type mbArtistCredit struct {
	Artist mbArtistRef `json:"artist"`
}

type mbArtistRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mbReleaseGroupDetail struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	PrimaryType  string           `json:"primary-type"`
	FirstRelease string           `json:"first-release-date"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
	Releases     []mbReleaseRef   `json:"releases"`
	Genres       []mbGenreTag     `json:"genres"`
	Tags         []mbGenreTag     `json:"tags"`
}

type mbReleaseRef struct {
	ID        string        `json:"id"`
	Date      string        `json:"date"`
	Country   string        `json:"country"`
	Status    string        `json:"status"`
	Barcode   string        `json:"barcode"`
	LabelInfo []mbLabelInfo `json:"label-info"`
}

type mbLabelInfo struct {
	Label mbLabelRef `json:"label"`
}

type mbLabelRef struct {
	Name string `json:"name"`
}

type mbGenreTag struct {
	Name string `json:"name"`
}

type mbReleaseDetail struct {
	ID    string         `json:"id"`
	Media []mbMediaEntry `json:"media"`
}

type mbMediaEntry struct {
	Position int            `json:"position"`
	Tracks   []mbTrackEntry `json:"tracks"`
}

type mbTrackEntry struct {
	Position  int            `json:"position"`
	Title     string         `json:"title"`
	Length    int            `json:"length"`
	Recording mbRecordingRef `json:"recording"`
}

type mbRecordingRef struct {
	Length int `json:"length"`
}

type mbArtistDetail struct {
	Annotation string `json:"annotation"`
}

type mbCoverArtResponse struct {
	Images []mbCoverImage `json:"images"`
}

type mbCoverImage struct {
	Image      string `json:"image"`
	Front      bool   `json:"front"`
	Thumbnails struct {
		Large string `json:"large"`
	} `json:"thumbnails"`
}
