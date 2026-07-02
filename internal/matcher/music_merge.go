package matcher

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// findCanonicalSibling looks for an existing artist row that represents
// the same person/group as `artistID` after enrichment resolved them.
// Returns (nil, false) when no sibling matches — caller proceeds with a
// normal in-place UPDATE in that case.
//
// Checks in priority order:
//  1. MBID match (excluding self) — strongest signal when upstream
//     gave us one
//  2. Post-update (name, disambig) match — what UpdateArtistEnrichedFields
//     will actually write after the CASE-WHEN preserves empty upstream
//     fields. This catches the upstream-no-MBID case where the canonical
//     name we'd write already exists on a sibling row (e.g. apple-keyed
//     hit returning name="花冷え。" while the sibling row already has it).
//
// contradicted=true means the name path DID find the tuple's owner, but its
// established MBID contradicts newMBID — two proven-distinct acts (e.g. the
// pair upsertMusicArtist deliberately split). The caller must neither merge
// nor adopt the identity: writing (postName, postDisambig) would land exactly
// on the sibling's uq_artists_name_disambig slot.
//
// A (nil, false) return on a non-NoRows error is logged and treated as
// "proceed with the UPDATE" — the worst case is we hit the unique constraint
// and fail this enrich, not data corruption.
func (m *Matcher) findCanonicalSibling(ctx context.Context, artistID int64, newMBID, postName, postDisambig string) (sibling *sqlc.Artist, contradicted bool) {
	// MBID path — strongest signal, but only for a real id. Empty is skipped by
	// the guard; a synthetic heya.media placeholder ("dddddddd-…") would match
	// any other row carrying the same placeholder and fuse unrelated artists,
	// so it's excluded too (the SQL also guards `musicbrainz_id != ''`).
	if newMBID != "" && !isSyntheticMBID(newMBID) {
		found, err := m.q.GetArtistByMusicBrainzIDExcludingID(ctx, sqlc.GetArtistByMusicBrainzIDExcludingIDParams{
			Mbid:      newMBID,
			ExcludeID: artistID,
		})
		if err == nil {
			return &found, false
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("mbid", newMBID).Msg("dup-artist MBID lookup failed")
		}
	}
	// Name path — only fires with a NON-EMPTY disambiguation. Same name + empty
	// disambiguation is too weak: two distinct acts that happen to share a name
	// ("Ado", "666") would otherwise fuse. Requiring a matching, non-empty
	// disambiguation keeps the legitimate transliteration-rename merge
	// (HANABIE / 花冷え。 carrying the same "metalcore band" disambig) while
	// dropping the ambiguous case. The SQL also guards `disambiguation != ''`.
	if postName != "" && postDisambig != "" {
		found, err := m.q.GetArtistByNameAndDisambiguationExcludingID(ctx, sqlc.GetArtistByNameAndDisambiguationExcludingIDParams{
			Lower:     postName,
			Lower_2:   postDisambig,
			ExcludeID: artistID,
		})
		if err == nil {
			if newMBID != "" && found.MusicbrainzID != "" && found.MusicbrainzID != newMBID &&
				!isSyntheticMBID(newMBID) && !isSyntheticMBID(found.MusicbrainzID) {
				// Both rows hold real, differing MBIDs — name equality must
				// not overrule that (it would re-fuse the pair the upsert
				// split, folding one act's discography into the other).
				log.Warn().Int64("artist_id", artistID).Str("new_mbid", newMBID).
					Int64("sibling_id", found.ID).Str("sibling_mbid", found.MusicbrainzID).
					Str("name", postName).
					Msg("name/disambig sibling holds a contradicting MBID; refusing merge")
				return nil, true
			}
			return &found, false
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("name", postName).Msg("dup-artist name lookup failed")
		}
	}
	return nil, false
}

