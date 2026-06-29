package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/karbowiak/heya/internal/titlematch"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// matchMusicLibrary processes all pending files in a music library by grouping
// them by release directory, then running a single artist+album+tracks upsert
// per group. This bypasses heya.media entirely — sources of truth are
// album.nfo, artist.nfo, and the curated path parser (in that order).
func (m *Matcher) matchMusicLibrary(ctx context.Context, libraryID int64) (MatchResult, error) {
	var result MatchResult

	files, err := m.q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     100000,
		Offset:    0,
	})
	if err != nil {
		return result, fmt.Errorf("listing pending music files: %w", err)
	}

	groups := groupMusicFilesByReleaseDir(files)
	cache := newEnrichCache()
	touchedArtists := make(map[int64]struct{}, 8)

	for _, group := range groups {
		matched, unmatched, errored, artistID := m.matchMusicGroup(ctx, libraryID, group, cache)
		result.Matched += matched
		result.Unmatched += unmatched
		result.Errors += errored
		if artistID > 0 {
			touchedArtists[artistID] = struct{}{}
		}
	}

	if len(touchedArtists) > 0 {
		result.MusicArtistIDs = make([]int64, 0, len(touchedArtists))
		for id := range touchedArtists {
			result.MusicArtistIDs = append(result.MusicArtistIDs, id)
		}
	}

	return result, nil
}

// enrichCache holds heya.media artist responses for the duration of a single
// matchMusicLibrary call. Keyed by MBID when known (cheapest lookup), falling
// back to lower-cased name. Stores both successful and failed lookups (nil
// value) so we never re-query the same artist twice within one scan.
type enrichCache struct {
	byKey map[string]*metadata.MediaDetail
	known map[string]bool
}

func newEnrichCache() *enrichCache {
	return &enrichCache{
		byKey: make(map[string]*metadata.MediaDetail),
		known: make(map[string]bool),
	}
}

//nolint:unused // reserved for upcoming cached-enrich call sites
func enrichCacheKey(mbid, name string) string {
	if mbid != "" {
		return "mbid:" + mbid
	}
	if name != "" {
		return "name:" + strings.ToLower(name)
	}
	return ""
}

// matchMusicSingleFile processes only the release group containing the given file.
// Used by re-resolve / single-file rescans.
func (m *Matcher) matchMusicSingleFile(ctx context.Context, file sqlc.LibraryFile, libraryID int64) (MatchInfo, error) {
	releaseDir := releaseDirOf(file.Path)
	siblings, err := m.q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusPending,
		Limit:     1000,
		Offset:    0,
	})
	if err != nil {
		return MatchInfo{}, fmt.Errorf("listing siblings: %w", err)
	}
	var group []sqlc.LibraryFile
	group = append(group, file)
	for _, s := range siblings {
		if s.ID == file.ID {
			continue
		}
		if releaseDirOf(s.Path) == releaseDir {
			group = append(group, s)
		}
	}
	matched, _, _, artistID := m.matchMusicGroup(ctx, libraryID, group, newEnrichCache())
	return MatchInfo{IsNew: matched > 0, ArtistID: artistID}, nil
}

var discSubdirRE = regexp.MustCompile(`(?i)^(?:disc|cd)\s*(\d+)$`)

// releaseDirOf returns the release directory for a track file, jumping over
// per-disc subfolders (e.g. ".../Album/Disc 1/track.flac" → ".../Album").
func releaseDirOf(filePath string) string {
	parent := filepath.Dir(filePath)
	if discSubdirRE.MatchString(filepath.Base(parent)) {
		return filepath.Dir(parent)
	}
	return parent
}

