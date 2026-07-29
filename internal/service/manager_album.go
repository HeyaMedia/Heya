package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/discography"
)

// Manager album detail: the Lidarr-style deepest level — one release with
// its tracklist and per-track file/quality state. Addressed by local album id
// ("123") or, for catalog-only releases the library doesn't have, by
// discography row ("d123") — those render facts without a tracklist (the
// provider catalog carries counts, not track listings).

type ManagerTrackView struct {
	ID        int64  `json:"id"`
	Disc      int32  `json:"disc"`
	Number    int32  `json:"number"`
	Title     string `json:"title"`
	Duration  int32  `json:"duration_seconds"`
	HasFile   bool   `json:"has_file"`
	Quality   string `json:"quality,omitempty" doc:"Best on-disk file: format + bit depth/rate, e.g. 'FLAC 24/96' or 'MP3 320'"`
	SizeBytes int64  `json:"size_bytes"`
}

type ManagerAlbumDetailView struct {
	Album      ManagerAlbumView   `json:"album"`
	ArtistID   int64              `json:"artist_media_item_id" doc:"media_item id of the artist, for breadcrumbs"`
	ArtistName string             `json:"artist_name"`
	ArtistSlug string             `json:"artist_slug"`
	Library    ManagerLibraryRef  `json:"library"`
	Overview   string             `json:"overview,omitempty"`
	Label      string             `json:"label,omitempty"`
	Country    string             `json:"country,omitempty"`
	Genres     []string           `json:"genres,omitempty"`
	Duration   int32              `json:"duration_seconds,omitempty"`
	Discs      int32              `json:"discs,omitempty"`
	Tracks     []ManagerTrackView `json:"tracks,omitempty"`
	Editions   []ManagerAlbumView `json:"editions,omitempty" doc:"Sibling editions sharing this release's edition_key"`
}

// GetManagerAlbumDetail resolves "123" (local album) or "d123" (catalog-only
// discography entry) into the album page payload.
func (a *App) GetManagerAlbumDetail(ctx context.Context, ref string) (*ManagerAlbumDetailView, error) {
	ref = strings.TrimSpace(ref)
	// A ref that doesn't parse can't name anything — that's a 404, not a 500.
	if id, ok := strings.CutPrefix(ref, "d"); ok {
		discoID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil, ErrManagerNotFound
		}
		return a.managerAlbumFromDiscography(ctx, discoID)
	}
	albumID, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return nil, ErrManagerNotFound
	}
	return a.managerAlbumFromLocal(ctx, albumID)
}

