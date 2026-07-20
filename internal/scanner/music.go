package scanner

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/audiotags"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/musicconsensus"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/slug"
	"github.com/karbowiak/heya/internal/titlematch"
)

const musicProbeTimeout = 90 * time.Second

// musicProbeConcurrency bounds the parallel tag probes in
// AnalyzeMusicWithOptions. Each probe is one short ffprobe subprocess whose
// cost is dominated by media round trips (often over network-mounted storage),
// so a small fan-out hides the network latency without stampeding the share
// or the local CPU. Matches the musicFetchConcurrency scale used elsewhere
// in this package.
const musicProbeConcurrency = 4

type MusicAnalysisOptions struct {
	Probe mediaprobe.Func
}

type MusicTrackPlan struct {
	Key                  string            `json:"key"`
	Artist               string            `json:"artist"`
	ArtistDisambiguation string            `json:"artist_disambiguation,omitempty"`
	Album                string            `json:"album"`
	Year                 string            `json:"year,omitempty"`
	ReleaseKind          string            `json:"release_kind,omitempty"`
	ExternalIDs          map[string]string `json:"external_ids,omitempty"`
	IdentityKeys         []string          `json:"identity_keys,omitempty"`
	NFO                  string            `json:"nfo,omitempty"`
	DiscNumber           int               `json:"disc_number,omitempty"`
	TrackNumber          int               `json:"track_number,omitempty"`
	TrackTitle           string            `json:"track_title"`
	RelPath              string            `json:"rel_path"`
	Format               string            `json:"format,omitempty"`
	Source               string            `json:"source"`
	Confidence           float64           `json:"confidence"`
	Issues               []string          `json:"issues,omitempty"`
}

type MusicAlbumPlan struct {
	Key                  string            `json:"key"`
	Artist               string            `json:"artist"`
	ArtistDisambiguation string            `json:"artist_disambiguation,omitempty"`
	Album                string            `json:"album"`
	Aliases              []string          `json:"aliases,omitempty"`
	Year                 string            `json:"year,omitempty"`
	ReleaseKind          string            `json:"release_kind,omitempty"`
	ExternalIDs          map[string]string `json:"external_ids,omitempty"`
	NFOs                 []string          `json:"nfos,omitempty"`
	Tracks               []MusicTrackPlan  `json:"tracks"`
	Files                []string          `json:"files"`
	Issues               []string          `json:"issues,omitempty"`
	Confidence           float64           `json:"confidence"`
}

type MusicArtistPlan struct {
	Key                  string            `json:"key"`
	Artist               string            `json:"artist"`
	ArtistDisambiguation string            `json:"artist_disambiguation,omitempty"`
	ExternalIDs          map[string]string `json:"external_ids,omitempty"`
	Albums               []MusicAlbumPlan  `json:"albums"`
	Files                []string          `json:"files"`
	Issues               []string          `json:"issues,omitempty"`
	Confidence           float64           `json:"confidence"`
}

type musicAlbumFolderInfo struct {
	Artist      string
	Album       string
	Year        string
	ReleaseKind string
	Source      string
	Confidence  float64
}

type musicTrackInfo struct {
	Disc         int
	DiscExplicit bool
	Track        int
	Title        string
}

type musicNFOEntry struct {
	file InventoryFile
	nfo  *nfo.ParsedNFO
}

var (
	musicStructuredAlbumRE      = regexp.MustCompile(`(?i)^(.+?)\s+-\s+(album|ep|single|compilation|soundtrack)\s+-\s+((?:19|20)\d{2}|1)\s+-\s+(.+)$`)
	musicArtistAlbumYearTailRE  = regexp.MustCompile(`^(.+?)\s+-\s+(.+?)\s+[\[(]((?:19|20)\d{2})[\])](?:\s+.*)?$`)
	musicYearPrefixRE           = regexp.MustCompile(`^((?:19|20)\d{2})\s+-\s+(.+)$`)
	musicTitleYearRE            = regexp.MustCompile(`^(.+?)\s+[\[(]((?:19|20)\d{2})[\])](?:.*)?$`)
	musicSceneCatalogRE         = regexp.MustCompile(`(?i)^\[[^\]]+\]\s*(.+?)\s+-\s+(.+?)(?:-\([^)]+\))?-(single|ep|album)-.*?((?:19|20)\d{2})`)
	musicDiscFolderRE           = regexp.MustCompile(`(?i)^(?:disc|disk|cd)\s*[_ -]*(\d+).*$`)
	musicFourDigitTrackRE       = regexp.MustCompile(`^(\d{2})(\d{2})\s*-\s*(.+)$`)
	musicTwoDigitTrackRE        = regexp.MustCompile(`^(\d{1,2})\s*[-_. ]+\s*(.+)$`)
	musicTrackArtistTitleDashRE = regexp.MustCompile(`^(.+?)\s+-\s+(.+)$`)
	musicTrailingFormatTagRE    = regexp.MustCompile(`(?i)\s*[\[(](?:flac|mp3|aac|m4a|ogg|opus|web|web[- ]?dl|cd|vinyl|lossless|remaster(?:ed)?|24bit|16bit|320|v0)[\])]$`)
	musicNumberedDisambigRE     = regexp.MustCompile(`(?i)\s+\(\d+\)\s*$`)
	musicSyntheticProbeTitleRE  = regexp.MustCompile(`(?i)^(?:sine|brown|pink|purple)\s*\((?:flac|mp3|aac|m4a|ogg|opus|wav)\)$`)
	musicWeakTrackTitleRE       = regexp.MustCompile(`(?i)^(?:bonus|sample|test)\s*\d+$`)
	musicKeyPartRE              = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

func AnalyzeMusic(ctx context.Context, inv Inventory, emit Emitter) ([]MusicTrackPlan, []MusicAlbumPlan, []MusicArtistPlan, error) {
	return AnalyzeMusicWithOptions(ctx, inv, emit, MusicAnalysisOptions{})
}

func AnalyzeMusicWithOptions(ctx context.Context, inv Inventory, emit Emitter, opts MusicAnalysisOptions) ([]MusicTrackPlan, []MusicAlbumPlan, []MusicArtistPlan, error) {
	var tracks []MusicTrackPlan
	for _, root := range inv.Roots {
		nfos := parseMusicNFOs(root, emit)
		type probedFile struct {
			file InventoryFile
			tags audiotags.Tags
			err  error
		}
		var audioFiles []InventoryFile
		for _, file := range root.Files {
			if file.Class != ClassPrimaryMedia || !mediafile.IsAudioExt(file.Ext) {
				continue
			}
			audioFiles = append(audioFiles, file)
		}

		// Probe tags with bounded parallelism: on a fresh import each probe
		// is an ffprobe round trip against the (often network-mounted) library,
		// and probing serially made this the dominant cost of a first music
		// scan. Results land at their input index so grouping below sees the
		// same order the serial loop produced. On cancellation we still
		// wg.Wait() before returning — the workers write into probed.
		probed := make([]probedFile, len(audioFiles))
		sem := make(chan struct{}, musicProbeConcurrency)
		var wg sync.WaitGroup
	probeFanout:
		for i, file := range audioFiles {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break probeFanout
			}
			wg.Add(1)
			go func(i int, file InventoryFile) {
				defer wg.Done()
				defer func() { <-sem }()
				tags, probeErr := probeMusicTags(ctx, file, opts, emit)
				probed[i] = probedFile{file: file, tags: tags, err: probeErr}
			}(i, file)
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return tracks, nil, nil, err
		}
		for _, item := range probed {
			if item.err != nil {
				return tracks, nil, nil, item.err
			}
		}

		byReleaseDir := map[string][]probedFile{}
		var releaseDirs []string
		for _, item := range probed {
			dir := musicReleasePlanningDir(item.file, item.tags)
			if _, seen := byReleaseDir[dir]; !seen {
				releaseDirs = append(releaseDirs, dir)
			}
			byReleaseDir[dir] = append(byReleaseDir[dir], item)
		}
		for _, dir := range releaseDirs {
			probed := byReleaseDir[dir]
			evidence := make([]musicconsensus.Evidence, 0, len(probed))
			for _, item := range probed {
				evidence = append(evidence, musicConsensusEvidence(item.tags))
			}
			consensus := musicconsensus.Build(evidence)
			if consensus.Artist.Strong || consensus.Album.Strong || consensus.Year.Strong {
				emit.Emit(Event{
					Event: "music.release.consensus",
					Data: map[string]any{
						"directory":      dir,
						"artist":         consensus.Artist.Value,
						"artist_support": consensus.Artist.Support,
						"album":          consensus.Album.Value,
						"album_support":  consensus.Album.Support,
						"tracks":         len(probed),
						"year":           consensus.Year.Value,
						"year_support":   consensus.Year.Support,
					},
				})
			}
			planningNFOs := nfos
			if !musicNFOTrustedByConsensus(nfos[dir], consensus) {
				planningNFOs = make(map[string]musicNFOEntry, len(nfos)-1)
				for nfoDir, entry := range nfos {
					if nfoDir != dir {
						planningNFOs[nfoDir] = entry
					}
				}
			}
			for _, item := range probed {
				// Feed inherited release fields into planning so a sibling with
				// missing ARTIST/ALBUM tags can still be planned even when its path
				// is weak. The original tags are retained for outlier/ID auditing.
				effectiveTags := inheritMusicReleaseConsensus(item.tags, consensus)
				plan, ok := planMusicTrack(item.file, planningNFOs, effectiveTags)
				if ok {
					plan = applyMusicReleaseConsensus(plan, item.tags, nfos[dir], consensus)
					plan = applyMusicStorageOwner(plan)
				}
				if !ok {
					emit.Emit(Event{
						Event:   "music.file.unplanned",
						RelPath: item.file.RelPath,
						Reason:  "no_music_identity",
						Message: "audio file could not be assigned to an artist and album",
					})
					continue
				}
				tracks = append(tracks, plan)
				emit.Emit(Event{
					Event:   "music.track.planned",
					RelPath: item.file.RelPath,
					Data: map[string]any{
						"album":      plan.Album,
						"artist":     plan.Artist,
						"confidence": plan.Confidence,
						"disc":       plan.DiscNumber,
						"track":      plan.TrackNumber,
					},
				})
			}
		}
	}
	sortMusicTracks(tracks)
	albums := groupMusicAlbums(tracks)
	artists := groupMusicArtists(albums)
	for _, album := range albums {
		emit.Emit(Event{
			Event: "music.album.planned",
			Data: map[string]any{
				"album":      album.Album,
				"artist":     album.Artist,
				"files":      len(album.Files),
				"tracks":     len(album.Tracks),
				"confidence": album.Confidence,
			},
		})
	}
	for _, artist := range artists {
		emit.Emit(Event{
			Event: "music.artist.planned",
			Data: map[string]any{
				"albums":     len(artist.Albums),
				"artist":     artist.Artist,
				"confidence": artist.Confidence,
				"files":      len(artist.Files),
			},
		})
	}
	return tracks, albums, artists, nil
}