// discNumFromPath returns the disc number encoded in a "Disc N" / "CD N" parent
// directory, or 0 if the file isn't inside one.
func discNumFromPath(filePath string) int {
	if m := discSubdirRE.FindStringSubmatch(filepath.Base(filepath.Dir(filePath))); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func groupMusicFilesByReleaseDir(files []sqlc.LibraryFile) [][]sqlc.LibraryFile {
	byDir := make(map[string][]sqlc.LibraryFile)
	order := []string{}
	for _, f := range files {
		dir := releaseDirOf(f.Path)
		if _, seen := byDir[dir]; !seen {
			order = append(order, dir)
		}
		byDir[dir] = append(byDir[dir], f)
	}
	groups := make([][]sqlc.LibraryFile, 0, len(order))
	for _, dir := range order {
		groups = append(groups, byDir[dir])
	}
	return groups
}

func (m *Matcher) matchMusicGroup(ctx context.Context, libraryID int64, files []sqlc.LibraryFile, cache *enrichCache) (matched, unmatched, errored int, artistID int64) {
	if len(files) == 0 {
		return 0, 0, 0, 0
	}

	var tracks, lyrics []sqlc.LibraryFile
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Path))
		switch ext {
		case ".flac", ".m4a", ".mp3", ".aac", ".wav", ".ogg", ".opus":
			tracks = append(tracks, f)
		case ".lrc":
			lyrics = append(lyrics, f)
		}
	}

	releaseDir := releaseDirOf(files[0].Path)
	artistDir := filepath.Dir(releaseDir)

	if len(tracks) == 0 {
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusUnmatched, "empty release directory (no audio tracks)", 0)
			unmatched++
		}
		log.Debug().Str("dir", releaseDir).Int("files", len(files)).Msg("skipping music release group with no tracks")
		return 0, unmatched, 0, 0
	}

	albumNFO := nfo.FindAndParseInDir(releaseDir)
	if albumNFO != nil && albumNFO.Kind != "album" {
		albumNFO = nil
	}
	artistNFO := nfo.FindAndParseInDir(artistDir)
	if artistNFO != nil && artistNFO.Kind != "artist" {
		artistNFO = nil
	}

	leadParsed, _ := parseFileResult(tracks[0].ParseResult)

	artistName := ""
	artistDisambig := ""
	artistMBID := ""
	artistBio := ""
	artistSortName := ""

	if artistNFO != nil {
		artistName = artistNFO.Title
		artistDisambig = artistNFO.Disambiguation
		artistMBID = artistNFO.MBID
		artistBio = artistNFO.Plot
		artistSortName = artistNFO.SortName
	}
	if leadParsed.Release != nil {
		if artistName == "" {
			artistName = leadParsed.Release.Artist
		}
		if artistDisambig == "" {
			artistDisambig = leadParsed.Release.ArtistDisambiguation
		}
	}
	if artistMBID == "" && albumNFO != nil {
		artistMBID = albumNFO.MBAlbumArtistID
	}

	if artistName == "" {
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusUnmatched, "no artist name from path or NFO", 0)
			unmatched++
		}
		return 0, unmatched, 0, 0
	}

	// Inline heya.media enrichment was removed in Phase 7: matchMusicGroup now
	// builds skeleton rows from NFO + path only (fast, deterministic, no
	// network). EnrichMediaItemWorker picks up the slack asynchronously
	// after the scan, populating bio / disambiguation / external IDs / album
	// metadata / track titles + durations from the heya.media response.
	_ = cache // retained for future per-scan caches; unused after decoupling

	artist, err := m.upsertMusicArtist(ctx, libraryID, artistName, artistDisambig, artistMBID, artistBio, artistSortName)
	if err != nil {
		log.Error().Err(err).Str("artist", artistName).Msg("upsert artist failed")
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusError, err.Error(), 0)
			errored++
		}
		return 0, 0, errored, 0
	}
	artistID = artist.ID

	albumTitle := ""
	albumYear := ""
	albumMBID := ""
	albumType := ""
	albumGenres := []string{}
	albumLabel := ""
	albumCountry := ""
	albumBarcode := ""
	albumReleaseDate := ""
	albumTags := []string{}
	totalDiscs := 1

	if albumNFO != nil {
		albumTitle = albumNFO.Title
		albumYear = albumNFO.Year
		albumMBID = albumNFO.MBAlbumID
		albumType = albumNFO.AlbumType
		albumGenres = albumNFO.Genres
		albumLabel = albumNFO.Label
		albumCountry = albumNFO.Country
		albumBarcode = albumNFO.Barcode
		albumReleaseDate = albumNFO.ReleaseDate
		albumTags = albumNFO.Tags
		for _, t := range albumNFO.Tracks {
			if t.Disc > totalDiscs {
				totalDiscs = t.Disc
			}
		}
	}
	if leadParsed.Release != nil {
		if albumTitle == "" {
			albumTitle = leadParsed.Release.Album
		}
		if albumYear == "" {
			albumYear = leadParsed.Release.Year
		}
		if albumType == "" {
			albumType = leadParsed.Release.ReleaseKind
		}
	}
	if albumType == "" {
		albumType = "album"
	}
	if albumTitle == "" {
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusUnmatched, "no album title from path or NFO", 0)
			unmatched++
		}
		return 0, unmatched, 0, 0
	}

	// embedded album / track enrichment also moved to EnrichMediaItemWorker.
	album, err := m.upsertMusicAlbum(ctx, artist.ID, musicAlbumInput{
		Title:       albumTitle,
		Year:        albumYear,
		MBID:        albumMBID,
		AlbumType:   albumType,
		Genres:      albumGenres,
		Label:       albumLabel,
		Country:     albumCountry,
		Barcode:     albumBarcode,
		ReleaseDate: albumReleaseDate,
		Tags:        albumTags,
		TotalTracks: len(tracks),
		TotalDiscs:  totalDiscs,
	})
	if err != nil {
		log.Error().Err(err).Str("album", albumTitle).Msg("upsert album failed")
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusError, err.Error(), 0)
			errored++
		}
		return 0, 0, errored, 0
	}

	for _, trackFile := range tracks {
		trackParsed, _ := parseFileResult(trackFile.ParseResult)

		discNum := 1
		trackNum := 0
		trackTitle := ""
		hasTrackInfo := false
		if trackParsed.Release != nil && trackParsed.Release.HasTrackInfo {
			discNum = trackParsed.Release.DiscNumber
			if discNum == 0 {
				discNum = 1
			}
			trackNum = trackParsed.Release.TrackNumber
			trackTitle = trackParsed.Release.TrackTitle
			hasTrackInfo = true
		}
		if d := discNumFromPath(trackFile.Path); d > 0 {
			discNum = d
		}

		if albumNFO != nil {
			for _, nfoT := range albumNFO.Tracks {
				nfoDisc := nfoT.Disc
				if nfoDisc == 0 {
					nfoDisc = 1
				}
				if nfoDisc == discNum && nfoT.Position == trackNum {
					if nfoT.Title != "" {
						trackTitle = nfoT.Title
					}
					break
				}
			}
		}

		duration := 0
		// (duration / canonical title from heya.media populated later by
		// EnrichMediaItemWorker via UpdateTrackFromEnrichment)

		if !hasTrackInfo && trackTitle == "" {
			base := filepath.Base(trackFile.Path)
			trackTitle = strings.TrimSuffix(base, filepath.Ext(base))
		}

		lyricsPath := findLyricsForTrack(trackFile, lyrics)

		track, err := m.q.GetOrCreateTrack(ctx, sqlc.GetOrCreateTrackParams{
			AlbumID:     album.ID,
			DiscNumber:  int32(discNum),
			TrackNumber: int32(trackNum),
			Title:       trackTitle,
			Duration:    int32(duration),
		})
		if err != nil {
			log.Warn().Err(err).Str("track", trackFile.Path).Msg("get-or-create track failed")
			m.markFile(ctx, trackFile.ID, sqlc.FileStatusError, err.Error(), 0)
			errored++
			continue
		}

		// If we now have richer data than what's on the existing track row,
		// overwrite. Cheap when fields are already correct.
		if (trackTitle != "" && trackTitle != track.Title) || (duration > 0 && int32(duration) != track.Duration) {
			newTitle := track.Title
			if trackTitle != "" {
				newTitle = trackTitle
			}
			newDuration := track.Duration
			if duration > 0 {
				newDuration = int32(duration)
			}
			if updated, updErr := m.q.UpdateTrackTitleAndDuration(ctx, sqlc.UpdateTrackTitleAndDurationParams{
				ID:       track.ID,
				Title:    newTitle,
				Duration: newDuration,
			}); updErr == nil {
				track = updated
			}
		}

		format := strings.ToLower(strings.TrimPrefix(filepath.Ext(trackFile.Path), "."))
		score := mediaprobe.ExtensionQualityBase(format)
		probe := parseAudioFromMediaInfo(trackFile.MediaInfo)
		if probe != nil {
			// Probe data was already written by FFProbeWorker — adopt it so
			// the matcher doesn't leave the row at extension-only quality.
			score = mediaprobe.RefinedQualityScore(format, probe.BitrateKbps, probe.BitDepth, probe.SampleRateHz)
		}

		insertedTrackFile, err := m.q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
			TrackID:       track.ID,
			LibraryFileID: trackFile.ID,
			Format:        format,
			QualityScore:  int32(score),
			LyricsPath:    lyricsPath,
			SizeBytes:     trackFile.Size,
		})
		if err != nil {
			log.Warn().Err(err).Str("file", vfs.RedactPath(trackFile.Path)).Msg("upsert track_file failed")
			m.markFile(ctx, trackFile.ID, sqlc.FileStatusError, err.Error(), 0)
			errored++
			continue
		}
		if probe != nil {
			if err := m.q.UpdateTrackFileProbeData(ctx, sqlc.UpdateTrackFileProbeDataParams{
				ID:           insertedTrackFile.ID,
				BitrateKbps:  int32(probe.BitrateKbps),
				SampleRateHz: int32(probe.SampleRateHz),
				BitDepth:     int32(probe.BitDepth),
				Channels:     int32(probe.Channels),
				Duration:     probe.Duration,
				QualityScore: int32(score),
			}); err != nil {
				log.Warn().Err(err).Int64("track_file_id", insertedTrackFile.ID).Msg("seed track_file probe data failed")
			}
		}

		if err := m.refreshTrackPrimary(ctx, track.ID); err != nil {
			log.Warn().Err(err).Int64("track_id", track.ID).Msg("refresh primary file failed")
		}

		m.markFile(ctx, trackFile.ID, sqlc.FileStatusMatched, "", artist.MediaItemID)
		matched++
	}

	for _, lf := range lyrics {
		m.markFile(ctx, lf.ID, sqlc.FileStatusMatched, "", artist.MediaItemID)
		matched++
	}

	log.Info().
		Str("artist", artistName).
		Str("album", albumTitle).
		Str("year", albumYear).
		Int("tracks", len(tracks)).
		Int("lyrics", len(lyrics)).
		Bool("from_nfo", albumNFO != nil).
		Msg("matched music release")

	// Trailing-edge debounce. The match worker's IsNew=true path enqueues
	// an immediate enrich for newly-created artists; for files landing
	// under an artist whose initial enrich already finished, the enrich
	// worker's idempotency gate would otherwise skip the re-fetch — so we
	// upsert a debounce row instead, and the periodic sweeper fires a
	// forced enrich once the library has been quiet for the debounce
	// window.
	if matched > 0 && artist.MediaItemID > 0 {
		m.maybeDebounceEnrich(ctx, artist.MediaItemID, "matcher.music")
	}

	return matched, unmatched, errored, artistID
}

