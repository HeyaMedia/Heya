package matcher

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// SplitArtistResult summarises a SplitArtistByFolder call.
type SplitArtistResult struct {
	SourceArtistID     int64
	NewArtistID        int64
	NewArtistMediaItem int64
	NewArtistName      string
	AlbumsMoved        int
}

// folderDisambigPattern peels a single trailing parenthetical off a folder name.
var folderDisambigPattern = regexp.MustCompile(`^(.*?)\s*\(([^)]*)\)\s*$`)

// folderToNameDisambig splits a top-level artist folder into a (name,
// disambiguation) pair:
//
//	"Avicii"                       -> ("Avicii", "")
//	"Adaro (Dutch DJ & producer)"  -> ("Adaro", "Dutch DJ & producer")
//
// If peeling the parenthetical would leave an empty name (e.g. "(techno)"), the
// whole folder is kept as the name.
func folderToNameDisambig(folder string) (string, string) {
	folder = strings.TrimSpace(folder)
	if m := folderDisambigPattern.FindStringSubmatch(folder); m != nil && strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}
	return folder, ""
}

// SplitArtistByFolder repairs an over-eager enrichment merge by moving every
// album of artistID whose files live under `folder` back out into their own
// artist row — the inverse of mergeArtistInto. The albums (and their tracks /
// track_files / track_facets, which ride along on album_id, so nothing is
// recomputed) are re-pointed at a find-or-created artist named after the
// folder. The destination is left un-enriched (discography_enriched_at NULL) so
// the caller / next scan re-enriches it under the current matching gates.
//
// Idempotent: a re-run finds the foreign albums already moved and is a no-op.
// All work goes through m.q, so a caller can drive it inside a transaction.
func (m *Matcher) SplitArtistByFolder(ctx context.Context, artistID int64, folder string) (SplitArtistResult, error) {
	res := SplitArtistResult{SourceArtistID: artistID}

	src, err := m.q.GetArtistByID(ctx, artistID)
	if err != nil {
		return res, fmt.Errorf("get artist %d: %w", artistID, err)
	}
	item, err := m.q.GetMediaItemByID(ctx, src.MediaItemID)
	if err != nil {
		return res, fmt.Errorf("get source media_item: %w", err)
	}

	albums, err := m.q.ListAlbumsByArtistUnderFolder(ctx, sqlc.ListAlbumsByArtistUnderFolderParams{
		ArtistID: artistID,
		Folder:   folder,
	})
	if err != nil {
		return res, fmt.Errorf("list albums under folder %q: %w", folder, err)
	}
	if len(albums) == 0 {
		return res, nil // nothing under that folder → no-op
	}

	name, disambig := folderToNameDisambig(folder)
	if name == "" {
		return res, fmt.Errorf("folder %q yields an empty artist name", folder)
	}

	// Find-or-create the destination artist (its own media_item + slug, via the
	// canonical scan-time path).
	target, err := m.upsertMusicArtist(ctx, item.LibraryID, name, disambig, "", "", "")
	if err != nil {
		return res, fmt.Errorf("create target artist %q: %w", name, err)
	}
	if target.ID == artistID {
		return res, fmt.Errorf("folder %q resolves to the same artist; refusing to split into self", folder)
	}
	res.NewArtistID = target.ID
	res.NewArtistMediaItem = target.MediaItemID
	res.NewArtistName = target.Name

	for _, al := range albums {
		if err := m.q.ReparentAlbumToArtist(ctx, sqlc.ReparentAlbumToArtistParams{
			DstArtistID: target.ID,
			AlbumID:     al.ID,
		}); err != nil {
			return res, fmt.Errorf("move album %d to artist %d: %w", al.ID, target.ID, err)
		}
		res.AlbumsMoved++
	}

	log.Info().
		Int64("src_artist_id", artistID).
		Int64("new_artist_id", target.ID).
		Str("folder", folder).
		Int("albums_moved", res.AlbumsMoved).
		Msg("split foreign-folder albums into their own artist")
	return res, nil
}