func musicReleaseDir(path string) string {
	dir := filepath.Dir(filepath.ToSlash(path))
	if musicDiscFolderRE.MatchString(filepath.Base(dir)) {
		dir = filepath.Dir(dir)
	}
	if dir == "." {
		return ""
	}
	return filepath.ToSlash(dir)
}

func musicReleasePlanningDir(file InventoryFile, tags audiotags.Tags) string {
	dir := musicReleaseDir(file.RelPath)
	if dir != "" {
		return dir
	}
	// Root-level files do not share a physical release directory. Bucket fully
	// tagged files by their embedded release identity so consensus can help
	// sibling tracks without letting the largest root album overwrite every
	// other loose file's tags.
	artist := strings.TrimSpace(firstNonEmpty(tags.AlbumArtist, tags.Artist))
	album := strings.TrimSpace(tags.Album)
	if !looksLikeUnusableMusicIdentity(artist) && !looksLikeUnusableMusicIdentity(album) &&
		!audiotags.IsPlaceholderName(artist) && !audiotags.IsPlaceholderValue(album) {
		return "@root-release:" + normalizeMusicKeyPart(artist) + "|" + normalizeMusicKeyPart(album)
	}
	return "@root-file:" + filepath.ToSlash(file.RelPath)
}

func musicConsensusEvidence(tags audiotags.Tags) musicconsensus.Evidence {
	artist := strings.TrimSpace(firstNonEmpty(tags.AlbumArtist, tags.Artist))
	if audiotags.IsPlaceholderName(artist) {
		artist = ""
	}
	album := strings.TrimSpace(tags.Album)
	if audiotags.IsPlaceholderValue(album) {
		album = ""
	}
	return musicconsensus.Evidence{
		Artist: artist,
		Album:  album,
		Year:   normalizeMusicYear(strings.TrimSpace(tags.Year)),
	}
}

func inheritMusicReleaseConsensus(tags audiotags.Tags, consensus musicconsensus.Release) audiotags.Tags {
	if consensus.Artist.Strong {
		tagArtist := firstNonEmpty(tags.AlbumArtist, tags.Artist)
		if !consensus.Artist.Matches(tagArtist) {
			tags.Artist = ""
			tags.AlbumArtist = consensus.Artist.Value
			tags.ArtistMBID = ""
			tags.AlbumArtistMBID = ""
		} else if strings.TrimSpace(tags.AlbumArtist) == "" {
			tags.AlbumArtist = consensus.Artist.Value
		}
	}
	if consensus.Album.Strong {
		if !consensus.Album.Matches(tags.Album) {
			tags.Album = consensus.Album.Value
			tags.AlbumMBID = ""
			tags.ReleaseGroupMBID = ""
		}
	}
	if consensus.Year.Strong {
		tags.Year = consensus.Year.Value
	}
	return tags
}

// applyMusicReleaseConsensus is intentionally a post-fusion step. A strong
// sibling consensus must be able to overrule a poisoned generated NFO as well
// as one contradictory embedded tag; otherwise the bad NFO wins the next scan
// and turns a single outlier into the identity of the whole folder.
func applyMusicReleaseConsensus(plan MusicTrackPlan, tags audiotags.Tags, entry musicNFOEntry, consensus musicconsensus.Release) MusicTrackPlan {
	tagArtist := strings.TrimSpace(firstNonEmpty(tags.AlbumArtist, tags.Artist))
	tagAlbum := strings.TrimSpace(tags.Album)
	nfoTrusted := musicNFOTrustedByConsensus(entry, consensus)

	if consensus.Artist.Strong {
		if !consensus.Artist.Matches(plan.Artist) {
			plan.Issues = appendMusicIssue(plan.Issues, "artist_overridden_by_folder_consensus")
			plan.ArtistDisambiguation = ""
		}
		plan.Artist = consensus.Artist.Value
	}
	if consensus.Album.Strong {
		if !consensus.Album.Matches(plan.Album) {
			plan.Issues = appendMusicIssue(plan.Issues, "album_overridden_by_folder_consensus")
		}
		plan.Album = consensus.Album.Value
	}
	if consensus.Year.Strong {
		if plan.Year != "" && !strings.EqualFold(strings.TrimSpace(plan.Year), consensus.Year.Value) {
			plan.Issues = appendMusicIssue(plan.Issues, "year_overridden_by_folder_consensus")
		}
		plan.Year = consensus.Year.Value
	}

	tagArtistContradicts := consensus.Artist.Strong && tagArtist != "" && !consensus.Artist.Matches(tagArtist)
	tagAlbumContradicts := consensus.Album.Strong && tagAlbum != "" && !consensus.Album.Matches(tagAlbum)
	if !nfoTrusted || tagArtistContradicts || tagAlbumContradicts {
		ids := map[string]string{}
		if nfoTrusted {
			for key, value := range musicExternalIDsFromNFO(entry.nfo) {
				ids[key] = value
			}
		}
		for key, value := range consensusSafeMusicTagIDs(tags, consensus) {
			ids[key] = value
		}
		plan.ExternalIDs = nonEmptyStringMap(ids)
	}
	if !nfoTrusted {
		plan.NFO = ""
		plan.Issues = appendMusicIssue(plan.Issues, "nfo_rejected_by_folder_consensus")
	}
	if tagArtistContradicts || tagAlbumContradicts {
		plan.Issues = appendMusicIssue(plan.Issues, "tag_outlier_rejected_by_folder_consensus")
	}
	if consensus.Artist.Strong || consensus.Album.Strong || consensus.Year.Strong {
		if plan.Source == "" {
			plan.Source = "folder_consensus"
		} else if !strings.Contains(plan.Source, "folder_consensus") {
			plan.Source += "+folder_consensus"
		}
		plan.Confidence = maxFloat(plan.Confidence, 0.92)
	}
	plan.IdentityKeys = musicAlbumIdentityKeys(plan)
	plan.Key = plan.IdentityKeys[0]
	return plan
}