func (m *Matcher) upsertMusicArtist(ctx context.Context, libraryID int64, name, disambig, mbid, biography, sortName string) (sqlc.Artist, error) {
	if mbid != "" {
		if existing, err := m.q.GetArtistByMusicBrainzID(ctx, mbid); err == nil {
			return existing, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("mbid", mbid).Msg("artist MBID lookup error")
		}
	}

	if name != "" {
		existing, err := m.q.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{
			Lower:   name,
			Lower_2: disambig,
		})
		if err == nil {
			if mbid != "" && existing.MusicbrainzID == "" {
				updated, updErr := m.q.UpdateArtist(ctx, sqlc.UpdateArtistParams{
					ID:             existing.ID,
					MusicbrainzID:  mbid,
					Name:           existing.Name,
					SortName:       existing.SortName,
					Disambiguation: existing.Disambiguation,
					Biography:      existing.Biography,
				})
				if updErr == nil {
					return updated, nil
				}
			}
			return existing, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("name", name).Msg("artist name lookup error")
		}
	}

	extIDs := map[string]string{}
	if mbid != "" {
		extIDs["musicbrainz_artist"] = mbid
		extIDs["mbid"] = mbid
	}
	extJSON, _ := json.Marshal(extIDs)

	item, err := m.q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   libraryID,
		MediaType:   sqlc.MediaTypeMusic,
		Title:       name,
		SortTitle:   strings.ToLower(name),
		ExternalIds: extJSON,
	})
	if err != nil {
		// Race recovery: a concurrent worker may have created the same artist
		// (same MBID, same library_id → trips idx_media_items_mbid_unique).
		// Re-query and use the winner.
		if mbid != "" {
			if existing, lookErr := m.q.GetArtistByMusicBrainzID(ctx, mbid); lookErr == nil {
				return existing, nil
			}
		}
		if existing, lookErr := m.q.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{
			Lower:   name,
			Lower_2: disambig,
		}); lookErr == nil {
			return existing, nil
		}
		return sqlc.Artist{}, fmt.Errorf("creating media item: %w", err)
	}

	itemSlug := slug.GenerateUnique(ctx, name, "", item.ID,
		func(ctx context.Context, s string, excludeID int64) (bool, error) {
			r, err := m.q.MediaItemSlugExists(ctx, sqlc.MediaItemSlugExistsParams{Slug: s, ID: excludeID})
			return r, err
		})
	if err := m.q.UpdateMediaItemSlug(ctx, sqlc.UpdateMediaItemSlugParams{ID: item.ID, Slug: itemSlug}); err != nil {
		log.Warn().Err(err).Int64("media_item", item.ID).Msg("failed to set media item slug")
	}

	_ = m.q.MarkMatched(ctx, item.ID)

	if sortName == "" {
		sortName = name
	}

	artist, err := m.q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:    item.ID,
		MusicbrainzID:  mbid,
		Name:           name,
		SortName:       sortName,
		Disambiguation: disambig,
		Biography:      biography,
	})
	if err != nil {
		if existing, lookErr := m.q.GetArtistByMediaItemID(ctx, item.ID); lookErr == nil {
			return existing, nil
		}
		return sqlc.Artist{}, fmt.Errorf("creating artist: %w", err)
	}
	return artist, nil
}

