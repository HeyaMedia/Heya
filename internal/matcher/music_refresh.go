package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/rs/zerolog/log"
)

// RefreshArtistResult summarises a single RefreshMusicArtist call. Useful for
// logging + UI progress feedback.
type RefreshArtistResult struct {
	ArtistID       int64
	Skipped        bool   // heya.media had no record
	AlbumsMatched  int    // DB albums that found a match in payload.albums
	AlbumsUpdated  int    // DB albums whose fields actually changed
	TracksUpdated  int    // tracks whose title or duration was upgraded
	HeyaProviderID string // for telemetry
}

// RefreshMusicArtist re-fetches an artist from heya.media and writes the
// canonical metadata back to the DB: artist row (bio / sort_name /
// disambiguation / MBID), media_item.external_ids (discogs / deezer / apple /
// spotify / wikidata), album rows (label / country / barcode / release_date /
// MBID / type / cover_path), and track rows (canonical title + duration).
//
// Idempotent: safe to call repeatedly. If heya.media has no record, marks the
// artist as enriched anyway so we don't immediately retry on the next scan.
// Returns a summary suitable for the worker's structured log line.
func (m *Matcher) RefreshMusicArtist(ctx context.Context, artistID int64) (RefreshArtistResult, error) {
	res := RefreshArtistResult{ArtistID: artistID}

	artist, err := m.q.GetArtistByID(ctx, artistID)
	if err != nil {
		return res, fmt.Errorf("get artist %d: %w", artistID, err)
	}

	detail := m.enrichArtistFromHeyaMedia(ctx, artist.MusicbrainzID, artist.Name)
	if detail == nil {
		// Negative cache: mark so the scan task's staleness gate skips it.
		if markErr := m.q.MarkArtistEnriched(ctx, artistID); markErr != nil {
			log.Warn().Err(markErr).Int64("artist_id", artistID).Msg("MarkArtistEnriched failed")
		}
		res.Skipped = true
		return res, nil
	}
	res.HeyaProviderID = "heya:" + detail.HeyaSlug

	// Artist row: only overwrite fields when the new value is non-empty
	// (UpdateArtistEnrichedFields handles that at the SQL level).
	newMBID := artist.MusicbrainzID
	if newMBID == "" {
		newMBID = detail.ExternalIDs["mbid"]
	}
	if err := m.q.UpdateArtistEnrichedFields(ctx, sqlc.UpdateArtistEnrichedFieldsParams{
		ID:      artistID,
		Column2: newMBID,
		Column3: detail.ArtistName,
		Column4: detail.ArtistSortName,
		Column5: detail.ArtistDisambiguation,
		Column6: detail.ArtistBio,
	}); err != nil {
		return res, fmt.Errorf("update artist %d: %w", artistID, err)
	}

	// media_item.external_ids: merge enriched IDs into whatever's there.
	if item, err := m.q.GetMediaItemByID(ctx, artist.MediaItemID); err == nil {
		existing := map[string]string{}
		_ = json.Unmarshal(item.ExternalIds, &existing)
		for k, v := range detail.ExternalIDs {
			if v != "" {
				existing[k] = v
			}
		}
		if newMBID != "" {
			existing["musicbrainz_artist"] = newMBID
		}
		merged, _ := json.Marshal(existing)
		if updErr := m.q.UpdateMediaItemExternalIds(ctx, sqlc.UpdateMediaItemExternalIdsParams{
			ID:          artist.MediaItemID,
			ExternalIds: merged,
		}); updErr != nil {
			log.Warn().Err(updErr).Int64("media_item", artist.MediaItemID).Msg("update external_ids failed")
		}
	}

	// Walk every DB album of this artist; for each, find a matching entry in
	// detail.Albums and upgrade fields. Then walk the album's tracks.
	dbAlbums, err := m.q.ListAlbumsByArtist(ctx, artistID)
	if err != nil {
		return res, fmt.Errorf("list albums: %w", err)
	}
	for _, dbAlbum := range dbAlbums {
		embedded := findEmbeddedAlbum(detail, dbAlbum.Title, dbAlbum.Year, dbAlbum.MusicbrainzID)
		if embedded == nil {
			continue
		}
		res.AlbumsMatched++

		albumMBID := dbAlbum.MusicbrainzID
		if albumMBID == "" {
			if mb := embedded.ExternalIDs["mb_release"]; mb != "" {
				albumMBID = mb
			} else if mb := embedded.ExternalIDs["mbid"]; mb != "" {
				albumMBID = mb
			}
		}
		newYear := dbAlbum.Year
		if newYear == "" && embedded.Year > 0 {
			newYear = strconv.Itoa(embedded.Year)
		}
		newType := dbAlbum.AlbumType
		if (newType == "" || newType == "album") && embedded.Type != "" {
			newType = embedded.Type
		}
		coverURL := dbAlbum.CoverPath
		if coverURL == "" {
			coverURL = embedded.CoverURL
		}

		changed := albumMBID != dbAlbum.MusicbrainzID ||
			newYear != dbAlbum.Year ||
			newType != dbAlbum.AlbumType ||
			embedded.Label != dbAlbum.Label ||
			embedded.Country != dbAlbum.Country ||
			embedded.Barcode != dbAlbum.Barcode ||
			coverURL != dbAlbum.CoverPath

		if changed {
			if err := m.q.UpdateAlbumEnrichedFields(ctx, sqlc.UpdateAlbumEnrichedFieldsParams{
				ID:       dbAlbum.ID,
				Column2:  albumMBID,
				Column3:  embedded.Title,
				Column4:  newYear,
				Column5:  newType,
				Column6:  embedded.Label,
				Column7:  embedded.Country,
				Column8:  embedded.Barcode,
				Column9:  pgDateFromString(embedded.ReleaseDate),
				Column10: coverURL,
			}); err != nil {
				log.Warn().Err(err).Int64("album", dbAlbum.ID).Msg("update album enriched failed")
			} else {
				res.AlbumsUpdated++
			}
		}

		dbTracks, err := m.q.ListTracksByAlbum(ctx, dbAlbum.ID)
		if err != nil {
			continue
		}
		for _, dbTrack := range dbTracks {
			embeddedTrack := findEmbeddedTrack(embedded, int(dbTrack.DiscNumber), int(dbTrack.TrackNumber))
			if embeddedTrack == nil {
				continue
			}
			newTitle := dbTrack.Title
			if embeddedTrack.Title != "" {
				newTitle = embeddedTrack.Title
			}
			newDuration := dbTrack.Duration
			if embeddedTrack.Duration > 0 {
				newDuration = int32(embeddedTrack.Duration)
			}
			if newTitle != dbTrack.Title || newDuration != dbTrack.Duration {
				if err := m.q.UpdateTrackFromEnrichment(ctx, sqlc.UpdateTrackFromEnrichmentParams{
					ID:      dbTrack.ID,
					Column2: newTitle,
					Column3: newDuration,
				}); err != nil {
					log.Warn().Err(err).Int64("track", dbTrack.ID).Msg("update track enriched failed")
				} else {
					res.TracksUpdated++
				}
			}
		}
	}

	if err := m.q.MarkArtistEnriched(ctx, artistID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Warn().Err(err).Int64("artist_id", artistID).Msg("MarkArtistEnriched failed")
	}
	return res, nil
}

// MediaItemIDForArtist returns the media_item.id that backs the given artist,
// used by the refresh worker to emit EventMediaUpdated.
func (m *Matcher) MediaItemIDForArtist(ctx context.Context, artistID int64) (int64, error) {
	a, err := m.q.GetArtistByID(ctx, artistID)
	if err != nil {
		return 0, err
	}
	return a.MediaItemID, nil
}

// EnrichArtistFromHeyaMedia is the exported wrapper around the internal helper
// so external packages (worker) can probe heya.media via the matcher's
// configured provider.
func (m *Matcher) EnrichArtistFromHeyaMedia(ctx context.Context, mbid, name string) *metadata.MediaDetail {
	return m.enrichArtistFromHeyaMedia(ctx, mbid, name)
}