// mergeArtistInto folds the src artist's children into dst. Used by
// RefreshMusicArtist when an enrichment-time lookup discovers that the
// row we're enriching shares an MBID with another existing row — the
// classic "user dropped HANABIE/ and 花冷え。/ as separate folders;
// they're actually the same artist" case.
//
// Idempotent — calling with src==dst returns nil without touching the
// DB. All work happens inside a single transaction so a failure leaves
// no half-moved children behind.
func (m *Matcher) mergeArtistInto(ctx context.Context, dstID, srcID int64) error {
	if dstID == srcID {
		return nil
	}
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	merged, err := mergeArtistIntoTx(ctx, m.q.WithTx(tx), dstID, srcID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if merged {
		log.Info().Int64("dst_artist_id", dstID).Int64("src_artist_id", srcID).Msg("merged duplicate artist into canonical row")
	}
	return nil
}

// mergeArtistIntoTx runs the artist merge inside a caller-supplied
// transaction and reports whether any work happened (false when src was
// already gone). Split out from mergeArtistInto so tests can drive it inside a
// rollback tx without committing to the database.
//
// Order:
//  1. Reparent albums — collision-safe: an album that clashes with a dst album
//     on (lower(title), year) is folded into it (mergeAlbumInto) rather than
//     tripping uq_albums_artist_title_year on a blind move.
//  2. Re-point any other artist's similar-list local_artist_id at dst
//  3. Drop src's derived rows (centroids, top-tracks, similar) — they'll
//     regenerate on dst's next sonic/refresh cycle
//  4. Merge user_artist_ratings, prefer the higher rating on collision
//  5. Merge user_favorites (love-as-artist), de-dupe on collision
//  6. Delete src artist + its media_item
func mergeArtistIntoTx(ctx context.Context, qtx *sqlc.Queries, dstID, srcID int64) (bool, error) {
	src, err := qtx.GetArtistByID(ctx, srcID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already merged or otherwise gone — treat as success.
			return false, nil
		}
		return false, fmt.Errorf("get src artist %d: %w", srcID, err)
	}

	if err := reparentAlbumsWithMerge(ctx, qtx, dstID, srcID); err != nil {
		return false, err
	}
	if err := qtx.ReparentSimilarLocalRefs(ctx, sqlc.ReparentSimilarLocalRefsParams{
		DstID: pgtype.Int8{Int64: dstID, Valid: true},
		SrcID: pgtype.Int8{Int64: srcID, Valid: true},
	}); err != nil {
		return false, fmt.Errorf("reparent similar local refs: %w", err)
	}
	if err := qtx.DeleteArtistCentroid(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src centroid: %w", err)
	}
	if err := qtx.DeleteArtistTopTracks(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src top tracks: %w", err)
	}
	if err := qtx.DeleteArtistSimilarArtists(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src similar artists: %w", err)
	}
	if err := qtx.MergeUserArtistRatings(ctx, sqlc.MergeUserArtistRatingsParams{DstID: dstID, SrcID: srcID}); err != nil {
		return false, fmt.Errorf("merge ratings: %w", err)
	}
	if err := qtx.DeleteUserArtistRatingsByArtist(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src ratings: %w", err)
	}
	if err := qtx.MergeArtistFavorites(ctx, sqlc.MergeArtistFavoritesParams{DstID: dstID, SrcID: srcID}); err != nil {
		return false, fmt.Errorf("merge favorites: %w", err)
	}
	if err := qtx.DeleteArtistFavorites(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src favorites: %w", err)
	}
	if err := qtx.DeleteArtist(ctx, srcID); err != nil {
		return false, fmt.Errorf("delete src artist: %w", err)
	}
	// media_items row referenced by src.media_item_id. Cascade-deletion
	// from media_items handles assets/extras/external_ratings/etc; we
	// drop the row explicitly so the orphaned media_item doesn't linger.
	if err := qtx.DeleteMediaItem(ctx, src.MediaItemID); err != nil {
		return false, fmt.Errorf("delete src media_item: %w", err)
	}
	return true, nil
}