// cachedEnrichArtist returns the cached heya.media result for an artist or
// fetches and caches it. Returning nil signals "tried and got nothing" so we
// don't re-query in the same scan.
//
//nolint:unused // reserved for upcoming cached-enrich call sites
func (m *Matcher) cachedEnrichArtist(ctx context.Context, cache *enrichCache, mbid, name string) *metadata.MediaDetail {
	keys := []string{}
	if mbid != "" {
		keys = append(keys, "mbid:"+mbid)
	}
	if name != "" {
		keys = append(keys, "name:"+strings.ToLower(name))
	}
	for _, k := range keys {
		if cache.known[k] {
			return cache.byKey[k]
		}
	}

	detail := m.enrichArtistFromHeyaMedia(ctx, mbid, name)
	for _, k := range keys {
		cache.known[k] = true
		cache.byKey[k] = detail
	}
	// Cross-link: if the enriched response includes the MBID and our input was
	// only a name (or vice-versa), cache under the other key too so a later
	// release that knows the MBID hits the cache.
	if detail != nil && detail.ExternalIDs["mbid"] != "" {
		k := "mbid:" + detail.ExternalIDs["mbid"]
		cache.known[k] = true
		cache.byKey[k] = detail
	}
	if detail != nil && detail.ArtistName != "" {
		k := "name:" + strings.ToLower(detail.ArtistName)
		cache.known[k] = true
		cache.byKey[k] = detail
	}
	return detail
}