func musicNFOTrustedByConsensus(entry musicNFOEntry, consensus musicconsensus.Release) bool {
	if entry.nfo == nil {
		return true
	}
	if consensus.Artist.Strong && entry.nfo.AlbumArtist != "" && !consensus.Artist.Matches(entry.nfo.AlbumArtist) {
		return false
	}
	if consensus.Album.Strong && entry.nfo.Title != "" && !consensus.Album.Matches(entry.nfo.Title) {
		return false
	}
	return true
}

func consensusSafeMusicTagIDs(tags audiotags.Tags, consensus musicconsensus.Release) map[string]string {
	ids := map[string]string{}
	tagArtist := strings.TrimSpace(firstNonEmpty(tags.AlbumArtist, tags.Artist))
	artistSafe := !consensus.Artist.Strong || consensus.Artist.Matches(tagArtist)
	albumSafe := !consensus.Album.Strong || consensus.Album.Matches(tags.Album)
	if tags.AlbumArtistMBID != "" && (!consensus.Artist.Strong || consensus.Artist.Matches(tags.AlbumArtist)) {
		ids["musicbrainz_album_artist"] = tags.AlbumArtistMBID
	}
	if tags.ArtistMBID != "" && (!consensus.Artist.Strong || consensus.Artist.Matches(tags.Artist)) {
		ids["musicbrainz_artist"] = tags.ArtistMBID
	}
	if artistSafe && albumSafe {
		if tags.AlbumMBID != "" {
			ids["musicbrainz_album"] = tags.AlbumMBID
		}
		if tags.ReleaseGroupMBID != "" {
			ids["musicbrainz_release_group"] = tags.ReleaseGroupMBID
		}
	}
	return nonEmptyStringMap(ids)
}

func appendMusicIssue(issues []string, issue string) []string {
	if !contains(issues, issue) {
		return append(issues, issue)
	}
	return issues
}

// applyMusicStorageOwner folds release-level collaboration credits back into
// the top-level library owner when that owner is explicitly present in the
// credit. This prevents an Ado scope from creating a second local artist named
// "Jax Jones, Ado", while still allowing a genuinely misfiled d4vd release in
// an ATRIP folder to remain d4vd. Release identifiers survive; artist-level
// identifiers are cleared because a multi-credit tag cannot safely tell us
// which ID belongs to the storage owner without provider resolution.
func applyMusicStorageOwner(plan MusicTrackPlan) MusicTrackPlan {
	segments := splitRelPath(plan.RelPath)
	if len(segments) < 2 {
		return plan
	}
	owner, disambiguation := splitMusicArtistFolder(segments[0])
	if owner == "" || looksLikeUnusableMusicIdentity(owner) || !musicCreditContainsArtist(plan.Artist, owner) {
		return plan
	}
	if musicConjunctionKey(plan.Artist) != musicConjunctionKey(owner) {
		if !musicCollaborationSeparatorRE.MatchString(plan.Artist) && !strings.Contains(plan.Artist, ",") {
			return plan
		}
		plan.Issues = appendMusicIssue(plan.Issues, "artist_collaboration_collapsed_to_storage_owner")
		for _, key := range []string{
			"musicbrainz_artist", "musicbrainz_album_artist", "itunes_artist",
			"apple_artist", "deezer_artist", "spotify_artist", "audiodb_artist",
		} {
			delete(plan.ExternalIDs, key)
		}
	}
	plan.Artist = owner
	plan.ArtistDisambiguation = disambiguation
	if plan.Source == "" {
		plan.Source = "storage_owner"
	} else if !strings.Contains(plan.Source, "storage_owner") {
		plan.Source += "+storage_owner"
	}
	plan.IdentityKeys = musicAlbumIdentityKeys(plan)
	plan.Key = plan.IdentityKeys[0]
	return plan
}

