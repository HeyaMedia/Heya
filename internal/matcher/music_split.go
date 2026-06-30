package matcher

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// SplitArtistResult summarises a SplitArtistByFolder call.
type SplitArtistResult struct {
	SourceArtistID     int64
	NewArtistID        int64
	NewArtistMediaItem int64
	NewArtistName      string
	AlbumsMoved        int // whole albums reparented wholesale
	AlbumsSplit        int // mixed albums whose folder track-files were peeled out
}

// Changed reports whether the split actually moved anything.
func (r SplitArtistResult) Changed() bool { return r.AlbumsMoved+r.AlbumsSplit > 0 }

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
// artist row — the inverse of mergeArtistInto. Albums entirely under the folder
// move wholesale (tracks / track_files / track_facets ride along on album_id, so
// nothing is recomputed). For a *mixed* album — one whose earlier same-title
// merge fused tracks from two folders, so a single track carries track_files
// from both — only the folder's track_files are peeled onto sibling tracks
// under a find-or-created destination album, leaving the rest with the source.
// The destination artist is left un-enriched (discography_enriched_at NULL) so
// the caller / next scan re-enriches it under the current matching gates.
//
// Idempotent: a re-run finds the foreign content already moved and is a no-op.
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
		mixed, err := m.q.AlbumHasFileOutsideFolder(ctx, sqlc.AlbumHasFileOutsideFolderParams{
			AlbumID: al.ID, Folder: folder,
		})
		if err != nil {
			return res, fmt.Errorf("inspect album %d: %w", al.ID, err)
		}
		if !mixed {
			// Whole album lives under the folder → move it wholesale (keeps the
			// album's identity + slug; tracks/track_files/facets ride along).
			if err := m.q.ReparentAlbumToArtist(ctx, sqlc.ReparentAlbumToArtistParams{
				DstArtistID: target.ID, AlbumID: al.ID,
			}); err != nil {
				return res, fmt.Errorf("move album %d to artist %d: %w", al.ID, target.ID, err)
			}
			res.AlbumsMoved++
			continue
		}
		// Mixed album: a prior bad merge fused same-titled releases from two
		// folders, so some tracks carry track_files from both. Peel only the
		// folder's files onto sibling tracks under a find-or-created destination
		// album, leaving the rest with the source artist.
		destAlbum, err := m.findOrCreateAlbum(ctx, target.ID, al.Title, al.Year)
		if err != nil {
			return res, fmt.Errorf("dest album for %q: %w", al.Title, err)
		}
		tracks, err := m.q.ListAlbumTracksUnderFolder(ctx, sqlc.ListAlbumTracksUnderFolderParams{
			AlbumID: al.ID, Folder: folder,
		})
		if err != nil {
			return res, fmt.Errorf("list folder tracks of album %d: %w", al.ID, err)
		}
		for _, t := range tracks {
			destTrack, err := m.q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{
				AlbumID: destAlbum, DiscNumber: t.DiscNumber, TrackNumber: t.TrackNumber, Title: t.Title,
			})
			if err != nil {
				return res, fmt.Errorf("dest track %d/%d: %w", t.DiscNumber, t.TrackNumber, err)
			}
			if destTrack.ID == t.ID {
				continue
			}
			if err := m.q.MoveTrackFilesUnderFolderToTrack(ctx, sqlc.MoveTrackFilesUnderFolderToTrackParams{
				DstTrackID: destTrack.ID, SrcTrackID: t.ID, Folder: folder,
			}); err != nil {
				return res, fmt.Errorf("move files of track %d: %w", t.ID, err)
			}
		}
		// Prune source tracks left fileless by the move, then the album if empty.
		if err := m.q.DeleteEmptyTracksOfAlbum(ctx, al.ID); err != nil {
			return res, fmt.Errorf("prune empty tracks of album %d: %w", al.ID, err)
		}
		hasTracks, err := m.q.AlbumHasTracks(ctx, al.ID)
		if err != nil {
			return res, fmt.Errorf("check album %d empty: %w", al.ID, err)
		}
		if !hasTracks {
			if err := m.q.DeleteAlbumByID(ctx, al.ID); err != nil {
				return res, fmt.Errorf("delete emptied album %d: %w", al.ID, err)
			}
		}
		res.AlbumsSplit++
	}

	log.Info().
		Int64("src_artist_id", artistID).
		Int64("new_artist_id", target.ID).
		Str("folder", folder).
		Int("albums_moved", res.AlbumsMoved).
		Int("albums_split", res.AlbumsSplit).
		Msg("split foreign-folder content into its own artist")
	return res, nil
}

// findOrCreateAlbum returns the id of artistID's album with (title, year),
// creating a bare one (slug/metadata filled by the next enrich) when absent.
func (m *Matcher) findOrCreateAlbum(ctx context.Context, artistID int64, title, year string) (int64, error) {
	existing, err := m.q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
		ArtistID: artistID, Lower: title, Year: year,
	})
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	created, err := m.q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artistID, Title: title, Year: year, Genres: []string{}, Tags: []string{},
	})
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}
