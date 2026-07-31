package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/manager/decision"
	"github.com/karbowiak/heya/internal/matcher"
)

// buildMusicTarget assembles the decision target for one music edition
// group (a manager_music_targets row): artist identity (name + aliases),
// every album title of the group (catalog rows + linked local albums — the
// engine verifies identity by artist AND album containment), and existing
// files derived from the local albums' track formats.
func (a *App) buildMusicTarget(ctx context.Context, itemID, musicTargetID int64) (decision.Target, searchTargetMeta, error) {
	var (
		target decision.Target
		meta   searchTargetMeta
	)
	var (
		artistID                     int64
		albumType, editionKey, title string
		yearText, artistName         string
		aliases                      []string
		targetMonitored              bool
		artistMediaItemID, libraryID int64
		profileID                    pgtype.Int8
	)
	err := a.db.QueryRow(ctx, `
		SELECT t.artist_id, t.album_type, t.edition_key, t.title, t.year, t.monitored,
		       ar.name, ar.aliases, ar.media_item_id, mi.library_id, mi.quality_profile_id
		FROM manager_music_targets t
		JOIN artists ar ON ar.id = t.artist_id
		JOIN media_items mi ON mi.id = ar.media_item_id
		WHERE t.id = $1`, musicTargetID).Scan(
		&artistID, &albumType, &editionKey, &title, &yearText, &targetMonitored,
		&artistName, &aliases, &artistMediaItemID, &libraryID, &profileID)
	if err != nil {
		return target, meta, fmt.Errorf("music target %d: %w", musicTargetID, err)
	}
	if artistMediaItemID != itemID {
		return target, meta, fmt.Errorf("music target %d belongs to a different artist", musicTargetID)
	}

	year, _ := strconv.Atoi(strings.TrimSpace(yearText))

	meta = searchTargetMeta{
		LibraryID: libraryID, Domain: "music", Title: artistName, Year: year,
		ScopeLabel:    title,
		MusicTargetID: musicTargetID, ArtistName: artistName,
		AlbumType: albumType, EditionKey: editionKey, AlbumTitle: title,
	}
	if profileID.Valid {
		meta.ProfileID = profileID.Int64
	}

	target = decision.Target{
		Domain:      "music",
		MediaItemID: artistMediaItemID,
		Year:        year,
		IDs:         map[string]string{},
	}
	for _, name := range append([]string{artistName}, aliases...) {
		if n := matcher.NormalizeTitle(name); n != "" {
			target.NormalizedTitles = append(target.NormalizedTitles, n)
		}
	}

	// Every title in the edition group corroborates identity: the catalog
	// rows (base + Deluxe + …) and any linked local album titles.
	titleRows, err := a.db.Query(ctx, `
		SELECT d.title FROM artist_discography d
		WHERE d.artist_id = $1 AND d.album_type = $2
		  AND COALESCE(NULLIF(d.edition_key, ''), lower(d.title)) = $3
		UNION
		SELECT al.title FROM artist_discography d
		JOIN albums al ON al.id = d.album_id
		WHERE d.artist_id = $1 AND d.album_type = $2
		  AND COALESCE(NULLIF(d.edition_key, ''), lower(d.title)) = $3`,
		artistID, albumType, editionKey)
	if err != nil {
		return target, meta, fmt.Errorf("music target titles: %w", err)
	}
	defer titleRows.Close()
	seen := map[string]bool{}
	for titleRows.Next() {
		var t string
		if err := titleRows.Scan(&t); err != nil {
			return target, meta, err
		}
		if n := matcher.NormalizeTitle(t); n != "" && !seen[n] {
			seen[n] = true
			target.AlbumTitles = append(target.AlbumTitles, n)
		}
	}
	if err := titleRows.Err(); err != nil {
		return target, meta, err
	}
	if base := matcher.NormalizeTitle(title); base != "" && !seen[base] {
		target.AlbumTitles = append(target.AlbumTitles, base)
	}

	// Existing files: the linked local albums' distinct track formats, one
	// ExistingFile per format tier. track_files carries probed audio facts —
	// this is media_info-grade provenance, not filename guessing. Runtime is
	// the longest linked album's total duration (size gate scale).
	unit := decision.Unit{
		Key:       fmt.Sprintf("music:%d", musicTargetID),
		Monitored: targetMonitored,
		Released:  year == 0 || year <= time.Now().Year(),
	}
	fileRows, err := a.db.Query(ctx, `
		SELECT DISTINCT COALESCE(tf.format, ''), COALESCE(tf.bitrate_kbps, 0), COALESCE(tf.bit_depth, 0)
		FROM artist_discography d
		JOIN albums al ON al.id = d.album_id
		JOIN tracks tr ON tr.album_id = al.id
		JOIN track_files tf ON tf.track_id = tr.id
		JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
		WHERE d.artist_id = $1 AND d.album_type = $2
		  AND COALESCE(NULLIF(d.edition_key, ''), lower(d.title)) = $3`,
		artistID, albumType, editionKey)
	if err != nil {
		return target, meta, fmt.Errorf("music target files: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var format string
		var bitrate, bitDepth int32
		if err := fileRows.Scan(&format, &bitrate, &bitDepth); err != nil {
			return target, meta, err
		}
		quality := musicFileQualityKey(format, bitrate, bitDepth)
		unit.Existing = append(unit.Existing, decision.ExistingFile{
			Basename:   fmt.Sprintf("%s [%s %d kbps]", title, format, bitrate),
			Quality:    quality,
			Provenance: "media_info",
			Uncertain:  quality == "",
		})
	}
	if err := fileRows.Err(); err != nil {
		return target, meta, err
	}

	var runtimeSec int
	err = a.db.QueryRow(ctx, `
		SELECT COALESCE(max(album_runtime), 0) FROM (
			SELECT sum(tr.duration) AS album_runtime
			FROM artist_discography d
			JOIN albums al ON al.id = d.album_id
			JOIN tracks tr ON tr.album_id = al.id
			WHERE d.artist_id = $1 AND d.album_type = $2
			  AND COALESCE(NULLIF(d.edition_key, ''), lower(d.title)) = $3
			GROUP BY al.id
		) runtimes`, artistID, albumType, editionKey).Scan(&runtimeSec)
	if err == nil && runtimeSec > 0 {
		target.RuntimeMinutes = runtimeSec / 60
	}

	target.Units = []decision.Unit{unit}
	return target, meta, nil
}

// musicFileQualityKey maps a local track file's probed audio facts onto the
// music quality ladder. Honest empty string when the tier is ambiguous.
func musicFileQualityKey(format string, bitrateKbps, bitDepth int32) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
	case "flac":
		if bitDepth > 16 {
			return "flac-24"
		}
		return "flac"
	case "alac":
		return "alac"
	case "ape":
		return "ape"
	case "wavpack", "wv":
		return "wavpack"
	case "wav":
		return "wav"
	case "mp3":
		switch {
		case bitrateKbps >= 300:
			return "mp3-320"
		case bitrateKbps >= 230:
			return "mp3-v0"
		case bitrateKbps >= 180:
			return "mp3-192"
		}
		return ""
	case "aac", "m4a":
		switch {
		case bitrateKbps >= 300:
			return "aac-320"
		case bitrateKbps >= 230:
			return "aac-256"
		}
		return ""
	case "ogg", "vorbis":
		if bitrateKbps >= 450 {
			return "ogg-vorbis-q10"
		}
		return ""
	}
	return ""
}

