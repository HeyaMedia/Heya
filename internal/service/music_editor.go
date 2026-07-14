package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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

// ApplyAlbumIdentify re-points one album at a chosen canonical Heya release
// group. External IDs are copied only as compatibility evidence; the durable
// identity is the canonical UUID binding.
func (a *App) ApplyAlbumIdentify(ctx context.Context, albumID int64, providerName, providerID string) error {
	if providerName != "heya" {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	detail, err := a.heya.GetDetail(ctx, providerID, nil)
	if err != nil {
		return fmt.Errorf("resolve canonical album match: %w", err)
	}
	if detail.CanonicalKind != "release_group" {
		return fmt.Errorf("album identify expected release_group, got %q", detail.CanonicalKind)
	}
	canonicalID, err := uuid.Parse(detail.CanonicalID)
	if err != nil {
		return fmt.Errorf("album identify returned invalid canonical UUID %q: %w", detail.CanonicalID, err)
	}

	q := sqlc.New(a.db)
	album, err := q.GetAlbumByID(ctx, albumID)
	if err != nil {
		return fmt.Errorf("album not found: %w", err)
	}
	artist, err := q.GetArtistByID(ctx, album.ArtistID)
	if err != nil {
		return fmt.Errorf("artist not found: %w", err)
	}
	newMBID := album.MusicbrainzID
	if external := detail.ExternalIDs["mbid"]; external != "" {
		newMBID = external
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
		return fmt.Errorf("store album external identity evidence: %w", err)
	}
	if _, err := q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "album", LocalID: album.ID, EntityID: canonicalID, EntityKind: "release_group",
		SchemaVersion: int32(detail.SchemaVersion), ProjectionVersion: detail.ProjectionVersion,
	}); err != nil {
		return fmt.Errorf("bind album to canonical metadata: %w", err)
	}

	item, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return fmt.Errorf("artist media item not found: %w", err)
	}
	return worker.EnqueueEnrichForce(ctx, a.river, item.ID, item.MediaType, worker.EnrichSourceForced)
}