// findEmbeddedAlbum matches a local release against an artist's embedded album
// list (payload.albums). MBID match wins; else lower(title) + year; else title
// only. Returns nil if no plausible match.
func findEmbeddedAlbum(enriched *metadata.MediaDetail, title, year, mbid string) *metadata.AlbumEntry {
	if enriched == nil || len(enriched.Albums) == 0 {
		return nil
	}
	if mbid != "" {
		// MusicBrainz tags can carry either the release MBID (specific
		// pressing) or the release-group MBID (the abstract work).
		// Compare against all three keys — release, release-group, and
		// the legacy "mbid" alias — so a Vorbis tag of either flavor
		// finds the upstream record.
		for i := range enriched.Albums {
			a := &enriched.Albums[i]
			if a.ExternalIDs["mb_release"] == mbid ||
				a.ExternalIDs["mb_release_group"] == mbid ||
				a.ExternalIDs["mbid"] == mbid {
				return a
			}
		}
	}
	if title == "" {
		return nil
	}

	// Strict pass: exact case-fold equality, optionally pinned to year.
	// Year only contributes when both sides have one — upstream sometimes
	// omits year on singles, and we don't want a year-tied match to drop
	// the right album.
	normTitle := strings.ToLower(title)
	yearInt, _ := strconv.Atoi(year)
	for i := range enriched.Albums {
		a := &enriched.Albums[i]
		if strings.ToLower(a.Title) == normTitle && yearInt > 0 && a.Year == yearInt {
			return a
		}
	}
	for i := range enriched.Albums {
		a := &enriched.Albums[i]
		if strings.ToLower(a.Title) == normTitle {
			return a
		}
	}

	// Fuzzy pass: same FuzzyEqual the top-tracks rail uses. Catches
	// parenthetical drift ("Title" vs "Title (Special Edition)"),
	// quote-mark variants ("Stay Gold (From 'BEYBLADE X')" vs
	// "Stay Gold (from BEYBLADE X)"), and kana/romaji asymmetries.
	// Scoped to one artist's catalog at the call site so substring
	// fallbacks don't bleed across artists.
	for i := range enriched.Albums {
		a := &enriched.Albums[i]
		if titlematch.FuzzyEqual(a.Title, title) {
			return a
		}
	}
	return nil
}

