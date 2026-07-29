// Package discography persists the full per-artist release-group catalog the
// metadata provider already returns on every artist enrich. The albums table
// stays local-first; this is the manager's "what exists" side of the ledger,
// so missing EPs/albums are countable the way missing TV episodes are.
package discography

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

// Sync replaces an artist's stored discography with the provider's current
// view, linking entries to local albums where the library has the release.
// A nil/empty entries slice is a no-op rather than a wipe: providers
// occasionally return partial payloads on errors upstream, and an empty
// discography for an artist with local albums is always wrong.
func Sync(ctx context.Context, q *sqlc.Queries, artistID int64, entries []metadata.AlbumEntry) error {
	if len(entries) == 0 {
		return nil
	}
	locals, err := q.ListAlbumsByArtist(ctx, artistID)
	if err != nil {
		return fmt.Errorf("discography: list local albums: %w", err)
	}

	if err := q.DeleteArtistDiscography(ctx, artistID); err != nil {
		return fmt.Errorf("discography: clear: %w", err)
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			continue
		}
		// One row per release group: canonical id when resolved, otherwise
		// title+year keeps unresolved relation evidence from duplicating.
		key := entry.CanonicalID
		if key == "" {
			key = "t:" + normalizeTitle(title) + "|" + strconv.Itoa(entry.Year)
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		externalIDs, _ := json.Marshal(entry.ExternalIDs)
		if entry.ExternalIDs == nil {
			externalIDs = []byte("{}")
		}
		year := ""
		if entry.Year > 0 {
			year = strconv.Itoa(entry.Year)
		}
		trackCount := entry.TrackCount
		if trackCount == 0 {
			trackCount = len(entry.Tracks)
		}
		if trackCount == 0 {
			// Fall back to the smallest non-zero edition — the base issue,
			// not a deluxe variant.
			for _, edition := range entry.Editions {
				if edition.TrackCount > 0 && (trackCount == 0 || edition.TrackCount < trackCount) {
					trackCount = edition.TrackCount
				}
			}
		}
		var albumID pgtype.Int8
		if local, ok := matchLocalAlbum(entry, locals); ok {
			albumID = pgtype.Int8{Int64: local.ID, Valid: true}
		}
		if err := q.InsertArtistDiscographyEntry(ctx, sqlc.InsertArtistDiscographyEntryParams{
			ArtistID:       artistID,
			CanonicalID:    entry.CanonicalID,
			Title:          title,
			AlbumType:      firstNonEmpty(entry.Type, "album"),
			SecondaryTypes: append([]string{}, entry.SecondaryTypes...),
			ReleaseDate:    parseDate(entry.ReleaseDate),
			Year:           year,
			TrackCount:     int32(trackCount),
			ExternalIds:    externalIDs,
			CoverUrl:       entry.CoverURL,
			AlbumID:        albumID,
			EditionKey:     EditionKey(title),
		}); err != nil {
			return fmt.Errorf("discography: insert %q: %w", title, err)
		}
	}
	return nil
}

// matchLocalAlbum links a provider release group to the library's album row.
// Precedence: shared external id (MBIDs and friends) → normalized title +
// year → normalized title alone.
func matchLocalAlbum(entry metadata.AlbumEntry, locals []sqlc.Album) (sqlc.Album, bool) {
	entryIDs := map[string]bool{}
	for _, value := range entry.ExternalIDs {
		if value = strings.TrimSpace(value); value != "" {
			entryIDs[strings.ToLower(value)] = true
		}
	}
	if len(entryIDs) > 0 {
		for _, local := range locals {
			if local.MusicbrainzID != "" && entryIDs[strings.ToLower(local.MusicbrainzID)] {
				return local, true
			}
			var localIDs map[string]string
			if len(local.ExternalIds) > 0 && json.Unmarshal(local.ExternalIds, &localIDs) == nil {
				for _, value := range localIDs {
					if value != "" && entryIDs[strings.ToLower(value)] {
						return local, true
					}
				}
			}
		}
	}

	wantTitle := normalizeTitle(entry.Title)
	wantYear := ""
	if entry.Year > 0 {
		wantYear = strconv.Itoa(entry.Year)
	}
	if wantYear != "" {
		for _, local := range locals {
			if normalizeTitle(local.Title) == wantTitle && local.Year == wantYear {
				return local, true
			}
		}
	}
	for _, local := range locals {
		if normalizeTitle(local.Title) == wantTitle {
			return local, true
		}
	}
	return sqlc.Album{}, false
}

// editionMarker matches a trailing parenthesized qualifier that names an
// EDITION of the same release — "(Deluxe)", "(Extended)", "(25th Anniversary
// Edition)", "(Plus)" — as opposed to parens that name a different release
// ("(live from Webster Hall)", "(commentary)"), which must stay distinct.
var editionMarker = regexp.MustCompile(`(?i)\(([^)]*(deluxe|extended|expanded|anniversary|edition|remaster|bonus|special|platinum|collector|definitive|reissue|complete|plus)[^)]*)\)\s*$`)

// EditionKey collapses edition variants of one release onto a shared key:
// "If I Can't Have Love, I Want Power (Deluxe)" and "(Extended)" group with
// the base album. Applied at sync time (stored per row) and to local-only
// albums at read time, so both sides agree.
func EditionKey(title string) string {
	t := strings.TrimSpace(title)
	for {
		next := strings.TrimSpace(editionMarker.ReplaceAllString(t, ""))
		if next == t || next == "" {
			break
		}
		t = next
	}
	return normalizeTitle(t)
}

func normalizeTitle(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseDate(value string) pgtype.Date {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Date{}
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}
	return pgtype.Date{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
