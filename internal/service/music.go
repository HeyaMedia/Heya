package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// MusicListPage is the standard envelope for paginated music listings.
type MusicListPage[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}

func clampMusicPage(limit, offset int32) (int32, int32) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// GetMusicArtistBySlug returns one artist by its media-item slug. Same row
// shape as ListMusicArtists so FE consumers don't need to branch when binding
// header data.
func (a *App) GetMusicArtistBySlug(ctx context.Context, slug string) (*sqlc.GetMusicArtistBySlugRow, error) {
	q := sqlc.New(a.db)
	row, err := q.GetMusicArtistBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetSimilarArtistsBySlug is the slug-addressed flavor of GetSimilarArtists.
// Resolves the slug → artist via GetMusicArtistBySlug (one extra row read,
// not hot path) so handlers don't have to duplicate the lookup boilerplate.
func (a *App) GetSimilarArtistsBySlug(ctx context.Context, slug string) ([]SimilarArtistRow, error) {
	row, err := a.GetMusicArtistBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return a.GetSimilarArtists(ctx, row.ID)
}

// SimilarMusicArtistsBySlug — slug flavor of SimilarMusicArtists.
func (a *App) SimilarMusicArtistsBySlug(ctx context.Context, slug string, limit int32) ([]sqlc.SimilarArtistsRow, error) {
	row, err := a.GetMusicArtistBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return a.SimilarMusicArtists(ctx, row.ID, limit)
}

// ListAlbumsByArtistSlug returns one artist's albums, paginated.
func (a *App) ListAlbumsByArtistSlug(ctx context.Context, slug string, limit, offset int32) (*MusicListPage[sqlc.ListAlbumsByArtistSlugRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)
	items, err := q.ListAlbumsByArtistSlug(ctx, sqlc.ListAlbumsByArtistSlugParams{Slug: slug, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("listing albums for artist %q: %w", slug, err)
	}
	total, _ := q.CountAlbumsByArtistSlug(ctx, slug)
	return &MusicListPage[sqlc.ListAlbumsByArtistSlugRow]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// MusicTrackDetail is the one-shot read shape for /api/music/tracks/{id}.
// Bundles the track + its files + the album/artist context the FE needs to
// render headers and breadcrumbs without follow-up fetches.
type MusicTrackDetail struct {
	sqlc.GetTrackDetailByIDRow
	Files []sqlc.TrackFile `json:"files"`
}

// GetMusicTrackDetail returns a track + its files + album/artist context. The
// caller still hits /facets / /waveform / /lyrics separately when needed —
// those are sized differently and have their own cache TTLs.
func (a *App) GetMusicTrackDetail(ctx context.Context, trackID int64) (*MusicTrackDetail, error) {
	q := sqlc.New(a.db)
	row, err := q.GetTrackDetailByID(ctx, trackID)
	if err != nil {
		return nil, err
	}
	files, _ := q.ListTrackFilesByTrack(ctx, trackID)
	if files == nil {
		files = []sqlc.TrackFile{}
	}
	return &MusicTrackDetail{GetTrackDetailByIDRow: row, Files: files}, nil
}

// ListTracksByArtistSlug returns one artist's tracks (flat, all albums), paginated.
func (a *App) ListTracksByArtistSlug(ctx context.Context, slug string, limit, offset int32) (*MusicListPage[sqlc.ListTracksByArtistSlugRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)
	items, err := q.ListTracksByArtistSlug(ctx, sqlc.ListTracksByArtistSlugParams{Slug: slug, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("listing tracks for artist %q: %w", slug, err)
	}
	total, _ := q.CountTracksByArtistSlug(ctx, slug)
	return &MusicListPage[sqlc.ListTracksByArtistSlugRow]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ListMusicArtists returns artists across every music library, paginated.
func (a *App) ListMusicArtists(ctx context.Context, limit, offset int32) (*MusicListPage[sqlc.ListMusicArtistsRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)

	items, err := q.ListMusicArtists(ctx, sqlc.ListMusicArtistsParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("listing music artists: %w", err)
	}
	total, _ := q.CountMusicArtists(ctx)
	return &MusicListPage[sqlc.ListMusicArtistsRow]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ListMusicAlbums returns albums across every music library, paginated.
func (a *App) ListMusicAlbums(ctx context.Context, limit, offset int32) (*MusicListPage[sqlc.ListMusicAlbumsRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)

	items, err := q.ListMusicAlbums(ctx, sqlc.ListMusicAlbumsParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("listing music albums: %w", err)
	}
	total, _ := q.CountMusicAlbums(ctx)
	return &MusicListPage[sqlc.ListMusicAlbumsRow]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// MusicAlbumDetail is the read shape for /api/music/artists/{a}/albums/{b}.
// Carries the parent artist info so the page can render breadcrumbs and a
// back-link without a second fetch.
type MusicAlbumDetail struct {
	Album       sqlc.Album  `json:"album"`
	Tracks      []TrackView `json:"tracks"`
	Artist      ArtistView  `json:"artist"`
	ArtistSlug  string      `json:"artist_slug"`
	MediaItemID int64       `json:"media_item_id"`
}

// GetAlbumDetail resolves an album by (artist_slug, album_slug) and returns
// it with all its tracks and per-track files in best-quality-first order.
func (a *App) GetAlbumDetail(ctx context.Context, artistSlug, albumSlug string) (*MusicAlbumDetail, error) {
	q := sqlc.New(a.db)

	album, err := q.GetAlbumByArtistAndSlug(ctx, sqlc.GetAlbumByArtistAndSlugParams{
		Slug:   artistSlug,
		Slug_2: albumSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("album not found: %w", err)
	}
	return a.assembleAlbumDetail(ctx, q, album)
}

// ResolveAlbumIDBySlugs looks up an album ID by (artist_slug, album_slug).
// Used by the slug-addressed album sub-endpoints (cover, sonic-similar) so
// callers can stay on the canonical URL form without redundant route variants.
func (a *App) ResolveAlbumIDBySlugs(ctx context.Context, artistSlug, albumSlug string) (int64, error) {
	q := sqlc.New(a.db)
	album, err := q.GetAlbumByArtistAndSlug(ctx, sqlc.GetAlbumByArtistAndSlugParams{
		Slug:   artistSlug,
		Slug_2: albumSlug,
	})
	if err != nil {
		return 0, fmt.Errorf("album not found: %w", err)
	}
	return album.ID, nil
}

// SimilarMusicAlbumsBySlugs — slug-addressed sonic-similar lookup for albums.
func (a *App) SimilarMusicAlbumsBySlugs(ctx context.Context, artistSlug, albumSlug string, limit int32) ([]sqlc.SimilarAlbumsRow, error) {
	id, err := a.ResolveAlbumIDBySlugs(ctx, artistSlug, albumSlug)
	if err != nil {
		return nil, err
	}
	return a.SimilarMusicAlbums(ctx, id, limit)
}

func (a *App) assembleAlbumDetail(ctx context.Context, q *sqlc.Queries, album sqlc.Album) (*MusicAlbumDetail, error) {
	artist, err := q.GetArtistByID(ctx, album.ArtistID)
	if err != nil {
		return nil, fmt.Errorf("artist not found: %w", err)
	}
	mediaItem, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return nil, fmt.Errorf("media item not found: %w", err)
	}

	tracks, _ := q.ListTracksByAlbum(ctx, album.ID)
	views := make([]TrackView, 0, len(tracks))
	for _, t := range tracks {
		files, _ := q.ListTrackFilesByTrack(ctx, t.ID)
		views = append(views, TrackView{Track: t, Files: files})
	}

	return &MusicAlbumDetail{
		Album:       album,
		Tracks:      views,
		Artist:      BuildArtistView(artist),
		ArtistSlug:  mediaItem.Slug,
		MediaItemID: mediaItem.ID,
	}, nil
}

// SimilarArtistRow is one row of the augmented /api/music/artists/{id}/similar
// response. LocalSlug + LocalArtistID are non-empty when the suggested artist
// already lives in one of our music libraries (matched on MBID first, name
// case-insensitive fallback).
type SimilarArtistRow struct {
	Name          string  `json:"name"`
	MBID          string  `json:"mbid,omitempty"`
	Image         string  `json:"image,omitempty"`
	Score         float64 `json:"score"`
	Source        string  `json:"source"`
	URL           string  `json:"url,omitempty"`
	LocalSlug     string  `json:"local_slug,omitempty"`
	LocalArtistID int64   `json:"local_artist_id,omitempty"`
}

// GetSimilarArtists fetches Last.fm + ListenBrainz similar suggestions for an
// artist and folds in any local matches so the UI can route to the in-library
// detail page rather than a dead-end "external" tile.
func (a *App) GetSimilarArtists(ctx context.Context, artistID int64) ([]SimilarArtistRow, error) {
	q := sqlc.New(a.db)

	artist, err := q.GetArtistByID(ctx, artistID)
	if err != nil {
		return nil, fmt.Errorf("get artist: %w", err)
	}

	heya := a.Metadata()
	if heya == nil {
		return nil, fmt.Errorf("heya provider unavailable")
	}

	hits, err := heya.SimilarArtists(ctx, artist.MusicbrainzID, artist.Name)
	if err != nil {
		return nil, fmt.Errorf("heya similar artists: %w", err)
	}
	if len(hits) == 0 {
		return []SimilarArtistRow{}, nil
	}

	// Index our local artists once for cheap MBID + name lookups. The pool
	// of artists is small (hundreds at most) so an in-memory map beats N
	// per-hit queries.
	type localRef struct {
		artistID int64
		slug     string
	}
	byMBID := map[string]localRef{}
	byName := map[string]localRef{}
	rows, err := a.db.Query(ctx, `
		SELECT a.id, a.name, a.musicbrainz_id, mi.slug
		FROM artists a
		JOIN media_items mi ON mi.id = a.media_item_id
		JOIN libraries   l  ON l.id  = mi.library_id
		WHERE l.media_type = 'music'
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name, mbid, slug string
			if err := rows.Scan(&id, &name, &mbid, &slug); err != nil {
				continue
			}
			ref := localRef{artistID: id, slug: slug}
			if mbid != "" {
				byMBID[mbid] = ref
			}
			byName[strings.ToLower(strings.TrimSpace(name))] = ref
		}
	}

	out := make([]SimilarArtistRow, 0, len(hits))
	for _, h := range hits {
		row := SimilarArtistRow{
			Name:   h.Name,
			MBID:   h.MBID,
			Image:  h.Image,
			Score:  h.Score,
			Source: h.Source,
			URL:    h.URL,
		}
		if h.MBID != "" {
			if ref, ok := byMBID[h.MBID]; ok {
				row.LocalSlug = ref.slug
				row.LocalArtistID = ref.artistID
			}
		}
		if row.LocalSlug == "" {
			if ref, ok := byName[strings.ToLower(strings.TrimSpace(h.Name))]; ok {
				row.LocalSlug = ref.slug
				row.LocalArtistID = ref.artistID
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// MusicHomeData powers the music landing page front view.
type MusicHomeData struct {
	RecentArtists []sqlc.ListRecentlyAddedArtistsRow `json:"recent_artists"`
	RecentAlbums  []sqlc.ListRecentlyAddedAlbumsRow  `json:"recent_albums"`
}

// GetMusicHome assembles the small set of rows the landing page renders.
// More rows (recently played, made for you, by genre) wait on play history
// and a richer recommendation surface.
func (a *App) GetMusicHome(ctx context.Context, limit int32) (*MusicHomeData, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	q := sqlc.New(a.db)
	artists, err := q.ListRecentlyAddedArtists(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("recent artists: %w", err)
	}
	albums, err := q.ListRecentlyAddedAlbums(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("recent albums: %w", err)
	}
	return &MusicHomeData{RecentArtists: artists, RecentAlbums: albums}, nil
}

// SetEntityLoved flips the user's love state for a polymorphic entity
// (track / album / artist). Returns the new state.
func (a *App) SetEntityLoved(ctx context.Context, userID int64, entityType string, entityID int64, loved bool) (bool, error) {
	q := sqlc.New(a.db)
	if loved {
		_, err := q.ToggleFavorite(ctx, sqlc.ToggleFavoriteParams{
			UserID:     userID,
			EntityType: entityType,
			EntityID:   entityID,
		})
		if err != nil {
			return false, err
		}
		return true, nil
	}
	err := q.RemoveFavorite(ctx, sqlc.RemoveFavoriteParams{
		UserID:     userID,
		EntityType: entityType,
		EntityID:   entityID,
	})
	if err != nil {
		return false, err
	}
	return false, nil
}

// SetTrackLoved is the legacy track-specific wrapper.
func (a *App) SetTrackLoved(ctx context.Context, userID, trackID int64, loved bool) (bool, error) {
	return a.SetEntityLoved(ctx, userID, "track", trackID, loved)
}

// ListUserLovedTrackIDs returns every track id the user has loved.
func (a *App) ListUserLovedTrackIDs(ctx context.Context, userID int64) ([]int64, error) {
	return sqlc.New(a.db).ListUserLovedTrackIDs(ctx, userID)
}

// ListUserLovedArtistIDs / ListUserLovedAlbumIDs — same shape for artist /
// album favorites so the UI can flip heart fills in one fetch per kind.
func (a *App) ListUserLovedArtistIDs(ctx context.Context, userID int64) ([]int64, error) {
	return sqlc.New(a.db).ListUserLovedArtistIDs(ctx, userID)
}
func (a *App) ListUserLovedAlbumIDs(ctx context.Context, userID int64) ([]int64, error) {
	return sqlc.New(a.db).ListUserLovedAlbumIDs(ctx, userID)
}

// ListUserLovedTracks returns the full enriched track list for the Loved
// tab. Paginated; ordered most-recently-loved first.
func (a *App) ListUserLovedTracks(ctx context.Context, userID int64, limit, offset int32) (*MusicListPage[sqlc.ListUserLovedTracksRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)

	rows, err := q.ListUserLovedTracks(ctx, sqlc.ListUserLovedTracksParams{UserID: userID, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list loved tracks: %w", err)
	}
	total, _ := q.CountUserLovedTracks(ctx, userID)
	return &MusicListPage[sqlc.ListUserLovedTracksRow]{Items: rows, Total: total, Limit: limit, Offset: offset}, nil
}

// ListUserLovedArtists / ListUserLovedAlbums power the My Media grids.
func (a *App) ListUserLovedArtists(ctx context.Context, userID int64, limit, offset int32) (*MusicListPage[sqlc.ListUserLovedArtistsRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)
	rows, err := q.ListUserLovedArtists(ctx, sqlc.ListUserLovedArtistsParams{UserID: userID, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list loved artists: %w", err)
	}
	total, _ := q.CountUserLovedArtists(ctx, userID)
	return &MusicListPage[sqlc.ListUserLovedArtistsRow]{Items: rows, Total: total, Limit: limit, Offset: offset}, nil
}
func (a *App) ListUserLovedAlbums(ctx context.Context, userID int64, limit, offset int32) (*MusicListPage[sqlc.ListUserLovedAlbumsRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)
	rows, err := q.ListUserLovedAlbums(ctx, sqlc.ListUserLovedAlbumsParams{UserID: userID, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list loved albums: %w", err)
	}
	total, _ := q.CountUserLovedAlbums(ctx, userID)
	return &MusicListPage[sqlc.ListUserLovedAlbumsRow]{Items: rows, Total: total, Limit: limit, Offset: offset}, nil
}

// GetTrack returns a single track by id.
func (a *App) GetTrack(ctx context.Context, id int64) (sqlc.Track, error) {
	return sqlc.New(a.db).GetTrackByID(ctx, id)
}

// GetTrackFile returns a single track_file by id.
func (a *App) GetTrackFile(ctx context.Context, id int64) (sqlc.TrackFile, error) {
	return sqlc.New(a.db).GetTrackFileByID(ctx, id)
}

// GetPrimaryTrackFile returns the highest-quality non-deleted file for a track.
func (a *App) GetPrimaryTrackFile(ctx context.Context, trackID int64) (sqlc.TrackFile, error) {
	return sqlc.New(a.db).GetPrimaryTrackFile(ctx, trackID)
}

// ListTrackFiles returns every file for a track, ordered best-first.
func (a *App) ListTrackFiles(ctx context.Context, trackID int64) ([]sqlc.TrackFile, error) {
	return sqlc.New(a.db).ListTrackFilesByTrack(ctx, trackID)
}

// ListMusicTracks returns a flat track listing across every music library.
func (a *App) ListMusicTracks(ctx context.Context, limit, offset int32) (*MusicListPage[sqlc.ListMusicTracksRow], error) {
	limit, offset = clampMusicPage(limit, offset)
	q := sqlc.New(a.db)

	items, err := q.ListMusicTracks(ctx, sqlc.ListMusicTracksParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("listing music tracks: %w", err)
	}
	total, _ := q.CountMusicTracks(ctx)
	return &MusicListPage[sqlc.ListMusicTracksRow]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}
