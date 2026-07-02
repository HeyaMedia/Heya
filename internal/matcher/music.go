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
	"time"
	"unicode"

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

	// Embedded-tag view of the release, read from the lead track. Probing on
	// demand here (when the async FFProbeWorker hasn't run yet) is what lets a
	// scene-garbage folder still resolve: its path yields no artist/album, but
	// its ID3/Vorbis tags do. leadTags is empty — and every fusion below falls
	// back to path/NFO exactly as before — when there's no prober or no tags.
	// The result is cached so the per-track loop reuses it instead of re-probing.
	probeCache := map[int64]*mediaprobe.MediaInfo{}
	leadInfo := m.musicFileProbe(ctx, tracks[0])
	probeCache[tracks[0].ID] = leadInfo
	leadTags := extractMusicTags(collectAudioTags(leadInfo))
	leadRawSegment := filepath.Base(releaseDir)
	if leadParsed.Release != nil && leadParsed.Release.RawName != "" {
		leadRawSegment = leadParsed.Release.RawName
	} else if leadParsed.ReleaseSegment != "" {
		leadRawSegment = leadParsed.ReleaseSegment
	}
	pTrust := pathTrust(leadRawSegment)

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
	// artistExplicit means the name came from a deliberate source (NFO or the
	// on-disk folder), not only from embedded tags. A human who foldered
	// "Various Artists" meant it — that's a legitimate shared bucket — whereas a
	// ripper's "Unknown Artist" tag is junk. Only the latter is rejected below.
	artistExplicit := artistNFO != nil
	if artistName == "" {
		pathArtist := ""
		if leadParsed.Release != nil {
			pathArtist = leadParsed.Release.Artist
		}
		// Prefer the embedded ALBUMARTIST over the per-track ARTIST so a
		// featured guest on one track can't hijack the album's artist.
		tagArtist := leadTags.AlbumArtist
		if tagArtist == "" {
			tagArtist = leadTags.Artist
		}
		fused := fuseText(pathArtist, tagArtist, pTrust, tagTrust(tagArtist))
		artistName = fused.Value
		if fused.Source == sourcePath || fused.Source == sourceBoth {
			artistExplicit = true
		}
		if fused.Source == sourceTag || fused.Source == sourceBoth {
			log.Debug().Str("artist", artistName).Str("source", fused.Source.String()).Float64("confidence", fused.Confidence).Msg("music artist resolved via tag fusion")
		}
	}
	if artistDisambig == "" && leadParsed.Release != nil {
		artistDisambig = leadParsed.Release.ArtistDisambiguation
	}
	if artistMBID == "" && albumNFO != nil {
		artistMBID = albumNFO.MBAlbumArtistID
	}
	if artistMBID == "" {
		// A tag MBID is only safe to stamp when the tag NAME it belongs to
		// matches the artist name we actually resolved. When the path (or NFO)
		// won a disagreement, the tag names a DIFFERENT act, and adopting its
		// MBID would stamp our artist with another artist's id and fuse them
		// globally via GetArtistByMusicBrainzID (artists has no library_id).
		// Prefer the album-artist MBID (belongs to ALBUMARTIST); fall back to the
		// track ARTIST MBID (belongs to ARTIST) — each gated on its own name.
		tagArtistMBID := ""
		switch {
		case leadTags.AlbumArtist != "" && titlematch.FuzzyEqual(leadTags.AlbumArtist, artistName):
			tagArtistMBID = leadTags.AlbumArtistMBID
		case leadTags.Artist != "" && titlematch.FuzzyEqual(leadTags.Artist, artistName):
			tagArtistMBID = leadTags.ArtistMBID
		}
		artistMBID = fuseMBID("", tagArtistMBID).Value
	}

	// The artists table has no library_id: its uniqueness is global. A
	// placeholder name that came ONLY from tags ("Unknown Artist" left by a
	// ripper) would fuse untagged releases across every library into one poison
	// row, so refuse it — the group stays retryable-unmatched. A placeholder
	// that a human deliberately foldered (artistExplicit) is kept, preserving
	// the pre-fusion behaviour and legitimate "Various Artists" compilations.
	if artistName == "" || (!artistExplicit && !isUsableArtist(artistName)) {
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusUnmatched, "no reliable artist name from path, tags, or NFO", 0)
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
	pathAlbum, pathAlbumYear := "", ""
	if leadParsed.Release != nil {
		pathAlbum = leadParsed.Release.Album
		pathAlbumYear = leadParsed.Release.Year
		if albumType == "" {
			albumType = leadParsed.Release.ReleaseKind
		}
	}
	if albumTitle == "" {
		fused := fuseText(pathAlbum, leadTags.Album, pTrust, tagTrust(leadTags.Album))
		albumTitle = fused.Value
		if fused.Source == sourceTag || fused.Source == sourceBoth {
			log.Debug().Str("album", albumTitle).Str("source", fused.Source.String()).Float64("confidence", fused.Confidence).Msg("music album resolved via tag fusion")
		}
	}
	if albumYear == "" {
		albumYear = fuseText(pathAlbumYear, leadTags.Year, pTrust, tagTrust(leadTags.Year)).Value
	}
	if albumMBID == "" && sameRelease(leadTags.Album, albumTitle) {
		// Only adopt the tag's RELEASE MBID when the tag names the exact same
		// release as the title we resolved — strict equality, not FuzzyEqual.
		// Fuzzy matching collapses editions ("X" vs "X (Deluxe)"), but each
		// edition has a distinct release MBID, so a fuzzy match would stamp one
		// edition's id onto another and mislink them in the global album dedup.
		albumMBID = fuseMBID("", leadTags.AlbumMBID).Value
	}
	if albumType == "" {
		albumType = "album"
	}
	if albumTitle == "" {
		for _, f := range files {
			m.markFile(ctx, f.ID, sqlc.FileStatusUnmatched, "no album title from path, tags, or NFO", 0)
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

	// Track numbering runs in two passes so an unnumbered file can never steal a
	// numbered file's slot regardless of scan order, and so incremental rescans
	// fill above the album's persisted tracks instead of colliding with them.
	type trackPlan struct {
		file  sqlc.LibraryFile
		info  *mediaprobe.MediaInfo
		disc  int
		want  int // fused track number; 0 = unknown, filled in pass 2
		title string
	}
	assigner := newTrackNumberAssigner()
	// Seed with the album's already-persisted track numbers (empty for a fresh
	// album) so a later scan of new, unnumbered files fills above them.
	if persisted, err := m.q.ListTracksByAlbum(ctx, album.ID); err == nil {
		for _, t := range persisted {
			assigner.reserve(int(t.DiscNumber), int(t.TrackNumber))
		}
	}

	// Pass 1: resolve disc / number / title / probe for each track and reserve
	// every KNOWN number.
	plans := make([]trackPlan, 0, len(tracks))
	for _, trackFile := range tracks {
		trackParsed, _ := parseFileResult(trackFile.ParseResult)

		discNum := 0
		pathTrackNum := 0
		pathTrackTitle := ""
		if trackParsed.Release != nil && trackParsed.Release.HasTrackInfo {
			discNum = trackParsed.Release.DiscNumber
			pathTrackNum = trackParsed.Release.TrackNumber
			pathTrackTitle = trackParsed.Release.TrackTitle
		}

		// Only spend an on-demand ffprobe when we actually need embedded tags:
		// the filename already carries both a track number and a title on a
		// well-named (curated) library, so those tracks stay path-only and match
		// exactly as fast as before. Always reuse the cached lead probe or any
		// media_info the FFProbeWorker already wrote (free), so quality still
		// lands when it's available.
		pathTrackComplete := pathTrackNum > 0 && pathTrackTitle != ""
		// Two-value lookup, not `== nil`: the lead track is seeded into the cache
		// even when its probe returned nil (failed/timed-out), and a plain nil
		// check would re-probe it — doubling the worst-case probe time on a
		// stalled mount, the very thing the probe timeout bounds.
		info, seen := probeCache[trackFile.ID]
		if !seen {
			if existing := parseMediaInfoBytes(trackFile.MediaInfo); existing != nil {
				info = existing
			} else if !pathTrackComplete {
				info = m.musicFileProbe(ctx, trackFile)
			}
			probeCache[trackFile.ID] = info
		}
		tags := extractMusicTags(collectAudioTags(info))

		// Disc number: parsed filename / "Disc N" subdir take precedence, then
		// the embedded tag, defaulting to 1.
		if d := discNumFromPath(trackFile.Path); d > 0 {
			discNum = d
		}
		if discNum == 0 {
			discNum = tags.DiscNumber
		}
		if discNum == 0 {
			discNum = 1
		}

		// Track number: path filename number fused with the tag (path wins a
		// disagreement — the filename reflects on-disk order).
		wantNum := fuseTrackNumber(pathTrackNum, tags.TrackNumber)

		// Track title: fused path/tag, then an exact NFO disc+position override,
		// then the filename stem as last resort.
		trackTitle := fuseText(pathTrackTitle, tags.Title, basePathTrust, tagTrust(tags.Title)).Value
		if albumNFO != nil {
			for _, nfoT := range albumNFO.Tracks {
				nfoDisc := nfoT.Disc
				if nfoDisc == 0 {
					nfoDisc = 1
				}
				if nfoDisc == discNum && nfoT.Position == wantNum && nfoT.Title != "" {
					trackTitle = nfoT.Title
					break
				}
			}
		}
		if trackTitle == "" {
			base := filepath.Base(trackFile.Path)
			trackTitle = strings.TrimSuffix(base, filepath.Ext(base))
		}

		assigner.reserve(discNum, wantNum) // no-op when wantNum == 0
		plans = append(plans, trackPlan{file: trackFile, info: info, disc: discNum, want: wantNum, title: trackTitle})
	}

	// Pass 2: create/link each track. A known number is used as-is — two files
	// sharing one are quality-alternates of a single track and merge via
	// GetOrCreateTrack's upsert; an unknown number is filled to a free per-disc
	// slot that collides with no reserved number, so distinct unnumbered files
	// stay distinct.
	for _, p := range plans {
		trackFile := p.file
		info := p.info
		discNum := p.disc
		trackTitle := p.title
		trackNum := p.want
		if trackNum == 0 {
			trackNum = assigner.fill(discNum)
			log.Debug().Str("file", vfs.RedactPath(trackFile.Path)).Int("disc", discNum).Int("assigned", trackNum).Msg("assigned synthetic track number (unnumbered file)")
		}

		duration := 0
		// (duration / canonical title from heya.media populated later by
		// EnrichMediaItemWorker via UpdateTrackFromEnrichment)

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
		probe := audioFieldsFromInfo(info)
		if probe != nil {
			// Probe data (from FFProbeWorker or our on-demand probe above) —
			// adopt it so the row doesn't stay at extension-only quality.
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

// artistResolution is resolveMusicArtist's verdict on the identities we hold
// for an incoming artist.
type artistResolution struct {
	artist sqlc.Artist
	found  bool
	// mbidSquatted: the library's media_item slot for this MBID
	// (idx_media_items_mbid_unique) is held by an artist whose own non-empty
	// musicbrainz_id CONTRADICTS it — an identity chimera, usually left behind
	// by a bad upstream merge that stamped one act's external ids onto another
	// act's media_item. The caller must not write the mbid key into a new
	// media_item's external_ids (it would trip the index on every scan,
	// forever), and must not adopt the squatter either (that would fuse two
	// acts' discographies).
	mbidSquatted bool
	// nameTaken: the exact (name, disambiguation) tuple belongs to an artist
	// whose established MBID contradicts ours — a different act (or a chimera
	// row). Adoption is refused; the caller must disambiguate the tuple before
	// creating or it trips uq_artists_name_disambig.
	nameTaken bool
}

// resolveMusicArtist tries every identity we have against the existing rows:
// artists.musicbrainz_id first, then the library's media_items external_ids
// mbid (which can diverge from the artists row — see
// GetArtistByLibraryMediaItemMBID), then (name, disambiguation). A real,
// non-synthetic MBID disagreement on either of the latter two vetoes
// adoption: the MBID is the strongest identity we hold, and name equality
// must not overrule it.
func (m *Matcher) resolveMusicArtist(ctx context.Context, libraryID int64, name, disambig, mbid string) artistResolution {
	var res artistResolution

	if mbid != "" {
		if existing, err := m.q.GetArtistByMusicBrainzID(ctx, mbid); err == nil {
			return artistResolution{artist: existing, found: true}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			log.Warn().Err(err).Str("mbid", mbid).Msg("artist MBID lookup error")
		}

		squatter, err := m.q.GetArtistByLibraryMediaItemMBID(ctx, sqlc.GetArtistByLibraryMediaItemMBIDParams{
			LibraryID: libraryID,
			Mbid:      mbid,
		})
		switch {
		case err == nil && (squatter.MusicbrainzID == "" || squatter.MusicbrainzID == mbid):
			// The media_item already claims this MBID and its artist row
			// doesn't contradict it — same act, the artists row just never got
			// the backfill. Adopt (and backfill) instead of colliding on
			// idx_media_items_mbid_unique.
			if squatter.MusicbrainzID == "" {
				if updated, updErr := m.q.UpdateArtist(ctx, sqlc.UpdateArtistParams{
					ID:             squatter.ID,
					MusicbrainzID:  mbid,
					Name:           squatter.Name,
					SortName:       squatter.SortName,
					Disambiguation: squatter.Disambiguation,
					Biography:      squatter.Biography,
				}); updErr == nil {
					return artistResolution{artist: updated, found: true}
				}
			}
			return artistResolution{artist: squatter, found: true}
		case err == nil:
			res.mbidSquatted = true
			log.Warn().Str("mbid", mbid).Str("artist", name).
				Int64("squatter_artist_id", squatter.ID).
				Str("squatter_name", squatter.Name).
				Str("squatter_mbid", squatter.MusicbrainzID).
				Msg("MBID already claimed by another artist's media_item; creating artist without media-item mbid")
		case !errors.Is(err, pgx.ErrNoRows):
			log.Warn().Err(err).Str("mbid", mbid).Msg("artist media-item MBID lookup error")
		}
	}

	if name != "" {
		existing, err := m.q.GetArtistByNameAndDisambiguation(ctx, sqlc.GetArtistByNameAndDisambiguationParams{
			Lower:   name,
			Lower_2: disambig,
		})
		switch {
		case err == nil && mbid != "" && existing.MusicbrainzID != "" && existing.MusicbrainzID != mbid &&
			!isSyntheticMBID(mbid) && !isSyntheticMBID(existing.MusicbrainzID):
			// Exact (name, disambiguation) match, but the row's established
			// MBID contradicts ours. Same-name adoption here would fuse two
			// acts' discographies — including re-adopting the very chimera the
			// squatter branch above just refused. Refuse; the caller creates a
			// separate row under a disambiguated tuple.
			res.nameTaken = true
			log.Warn().Str("mbid", mbid).Str("artist", name).
				Int64("existing_artist_id", existing.ID).
				Str("existing_mbid", existing.MusicbrainzID).
				Msg("(name, disambiguation) held by an artist with a contradicting MBID; refusing name-based adoption")
		case err == nil:
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
					res.artist, res.found = updated, true
					return res
				}
			}
			res.artist, res.found = existing, true
			return res
		case !errors.Is(err, pgx.ErrNoRows):
			log.Warn().Err(err).Str("name", name).Msg("artist name lookup error")
		}
	}

	return res
}

func (m *Matcher) upsertMusicArtist(ctx context.Context, libraryID int64, name, disambig, mbid, biography, sortName string) (sqlc.Artist, error) {
	res := m.resolveMusicArtist(ctx, libraryID, name, disambig, mbid)
	if res.found {
		return res.artist, nil
	}

	if res.nameTaken {
		// The (name, disambiguation) tuple belongs to a contradicting-MBID
		// artist — creating under it would trip uq_artists_name_disambig.
		// Stamp a short MBID marker into the disambiguation; the next enrich
		// (same MBID passes the identity guard) replaces it with the
		// upstream's real disambiguation.
		marker := mbid
		if len(marker) > 8 {
			marker = marker[:8]
		}
		disambig = strings.TrimSpace(disambig + " (mbid " + marker + ")")
	}

	extIDs := map[string]string{}
	if mbid != "" {
		extIDs["musicbrainz_artist"] = mbid
		if !res.mbidSquatted {
			extIDs["mbid"] = mbid
		}
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
		// Re-resolve through every identity and use the winner.
		if rr := m.resolveMusicArtist(ctx, libraryID, name, disambig, mbid); rr.found {
			return rr.artist, nil
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

	created, err := m.q.CreateArtist(ctx, sqlc.CreateArtistParams{
		MediaItemID:    item.ID,
		MusicbrainzID:  mbid,
		Name:           name,
		SortName:       sortName,
		Disambiguation: disambig,
		Biography:      biography,
	})
	if err != nil {
		// Don't leave the just-created media_item behind: an orphaned row
		// holding the mbid key would squat idx_media_items_mbid_unique and
		// block this artist from ever being created again.
		if delErr := m.q.DeleteMediaItem(ctx, item.ID); delErr != nil {
			log.Warn().Err(delErr).Int64("media_item", item.ID).Msg("cleanup of orphaned artist media_item failed")
		}
		if rr := m.resolveMusicArtist(ctx, libraryID, name, disambig, mbid); rr.found {
			return rr.artist, nil
		}
		return sqlc.Artist{}, fmt.Errorf("creating artist: %w", err)
	}
	return created, nil
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

	detail := m.enrichArtistFromHeyaMedia(ctx, mbid, name, "")
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
func (m *Matcher) enrichArtistFromHeyaMedia(ctx context.Context, mbid, name, disambig string) *metadata.MediaDetail {
	if m.heya == nil {
		return nil
	}

	if mbid != "" && !isSyntheticMBID(mbid) {
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
	// Verification gate: only accept a search hit whose name actually
	// corresponds to the local artist. heya.media's discover can return a
	// wildly wrong artist ("Avicii" → "Alicia Keys") or collapse a
	// collaboration onto a member ("Charly Lownoise & Mental Theo" → "Charly
	// Lownoise"); accepting either writes the wrong identity + MBID onto the
	// local row, and findCanonicalSibling then fuses two distinct artists.
	// Reject and keep the artist distinct + un-enriched — under-enrich beats
	// mis-merge. (Collaboration→member linking is the deferred collaboration
	// graph; see FUTURE.md.)
	if !artistNameAcceptable(name, hit.Name) {
		log.Debug().
			Str("name", name).
			Str("hit_name", hit.Name).
			Str("hit_id", hit.ID).
			Float64("score", hit.Score).
			Msg("rejecting dissimilar artist match; keeping local artist distinct")
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
	// Same-name guard: a verified name match can still be the WRONG artist when
	// two acts share a name ("Ado" the Japanese vocalist vs "Ado" the techno
	// producer). When both rows carry a disambiguation and they clearly
	// describe different acts, reject — accepting it would write the other
	// act's MBID and let findCanonicalSibling fuse the two same-named rows.
	if disambiguationConflict(disambig, detail.ArtistDisambiguation) {
		log.Debug().
			Str("name", name).
			Str("local_disambig", disambig).
			Str("matched_disambig", detail.ArtistDisambiguation).
			Str("hit_id", hit.ID).
			Msg("rejecting artist match: disambiguation conflict")
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

// collabSeparators are the high-confidence "this folder names more than one
// artist" tokens. Kept tight on purpose — each is space-padded so it only
// matches as a standalone separator, never as a substring of a real word
// ("Daft Punk" / "Left Eye" must not read as " ft ", "AT&T" / "C+C" must not
// read as " & " / " + "). Markers like "," / " and " / " x " / " with " are
// deliberately excluded: they appear inside legitimate single-artist names
// ("Tyler, the Creator", "Hall and Oates") and would cause false rejections.
var collabSeparators = []string{
	" & ", " feat. ", " feat ", " featuring ", " ft. ", " ft ", " vs. ", " vs ", " versus ", " + ",
}

// isCollaborationName reports whether a name reads as a multi-artist
// collaboration ("A & B", "A feat. B"). Case-insensitive; the name is
// space-padded so a separator at the very start/end still matches.
func isCollaborationName(name string) bool {
	s := " " + strings.ToLower(strings.TrimSpace(name)) + " "
	for _, sep := range collabSeparators {
		if strings.Contains(s, sep) {
			return true
		}
	}
	return false
}

// collaborationCollapsed reports whether enriching `local` with a match named
// `matched` would reduce a collaboration to a single contributor — the signal
// that the upstream search returned a member instead of the collaboration
// itself. True only when the local name is a collaboration and the matched
// name has dropped the collaboration form. A fixed-name duo that maps to
// itself ("Simon & Garfunkel" → "Simon & Garfunkel", "Chase & Status" →
// "Chase & Status") keeps its markers on both sides and is allowed through.
func collaborationCollapsed(local, matched string) bool {
	return isCollaborationName(local) && !isCollaborationName(matched)
}

// artistNameAcceptable reports whether `matched` (an upstream search-hit name)
// is a trustworthy identity for the local artist `local`. heya.media's discover
// — especially a self-hosted instance — can return a wildly wrong artist (the
// "Avicii" folder resolving to "Alicia Keys"), and accepting it writes that
// identity + MBID onto the local row, which then lets findCanonicalSibling fuse
// two unrelated artists. The match must be FuzzyEqual (transliteration / casing
// / punctuation tolerant, so HANABIE↔花冷え。 and "Charli xcx"↔"Charli XCX" pass)
// AND must not be a collaboration reduced to one member (FuzzyEqual's substring
// fallback would otherwise wave "A & B"→"A" through). Rejection leaves the
// artist un-enriched but distinct — the safe failure: under-enrich beats
// mis-merge.
func artistNameAcceptable(local, matched string) bool {
	if collaborationCollapsed(local, matched) {
		return false
	}
	if titlematch.FuzzyEqual(local, matched) {
		return true
	}
	// Cross-script pairs (romaji "HANABIE" vs kana "花冷え。") frequently don't
	// survive transliteration cleanly, yet a different-script match is almost
	// always a legitimate localized name rather than the wrong artist — and
	// rejecting it would break the transliteration-rename merge this was built
	// for. Only same-script-yet-dissimilar pairs are the "Avicii → Alicia Keys"
	// wrong-match signal, so reject those.
	return !sameScript(local, matched)
}

// hasCJK reports whether a string contains any Han / kana / Hangul rune — the
// cheap "is this a CJK-script name" test.
func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// sameScript reports whether two names sit in the same broad script family.
// Coarse on purpose: it only distinguishes CJK from non-CJK, which is enough to
// tell a transliteration pair (cross-script) from a same-script wrong match.
func sameScript(a, b string) bool {
	return hasCJK(a) == hasCJK(b)
}

// disambiguationConflict reports whether two non-empty disambiguations clearly
// describe different acts — the last line of defence for same-name artists a
// name match can't tell apart ("Ado" the Japanese vocalist vs "Ado" the techno
// producer). Conservative on purpose: only a conflict when BOTH sides are set
// and they share no significant token (length ≥ 4, to skip "the"/"from"/"and"),
// so a paraphrase ("Japanese vocalist" vs "Japanese singer") is NOT treated as
// a conflict. Empty on either side → no signal → not a conflict.
func disambiguationConflict(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	sig := func(s string) map[string]struct{} {
		m := map[string]struct{}{}
		for _, tok := range titlematch.Tokenize(strings.ToLower(s)) {
			if len([]rune(tok)) >= 4 {
				m[tok] = struct{}{}
			}
		}
		return m
	}
	aSet, bSet := sig(a), sig(b)
	if len(aSet) == 0 || len(bSet) == 0 {
		return false // nothing significant to compare → don't reject
	}
	for tok := range aSet {
		if _, ok := bSet[tok]; ok {
			return false // shared significant token → plausibly the same act
		}
	}
	return true
}

// isSyntheticMBID reports whether an MBID is a heya.media placeholder rather
// than a real MusicBrainz id. The self-hosted aggregator emits ids like
// "dddddddd-dddd-dddd-dddd-ddd513923292" when it has no real MBID; two distinct
// artists sharing one of those must never be fused by the MBID merge path.
func isSyntheticMBID(mbid string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mbid)), "dddddddd-")
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

// parseMediaInfoBytes parses a library_files.media_info blob into MediaInfo,
// returning nil for the unprobed sentinel ('{}') or any parse error.
func parseMediaInfoBytes(raw []byte) *mediaprobe.MediaInfo {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	info, err := mediaprobe.Parse(raw)
	if err != nil {
		return nil
	}
	return info
}

// audioFieldsFromInfo pulls the primary audio stream's quality fields out of
// already-parsed probe info. Returns nil when there's no audio stream or
// nothing useful was decoded.
func audioFieldsFromInfo(info *mediaprobe.MediaInfo) *mediaprobe.AudioFields {
	if info == nil {
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

// musicFileProbe returns parsed ffprobe info for a track file. It reuses the
// media_info the async FFProbeWorker may already have written; when that
// hasn't landed yet (match usually beats ffprobe in the queue) it probes on
// demand and persists the result. Probing here — before the artist/album
// decision — lets tag fusion read embedded ID3/Vorbis tags rather than trust a
// possibly-garbage path, and makes the later FFProbeWorker pass a cheap
// re-probe. Returns nil when no prober is wired (tests) or the probe fails;
// callers then fall back to path/NFO only, exactly as before tag fusion.
// musicProbeTimeout bounds a single on-demand probe during matching, mirroring
// the bounds the other worker.ProbeFile callers use.
const musicProbeTimeout = 90 * time.Second

func (m *Matcher) musicFileProbe(ctx context.Context, file sqlc.LibraryFile) *mediaprobe.MediaInfo {
	if info := parseMediaInfoBytes(file.MediaInfo); info != nil {
		return info
	}
	if m.probe == nil {
		return nil
	}
	// Bound every on-demand probe like the other ProbeFile call sites do
	// (FFProbeWorker 120s, EnsureFileProbed 60s). Without this the raw match-job
	// context (River JobTimeout is 6h) would let a stalled SMB read hang ffprobe
	// on its pipe and wedge the single-worker match queue — which serves every
	// media type — for hours.
	probeCtx, cancel := context.WithTimeout(ctx, musicProbeTimeout)
	defer cancel()
	info, err := m.probe(probeCtx, file.Path)
	if err != nil {
		log.Debug().Err(err).Str("path", vfs.RedactPath(file.Path)).Msg("on-demand music probe failed; using path/NFO only")
		return nil
	}
	if infoJSON, mErr := json.Marshal(info); mErr == nil && file.ID > 0 {
		if err := m.q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{ID: file.ID, MediaInfo: infoJSON}); err != nil {
			log.Debug().Err(err).Int64("file_id", file.ID).Msg("persist on-demand music probe failed")
		}
	}
	return info
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