// resolveAlbumType collapses an upstream (primary_type, secondary_types)
// pair into a single album_type string. MusicBrainz emits primary as
// "Album" plus secondaries like ["Compilation"] / ["Soundtrack"] /
// ["Remix"] / ["Live"] on the same release group — the secondary is the
// useful one for shelf grouping. When secondaries are absent the
// primary stands. Empty result means "don't overwrite the existing DB
// value" (the caller's CASE WHEN logic preserves it).
func resolveAlbumType(primary string, secondaries []string) string {
	for _, s := range secondaries {
		switch strings.ToLower(s) {
		case "compilation":
			return "compilation"
		case "soundtrack":
			return "soundtrack"
		case "remix":
			return "remix"
		case "live":
			return "live"
		case "demo":
			return "demo"
		case "audio drama", "audiobook", "spokenword":
			return "other"
		}
	}
	return strings.ToLower(primary)
}

// findEmbeddedTrack picks the embedded track matching disc+position. Returns
// nil when no exact match (we don't fuzz-match tracks — only exact disc+pos
// is safe; otherwise we'd risk overriding a real track title with a wrong one).
func findEmbeddedTrack(album *metadata.AlbumEntry, disc, position int) *metadata.TrackDetail {
	if album == nil {
		return nil
	}
	for i := range album.Tracks {
		t := &album.Tracks[i]
		td := t.DiscNumber
		if td == 0 {
			td = 1
		}
		if td == disc && t.TrackNumber == position {
			return t
		}
	}
	return nil
}

