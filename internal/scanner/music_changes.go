package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type musicLibraryCatalogIndex struct {
	artistMBIDs map[string]bool
	artistNames map[string]bool
	releaseIDs  map[string]bool
	groupIDs    map[string]bool
	tuples      map[string]bool
	titles      map[string]bool
}

func markChangedMusicReleases(ctx context.Context, db *pgxpool.Pool, libraryID int64, artists []MusicArtistPlan) error {
	index, err := loadMusicLibraryCatalogIndex(ctx, db, libraryID)
	if err != nil {
		return err
	}
	markChangedMusicReleasesFromIndex(artists, index)
	return nil
}

func loadMusicLibraryCatalogIndex(ctx context.Context, db *pgxpool.Pool, libraryID int64) (musicLibraryCatalogIndex, error) {
	index := newMusicLibraryCatalogIndex()
	rows, err := db.Query(ctx, `
		SELECT artist.name,
		       artist.musicbrainz_id,
		       album.title,
		       album.year,
		       album.musicbrainz_id,
		       COALESCE(album.external_ids, '{}'::jsonb)
		FROM artists artist
		JOIN media_items item ON item.id=artist.media_item_id
		LEFT JOIN albums album ON album.artist_id=artist.id
		WHERE item.library_id=$1`,
		libraryID,
	)
	if err != nil {
		return index, fmt.Errorf("load existing music catalog: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var artistName, artistMBID string
		var albumTitle, albumYear, albumMBID *string
		var rawExternalIDs []byte
		if err := rows.Scan(&artistName, &artistMBID, &albumTitle, &albumYear, &albumMBID, &rawExternalIDs); err != nil {
			return index, fmt.Errorf("scan existing music catalog: %w", err)
		}
		artistKey := normalizeMusicKeyPart(artistName)
		if artistKey != "" {
			index.artistNames[artistKey] = true
		}
		if mbid := normalizedMusicIdentifier(artistMBID); mbid != "" {
			index.artistMBIDs[mbid] = true
		}
		if albumTitle == nil {
			continue
		}
		titleKey := normalizeMusicKeyPart(*albumTitle)
		year := ""
		if albumYear != nil {
			year = strings.TrimSpace(*albumYear)
		}
		if artistKey != "" && titleKey != "" {
			index.tuples[musicLibraryAlbumTuple(artistKey, titleKey, year)] = true
			index.titles[musicLibraryAlbumTitle(artistKey, titleKey)] = true
		}
		if albumMBID != nil {
			if value := normalizedMusicIdentifier(*albumMBID); value != "" {
				index.groupIDs[value] = true
			}
		}
		externalIDs := map[string]string{}
		_ = json.Unmarshal(rawExternalIDs, &externalIDs)
		if value := normalizedMusicIdentifier(externalIDs["musicbrainz_release_group"]); value != "" {
			index.groupIDs[value] = true
		}
		if value := normalizedMusicIdentifier(externalIDs["musicbrainz_album"]); value != "" {
			index.releaseIDs[value] = true
		}
	}
	if err := rows.Err(); err != nil {
		return index, fmt.Errorf("read existing music catalog: %w", err)
	}
	return index, nil
}

func newMusicLibraryCatalogIndex() musicLibraryCatalogIndex {
	return musicLibraryCatalogIndex{
		artistMBIDs: map[string]bool{},
		artistNames: map[string]bool{},
		releaseIDs:  map[string]bool{},
		groupIDs:    map[string]bool{},
		tuples:      map[string]bool{},
		titles:      map[string]bool{},
	}
}

func markChangedMusicReleasesFromIndex(artists []MusicArtistPlan, index musicLibraryCatalogIndex) {
	for artistIndex := range artists {
		artist := &artists[artistIndex]
		artistKey := normalizeMusicKeyPart(artist.Artist)
		artistMBID := normalizedMusicIdentifier(artist.ExternalIDs["mbid"])
		artistKnown := (artistMBID != "" && index.artistMBIDs[artistMBID]) || (artistKey != "" && index.artistNames[artistKey])
		if !artistKnown {
			continue
		}
		for albumIndex := range artist.Albums {
			album := &artist.Albums[albumIndex]
			releaseID := normalizedMusicIdentifier(album.ExternalIDs["musicbrainz_album"])
			groupID := normalizedMusicIdentifier(album.ExternalIDs["musicbrainz_release_group"])
			titleKey := normalizeMusicKeyPart(album.Album)
			tuple := musicLibraryAlbumTuple(artistKey, titleKey, strings.TrimSpace(album.Year))
			known := (releaseID != "" && index.releaseIDs[releaseID]) ||
				(groupID != "" && index.groupIDs[groupID]) ||
				index.tuples[tuple] ||
				index.titles[musicLibraryAlbumTitle(artistKey, titleKey)]
			album.Changed = !known
		}
	}
}

func normalizedMusicIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func musicLibraryAlbumTuple(artist, album, year string) string {
	return artist + "\x00" + album + "\x00" + year
}

func musicLibraryAlbumTitle(artist, album string) string {
	return artist + "\x00" + album
}