func musicCreditContainsArtist(credit, artist string) bool {
	creditFields := strings.Fields(musicConjunctionKey(credit))
	artistFields := strings.Fields(musicConjunctionKey(artist))
	if len(creditFields) == 0 || len(artistFields) == 0 || len(artistFields) > len(creditFields) {
		return false
	}
	for start := 0; start+len(artistFields) <= len(creditFields); start++ {
		matches := true
		for offset := range artistFields {
			if creditFields[start+offset] != artistFields[offset] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func probeMusicTags(ctx context.Context, file InventoryFile, opts MusicAnalysisOptions, emit Emitter) (audiotags.Tags, error) {
	if opts.Probe == nil {
		return audiotags.Tags{}, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, musicProbeTimeout)
	defer cancel()
	info, err := opts.Probe(probeCtx, file.Path)
	if err != nil {
		if terminal := providerContextTermination(probeCtx.Err(), err); terminal != nil {
			return audiotags.Tags{}, terminal
		}
		emit.Emit(Event{
			Event:    "music.tags.probe_failed",
			Severity: SeverityWarn,
			RelPath:  file.RelPath,
			Message:  err.Error(),
		})
		return audiotags.Tags{}, nil
	}
	tags := audiotags.FromMediaInfo(info)
	if tags.HasAny() {
		emit.Emit(Event{
			Event:   "music.tags.probed",
			RelPath: file.RelPath,
			Data: map[string]any{
				"album":        tags.Album,
				"album_artist": tags.AlbumArtist,
				"artist":       tags.Artist,
				"title":        tags.Title,
				"track":        tags.TrackNumber,
				"year":         tags.Year,
			},
		})
	}
	return tags, nil
}

func planMusicTrack(file InventoryFile, nfos map[string]musicNFOEntry, tags audiotags.Tags) (MusicTrackPlan, bool) {
	segments := splitRelPath(file.RelPath)
	if len(segments) == 1 {
		// A fully tagged file at the library root has no path-derived artist or
		// album folders. Give the normal planner a virtual layout sourced only
		// from trustworthy embedded identity; incomplete root files remain
		// unplanned instead of inventing a "Loose Tracks" album.
		tagArtist := strings.TrimSpace(firstNonEmpty(tags.AlbumArtist, tags.Artist))
		tagAlbum := strings.TrimSpace(tags.Album)
		if looksLikeUnusableMusicIdentity(tagArtist) || looksLikeUnusableMusicIdentity(tagAlbum) ||
			audiotags.IsPlaceholderName(tagArtist) || audiotags.IsPlaceholderValue(tagAlbum) {
			return MusicTrackPlan{}, false
		}
		segments = []string{tagArtist, tagAlbum, segments[0]}
	}
	if len(segments) < 2 {
		return MusicTrackPlan{}, false
	}

	parsed := parser.ParseStoragePath(file.RelPath)
	release := parsed.Release

	discFolder, discFromFolder := musicDiscFromFolder(parentSegment(segments))
	albumIndex := len(segments) - 2
	if discFolder && len(segments) >= 3 {
		albumIndex = len(segments) - 3
	}
	if albumIndex < 0 {
		return MusicTrackPlan{}, false
	}
	albumDir := strings.Join(segments[:albumIndex+1], "/")
	albumFolder := segments[albumIndex]
	artistFolder := ""
	if albumIndex > 0 {
		artistFolder = segments[albumIndex-1]
	}

	artist, disambig := splitMusicArtistFolder(artistFolder)
	albumInfo := parseMusicAlbumFolder(albumFolder, artist)
	if albumInfo.Artist != "" {
		replaceMusicArtist(&artist, &disambig, albumInfo.Artist)
	}
	album := albumInfo.Album
	year := albumInfo.Year
	releaseKind := albumInfo.ReleaseKind
	source := albumInfo.Source
	confidence := albumInfo.Confidence
	externalIDs := map[string]string{}
	var localNFO musicNFOEntry
	hasNFO := false

	if release != nil && release.Media == parser.MediaAudio {
		if release.Artist != "" {
			replaceMusicArtist(&artist, &disambig, release.Artist)
		}
		if release.ArtistDisambiguation != "" {
			disambig = release.ArtistDisambiguation
		}
		if release.Album != "" {
			releaseAlbum := cleanMusicAlbumName(release.Album)
			if shouldUseMusicReleaseAlbum(album, releaseAlbum, source) {
				album = releaseAlbum
			}
		} else if release.Title != "" && album == "" && source == "" {
			album = release.Title
		}
		if release.Year != "" {
			year = release.Year
		}
		if release.ReleaseKind != "" {
			releaseKind = release.ReleaseKind
		}
		if release.Strategy != "" {
			source = string(release.Strategy)
		}
		if release.Score > 0 {
			confidence = maxFloat(confidence, float64(release.Score)/100)
		}
	}

	track := parseMusicTrackFilename(file.Name)
	if discFromFolder > 0 && !track.DiscExplicit {
		track.Disc = discFromFolder
	}
	if release != nil && release.HasTrackInfo {
		track.Disc = release.DiscNumber
		track.Track = release.TrackNumber
		track.Title = release.TrackTitle
	}
	if track.Disc == 0 {
		track.Disc = 1
	}
	if track.Title == "" {
		track.Title = strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
	}
	pathTrackTitle := track.Title
	pathTrackNumber := track.Track

	tagsApplied := false
	tagArtist := firstNonEmpty(tags.AlbumArtist, tags.Artist)
	if shouldUseMusicTagName(artist, tagArtist, source) {
		replaceMusicArtist(&artist, &disambig, tagArtist)
		tagsApplied = true
	}
	if shouldUseMusicTagText(album, tags.Album, source) {
		album = cleanMusicAlbumName(tags.Album)
		tagsApplied = true
	}
	if year == "" && tags.Year != "" {
		year = tags.Year
		tagsApplied = true
	}
	if tags.DiscNumber > 0 && (!track.DiscExplicit || track.Disc == 0) {
		track.Disc = tags.DiscNumber
		tagsApplied = true
	}
	if tags.TrackNumber > 0 && track.Track == 0 {
		track.Track = tags.TrackNumber
		tagsApplied = true
	}
	if shouldUseMusicTagTitle(track.Title, tags.Title, pathTrackNumber, pathTrackTitle) {
		track.Title = tags.Title
		tagsApplied = true
	}
	for k, v := range musicExternalIDsFromTags(tags) {
		externalIDs[k] = v
		tagsApplied = true
	}
	if tagsApplied {
		if source == "" || source == "path" || source == "plain_album_folder" {
			source = "tag"
		} else {
			source += "+tag"
		}
		confidence = maxFloat(confidence, 0.88)
	}

	if entry, ok := nfos[albumDir]; ok && entry.nfo != nil {
		localNFO = entry
		hasNFO = true
		if localNFO.nfo.AlbumArtist != "" {
			replaceMusicArtist(&artist, &disambig, localNFO.nfo.AlbumArtist)
		}
		if localNFO.nfo.Title != "" {
			album = cleanMusicAlbumName(localNFO.nfo.Title)
		}
		if localNFO.nfo.Year != "" {
			year = localNFO.nfo.Year
		}
		if localNFO.nfo.AlbumType != "" {
			releaseKind = localNFO.nfo.AlbumType
		}
		for k, v := range musicExternalIDsFromNFO(localNFO.nfo) {
			externalIDs[k] = v
		}
		if title := musicTrackTitleFromNFO(localNFO.nfo, track.Disc, track.Track); title != "" {
			track.Title = title
		}
		source = "nfo"
		confidence = maxFloat(confidence, 0.96)
	}

	var issues []string
	if artist == "" || looksLikeUnusableMusicIdentity(artist) {
		issues = append(issues, "missing_artist")
	}
	if album == "" || looksLikeUnusableMusicIdentity(album) {
		issues = append(issues, "missing_album")
	}
	if track.Track == 0 {
		issues = append(issues, "missing_track_number")
	}
	if source == "" {
		source = "path"
	}
	if confidence == 0 {
		confidence = 0.55
	}
	if len(issues) > 0 {
		confidence = minFloat(confidence, 0.45)
	}

	if contains(issues, "missing_artist") || contains(issues, "missing_album") {
		return MusicTrackPlan{}, false
	}

	plan := MusicTrackPlan{
		Artist:               strings.TrimSpace(artist),
		ArtistDisambiguation: strings.TrimSpace(disambig),
		Album:                strings.TrimSpace(album),
		Year:                 strings.TrimSpace(year),
		ReleaseKind:          normalizeMusicReleaseKind(releaseKind),
		ExternalIDs:          nonEmptyStringMap(externalIDs),
		DiscNumber:           track.Disc,
		TrackNumber:          track.Track,
		TrackTitle:           strings.TrimSpace(track.Title),
		RelPath:              file.RelPath,
		Format:               strings.TrimPrefix(strings.ToLower(file.Ext), "."),
		Source:               source,
		Confidence:           confidence,
		Issues:               issues,
	}
	if hasNFO {
		plan.NFO = localNFO.file.RelPath
	}
	plan.IdentityKeys = musicAlbumIdentityKeys(plan)
	plan.Key = plan.IdentityKeys[0]
	return plan, true
}

func groupMusicAlbums(tracks []MusicTrackPlan) []MusicAlbumPlan {
	var grouped []*MusicAlbumPlan
	conflictedIDs := map[*MusicAlbumPlan]map[string]bool{}
	for _, track := range tracks {
		album := musicAlbumGroup(track, grouped)
		if album == nil {
			key := uniqueMusicAlbumGroupKey(track, grouped)
			album = &MusicAlbumPlan{
				Key:                  key,
				Artist:               track.Artist,
				ArtistDisambiguation: track.ArtistDisambiguation,
				Album:                track.Album,
				Year:                 track.Year,
				ReleaseKind:          track.ReleaseKind,
				ExternalIDs:          copyMusicExternalIDs(track.ExternalIDs),
				Confidence:           track.Confidence,
			}
			grouped = append(grouped, album)
		}
		album.Tracks = append(album.Tracks, track)
		album.Files = append(album.Files, track.RelPath)
		album.Confidence = minFloat(album.Confidence, track.Confidence)
		for k, v := range track.ExternalIDs {
			if album.ExternalIDs == nil {
				album.ExternalIDs = map[string]string{}
			}
			if conflictedIDs[album][k] {
				continue
			}
			if current := album.ExternalIDs[k]; current != "" && !strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(v)) {
				delete(album.ExternalIDs, k)
				if conflictedIDs[album] == nil {
					conflictedIDs[album] = map[string]bool{}
				}
				conflictedIDs[album][k] = true
				if musicArtistExternalIDKey(k) {
					album.Issues = appendMusicIssue(album.Issues, "conflicting_"+k+"_ids")
				}
				continue
			}
			if album.ExternalIDs[k] == "" {
				album.ExternalIDs[k] = v
			}
		}
		if track.NFO != "" && !contains(album.NFOs, track.NFO) {
			album.NFOs = append(album.NFOs, track.NFO)
		}
		if track.Album != "" && track.Album != album.Album && !contains(album.Aliases, track.Album) {
			album.Aliases = append(album.Aliases, track.Album)
		}
		for _, issue := range track.Issues {
			if !contains(album.Issues, issue) {
				album.Issues = append(album.Issues, issue)
			}
		}
		if album.ReleaseKind == "" && track.ReleaseKind != "" {
			album.ReleaseKind = track.ReleaseKind
		}
	}
	albums := make([]MusicAlbumPlan, 0, len(grouped))
	for _, album := range grouped {
		sortMusicTracks(album.Tracks)
		sort.Strings(album.Files)
		sort.Strings(album.Aliases)
		sort.Strings(album.NFOs)
		sort.Strings(album.Issues)
		albums = append(albums, *album)
	}
	sort.Slice(albums, func(i, j int) bool {
		if albums[i].Artist == albums[j].Artist {
			if albums[i].Year == albums[j].Year {
				if albums[i].Album == albums[j].Album {
					return albums[i].Key < albums[j].Key
				}
				return albums[i].Album < albums[j].Album
			}
			return albums[i].Year < albums[j].Year
		}
		return albums[i].Artist < albums[j].Artist
	})
	return albums
}

func musicArtistExternalIDKey(key string) bool {
	switch key {
	case "musicbrainz_album_artist", "musicbrainz_artist", "itunes_artist", "apple_artist", "deezer_artist", "discogs_artist", "spotify_artist", "audiodb_artist":
		return true
	default:
		return false
	}
}

func musicAlbumGroup(track MusicTrackPlan, existing []*MusicAlbumPlan) *MusicAlbumPlan {
	var identityMatches []*MusicAlbumPlan
	for _, album := range existing {
		if !musicAlbumHardIDsCompatible(track.ExternalIDs, album.ExternalIDs) {
			continue
		}
		if musicAlbumIdentityKeysOverlap(track, *album) {
			identityMatches = append(identityMatches, album)
		}
	}
	if len(identityMatches) > 0 {
		return unambiguousMusicAlbumGroup(identityMatches)
	}
	var fuzzyMatches []*MusicAlbumPlan
	for _, album := range existing {
		if !musicAlbumHardIDsCompatible(track.ExternalIDs, album.ExternalIDs) ||
			!sameMusicArtist(track.Artist, album.Artist) || track.Year != album.Year {
			continue
		}
		if titlematch.FuzzyEqual(track.Album, album.Album) {
			fuzzyMatches = append(fuzzyMatches, album)
			continue
		}
		for _, alias := range album.Aliases {
			if titlematch.FuzzyEqual(track.Album, alias) {
				fuzzyMatches = append(fuzzyMatches, album)
				break
			}
		}
	}
	return unambiguousMusicAlbumGroup(fuzzyMatches)
}

func unambiguousMusicAlbumGroup(matches []*MusicAlbumPlan) *MusicAlbumPlan {
	if len(matches) == 1 {
		return matches[0]
	}
	// Once contradictory hard-ID groups exist for the same fallback identity,
	// an untagged track cannot safely choose either. Keep one shared no-ID
	// bucket for that ambiguous material instead of attaching it arbitrarily
	// (or creating a new album for every track).
	var withoutHardID *MusicAlbumPlan
	for _, album := range matches {
		if musicAlbumHasHardID(album.ExternalIDs) {
			continue
		}
		if withoutHardID != nil {
			return nil
		}
		withoutHardID = album
	}
	return withoutHardID
}

func musicAlbumHasHardID(ids map[string]string) bool {
	for _, key := range []string{
		"musicbrainz_release_group", "musicbrainz_album", "itunes_album",
		"audiodb_album", "deezer_album", "discogs_album", "spotify_album",
	} {
		if strings.TrimSpace(ids[key]) != "" {
			return true
		}
	}
	return false
}

func uniqueMusicAlbumGroupKey(track MusicTrackPlan, existing []*MusicAlbumPlan) string {
	used := map[string]bool{}
	for _, album := range existing {
		used[album.Key] = true
	}
	for _, key := range musicTrackAlbumIdentityKeys(track) {
		if key != "" && !used[key] {
			return key
		}
	}
	base := musicAlbumKey(track.Artist, track.Album, track.Year)
	if !used[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		key := fmt.Sprintf("%s|identity:%d", base, suffix)
		if !used[key] {
			return key
		}
	}
}

func musicAlbumIdentityKeysOverlap(track MusicTrackPlan, album MusicAlbumPlan) bool {
	albumKeys := musicAlbumPlanIdentityKeys(album)
	for _, trackKey := range musicTrackAlbumIdentityKeys(track) {
		for _, albumKey := range albumKeys {
			if trackKey != "" && strings.EqualFold(trackKey, albumKey) {
				return true
			}
		}
	}
	return false
}

func musicTrackAlbumIdentityKeys(track MusicTrackPlan) []string {
	if len(track.IdentityKeys) > 0 {
		return track.IdentityKeys
	}
	return musicAlbumIdentityKeys(track)
}

func musicAlbumPlanIdentityKeys(album MusicAlbumPlan) []string {
	plan := MusicTrackPlan{
		Artist: album.Artist, Album: album.Album, Year: album.Year,
		ExternalIDs: album.ExternalIDs,
	}
	keys := musicAlbumIdentityKeys(plan)
	for _, alias := range album.Aliases {
		key := musicAlbumKey(album.Artist, alias, album.Year)
		if !contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func musicAlbumHardIDsCompatible(left, right map[string]string) bool {
	leftGroup := strings.TrimSpace(left["musicbrainz_release_group"])
	rightGroup := strings.TrimSpace(right["musicbrainz_release_group"])
	if leftGroup != "" && rightGroup != "" {
		// Issued release IDs (and provider edition IDs) may legitimately differ
		// inside one MusicBrainz release group. The shared group is the stronger
		// identity; contradictory group IDs are an unconditional cannot-link.
		return strings.EqualFold(leftGroup, rightGroup)
	}
	for _, key := range []string{
		"musicbrainz_album", "itunes_album",
		"audiodb_album", "deezer_album", "discogs_album", "spotify_album",
	} {
		leftID := strings.TrimSpace(left[key])
		rightID := strings.TrimSpace(right[key])
		if leftID != "" && rightID != "" && !strings.EqualFold(leftID, rightID) {
			return false
		}
	}
	return true
}

func sameMusicArtist(a, b string) bool {
	return normalizeMusicKeyPart(a) == normalizeMusicKeyPart(b)
}

func groupMusicArtists(albums []MusicAlbumPlan) []MusicArtistPlan {
	var grouped []*MusicArtistPlan
	assigned := make([]bool, len(albums))
	groupMBID := map[*MusicArtistPlan]string{}
	unidentifiedAlbumKey := map[*MusicArtistPlan]string{}
	byMBID := map[string]*MusicArtistPlan{}
	byName := map[string][]*MusicArtistPlan{}
	appendAlbum := func(artist *MusicArtistPlan, album MusicAlbumPlan) {
		artist.Albums = append(artist.Albums, album)
		artist.Files = append(artist.Files, album.Files...)
		artist.Confidence = minFloat(artist.Confidence, album.Confidence)
		for _, issue := range album.Issues {
			if !contains(artist.Issues, issue) {
				artist.Issues = append(artist.Issues, issue)
			}
		}
		nameKey := musicArtistKey(album.Artist, album.ArtistDisambiguation)
		if !containsMusicArtistGroup(byName[nameKey], artist) {
			byName[nameKey] = append(byName[nameKey], artist)
		}
	}
	newGroup := func(album MusicAlbumPlan, mbid string) *MusicArtistPlan {
		artist := &MusicArtistPlan{
			Artist:               album.Artist,
			ArtistDisambiguation: album.ArtistDisambiguation,
			Confidence:           album.Confidence,
		}
		grouped = append(grouped, artist)
		if mbid != "" {
			groupMBID[artist] = mbid
			byMBID[mbid] = artist
		}
		appendAlbum(artist, album)
		return artist
	}

	// A single MusicBrainz artist ID is the strongest local grouping spine.
	// Process it first so spelling/localization drift cannot split one artist,
	// while identical names carrying different MBIDs can never be collapsed.
	for i, album := range albums {
		mbid := trustworthyMusicArtistMBID(album)
		if mbid == "" {
			continue
		}
		artist := byMBID[mbid]
		if artist == nil {
			newGroup(album, mbid)
			assigned[i] = true
			continue
		}
		appendAlbum(artist, album)
		assigned[i] = true
	}

	// A collaborative album may carry several album-artist IDs. Attach it only
	// when exactly one of those IDs is already an independently established
	// local artist spine; this preserves e.g. Alex Mind's solo + collaborations
	// without arbitrarily choosing a member from a collaboration-only scope.
	for i, album := range albums {
		if assigned[i] {
			continue
		}
		albumArtistIDs := splitMusicProviderIDs(album.ExternalIDs["musicbrainz_album_artist"])
		var candidate *MusicArtistPlan
		ambiguous := false
		for mbid := range albumArtistIDs {
			artist := byMBID[mbid]
			if artist == nil {
				continue
			}
			if candidate != nil && candidate != artist {
				ambiguous = true
				break
			}
			candidate = artist
		}
		if candidate == nil || ambiguous {
			continue
		}
		appendAlbum(candidate, album)
		assigned[i] = true
	}

	// Snapshot the names backed by an authoritative album-artist MBID before
	// adding any name-only groups. A loose release sharing one of those names
	// is not safe to attach to the sole known MBID: LISA and LiSA demonstrate
	// that case differs while our normalised name key intentionally does not.
	identifiedByName := make(map[string][]*MusicArtistPlan, len(byName))
	for nameKey, candidates := range byName {
		identifiedByName[nameKey] = append([]*MusicArtistPlan(nil), candidates...)
	}

	// Track credits are weaker than Picard's album-artist identity and one bad
	// tag must never select a namesake. They become useful corroboration when
	// at least two independent releases agree with an authoritative artist
	// spine already present in this same owner scope. This converges the common
	// partial-tagging shape without allowing a lone poisoned release to attach.
	type trackArtistConsensus struct {
		artist   *MusicArtistPlan
		albums   []int
		releases map[string]struct{}
	}
	trackConsensus := map[string]*trackArtistConsensus{}
	for i, album := range albums {
		if assigned[i] || !musicTrackArtistConsensusAllowed(album.Artist) {
			continue
		}
		mbid := singleMusicProviderID(album.ExternalIDs["musicbrainz_artist"])
		candidate := byMBID[mbid]
		if candidate == nil || album.Artist != candidate.Artist ||
			album.ArtistDisambiguation != candidate.ArtistDisambiguation {
			continue
		}
		key := musicArtistKey(album.Artist, album.ArtistDisambiguation) + "\x00" + mbid
		consensus := trackConsensus[key]
		if consensus == nil {
			consensus = &trackArtistConsensus{artist: candidate, releases: map[string]struct{}{}}
			trackConsensus[key] = consensus
		}
		consensus.albums = append(consensus.albums, i)
		consensus.releases[firstNonEmpty(album.Key, musicAlbumKey(album.Artist, album.Album, album.Year))] = struct{}{}
	}
	for _, consensus := range trackConsensus {
		if len(consensus.releases) < 2 {
			continue
		}
		for _, i := range consensus.albums {
			appendAlbum(consensus.artist, albums[i])
			assigned[i] = true
		}
	}

	nameOnly := map[string]*MusicArtistPlan{}
	for i, album := range albums {
		if assigned[i] {
			continue
		}
		nameKey := musicArtistKey(album.Artist, album.ArtistDisambiguation)
		identifiedCandidates := identifiedByName[nameKey]
		if len(identifiedCandidates) == 1 &&
			strings.TrimSpace(album.ExternalIDs["musicbrainz_artist"]) == "" &&
			album.Artist == identifiedCandidates[0].Artist &&
			album.ArtistDisambiguation == identifiedCandidates[0].ArtistDisambiguation {
			// Truly untagged sibling releases can inherit one exact, case-sensitive
			// local owner. A track-level artist ID, even a matching one, deliberately
			// disables this shortcut so acoustic evidence gets a chance to challenge
			// a poisoned tag.
			appendAlbum(identifiedCandidates[0], album)
			assigned[i] = true
			continue
		}
		if len(identifiedCandidates) > 0 {
			// Keep every unidentified release independently matchable. Search can
			// then use its own release hints and, when necessary, Chromaprint to
			// converge several plans back onto the same canonical artist. Grouping
			// them here would let one mixed namesake release block all the others.
			artist := newGroup(album, "")
			unidentifiedAlbumKey[artist] = firstNonEmpty(album.Key, musicAlbumKey(album.Artist, album.Album, album.Year))
			artist.Issues = appendMusicIssue(artist.Issues, "ambiguous_artist_identity_missing_album_artist_mbid")
			continue
		}
		artist := nameOnly[nameKey]
		if artist == nil {
			artist = newGroup(album, "")
			nameOnly[nameKey] = artist
			if strings.TrimSpace(album.ExternalIDs["musicbrainz_artist"]) != "" {
				artist.Issues = appendMusicIssue(artist.Issues, "untrusted_track_artist_mbid")
			}
			continue
		}
		appendAlbum(artist, album)
		if strings.TrimSpace(album.ExternalIDs["musicbrainz_artist"]) != "" {
			artist.Issues = appendMusicIssue(artist.Issues, "untrusted_track_artist_mbid")
		}
	}

	artists := make([]MusicArtistPlan, 0, len(grouped))
	for _, artist := range grouped {
		baseKey := musicArtistKey(artist.Artist, artist.ArtistDisambiguation)
		artist.Key = baseKey
		// A name is not an artist identity. Keep the authoritative MBID in the
		// durable key even when this particular scope contains only one artist.
		// Otherwise LISA and LiSA both become artist:lisa in their clean owner
		// folders and a decision made for one scope can poison the other later.
		if mbid := groupMBID[artist]; mbid != "" {
			artist.Key += "|mbid:" + mbid
		} else if albumKey := unidentifiedAlbumKey[artist]; albumKey != "" {
			artist.Key += "|unidentified_album:" + albumKey
		} else if len(byName[baseKey]) > 1 {
			artist.Key += "|unidentified"
		}
		artist.ExternalIDs = consistentMusicArtistExternalIDs(artist.Albums, &artist.Issues)
		sortMusicAlbums(artist.Albums)
		sort.Strings(artist.Files)
		sort.Strings(artist.Issues)
		artists = append(artists, *artist)
	}
	sort.Slice(artists, func(i, j int) bool {
		if artists[i].Artist == artists[j].Artist {
			return artists[i].Key < artists[j].Key
		}
		return artists[i].Artist < artists[j].Artist
	})
	return artists
}

func containsMusicArtistGroup(groups []*MusicArtistPlan, target *MusicArtistPlan) bool {
	for _, group := range groups {
		if group == target {
			return true
		}
	}
	return false
}

func trustworthyMusicArtistMBID(album MusicAlbumPlan) string {
	return singleMusicProviderID(musicArtistExternalIDsFromAlbum(album)["mbid"])
}

func singleMusicProviderID(value string) string {
	ids := splitMusicProviderIDs(value)
	if len(ids) != 1 {
		return ""
	}
	for id := range ids {
		return strings.ToLower(strings.TrimSpace(id))
	}
	return ""
}

func musicTrackArtistConsensusAllowed(artist string) bool {
	if musicPrimaryCollaborationArtist(artist) != "" {
		return false
	}
	switch normalizeMusicKeyPart(artist) {
	case "various artists", "various", "va", "unknown artist", "unknown", "soundtrack", "original soundtrack":
		return false
	default:
		return true
	}
}

// replaceMusicArtist keeps a folder disambiguation attached to the artist it
// actually described. Tags and NFOs can legitimately replace a folder artist
// (compilations and misfiled releases are common); carrying the old folder's
// qualifier onto the replacement creates a new, false local identity.
func replaceMusicArtist(artist, disambiguation *string, replacement string) {
	replacement = strings.TrimSpace(replacement)
	if replacement == "" {
		return
	}
	if strings.TrimSpace(*disambiguation) != "" &&
		normalizeMusicKeyPart(*artist) != normalizeMusicKeyPart(replacement) {
		*disambiguation = ""
	}
	*artist = replacement
}

// consistentMusicArtistExternalIDs promotes only artist identifiers that do
// not disagree across the albums grouped into one local artist. The previous
// first-value-wins behaviour paired arbitrary MusicBrainz and Apple IDs and
// turned inconsistent local evidence into a misleading upstream conflict.
func consistentMusicArtistExternalIDs(albums []MusicAlbumPlan, issues *[]string) map[string]string {
	values := map[string][]map[string]struct{}{}
	for _, album := range albums {
		for provider, value := range musicArtistExternalIDsFromAlbum(album) {
			providerIDs := splitMusicProviderIDs(value)
			if len(providerIDs) == 0 {
				continue
			}
			values[provider] = append(values[provider], providerIDs)
		}
	}

	result := map[string]string{}
	for _, provider := range []string{"mbid", "apple"} {
		providerSets := values[provider]
		if len(providerSets) == 0 {
			continue
		}
		intersection := make(map[string]struct{}, len(providerSets[0]))
		for value := range providerSets[0] {
			intersection[value] = struct{}{}
		}
		for _, providerSet := range providerSets[1:] {
			for value := range intersection {
				if _, present := providerSet[value]; !present {
					delete(intersection, value)
				}
			}
		}
		if len(intersection) == 1 {
			for value := range intersection {
				result[provider] = value
			}
		} else {
			issue := "conflicting_artist_" + provider + "_ids"
			if !contains(*issues, issue) {
				*issues = append(*issues, issue)
			}
		}
	}
	return nonEmptyStringMap(result)
}

func splitMusicProviderIDs(value string) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' }) {
		if id := strings.ToLower(strings.TrimSpace(part)); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func musicArtistExternalIDsFromAlbum(album MusicAlbumPlan) map[string]string {
	ids := map[string]string{}
	// MUSICBRAINZ_ARTISTID describes the performing track credit, not the
	// album artist. Promoting it here lets one mistagged loose track poison the
	// identity of an entire artist before acoustic evidence is considered.
	// Picard's MUSICBRAINZ_ALBUMARTISTID is the artist-level grouping spine.
	if album.ExternalIDs["musicbrainz_album_artist"] != "" {
		ids["mbid"] = album.ExternalIDs["musicbrainz_album_artist"]
	}
	if album.ExternalIDs["itunes_artist"] != "" {
		ids["apple"] = album.ExternalIDs["itunes_artist"]
	}
	return nonEmptyStringMap(ids)
}

func parseMusicAlbumFolder(folder, fallbackArtist string) musicAlbumFolderInfo {
	name := strings.TrimSpace(folder)
	if name == "" {
		return musicAlbumFolderInfo{}
	}
	if m := musicStructuredAlbumRE.FindStringSubmatch(name); m != nil {
		return musicAlbumFolderInfo{
			Artist:      strings.TrimSpace(m[1]),
			ReleaseKind: normalizeMusicReleaseKind(m[2]),
			Year:        normalizeMusicYear(m[3]),
			Album:       strings.TrimSpace(m[4]),
			Source:      "curated_folder",
			Confidence:  0.92,
		}
	}
	if m := musicSceneCatalogRE.FindStringSubmatch(name); m != nil {
		return musicAlbumFolderInfo{
			Artist:      strings.TrimSpace(m[1]),
			Album:       strings.TrimSpace(strings.TrimSuffix(m[2], "-")),
			ReleaseKind: normalizeMusicReleaseKind(m[3]),
			Year:        m[4],
			Source:      "scene_folder",
			Confidence:  0.72,
		}
	}
	if m := musicArtistAlbumYearTailRE.FindStringSubmatch(name); m != nil {
		return musicAlbumFolderInfo{
			Artist:     strings.TrimSpace(m[1]),
			Album:      strings.TrimSpace(m[2]),
			Year:       m[3],
			Source:     "artist_album_year_folder",
			Confidence: 0.66,
		}
	}
	if m := musicYearPrefixRE.FindStringSubmatch(name); m != nil {
		return musicAlbumFolderInfo{
			Artist:     fallbackArtist,
			Year:       m[1],
			Album:      strings.TrimSpace(m[2]),
			Source:     "year_album_folder",
			Confidence: 0.68,
		}
	}
	if m := musicTitleYearRE.FindStringSubmatch(name); m != nil {
		return musicAlbumFolderInfo{
			Artist:     fallbackArtist,
			Album:      strings.TrimSpace(m[1]),
			Year:       m[2],
			Source:     "album_year_folder",
			Confidence: 0.66,
		}
	}
	if strings.Contains(name, " - ") {
		left, right, _ := strings.Cut(name, " - ")
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		if fallbackArtist == "" || strings.EqualFold(left, fallbackArtist) {
			return musicAlbumFolderInfo{Artist: left, Album: right, Source: "artist_album_folder", Confidence: 0.62}
		}
	}
	return musicAlbumFolderInfo{Artist: fallbackArtist, Album: name, Source: "plain_album_folder", Confidence: 0.55}
}

func parseMusicNFOs(root InventoryRoot, emit Emitter) map[string]musicNFOEntry {
	out := make(map[string]musicNFOEntry)
	for _, file := range root.Files {
		if file.Generated || file.Class != ClassNFO || file.Kind != "album" {
			continue
		}
		parsed := nfo.ParseFile(root.FS, file.RelPath, file.Kind)
		if parsed == nil {
			emit.Emit(Event{
				Event:    "nfo.parse_failed",
				Severity: SeverityWarn,
				RelPath:  file.RelPath,
				Message:  "album NFO could not be parsed",
			})
			continue
		}
		dir := filepath.Dir(file.RelPath)
		if dir == "." {
			dir = ""
		}
		out[filepath.ToSlash(dir)] = musicNFOEntry{file: file, nfo: parsed}
		emit.Emit(Event{
			Event:   "nfo.parsed",
			RelPath: file.RelPath,
			Data: map[string]any{
				"kind":  parsed.Kind,
				"title": parsed.Title,
				"ids":   musicExternalIDsFromNFO(parsed),
			},
		})
	}
	return out
}

func musicExternalIDsFromNFO(parsed *nfo.ParsedNFO) map[string]string {
	if parsed == nil {
		return nil
	}
	ids := map[string]string{}
	if parsed.MBAlbumID != "" {
		ids["musicbrainz_album"] = parsed.MBAlbumID
	}
	if parsed.MBReleaseGroupID != "" {
		ids["musicbrainz_release_group"] = parsed.MBReleaseGroupID
	}
	if parsed.MBAlbumArtistID != "" {
		ids["musicbrainz_album_artist"] = parsed.MBAlbumArtistID
	}
	if parsed.AudioDBAlbumID != "" {
		ids["audiodb_album"] = parsed.AudioDBAlbumID
	}
	if parsed.AudioDBArtistID != "" {
		ids["audiodb_artist"] = parsed.AudioDBArtistID
	}
	if parsed.ITunesAlbumID != "" {
		ids["itunes_album"] = parsed.ITunesAlbumID
	}
	if parsed.ITunesArtistID != "" {
		ids["itunes_artist"] = parsed.ITunesArtistID
	}
	return nonEmptyStringMap(ids)
}

func musicExternalIDsFromTags(tags audiotags.Tags) map[string]string {
	ids := map[string]string{}
	if tags.AlbumMBID != "" {
		ids["musicbrainz_album"] = tags.AlbumMBID
	}
	if tags.ReleaseGroupMBID != "" {
		ids["musicbrainz_release_group"] = tags.ReleaseGroupMBID
	}
	if tags.AlbumArtistMBID != "" {
		ids["musicbrainz_album_artist"] = tags.AlbumArtistMBID
	}
	if tags.ArtistMBID != "" {
		ids["musicbrainz_artist"] = tags.ArtistMBID
	}
	return nonEmptyStringMap(ids)
}

func musicTrackTitleFromNFO(parsed *nfo.ParsedNFO, disc, track int) string {
	if parsed == nil || track == 0 {
		return ""
	}
	if disc == 0 {
		disc = 1
	}
	for _, nfoTrack := range parsed.Tracks {
		nfoDisc := nfoTrack.Disc
		if nfoDisc == 0 {
			nfoDisc = 1
		}
		if nfoDisc == disc && nfoTrack.Position == track && strings.TrimSpace(nfoTrack.Title) != "" {
			return strings.TrimSpace(nfoTrack.Title)
		}
	}
	return ""
}

func musicAlbumIdentityKeys(plan MusicTrackPlan) []string {
	var keys []string
	add := func(key string) {
		if key != "" && !contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if plan.ExternalIDs["musicbrainz_release_group"] != "" {
		add("musicbrainz_release_group:" + plan.ExternalIDs["musicbrainz_release_group"])
	}
	if plan.ExternalIDs["musicbrainz_album"] != "" {
		add("musicbrainz_album:" + plan.ExternalIDs["musicbrainz_album"])
	}
	if plan.ExternalIDs["itunes_album"] != "" {
		add("itunes_album:" + plan.ExternalIDs["itunes_album"])
	}
	if plan.ExternalIDs["audiodb_album"] != "" {
		add("audiodb_album:" + plan.ExternalIDs["audiodb_album"])
	}
	if plan.ExternalIDs["deezer_album"] != "" {
		add("deezer_album:" + plan.ExternalIDs["deezer_album"])
	}
	if plan.ExternalIDs["discogs_album"] != "" {
		add("discogs_album:" + plan.ExternalIDs["discogs_album"])
	}
	if plan.ExternalIDs["spotify_album"] != "" {
		add("spotify_album:" + plan.ExternalIDs["spotify_album"])
	}
	add(musicAlbumKey(plan.Artist, plan.Album, plan.Year))
	return keys
}

func nonEmptyStringMap(values map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		if k != "" && v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func copyMusicExternalIDs(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

func parseMusicTrackFilename(name string) musicTrackInfo {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(base)
	if m := musicFourDigitTrackRE.FindStringSubmatch(base); m != nil {
		disc, _ := strconv.Atoi(m[1])
		track, _ := strconv.Atoi(m[2])
		if disc == 0 {
			disc = 1
		}
		return musicTrackInfo{Disc: disc, DiscExplicit: true, Track: track, Title: strings.TrimSpace(m[3])}
	}
	if m := musicTwoDigitTrackRE.FindStringSubmatch(base); m != nil {
		track, _ := strconv.Atoi(m[1])
		return musicTrackInfo{Disc: 1, Track: track, Title: strings.TrimSpace(m[2])}
	}
	if m := musicTrackArtistTitleDashRE.FindStringSubmatch(base); m != nil {
		return musicTrackInfo{Title: strings.TrimSpace(m[2])}
	}
	return musicTrackInfo{Title: base}
}

func musicDiscFromFolder(folder string) (bool, int) {
	if m := musicDiscFolderRE.FindStringSubmatch(strings.TrimSpace(folder)); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			n = 1
		}
		return true, n
	}
	return false, 0
}

func splitMusicArtistFolder(folder string) (artist, disambig string) {
	name := strings.TrimSpace(folder)
	if name == "" {
		return "", ""
	}
	if strings.HasPrefix(name, ".") {
		return "", ""
	}
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		return strings.TrimSpace(name[:i]), strings.TrimSuffix(strings.TrimSpace(name[i+2:]), ")")
	}
	return name, ""
}

func normalizeMusicReleaseKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "album":
		return "album"
	case "ep":
		return "ep"
	case "single":
		return "single"
	case "compilation":
		return "compilation"
	case "soundtrack", "ost":
		return "soundtrack"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeMusicYear(value string) string {
	if value == "1" {
		return ""
	}
	return value
}

func musicAlbumKey(artist, album, year string) string {
	base := fmt.Sprintf("artist_album:%s|%s", normalizeMusicKeyPart(artist), normalizeMusicKeyPart(album))
	if year != "" {
		base += "|" + year
	}
	return base
}

func musicArtistKey(artist, disambig string) string {
	key := "artist:" + normalizeMusicKeyPart(artist)
	if disambig != "" {
		key += "|" + normalizeMusicKeyPart(disambig)
	}
	return key
}

func normalizeMusicKeyPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(slug.Transliterate(value)))
	value = musicKeyPartRE.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func looksLikeUnusableMusicIdentity(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	v := normalizeMusicKeyPart(value)
	switch v {
	case "track", "unknown", "unknown artist", "untitled", "absolutely cursed audio", "loose tracks":
		return true
	default:
		return false
	}
}

func cleanMusicAlbumName(value string) string {
	name := strings.TrimSpace(value)
	for {
		next := strings.TrimSpace(musicTrailingFormatTagRE.ReplaceAllString(name, ""))
		if next == name {
			break
		}
		name = next
	}
	if m := musicTitleYearRE.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1])
	}
	return name
}