// enrichArtistFromHeyaMedia consults heya.media for canonical artist metadata.
// Strategy: prefer MBID-direct lookup (fast, exact); fall back to name search
// (slower, fuzzier). Returns nil on any error — enrichment is best-effort.
func (m *Matcher) enrichArtistFromHeyaMedia(ctx context.Context, mbid, name string) *metadata.MediaDetail {
	if m.heya == nil {
		return nil
	}

	if mbid != "" {
		detail, _, err := m.heya.LookupByNFO(ctx, metadata.KindMusic, metadata.NFOIDs{MBID: mbid}, nil)
		if err == nil && detail != nil {
			log.Info().Str("mbid", mbid).Str("artist", detail.ArtistName).Msg("enriched artist via heya.media MBID lookup")
			return detail
		}
		log.Debug().Err(err).Str("mbid", mbid).Msg("heya.media MBID lookup failed; trying name search")
	}

	if name == "" {
		return nil
	}
	// Two-step (heya.media v0.3.0): cheap /search for disambiguation, then
	// /api/v1/artist/{id} for the full enriched doc. /search sorts
	// already-enriched hits first, so the warm-cache case stays one round
	// trip end-to-end.
	hit, err := m.heya.SearchArtistBest(ctx, name)
	if err != nil || hit == nil {
		if err != nil {
			log.Debug().Err(err).Str("name", name).Msg("heya.media artist search failed")
		}
		return nil
	}
	detail, _, err := m.heya.FetchByKindID(ctx, "artist", hit.ID)
	if err != nil || detail == nil {
		// heya.media has the artist in its search index (with an image
		// URL) but no full enriched record yet — common for hits keyed
		// on discogs/deezer when warm enrichment hasn't run. Synthesise
		// a minimal MediaDetail so the caller can at least download the
		// search-hit image; without this we'd throw away a perfectly
		// good poster URL and the artist would stay imageless until
		// heya.media's backend enriches it (which may never happen).
		if hit.Image != "" {
			log.Info().
				Str("name", name).
				Str("hit_id", hit.ID).
				Err(err).
				Msg("heya.media artist fetch failed; using search-hit image")
			return &metadata.MediaDetail{
				Title:       hit.Name,
				ArtistName:  hit.Name,
				PosterURL:   hit.Image,
				HeyaSlug:    hit.Slug,
				ExternalIDs: map[string]string{},
			}
		}
		log.Debug().Err(err).Str("name", name).Str("hit_id", hit.ID).Msg("heya.media artist fetch failed")
		return nil
	}
	// Detail came back but heya.media's payload may not include any
	// artwork — fall back to the search-hit image so at least the poster
	// is populated even when the warm enrichment payload is image-less.
	if detail.PosterURL == "" && hit.Image != "" {
		detail.PosterURL = hit.Image
	}
	log.Info().
		Str("name", name).
		Str("hit_id", hit.ID).
		Bool("enriched_hit", hit.Enriched).
		Float64("score", hit.Score).
		Str("artist", detail.ArtistName).
		Msg("enriched artist via heya.media discover+fetch")
	return detail
}

type musicAlbumInput struct {
	Title       string
	Year        string
	MBID        string
	AlbumType   string
	Genres      []string
	Label       string
	Country     string
	Barcode     string
	ReleaseDate string
	Tags        []string
	TotalTracks int
	TotalDiscs  int
}

func (m *Matcher) upsertMusicAlbum(ctx context.Context, artistID int64, in musicAlbumInput) (sqlc.Album, error) {
	if in.MBID != "" {
		if existing, err := m.q.GetAlbumByMusicBrainzID(ctx, in.MBID); err == nil {
			return existing, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("mbid", in.MBID).Msg("album MBID lookup error")
		}
	}

	existing, err := m.q.GetAlbumByArtistTitleYear(ctx, sqlc.GetAlbumByArtistTitleYearParams{
		ArtistID: artistID,
		Lower:    in.Title,
		Year:     in.Year,
	})
	if err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Warn().Err(err).Str("title", in.Title).Msg("album lookup error")
	}

	album, err := m.q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID:      artistID,
		Title:         in.Title,
		Year:          in.Year,
		MusicbrainzID: in.MBID,
		AlbumType:     in.AlbumType,
		Genres:        emptyIfNil(in.Genres),
		ReleaseDate:   pgDateFromString(in.ReleaseDate),
		Label:         in.Label,
		Country:       in.Country,
		Barcode:       in.Barcode,
		TotalTracks:   int32(in.TotalTracks),
		TotalDiscs:    int32(in.TotalDiscs),
		Tags:          emptyIfNil(in.Tags),
	})
	if err != nil {
		return sqlc.Album{}, fmt.Errorf("creating album: %w", err)
	}
	album.Slug = m.assignAlbumSlug(ctx, artistID, album.ID, in.Title, in.Year)
	return album, nil
}

