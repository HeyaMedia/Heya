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

// Sync reconciles an artist's stored discography with the provider's current
// view, linking entries to local albums where the library has the release.
// Rows are upserted on natural_key so ids stay stable across refreshes — the
// manager addresses catalog-only releases as d<row id> — and releases the
// provider dropped are removed in a trailing stale-delete pass. A nil/empty
// entries slice is a no-op rather than a wipe: providers occasionally return
// partial payloads on errors upstream, and an empty discography for an artist
// with local albums is always wrong. Run inside a transaction where possible
// so a mid-loop failure can't strand a half-reconciled catalog.
func Sync(ctx context.Context, q *sqlc.Queries, artistID int64, entries []metadata.AlbumEntry) error {
	if len(entries) == 0 {
		return nil
	}
	locals, err := q.ListAlbumsByArtist(ctx, artistID)
	if err != nil {
		return fmt.Errorf("discography: list local albums: %w", err)
	}

	// Dedup first: one row per release group — canonical id when resolved,
	// otherwise title+year keeps unresolved relation evidence from duplicating.
	seen := map[string]bool{}
	deduped := make([]metadata.AlbumEntry, 0, len(entries))
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			continue
		}
		key := entry.CanonicalID
		if key == "" {
			key = "t:" + normalizeTitle(title) + "|" + strconv.Itoa(entry.Year)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, entry)
		keys = append(keys, key)
	}

	albumIDs := matchLocalAlbums(deduped, locals)

	for i, entry := range deduped {
		title := strings.TrimSpace(entry.Title)
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
		if err := q.UpsertArtistDiscographyEntry(ctx, sqlc.UpsertArtistDiscographyEntryParams{
			ArtistID:       artistID,
			NaturalKey:     keys[i],
			CanonicalID:    entry.CanonicalID,
			Title:          title,
			AlbumType:      firstNonEmpty(entry.Type, "album"),
			SecondaryTypes: append([]string{}, entry.SecondaryTypes...),
			ReleaseDate:    parseDate(entry.ReleaseDate),
			Year:           year,
			TrackCount:     int32(trackCount),
			ExternalIds:    externalIDs,
			CoverUrl:       entry.CoverURL,
			AlbumID:        albumIDs[i],
			EditionKey:     EditionKey(title),
		}); err != nil {
			return fmt.Errorf("discography: upsert %q: %w", title, err)
		}
	}
	if err := q.DeleteStaleArtistDiscography(ctx, sqlc.DeleteStaleArtistDiscographyParams{
		ArtistID:    artistID,
		NaturalKeys: keys,
	}); err != nil {
		return fmt.Errorf("discography: prune stale: %w", err)
	}
	return nil
}

// matchLocalAlbums links provider release groups to the library's album rows,
// each local album claimed at most once — a single sharing its parent album's
// title must not attach to the same row and duplicate it in the merged view.
// External-id matches (exact) claim in a first pass so a looser title match
// earlier in the list can never steal a local from its true release group;
// the title passes then run over what's left. Per-entry precedence stays
// ext-id → normalized title + year → normalized title with compatible year.
func matchLocalAlbums(entries []metadata.AlbumEntry, locals []sqlc.Album) []pgtype.Int8 {
	albumIDs := make([]pgtype.Int8, len(entries))
	claimed := make(map[int64]bool, len(locals))
	claim := func(i int, local sqlc.Album) {
		albumIDs[i] = pgtype.Int8{Int64: local.ID, Valid: true}
		claimed[local.ID] = true
	}

	for i, entry := range entries {
		// MusicBrainz ids are UUIDs — collision-free across providers, so the
		// dedicated column matches against any entry value. The generic map
		// comparison pairs provider FAMILIES ("deezer" and "deezer:album" are
		// aliases; "mbid" is MusicBrainz): providers with short numeric ids
		// (tidal, deezer) can collide on bare values across families.
		entryIDs := map[string]map[string]bool{}
		anyEntryID := map[string]bool{}
		for provider, value := range entry.ExternalIDs {
			if value = strings.TrimSpace(value); value != "" {
				family := providerFamily(provider)
				if entryIDs[family] == nil {
					entryIDs[family] = map[string]bool{}
				}
				entryIDs[family][strings.ToLower(value)] = true
				anyEntryID[strings.ToLower(value)] = true
			}
		}
		if len(entryIDs) == 0 {
			continue
		}
		for _, local := range locals {
			if claimed[local.ID] {
				continue
			}
			if local.MusicbrainzID != "" && anyEntryID[strings.ToLower(local.MusicbrainzID)] {
				claim(i, local)
				break
			}
			var localIDs map[string]string
			if len(local.ExternalIds) > 0 && json.Unmarshal(local.ExternalIds, &localIDs) == nil {
				matched := false
				for provider, value := range localIDs {
					if value == "" {
						continue
					}
					if entryIDs[providerFamily(provider)][strings.ToLower(strings.TrimSpace(value))] {
						matched = true
						break
					}
				}
				if matched {
					claim(i, local)
					break
				}
			}
		}
	}

	for i, entry := range entries {
		if albumIDs[i].Valid {
			continue
		}
		wantTitle := normalizeTitle(entry.Title)
		wantYear := ""
		if entry.Year > 0 {
			wantYear = strconv.Itoa(entry.Year)
		}
		if wantYear != "" {
			for _, local := range locals {
				if !claimed[local.ID] && normalizeTitle(local.Title) == wantTitle && local.Year == wantYear {
					claim(i, local)
					break
				}
			}
		}
		if albumIDs[i].Valid {
			continue
		}
		for _, local := range locals {
			if !claimed[local.ID] && normalizeTitle(local.Title) == wantTitle && yearsCompatible(entry.Year, local.Year) {
				claim(i, local)
				break
			}
		}
	}
	return albumIDs
}

// providerFamily normalizes an external-id key to its provider family:
// "deezer:album" and "deezer" are the same namespace, "musicbrainz:release_group",
// "musicbrainz_album", and "mbid" are all MusicBrainz. Matching within a family
// keeps alias drift working while never comparing, say, a Tidal id against a
// Deezer id that happens to share the digits.
func providerFamily(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if i := strings.IndexAny(p, ":_"); i > 0 {
		p = p[:i]
	}
	if p == "mbid" || p == "mb" {
		return "musicbrainz"
	}
	return p
}

// yearsCompatible gates the title-only fallback: release-group vs rip years
// drift by a year routinely (regional releases, remaster tags), but a decade
// apart means a reissue or an unrelated same-titled release — don't link.
func yearsCompatible(entryYear int, localYear string) bool {
	if entryYear == 0 || localYear == "" {
		return true
	}
	local, err := strconv.Atoi(localYear)
	if err != nil {
		return true
	}
	diff := entryYear - local
	return diff >= -1 && diff <= 1
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