func shouldUseMusicReleaseAlbum(current, releaseAlbum, source string) bool {
	if releaseAlbum == "" {
		return false
	}
	if current == "" {
		return true
	}
	switch source {
	case "", "path", "plain_album_folder":
		return true
	default:
		return normalizeMusicKeyPart(current) == normalizeMusicKeyPart(releaseAlbum)
	}
}

func shouldUseMusicTagName(current, tagged, source string) bool {
	tagged = strings.TrimSpace(tagged)
	if audiotags.IsPlaceholderName(tagged) {
		return false
	}
	current = strings.TrimSpace(current)
	if current == "" || looksLikeUnusableMusicIdentity(current) {
		return true
	}
	if titlematch.FuzzyEqual(current, tagged) {
		return false
	}
	return source == "" || source == "path" || source == "plain_album_folder"
}

func shouldUseMusicTagText(current, tagged, source string) bool {
	tagged = strings.TrimSpace(tagged)
	if audiotags.IsPlaceholderValue(tagged) {
		return false
	}
	current = strings.TrimSpace(current)
	if current == "" || looksLikeUnusableMusicIdentity(current) {
		return true
	}
	if titlematch.FuzzyEqual(current, tagged) {
		return false
	}
	return source == "" || source == "path" || source == "plain_album_folder"
}