// assignAlbumSlug picks a unique-within-artist slug and writes it back.
// Logged-only on failure — a missing slug just means the album won't appear
// in /music/{artist}/{album} URLs until the next refresh.
func (m *Matcher) assignAlbumSlug(ctx context.Context, artistID, albumID int64, title, year string) string {
	s := slug.GenerateUnique(ctx, title, year, albumID, func(ctx context.Context, candidate string, excludeID int64) (bool, error) {
		return m.q.AlbumSlugExists(ctx, sqlc.AlbumSlugExistsParams{
			ArtistID: artistID,
			Slug:     candidate,
			ID:       excludeID,
		})
	})
	if err := m.q.SetAlbumSlug(ctx, sqlc.SetAlbumSlugParams{ID: albumID, Slug: s}); err != nil {
		log.Warn().Err(err).Int64("album_id", albumID).Str("slug", s).Msg("failed to set album slug")
		return ""
	}
	return s
}

func (m *Matcher) markFile(ctx context.Context, fileID int64, status sqlc.FileStatus, errMsg string, mediaItemID int64) {
	params := sqlc.UpdateLibraryFileStatusParams{
		ID:           fileID,
		Status:       status,
		ErrorMessage: errMsg,
	}
	if mediaItemID != 0 {
		params.MediaItemID = pgInt8(mediaItemID)
	}
	if err := m.q.UpdateLibraryFileStatus(ctx, params); err != nil {
		log.Warn().Err(err).Int64("file_id", fileID).Str("status", string(status)).Msg("failed to update library file status")
	}
}

// parseAudioFromMediaInfo reads the JSON FFProbeWorker stored in
// library_files.media_info and pulls out audio properties. Returns nil if the
// row hasn't been probed yet or has no audio stream.
func parseAudioFromMediaInfo(raw []byte) *mediaprobe.AudioFields {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	info, err := mediaprobe.Parse(raw)
	if err != nil {
		return nil
	}
	audio := mediaprobe.PrimaryAudio(info)
	if audio == nil {
		return nil
	}
	out := mediaprobe.AudioFieldsFrom(info, audio)
	if out.BitrateKbps == 0 && out.SampleRateHz == 0 && out.Duration == 0 {
		return nil
	}
	return &out
}

// refreshTrackPrimary picks the highest-quality non-deleted file backing the
// track and denormalizes its path / library_file_id / lyrics onto the track
// row, so playback URLs don't need an extra join.
func (m *Matcher) refreshTrackPrimary(ctx context.Context, trackID int64) error {
	files, err := m.q.ListTrackFilesByTrack(ctx, trackID)
	if err != nil {
		return fmt.Errorf("list track files: %w", err)
	}
	if len(files) == 0 {
		return nil
	}
	primary := files[0]
	lf, err := m.q.GetLibraryFileByID(ctx, primary.LibraryFileID)
	if err != nil {
		return fmt.Errorf("get primary library file: %w", err)
	}
	return m.q.UpdateTrackPrimary(ctx, sqlc.UpdateTrackPrimaryParams{
		ID:            trackID,
		FilePath:      lf.Path,
		LibraryFileID: pgInt8(lf.ID),
		LyricsPath:    primary.LyricsPath,
	})
}

func findLyricsForTrack(track sqlc.LibraryFile, lyrics []sqlc.LibraryFile) string {
	trackBase := strings.TrimSuffix(filepath.Base(track.Path), filepath.Ext(track.Path))
	trackDir := filepath.Dir(track.Path)
	for _, lyr := range lyrics {
		if filepath.Dir(lyr.Path) != trackDir {
			continue
		}
		lyrBase := strings.TrimSuffix(filepath.Base(lyr.Path), filepath.Ext(lyr.Path))
		if lyrBase == trackBase {
			return lyr.Path
		}
	}
	return ""
}