// ── Queue-side music recognition ─────────────────────────────────────────

type musicQueueTarget struct {
	id        int64
	title     string
	compact   string
	monitored bool
}

type musicQueueArtist struct {
	artistID    int64
	mediaItemID int64
	libraryID   int64
	name        string
	monitored   bool
	compact     []string
	targets     []musicQueueTarget
}

// musicQueueIndex recognizes scene music names against the library's
// artists and their release groups.
type musicQueueIndex struct {
	artists []*musicQueueArtist
}

func (a *App) buildMusicQueueIndex(ctx context.Context) (*musicQueueIndex, error) {
	rows, err := a.db.Query(ctx, `
		SELECT ar.id, ar.media_item_id, mi.library_id, ar.name, ar.aliases, mi.monitored,
		       COALESCE(t.id, 0), COALESCE(t.title, ''), COALESCE(t.monitored, false)
		FROM artists ar
		JOIN media_items mi ON mi.id = ar.media_item_id
		LEFT JOIN manager_music_targets t ON t.artist_id = ar.id
		ORDER BY ar.id`)
	if err != nil {
		return nil, fmt.Errorf("building music queue index: %w", err)
	}
	defer rows.Close()

	index := &musicQueueIndex{}
	byArtist := map[int64]*musicQueueArtist{}
	for rows.Next() {
		var (
			artistID, mediaItemID, libraryID, targetID int64
			name, targetTitle                          string
			aliases                                    []string
			artistMonitored, targetMonitored           bool
		)
		if err := rows.Scan(&artistID, &mediaItemID, &libraryID, &name, &aliases,
			&artistMonitored, &targetID, &targetTitle, &targetMonitored); err != nil {
			return nil, err
		}
		artist, ok := byArtist[artistID]
		if !ok {
			artist = &musicQueueArtist{
				artistID: artistID, mediaItemID: mediaItemID, libraryID: libraryID,
				name: name, monitored: artistMonitored,
			}
			for _, alias := range append([]string{name}, aliases...) {
				if c := compactQueueTitle(alias); len(c) >= 3 {
					artist.compact = append(artist.compact, c)
				}
			}
			byArtist[artistID] = artist
			index.artists = append(index.artists, artist)
		}
		if targetID != 0 {
			if c := compactQueueTitle(targetTitle); len(c) >= 3 {
				artist.targets = append(artist.targets, musicQueueTarget{
					id: targetID, title: targetTitle, compact: c, monitored: targetMonitored,
				})
			}
		}
	}
	return index, rows.Err()
}

func compactQueueTitle(s string) string {
	return strings.ReplaceAll(matcher.NormalizeTitle(s), " ", "")
}

// match finds the artist whose name (or alias) PREFIXES the compact release
// name — scene music names lead with the artist. The longest artist match
// wins; a length tie between different artists is ambiguous and never
// guesses. The longest release-group title contained in the name follows.
func (idx *musicQueueIndex) match(name string) (*musicQueueArtist, *musicQueueTarget) {
	compact := compactQueueTitle(name)
	if compact == "" {
		return nil, nil
	}
	var best *musicQueueArtist
	bestLen := 0
	ambiguous := false
	for _, artist := range idx.artists {
		for _, cn := range artist.compact {
			if !strings.HasPrefix(compact, cn) {
				continue
			}
			switch {
			case len(cn) > bestLen:
				best, bestLen, ambiguous = artist, len(cn), false
			case len(cn) == bestLen && best != nil && artist != best:
				ambiguous = true
			}
		}
	}
	if best == nil || ambiguous {
		return nil, nil
	}
	var bestTarget *musicQueueTarget
	bestTargetLen := 0
	for i := range best.targets {
		t := &best.targets[i]
		if strings.Contains(compact, t.compact) && len(t.compact) > bestTargetLen {
			bestTarget, bestTargetLen = t, len(t.compact)
		}
	}
	return best, bestTarget
}
