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
	// Artist-level artwork from heya.media. PosterURL / BackdropURL are the
	// upstream's "primary" picks; ArtistImages is the full classified pool
	// the worker uses to fill remaining gaps (logo / banner / clearart /
	// thumb plus secondary backdrops). The matcher only carries these
	// through — it doesn't queue downloads or write media_assets rows.
	PosterURL    string
	BackdropURL  string
	ArtistImages []metadata.ArtworkResult
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

	detail := m.enrichArtistFromHeyaMedia(ctx, artist.MusicbrainzID, artist.Name, artist.Disambiguation)
	if detail == nil {
		// Negative cache: mark so the scan task's staleness gate skips it.
		if markErr := m.q.MarkArtistDiscographyEnriched(ctx, artistID); markErr != nil {
			log.Warn().Err(markErr).Int64("artist_id", artistID).Msg("MarkArtistDiscographyEnriched failed")
		}
		res.Skipped = true
		return res, nil
	}
	// Identity guard: when both sides carry a real MBID and they disagree, the
	// upstream record describes a DIFFERENT artist than the locally established
	// identity — adopting its name/ids would clobber this row with another act
	// (the Big Red Machine → "Taylor Swift" chimera: a bad upstream merge
	// renamed the local artist and stamped the other act's external ids onto
	// the media_item, squatting idx_media_items_mbid_unique for the whole
	// library). Keep the local identity, negative-cache, and let a corrected
	// upstream record (same MBID) heal things on a later refresh.
	if upstreamMBID := detail.ExternalIDs["mbid"]; artist.MusicbrainzID != "" && upstreamMBID != "" &&
		upstreamMBID != artist.MusicbrainzID &&
		!isSyntheticMBID(artist.MusicbrainzID) && !isSyntheticMBID(upstreamMBID) {
		log.Warn().Int64("artist_id", artistID).Str("artist", artist.Name).
			Str("local_mbid", artist.MusicbrainzID).Str("upstream_mbid", upstreamMBID).
			Str("upstream_name", detail.ArtistName).Str("upstream_slug", detail.HeyaSlug).
			Msg("upstream artist record contradicts local MBID; skipping enrich to avoid identity clobber")
		if markErr := m.q.MarkArtistDiscographyEnriched(ctx, artistID); markErr != nil {
			log.Warn().Err(markErr).Int64("artist_id", artistID).Msg("MarkArtistDiscographyEnriched failed")
		}
		res.Skipped = true
		return res, nil
	}

	res.HeyaProviderID = "heya:" + detail.HeyaSlug
	res.PosterURL = detail.PosterURL
	res.BackdropURL = detail.BackdropURL
	res.ArtistImages = detail.ArtistImages

	// Artist row: only overwrite fields when the new value is non-empty
	// (UpdateArtistEnrichedFields handles that at the SQL level).
	newMBID := artist.MusicbrainzID
	if newMBID == "" {
		newMBID = detail.ExternalIDs["mbid"]
	}

	// Pre-update merge: if the enrich resolved this artist to something
	// that's already claimed by another local row, fold this row's
	// children into the canonical one. Otherwise the UpdateArtist call
	// below would collide on uq_artists_name_disambig (the
	// HANABIE / 花冷え。 case, where both folders matched separately at
	// scan time and only enrich learns they're the same artist).
	//
	// `postName` / `postDisambig` are what the UpdateArtistEnrichedFields
	// CASE-WHEN logic will actually write — empty upstream values
	// preserve the existing row's columns. We need to match against
	// those (not raw detail.*) because upstream sometimes returns
	// `disambiguation=null` for apple/discogs/deezer-keyed lookups,
	// which preserves the local "metalcore band" disambig and trips
	// the unique constraint against a sibling that's already canonical.
	postName := detail.ArtistName
	if postName == "" {
		postName = artist.Name
	}
	postDisambig := detail.ArtistDisambiguation
	if postDisambig == "" {
		postDisambig = artist.Disambiguation
	}
	if canonical := m.findCanonicalSibling(ctx, artistID, newMBID, postName, postDisambig); canonical != nil {
		if mergeErr := m.mergeArtistInto(ctx, canonical.ID, artistID); mergeErr != nil {
			return res, fmt.Errorf("merge artist %d into %d: %w", artistID, canonical.ID, mergeErr)
		}
		// Continue the refresh on the canonical row — children now live
		// there, including the freshly-reparented albums.
		res.ArtistID = canonical.ID
		artist = *canonical
		artistID = canonical.ID
	}

	// Persist the canonical heya.media slug on the parent media_item.
	// Stable lookup key for future refreshes — heya.media accepts
	// slug:<slug> as an artist lookup id alongside mbid:<id> and
	// per-provider keys. Runs AFTER the dup-merge above: a row that just
	// got folded into a canonical sibling (which already owns this slug in
	// the library) would otherwise issue a doomed UPDATE that trips
	// idx_media_items_heya_slug. Post-merge `artist` points at the canonical.
	if detail.HeyaSlug != "" {
		item, err := m.q.GetMediaItemByID(ctx, artist.MediaItemID)
		if err == nil && item.HeyaSlug != detail.HeyaSlug {
			if err := m.q.UpdateMediaItemHeyaSlug(ctx, sqlc.UpdateMediaItemHeyaSlugParams{
				ID:       artist.MediaItemID,
				HeyaSlug: detail.HeyaSlug,
			}); err != nil {
				log.Warn().Err(err).Int64("media_item_id", artist.MediaItemID).Msg("update heya_slug failed")
			}
		}
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

	// Extended artist metadata — all the post-00019 columns. Failures here
	// are logged but not fatal: a refresh that wrote name/bio/MBID
	// successfully shouldn't reroll if listeners/popularity update bombs
	// (most likely cause: JSONB encoding of a sparse heya.media field).
	if err := m.writeArtistExtendedMetadata(ctx, artistID, detail); err != nil {
		log.Warn().Err(err).Int64("artist_id", artistID).Msg("write artist extended metadata failed")
	}
	if err := m.writeArtistTopTracks(ctx, artistID, detail.ArtistTopTracks); err != nil {
		log.Warn().Err(err).Int64("artist_id", artistID).Msg("write artist top tracks failed")
	}
	if err := m.writeArtistSimilarArtists(ctx, artistID, detail.ArtistSimilarArtists); err != nil {
		log.Warn().Err(err).Int64("artist_id", artistID).Msg("write artist similar artists failed")
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
		// Album type resolution. Two cases worth handling:
		//   1. Most local rows start at the default 'album' — adopt the
		//      upstream type when it has anything to say.
		//   2. MusicBrainz often emits primary='Album' with secondaries
		//      like ['Compilation'] / ['Soundtrack'] — resolveAlbumType
		//      collapses the pair down to the more-specific bucket.
		newType := dbAlbum.AlbumType
		if upstreamType := resolveAlbumType(embedded.Type, embedded.SecondaryTypes); upstreamType != "" {
			if newType == "" || newType == "album" {
				newType = upstreamType
			}
		}
		coverURL := dbAlbum.CoverPath
		if coverURL == "" {
			coverURL = embedded.CoverURL
		}

		// Guard uq_albums_artist_title_year before writing: enrichment can
		// collapse two distinct local albums onto the same (artist, title,
		// year) tuple (a deluxe/standard pair fuzzy-matching one upstream
		// release, or a year backfill landing on a same-titled sibling), and
		// that rewrite would fail the album's UPDATE outright. albumWriteTitleYear
		// falls back to the local title+year when the tuple is already owned by
		// another album of this artist, so the row keeps its index slot and the
		// remaining enriched columns still land. See the helper for the detail.
		var collidedWith int64
		newTitle, newYear := albumWriteTitleYear(embedded.Title, dbAlbum.Title, newYear, dbAlbum.Year, func(title, year string) bool {
			sib, err := m.q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
				ArtistID: artistID,
				Lower:    title,
				Year:     year,
			})
			if err == nil && sib.ID != dbAlbum.ID {
				collidedWith = sib.ID
				return true
			}
			return false
		})
		if collidedWith != 0 {
			log.Debug().
				Int64("album", dbAlbum.ID).
				Int64("collides_with", collidedWith).
				Str("title", embedded.Title).
				Str("year", newYear).
				Msg("album enrichment would duplicate a sibling tuple; keeping local title+year")
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
				Column3:  newTitle,
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
		// Backfill / refresh the slug if the album hasn't got one yet, or
		// the title changed under us. Slug stays stable as a URL identifier
		// even when title cosmetics change later, so don't re-derive blindly.
		if dbAlbum.Slug == "" && embedded.Title != "" {
			m.assignAlbumSlug(ctx, artist.ID, dbAlbum.ID, embedded.Title, newYear)
		}

		// Extended album metadata (post-00019 columns). Best-effort —
		// failures here don't abort the per-album loop.
		if err := m.writeAlbumExtendedMetadata(ctx, dbAlbum.ID, embedded); err != nil {
			log.Warn().Err(err).Int64("album", dbAlbum.ID).Msg("write album extended metadata failed")
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

			// Extended track metadata — external_ids / isrc /
			// recording_mbid / preview_url / explicit / artist_credits.
			// Separate from the title/duration update path so the
			// title-only "did the row actually change?" gate doesn't
			// block writes to these.
			if err := m.writeTrackExtendedMetadata(ctx, dbTrack.ID, embeddedTrack); err != nil {
				log.Warn().Err(err).Int64("track", dbTrack.ID).Msg("write track extended metadata failed")
			}
		}
	}

	// Backfill any still-empty slugs from the DB-side title. This catches
	// albums heya.media has no record of (local-only releases, releases
	// not yet enriched upstream, or — common when the user is on a self-
	// hosted heya.media without MusicBrainz reachability — albums whose
	// MBID lookup 404'd). The transliteration pass in slug.Generate
	// handles kana/kanji, so a previously-stuck "untitled" slug becomes
	// a real romanized one as soon as a refresh runs.
	for _, dbAlbum := range dbAlbums {
		if dbAlbum.Slug != "" || dbAlbum.Title == "" {
			continue
		}
		m.assignAlbumSlug(ctx, artistID, dbAlbum.ID, dbAlbum.Title, dbAlbum.Year)
	}

	if err := m.q.MarkArtistDiscographyEnriched(ctx, artistID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		log.Warn().Err(err).Int64("artist_id", artistID).Msg("MarkArtistDiscographyEnriched failed")
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
func (m *Matcher) EnrichArtistFromHeyaMedia(ctx context.Context, mbid, name, disambig string) *metadata.MediaDetail {
	return m.enrichArtistFromHeyaMedia(ctx, mbid, name, disambig)
}