func (a *App) managerAlbumFromLocal(ctx context.Context, albumID int64) (*ManagerAlbumDetailView, error) {
	view := &ManagerAlbumDetailView{}
	var libraryID int64
	var secondary []string
	var release pgtype.Date
	var rating pgtype.Numeric
	album := &view.Album
	err := a.db.QueryRow(ctx, `
		SELECT al.id, al.title, al.slug, al.album_type, al.secondary_types, al.release_date, al.year,
		       al.rating, al.label, al.country, al.genres, al.description, al.duration_seconds, al.total_discs,
		       ar.media_item_id, ar.name, mi.slug, mi.library_id
		FROM albums al
		JOIN artists ar ON ar.id = al.artist_id
		JOIN media_items mi ON mi.id = ar.media_item_id
		WHERE al.id = $1`, albumID).
		Scan(&album.ID, &album.Title, &album.Slug, &album.Type, &secondary, &release, &album.Year,
			&rating, &view.Label, &view.Country, &view.Genres, &view.Overview, &view.Duration, &view.Discs,
			&view.ArtistID, &view.ArtistName, &view.ArtistSlug, &libraryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, fmt.Errorf("manager album: %w", err)
	}
	album.Type = effectiveReleaseType(album.Type, secondary, album.Title)
	album.EditionKey = discography.EditionKey(album.Title)
	album.ReleaseDate = dateToString(release)
	album.Rating = numericToFloat(rating)
	album.InLibrary = true

	lib, err := a.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	view.Library = ManagerLibraryRef{ID: lib.ID, Name: lib.Name, MediaType: string(lib.MediaType), Domain: managerProfileDomain(string(lib.MediaType))}

	// Per-track rows show the BEST file (that's the quality that matters);
	// all_bytes carries every live copy so the album total agrees with the
	// artist page and the library lens, which both count what the release
	// costs on disk — a track kept in FLAC and MP3 is two files.
	rows, err := a.db.Query(ctx, `
		SELECT t.id, t.disc_number, t.track_number, t.title, t.duration,
		       best.id IS NOT NULL,
		       COALESCE(best.format, ''), COALESCE(best.bitrate_kbps, 0),
		       COALESCE(best.bit_depth, 0), COALESCE(best.sample_rate_hz, 0),
		       COALESCE(best.size_bytes, 0),
		       COALESCE((SELECT sum(tf2.size_bytes)
		                 FROM track_files tf2
		                 JOIN library_files lf2 ON lf2.id = tf2.library_file_id AND lf2.deleted_at IS NULL
		                 WHERE tf2.track_id = t.id), 0)::bigint
		FROM tracks t
		LEFT JOIN LATERAL (
		  SELECT tf.id, tf.format, tf.bitrate_kbps, tf.bit_depth, tf.sample_rate_hz, tf.size_bytes
		  FROM track_files tf
		  JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
		  WHERE tf.track_id = t.id
		  ORDER BY tf.quality_score DESC
		  LIMIT 1
		) best ON true
		WHERE t.album_id = $1
		ORDER BY t.disc_number, t.track_number`, albumID)
	if err != nil {
		return nil, fmt.Errorf("manager album tracks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t ManagerTrackView
		var format string
		var bitrate, bitDepth, sampleRate int32
		var allBytes int64
		if err := rows.Scan(&t.ID, &t.Disc, &t.Number, &t.Title, &t.Duration,
			&t.HasFile, &format, &bitrate, &bitDepth, &sampleRate, &t.SizeBytes, &allBytes); err != nil {
			return nil, err
		}
		if t.HasFile {
			t.Quality = audioQualityLabel(format, bitrate, bitDepth, sampleRate)
			album.TracksHave++
		}
		album.TracksTotal++
		album.SizeBytes += allBytes
		view.Tracks = append(view.Tracks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := a.attachManagerAlbumEditions(ctx, view); err != nil {
		return nil, err
	}
	return view, nil
}

func (a *App) managerAlbumFromDiscography(ctx context.Context, discoID int64) (*ManagerAlbumDetailView, error) {
	view := &ManagerAlbumDetailView{}
	var libraryID, trackCount int64
	var secondary []string
	var release pgtype.Date
	album := &view.Album
	err := a.db.QueryRow(ctx, `
		SELECT d.id, d.title, d.album_type, d.secondary_types, d.release_date, d.year,
		       d.track_count, d.cover_url, d.edition_key,
		       ar.media_item_id, ar.name, mi.slug, mi.library_id
		FROM artist_discography d
		JOIN artists ar ON ar.id = d.artist_id
		JOIN media_items mi ON mi.id = ar.media_item_id
		WHERE d.id = $1`, discoID).
		Scan(&album.DiscographyID, &album.Title, &album.Type, &secondary, &release, &album.Year,
			&trackCount, &album.CoverURL, &album.EditionKey,
			&view.ArtistID, &view.ArtistName, &view.ArtistSlug, &libraryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, fmt.Errorf("manager album (catalog): %w", err)
	}
	album.Type = effectiveReleaseType(album.Type, secondary, album.Title)
	if album.EditionKey == "" {
		album.EditionKey = discography.EditionKey(album.Title)
	}
	album.ReleaseDate = dateToString(release)
	album.TracksTotal = int32(trackCount)

	lib, err := a.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	view.Library = ManagerLibraryRef{ID: lib.ID, Name: lib.Name, MediaType: string(lib.MediaType), Domain: managerProfileDomain(string(lib.MediaType))}

	if err := a.attachManagerAlbumEditions(ctx, view); err != nil {
		return nil, err
	}
	return view, nil
}

// attachManagerAlbumEditions lists sibling releases sharing this album's
// edition_key (and type bucket) so the page can hop between Deluxe/Extended
// variants.
func (a *App) attachManagerAlbumEditions(ctx context.Context, view *ManagerAlbumDetailView) error {
	all, err := a.managerArtistAlbums(ctx, view.ArtistID)
	if err != nil {
		return err
	}
	self := func(al ManagerAlbumView) bool {
		if view.Album.ID != 0 {
			return al.ID == view.Album.ID
		}
		return al.DiscographyID == view.Album.DiscographyID
	}
	for _, al := range all {
		if al.EditionKey == view.Album.EditionKey && al.Type == view.Album.Type && !self(al) {
			al.TrackTitles = nil
			view.Editions = append(view.Editions, al)
		}
	}
	return nil
}

// audioQualityLabel renders the best file the way collectors read it:
// lossless as bits/kHz, lossy as bitrate.
func audioQualityLabel(format string, bitrateKbps, bitDepth, sampleRateHz int32) string {
	name := strings.ToUpper(strings.TrimSpace(format))
	if name == "" {
		return ""
	}
	switch strings.ToLower(format) {
	case "flac", "alac", "wav", "aiff", "ape", "wv":
		if bitDepth > 0 && sampleRateHz > 0 {
			khz := float64(sampleRateHz) / 1000
			text := strconv.FormatFloat(khz, 'f', -1, 64)
			return fmt.Sprintf("%s %d/%s", name, bitDepth, text)
		}
		return name
	default:
		if bitrateKbps > 0 {
			return fmt.Sprintf("%s %d", name, bitrateKbps)
		}
		return name
	}
}
