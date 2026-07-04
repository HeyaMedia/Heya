package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/worker"
)

// UpdateAlbumReq holds the album fields the metadata editor can patch.
// Slug and cover are deliberately absent: slugs are stable user-facing URLs,
// and covers go through the artwork pipeline.
type UpdateAlbumReq struct {
	Title       *string  `json:"title"`
	Year        *string  `json:"year"`
	AlbumType   *string  `json:"album_type"`
	Label       *string  `json:"label"`
	Country     *string  `json:"country"`
	Barcode     *string  `json:"barcode"`
	Genres      []string `json:"genres"`
	ReleaseDate *string  `json:"release_date"`
}

// UpdateAlbumMetadata patches an album row. Full-row write underneath, so
// every untouched field is copied from the current row.
func (a *App) UpdateAlbumMetadata(ctx context.Context, albumID int64, req UpdateAlbumReq) (sqlc.Album, error) {
	q := sqlc.New(a.db)

	album, err := q.GetAlbumByID(ctx, albumID)
	if err != nil {
		return sqlc.Album{}, fmt.Errorf("album not found: %w", err)
	}

	p := sqlc.UpdateAlbumParams{
		ID:            album.ID,
		Title:         album.Title,
		Slug:          album.Slug,
		Year:          album.Year,
		MusicbrainzID: album.MusicbrainzID,
		AlbumType:     album.AlbumType,
		Genres:        album.Genres,
		CoverPath:     album.CoverPath,
		ReleaseDate:   album.ReleaseDate,
		Label:         album.Label,
		Country:       album.Country,
		Barcode:       album.Barcode,
		TotalTracks:   album.TotalTracks,
		TotalDiscs:    album.TotalDiscs,
		Tags:          album.Tags,
	}
	if req.Title != nil && *req.Title != "" {
		p.Title = *req.Title
	}
	if req.Year != nil {
		p.Year = *req.Year
	}
	if req.AlbumType != nil && *req.AlbumType != "" {
		p.AlbumType = *req.AlbumType
	}
	if req.Label != nil {
		p.Label = *req.Label
	}
	if req.Country != nil {
		p.Country = *req.Country
	}
	if req.Barcode != nil {
		p.Barcode = *req.Barcode
	}
	if req.Genres != nil {
		p.Genres = req.Genres
	}
	if req.ReleaseDate != nil {
		p.ReleaseDate = pgDateFromStr(*req.ReleaseDate)
	}

	updated, err := q.UpdateAlbum(ctx, p)
	if err != nil {
		return sqlc.Album{}, fmt.Errorf("updating album: %w", err)
	}
	return updated, nil
}

// AlbumIdentifySearch searches heya.media's album index for candidate matches
// for one local album, scoped to its artist's name. Falls back to the album's
// own title when no query is given.
func (a *App) AlbumIdentifySearch(ctx context.Context, albumID int64, query string) (IdentifySearchResult, error) {
	q := sqlc.New(a.db)

	album, err := q.GetAlbumByID(ctx, albumID)
	if err != nil {
		return IdentifySearchResult{}, fmt.Errorf("album not found: %w", err)
	}
	artist, err := q.GetArtistByID(ctx, album.ArtistID)
	if err != nil {
		return IdentifySearchResult{}, fmt.Errorf("artist not found: %w", err)
	}

	if query == "" {
		query = album.Title
	}

	results, err := a.heya.SearchAlbums(ctx, query, artist.Name)
	if err != nil {
		return IdentifySearchResult{}, fmt.Errorf("album search failed: %w", err)
	}
	return IdentifySearchResult{Results: results}, nil
}

// ApplyAlbumIdentify re-points one album at a chosen upstream release group.
// Stamps the MusicBrainz id on the album row and enqueues a forced enrich for
// the parent artist: the refresh pipeline matches embedded upstream albums
// MBID-first (findEmbeddedAlbum), so the album's canonical title / year /
// label / cover adopt from the newly pinned record.
func (a *App) ApplyAlbumIdentify(ctx context.Context, albumID int64, providerName, providerID string) error {
	if providerName != "heya" {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	// providerID shape: heya:album:mbid:<uuid> (from SearchAlbums results).
	rest := strings.TrimPrefix(providerID, "heya:")
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] != "album" || parts[1] != "mbid" || parts[2] == "" {
		return fmt.Errorf("album identify needs a MusicBrainz-backed match (got %q)", providerID)
	}
	newMBID := parts[2]

	q := sqlc.New(a.db)
	album, err := q.GetAlbumByID(ctx, albumID)
	if err != nil {
		return fmt.Errorf("album not found: %w", err)
	}
	artist, err := q.GetArtistByID(ctx, album.ArtistID)
	if err != nil {
		return fmt.Errorf("artist not found: %w", err)
	}

	if _, err := q.UpdateAlbum(ctx, sqlc.UpdateAlbumParams{
		ID:            album.ID,
		Title:         album.Title,
		Slug:          album.Slug,
		Year:          album.Year,
		MusicbrainzID: newMBID,
		AlbumType:     album.AlbumType,
		Genres:        album.Genres,
		CoverPath:     album.CoverPath,
		ReleaseDate:   album.ReleaseDate,
		Label:         album.Label,
		Country:       album.Country,
		Barcode:       album.Barcode,
		TotalTracks:   album.TotalTracks,
		TotalDiscs:    album.TotalDiscs,
		Tags:          album.Tags,
	}); err != nil {
		return fmt.Errorf("stamp album mbid: %w", err)
	}

	item, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return fmt.Errorf("artist media item not found: %w", err)
	}
	return worker.EnqueueEnrichForce(ctx, a.river, item.ID, item.MediaType, worker.EnrichSourceForced)
}