// reparentAlbumsWithMerge moves every src album onto dst. An album that would
// collide with a dst album on (lower(title), year) — the same release dropped
// under two folders of what turns out to be one artist — is folded into that
// dst album via mergeAlbumInto instead of blind-moved (which would trip
// uq_albums_artist_title_year and abort the whole artist merge). Non-colliding
// albums move with ReparentAlbumToArtist, which also clears a slug that would
// clash on uq_albums_artist_slug.
func reparentAlbumsWithMerge(ctx context.Context, qtx *sqlc.Queries, dstID, srcID int64) error {
	srcAlbums, err := qtx.ListAlbumsByArtist(ctx, srcID)
	if err != nil {
		return fmt.Errorf("list src albums: %w", err)
	}
	for _, sa := range srcAlbums {
		dst, err := qtx.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
			ArtistID: dstID,
			Lower:    sa.Title,
			Year:     sa.Year,
		})
		switch {
		case err == nil && dst.ID != sa.ID:
			if err := mergeAlbumInto(ctx, qtx, dst.ID, sa.ID); err != nil {
				return fmt.Errorf("merge album %d into %d: %w", sa.ID, dst.ID, err)
			}
		case err == nil, errors.Is(err, pgx.ErrNoRows):
			// No distinct collision (no row, or the row is sa itself): move it.
			if err := qtx.ReparentAlbumToArtist(ctx, sqlc.ReparentAlbumToArtistParams{
				DstArtistID: dstID,
				AlbumID:     sa.ID,
			}); err != nil {
				return fmt.Errorf("reparent album %d: %w", sa.ID, err)
			}
		default:
			return fmt.Errorf("lookup dst album for %d: %w", sa.ID, err)
		}
	}
	return nil
}

// mergeAlbumInto folds src_album's tracks into dst_album and deletes the
// emptied src album. Tracks that collide on (disc, track_number) keep the dst
// track but inherit src's track_files (the audio survives); the rest move
// across. Album ratings/favorites migrate too. Caller must run this inside a
// transaction.
func mergeAlbumInto(ctx context.Context, qtx *sqlc.Queries, dstAlbumID, srcAlbumID int64) error {
	// Fold everything attached to a colliding src track onto the surviving dst
	// track BEFORE deleting it — the audio (track_files) and the user-scoped
	// rows (ratings, playlist memberships, play history). Otherwise the
	// CASCADE on the track delete would silently drop that user data.
	if err := qtx.ReparentCollidingAlbumTrackFiles(ctx, sqlc.ReparentCollidingAlbumTrackFilesParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("move colliding track files: %w", err)
	}
	if err := qtx.MergeCollidingAlbumTrackRatings(ctx, sqlc.MergeCollidingAlbumTrackRatingsParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("merge colliding track ratings: %w", err)
	}
	if err := qtx.MergeCollidingAlbumTrackPlaylists(ctx, sqlc.MergeCollidingAlbumTrackPlaylistsParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("merge colliding track playlists: %w", err)
	}
	if err := qtx.ReparentCollidingAlbumTrackPlayEvents(ctx, sqlc.ReparentCollidingAlbumTrackPlayEventsParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("move colliding track play events: %w", err)
	}
	if err := qtx.DeleteCollidingAlbumTracks(ctx, sqlc.DeleteCollidingAlbumTracksParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("delete colliding tracks: %w", err)
	}
	if err := qtx.ReparentAlbumTracks(ctx, sqlc.ReparentAlbumTracksParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("reparent surviving tracks: %w", err)
	}
	if err := qtx.MergeAlbumRatings(ctx, sqlc.MergeAlbumRatingsParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("merge album ratings: %w", err)
	}
	if err := qtx.MergeAlbumFavorites(ctx, sqlc.MergeAlbumFavoritesParams{
		DstAlbumID: dstAlbumID, SrcAlbumID: srcAlbumID,
	}); err != nil {
		return fmt.Errorf("merge album favorites: %w", err)
	}
	if err := qtx.DeleteAlbumByID(ctx, srcAlbumID); err != nil {
		return fmt.Errorf("delete src album: %w", err)
	}
	return nil
}
