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
// Returns nil when no sibling matches — caller proceeds with a normal
// in-place UPDATE in that case.
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
// A nil return on a non-NoRows error is logged and treated as "proceed
// with the UPDATE" — the worst case is we hit the unique constraint and
// fail this enrich, not data corruption.
func (m *Matcher) findCanonicalSibling(ctx context.Context, artistID int64, newMBID, postName, postDisambig string) *sqlc.Artist {
	if newMBID != "" {
		sibling, err := m.q.GetArtistByMusicBrainzIDExcludingID(ctx, sqlc.GetArtistByMusicBrainzIDExcludingIDParams{
			Mbid:      newMBID,
			ExcludeID: artistID,
		})
		if err == nil {
			return &sibling
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("mbid", newMBID).Msg("dup-artist MBID lookup failed")
		}
	}
	if postName != "" {
		sibling, err := m.q.GetArtistByNameAndDisambiguationExcludingID(ctx, sqlc.GetArtistByNameAndDisambiguationExcludingIDParams{
			Lower:     postName,
			Lower_2:   postDisambig,
			ExcludeID: artistID,
		})
		if err == nil {
			return &sibling
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("name", postName).Msg("dup-artist name lookup failed")
		}
	}
	return nil
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
//
// Order:
//  1. Reparent albums (tracks follow via album_id)
//  2. Re-point any other artist's similar-list local_artist_id at dst
//  3. Drop src's derived rows (centroids, top-tracks, similar) — they'll
//     regenerate on dst's next sonic/refresh cycle
//  4. Merge user_artist_ratings, prefer the higher rating on collision
//  5. Merge user_favorites (love-as-artist), de-dupe on collision
//  6. Delete src artist + its media_item
func (m *Matcher) mergeArtistInto(ctx context.Context, dstID, srcID int64) error {
	if dstID == srcID {
		return nil
	}
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := m.q.WithTx(tx)

	src, err := qtx.GetArtistByID(ctx, srcID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already merged or otherwise gone — treat as success.
			return nil
		}
		return fmt.Errorf("get src artist %d: %w", srcID, err)
	}

	if err := qtx.ReparentAlbums(ctx, sqlc.ReparentAlbumsParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("reparent albums: %w", err)
	}
	if err := qtx.ReparentSimilarLocalRefs(ctx, sqlc.ReparentSimilarLocalRefsParams{
		DstID: pgtype.Int8{Int64: dstID, Valid: true},
		SrcID: pgtype.Int8{Int64: srcID, Valid: true},
	}); err != nil {
		return fmt.Errorf("reparent similar local refs: %w", err)
	}
	if err := qtx.DeleteArtistCentroid(ctx, srcID); err != nil {
		return fmt.Errorf("delete src centroid: %w", err)
	}
	if err := qtx.DeleteArtistTopTracks(ctx, srcID); err != nil {
		return fmt.Errorf("delete src top tracks: %w", err)
	}
	if err := qtx.DeleteArtistSimilarArtists(ctx, srcID); err != nil {
		return fmt.Errorf("delete src similar artists: %w", err)
	}
	if err := qtx.MergeUserArtistRatings(ctx, sqlc.MergeUserArtistRatingsParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("merge ratings: %w", err)
	}
	if err := qtx.DeleteUserArtistRatingsByArtist(ctx, srcID); err != nil {
		return fmt.Errorf("delete src ratings: %w", err)
	}
	if err := qtx.MergeArtistFavorites(ctx, sqlc.MergeArtistFavoritesParams{DstID: dstID, SrcID: srcID}); err != nil {
		return fmt.Errorf("merge favorites: %w", err)
	}
	if err := qtx.DeleteArtistFavorites(ctx, srcID); err != nil {
		return fmt.Errorf("delete src favorites: %w", err)
	}
	if err := qtx.DeleteArtist(ctx, srcID); err != nil {
		return fmt.Errorf("delete src artist: %w", err)
	}
	// media_items row referenced by src.media_item_id. Cascade-deletion
	// from media_items handles assets/extras/external_ratings/etc; we
	// drop the row explicitly so the orphaned media_item doesn't linger.
	if err := qtx.DeleteMediaItem(ctx, src.MediaItemID); err != nil {
		return fmt.Errorf("delete src media_item: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Info().Int64("dst_artist_id", dstID).Int64("src_artist_id", srcID).Msg("merged duplicate artist into canonical row")
	return nil
}