func shouldUseMusicTagTitle(current, tagged string, pathTrackNumber int, pathTrackTitle string) bool {
	tagged = strings.TrimSpace(tagged)
	if audiotags.IsPlaceholderValue(tagged) || musicSyntheticProbeTitleRE.MatchString(tagged) {
		return false
	}
	current = strings.TrimSpace(current)
	if current == "" || audiotags.IsPlaceholderValue(current) {
		return true
	}
	if pathTrackNumber == 0 && current == strings.TrimSpace(pathTrackTitle) && !strings.Contains(current, " - ") {
		return true
	}
	return false
}

func musicLocalTrackTitleWeak(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || audiotags.IsPlaceholderValue(title) {
		return true
	}
	return musicSyntheticProbeTitleRE.MatchString(title) || musicWeakTrackTitleRE.MatchString(title)
}

func splitRelPath(path string) []string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

func parentSegment(segments []string) string {
	if len(segments) < 2 {
		return ""
	}
	return segments[len(segments)-2]
}

func sortMusicTracks(tracks []MusicTrackPlan) {
	sort.Slice(tracks, func(i, j int) bool {
		if tracks[i].Artist == tracks[j].Artist {
			if tracks[i].Album == tracks[j].Album {
				if tracks[i].DiscNumber == tracks[j].DiscNumber {
					if tracks[i].TrackNumber == tracks[j].TrackNumber {
						return tracks[i].RelPath < tracks[j].RelPath
					}
					return tracks[i].TrackNumber < tracks[j].TrackNumber
				}
				return tracks[i].DiscNumber < tracks[j].DiscNumber
			}
			return tracks[i].Album < tracks[j].Album
		}
		return tracks[i].Artist < tracks[j].Artist
	})
}

func sortMusicAlbums(albums []MusicAlbumPlan) {
	sort.Slice(albums, func(i, j int) bool {
		if albums[i].Year == albums[j].Year {
			if albums[i].Album == albums[j].Album {
				return albums[i].Key < albums[j].Key
			}
			return albums[i].Album < albums[j].Album
		}
		return albums[i].Year < albums[j].Year
	})
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
