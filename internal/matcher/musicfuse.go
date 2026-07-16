package matcher

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/musicconsensus"
	"github.com/karbowiak/heya/internal/titlematch"
)

// Local-signal fusion for music. A music release carries up to three local
// sources of truth for its artist / album / track identity: the on-disk path
// (folder + filename), the embedded ID3v2 / Vorbis tags, and a sidecar NFO.
// NFO stays authoritative (curated), but path and tags are fused per-field —
// because either can lie. A curated folder is usually right; a scene dump like
//
//	someartist-somealbum-2015-releasegroupqualityblabla_asdf_fdsa_mp3_256k/01.mp3
//
// has a useless path but clean tags, while an auto-ripper leaves "Unknown
// Artist" / "Track 01" tags on a perfectly-foldered album. So we weight the two
// sources, lean slightly on the path (a human foldered it on purpose), and let
// the weaker source lose when it is visibly untrustworthy.
const (
	// basePathTrust / baseTagTrust encode the 55/45 lean: on a straight
	// disagreement with both sources looking equally healthy, the path wins.
	basePathTrust = 0.55
	baseTagTrust  = 0.45
	// minSourceTrust is the floor a decayed source keeps, so it can still beat
	// a genuinely empty other side (empty contributes 0).
	minSourceTrust = 0.10
	// agreementBoost rewards path and tags corroborating each other.
	agreementBoost = 0.25
)

// signalSource records which local source won a fused field. Used for logging
// and to translate into media_items.field_provenance (path/tag/both all map to
// the "local" provenance bucket; NFO does too — provenance only distinguishes
// local vs remote vs user).
type signalSource uint8

const (
	sourceNone signalSource = iota
	sourcePath
	sourceTag
	sourceBoth
	sourceNFO
)

func (s signalSource) String() string {
	switch s {
	case sourcePath:
		return "path"
	case sourceTag:
		return "tag"
	case sourceBoth:
		return "both"
	case sourceNFO:
		return "nfo"
	default:
		return ""
	}
}

// fusedText is a resolved text field: the chosen value, which source it came
// from, and a 0..1 confidence.
type fusedText struct {
	Value      string
	Source     signalSource
	Confidence float64
}

// musicTags is the embedded-tag view of one audio file, already extracted and
// parsed. The zero value means "no usable tags" and makes every fusion fall
// back to path/NFO — i.e. exactly the pre-fusion behaviour.
type musicTags struct {
	Artist          string // track performer (TPE1 / ARTIST)
	AlbumArtist     string // release artist (TPE2 / ALBUMARTIST) — preferred for grouping
	Album           string
	Title           string
	Year            string
	TrackNumber     int
	TrackTotal      int
	DiscNumber      int
	ArtistMBID      string // track performer (musicbrainz_artistid)
	AlbumArtistMBID string // release artist (musicbrainz_albumartistid) — matches AlbumArtist
	AlbumMBID       string // per-edition RELEASE MBID only (never the release-group id)
}

func firstNonEmptyMusicTag(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func matcherMusicConsensusEvidence(tags musicTags) musicconsensus.Evidence {
	artist := firstNonEmptyMusicTag(tags.AlbumArtist, tags.Artist)
	if !isUsableArtist(artist) {
		artist = ""
	}
	album := strings.TrimSpace(tags.Album)
	if isPlaceholderValue(album) {
		album = ""
	}
	return musicconsensus.Evidence{Artist: artist, Album: album, Year: strings.TrimSpace(tags.Year)}
}

// applyMatcherMusicConsensus turns the release-level winners back into the
// lead-tag shape consumed by the existing fusion pipeline. Hard identifiers
// are selected only from tracks that support the winning names; a dissenting
// track can therefore never smuggle its artist/release MBID into the winner.
func applyMatcherMusicConsensus(lead musicTags, all []musicTags, consensus musicconsensus.Release) musicTags {
	out := lead
	// Start from a track that actually supports the winning release identity.
	// This prevents non-consensus fields (notably YEAR) from leaking out of a
	// poisoned lead file when only artist or album reached the threshold.
	if consensus.Artist.Strong {
		for _, tags := range all {
			if consensus.Artist.Matches(firstNonEmptyMusicTag(tags.AlbumArtist, tags.Artist)) {
				out = tags
				break
			}
		}
	} else if consensus.Album.Strong {
		for _, tags := range all {
			if consensus.Album.Matches(tags.Album) {
				out = tags
				break
			}
		}
	}
	if consensus.Artist.Strong {
		out.Artist = consensus.Artist.Value
		out.AlbumArtist = consensus.Artist.Value
		out.ArtistMBID = ""
		out.AlbumArtistMBID = ""
		for _, tags := range all {
			switch {
			case tags.AlbumArtist != "" && consensus.Artist.Matches(tags.AlbumArtist) && looksLikeMBID(tags.AlbumArtistMBID):
				out.AlbumArtistMBID = tags.AlbumArtistMBID
			case tags.Artist != "" && consensus.Artist.Matches(tags.Artist) && looksLikeMBID(tags.ArtistMBID):
				out.AlbumArtistMBID = tags.ArtistMBID
			}
			if out.AlbumArtistMBID != "" {
				out.ArtistMBID = out.AlbumArtistMBID
				break
			}
		}
	}
	if consensus.Album.Strong {
		out.Album = consensus.Album.Value
		out.AlbumMBID = ""
		for _, tags := range all {
			artist := firstNonEmptyMusicTag(tags.AlbumArtist, tags.Artist)
			artistSafe := !consensus.Artist.Strong || consensus.Artist.Matches(artist)
			if artistSafe && consensus.Album.Matches(tags.Album) && looksLikeMBID(tags.AlbumMBID) {
				out.AlbumMBID = tags.AlbumMBID
				break
			}
		}
	}
	if consensus.Year.Strong {
		out.Year = consensus.Year.Value
	}
	return out
}

// collectAudioTags merges the primary audio stream's tags with the container
// (format) tags for one probed file, format-level winning on conflict. FLAC /
// Vorbis and MP3 / ID3 both land tags at the format level; a few containers
// only expose them on the audio stream, so we read both.
func collectAudioTags(info *mediaprobe.MediaInfo) map[string]string {
	if info == nil {
		return nil
	}
	merged := map[string]string{}
	if a := mediaprobe.PrimaryAudio(info); a != nil {
		for k, v := range a.Tags {
			merged[k] = v
		}
	}
	for k, v := range info.Format.Tags {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// extractMusicTags pulls the identity fields out of a raw ffprobe tag map,
// tolerating the casing and naming drift between taggers (ARTIST vs artist,
// ALBUMARTIST vs "album_artist" vs "ALBUM ARTIST", TRCK vs TRACKNUMBER).
func extractMusicTags(raw map[string]string) musicTags {
	if len(raw) == 0 {
		return musicTags{}
	}
	n := normalizeTagKeys(raw)
	trackNum, trackTotal := parseSlashInt(firstTag(n, "tracknumber", "track", "trck", "trackno"))
	discNum, _ := parseSlashInt(firstTag(n, "discnumber", "disc", "discno", "tpos"))
	return musicTags{
		Artist:          firstTag(n, "artist", "tpe1"),
		AlbumArtist:     firstTag(n, "album_artist", "albumartist", "album artist", "tpe2"),
		Album:           firstTag(n, "album", "talb"),
		Title:           firstTag(n, "title", "tit2"),
		Year:            extractYear(firstTag(n, "originalyear", "originaldate", "date", "year", "tdrc", "tyer", "tdor")),
		TrackNumber:     trackNum,
		TrackTotal:      trackTotal,
		DiscNumber:      discNum,
		ArtistMBID:      firstTag(n, "musicbrainz_artistid", "musicbrainz artist id", "musicbrainzartistid"),
		AlbumArtistMBID: firstTag(n, "musicbrainz_albumartistid", "musicbrainz album artist id", "musicbrainzalbumartistid"),
		// RELEASE MBID only. The release-group id is deliberately excluded: it
		// is shared by every edition/pressing of a work, so feeding it to the
		// global album MBID dedup would collapse distinct editions into one.
		AlbumMBID: firstTag(n, "musicbrainz_albumid", "musicbrainz album id", "musicbrainzalbumid"),
	}
}

// normalizeTagKeys lower-cases and trims tag keys/values into a fresh map so
// lookups are a single direct hit rather than an O(n) case-fold scan per field.
func normalizeTagKeys(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		// First writer wins for a normalized-key collision — ffprobe rarely
		// emits two casings of the same key, but if it does, keep the first.
		if _, seen := out[key]; !seen {
			out[key] = val
		}
	}
	return out
}

// firstTag returns the first non-empty value among the candidate keys.
func firstTag(norm map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := norm[k]; v != "" {
			return v
		}
	}
	return ""
}

var slashIntRE = regexp.MustCompile(`^\s*(\d+)(?:\s*/\s*(\d+))?`)

// parseSlashInt reads a leading integer and an optional "/ total" suffix, the
// shape ID3 TRCK / TPOS use ("3", "03", "3/12", "1/2"). Returns (0, 0) on no
// leading digits.
func parseSlashInt(s string) (num, total int) {
	m := slashIntRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, 0
	}
	num, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		total, _ = strconv.Atoi(m[2])
	}
	return num, total
}

var yearRE = regexp.MustCompile(`(?:19|20)\d{2}`)

// extractYear pulls a 4-digit year out of a DATE / YEAR tag, which may be a
// bare year, an ISO date ("2015-03-01"), or a partial ("2015-03").
func extractYear(s string) string {
	return yearRE.FindString(s)
}

// sceneNoiseRE matches codec / encoding tokens that mark a folder name as a
// scene dump rather than a curated title. Deliberately limited to UNAMBIGUOUS
// codec/encoding markers: ambiguous English words that legitimately appear in
// album titles (cd, web, tape, vinyl, bit, tidal, proper, reissue, remux, …)
// are excluded so a real album like "Fiona Apple - Tidal" or "Proper Dose"
// isn't wrongly down-weighted. The stronger scene signal is the unspaced
// hyphen/underscore joins (sceneJoinRE) below, which catch release-group cruft
// regardless of vocabulary.
var sceneNoiseRE = regexp.MustCompile(`(?i)\b(?:flac|mp3|aac|alac|ogg|opus|webflac|webmp3|webaac|cbr|vbr|kbps)\b`)

// sceneJoinRE matches a hyphen/underscore glued between two word characters
// with no surrounding spaces ("some-artist", "asdf_fdsa"). Curated names use
// spaced " - " separators, so an unspaced join is a release-group / scene
// fingerprint.
var sceneJoinRE = regexp.MustCompile(`\w[-_]\w`)

// pathTrust scores how far to trust the path-derived artist/album for a release
// whose raw folder segment is `raw`. A clean, human-foldered name keeps full
// basePathTrust; scene cruft decays it toward minSourceTrust so clean tags win
// the disagreement. `raw` should be the release folder's original name (e.g.
// SceneReleaseParse.RawName), not the parser's cleaned output.
func pathTrust(raw string) float64 {
	t := basePathTrust
	if sceneNoiseRE.MatchString(raw) {
		t -= 0.25
	}
	switch joins := len(sceneJoinRE.FindAllString(raw, -1)); {
	case joins >= 2:
		t -= 0.25
	case joins == 1:
		t -= 0.08
	}
	if strings.Contains(raw, "_") {
		// Underscores are vanishingly rare in curated music folder names and
		// ubiquitous in scene release names.
		t -= 0.08
	}
	if t < minSourceTrust {
		t = minSourceTrust
	}
	return t
}

// placeholderRE matches tag values a tagger or ripper leaves when it knows
// nothing real: "Unknown Artist", "Various", "Untitled", "Track 7", ripper
// advertising, etc. Such a value must not out-weigh a real path.
var placeholderRE = regexp.MustCompile(`(?i)^(?:unknown(?:\s+(?:artist|album|title))?|<unknown>|various(?:\s+artists)?|va|untitled|track\s*\d*|audio\s*track|no\s+(?:artist|album|title)|ripped by\b.*|.*\b(?:www\.\S+|\S+\.com)\b.*|cdda)$`)

var numericOnlyRE = regexp.MustCompile(`^\d+$`)

// isPlaceholderValue reports whether a tag value is junk rather than a real
// title/name. Empty, a bare number ("01"), or a known placeholder phrase.
func isPlaceholderValue(val string) bool {
	v := strings.TrimSpace(val)
	if v == "" {
		return true
	}
	if numericOnlyRE.MatchString(v) {
		return true
	}
	return placeholderRE.MatchString(v)
}

// tagTrust is baseTagTrust for a real value, collapsing to the floor for a
// placeholder so it loses to any non-empty path value.
func tagTrust(val string) float64 {
	if isPlaceholderValue(val) {
		return minSourceTrust
	}
	return baseTagTrust
}

// placeholderNameRE matches artist/album names a tagger emits when it knows
// nothing real. Unlike isPlaceholderValue it does NOT reject a bare number —
// real bands are named "311" / "112" / "1349", and rejecting those would drop
// correctly-tagged releases whose path lacks an artist.
var placeholderNameRE = regexp.MustCompile(`(?i)^(?:unknown(?:\s+(?:artist|album|title))?|<unknown>|various(?:\s+artists)?|va|untitled|no\s+(?:artist|album|title)|cdda)$`)

// isUsableArtist reports whether a fused artist name is a real identity we can
// safely create/link. The artists table has no library_id — its uniqueness is
// (lower(name), lower(disambiguation)) across ALL libraries — so materializing
// a placeholder like "Unknown Artist" would fuse every untagged release in
// every library into one poison row. Reject those; the group stays retryable-
// unmatched instead of polluting the global artist namespace. A pure-numeric
// name is allowed (it can be a real band).
func isUsableArtist(name string) bool {
	n := strings.TrimSpace(name)
	return n != "" && !placeholderNameRE.MatchString(n)
}

// preferValue picks which spelling to keep when path and tag agree (fuzzy).
// We keep the path spelling for stability — it matches what already-scanned
// curated libraries produced, so re-scans don't churn — unless the path value
// is empty.
func preferValue(pathVal, tagVal string) string {
	if strings.TrimSpace(pathVal) != "" {
		return pathVal
	}
	return tagVal
}

// fuseText resolves one text field from a path candidate and a tag candidate
// using their (already decayed) trust scores. Agreement short-circuits to high
// confidence; a real disagreement is won by the higher-trust source, with the
// path taking exact ties (the 55 lean).
func fuseText(pathVal, tagVal string, pTrust, tTrust float64) fusedText {
	p := strings.TrimSpace(pathVal)
	t := strings.TrimSpace(tagVal)
	switch {
	case p == "" && t == "":
		return fusedText{}
	case t == "":
		return fusedText{Value: p, Source: sourcePath, Confidence: pTrust}
	case p == "":
		return fusedText{Value: t, Source: sourceTag, Confidence: tTrust}
	}
	if titlematch.FuzzyEqual(p, t) {
		conf := pTrust + tTrust*0.5 + agreementBoost
		if conf > 1 {
			conf = 1
		}
		return fusedText{Value: preferValue(p, t), Source: sourceBoth, Confidence: conf}
	}
	if pTrust >= tTrust {
		return fusedText{Value: p, Source: sourcePath, Confidence: pTrust}
	}
	return fusedText{Value: t, Source: sourceTag, Confidence: tTrust}
}

var mbidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// looksLikeMBID reports whether a string is a well-formed MusicBrainz UUID.
func looksLikeMBID(s string) bool {
	return mbidRE.MatchString(strings.TrimSpace(s))
}

// sameRelease reports whether two album titles name the SAME release for the
// purpose of adopting a tag's release MBID. This is intentionally STRICT
// (normalized case + whitespace, exact otherwise) rather than titlematch.FuzzyEqual:
// fuzzy equality collapses editions — "The Better Life" vs "The Better Life
// (Deluxe Edition)" — but each edition has its own release MBID, so adopting a
// tag MBID across an edition drift would stamp one release's id onto another and
// mislink them in the global album MBID dedup. Different-script variants
// (romaji vs kana) also fail this and simply go un-adopted (enrichment fills the
// MBID later) — under-adopt beats mislink.
func sameRelease(a, b string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
	}
	na := norm(a)
	return na != "" && na == norm(b)
}

// fuseMBID resolves an MBID from local sources. An MBID is a hard identifier
// (a UUID, not a fuzzy human string), so it bypasses the 55/45 text weighting:
// NFO wins, then a well-formed tag MBID at near-full trust. Path effectively
// never carries a real MBID. (Synthetic heya-media placeholder MBIDs are
// rejected later at enrich via isSyntheticMBID, not here.)
func fuseMBID(nfoMBID, tagMBID string) fusedText {
	if v := strings.TrimSpace(nfoMBID); v != "" {
		return fusedText{Value: v, Source: sourceNFO, Confidence: 1}
	}
	if v := strings.TrimSpace(tagMBID); looksLikeMBID(v) {
		return fusedText{Value: v, Source: sourceTag, Confidence: 0.95}
	}
	return fusedText{}
}

// fuseTrackNumber fuses the path and tag track numbers. When both are present
// and agree, that number stands; on disagreement the path wins, because a
// leading filename number ("0104 - …") reflects the actual on-disk ordering
// more reliably than a tag a downloader may have mangled. Returns 0 when
// neither source knows — the caller's collision guard then assigns a synthetic
// number so distinct files never collapse onto one track row.
func fuseTrackNumber(pathNum, tagNum int) int {
	if pathNum > 0 {
		return pathNum
	}
	if tagNum > 0 {
		return tagNum
	}
	return 0
}

// trackNumberAssigner hands out per-disc track numbers so that untagged,
// unnumbered files never collapse onto one (album, disc, 0) row. It is used in
// two passes:
//
//   - reserve(disc, n) records a KNOWN positive number — from a file's fused
//     number, or from a track already persisted for the album. Reserving the
//     same (disc, n) twice is fine and intended: two files that legitimately
//     share a number are quality-alternates of one track (FLAC + MP3 in a
//     folder) and must merge, so a known number is always returned as-is.
//   - fill(disc) hands an UNKNOWN-numbered file (fused number 0) the next slot
//     that collides with no reserved number on that disc.
//
// Reserving every known number (batch + persisted) BEFORE filling any unknown
// makes the result independent of file ordering: an unnumbered file processed
// first can't steal a slot a later numbered file needs, and a rescan that adds
// unnumbered files to an existing album fills above its persisted tracks
// instead of colliding with them.
type trackNumberAssigner struct {
	usedByDisc map[int]map[int]bool
	maxByDisc  map[int]int
}

func newTrackNumberAssigner() *trackNumberAssigner {
	return &trackNumberAssigner{
		usedByDisc: map[int]map[int]bool{},
		maxByDisc:  map[int]int{},
	}
}

func (a *trackNumberAssigner) discMap(disc int) map[int]bool {
	if disc <= 0 {
		disc = 1
	}
	used := a.usedByDisc[disc]
	if used == nil {
		used = map[int]bool{}
		a.usedByDisc[disc] = used
	}
	return used
}

// reserve marks a known positive track number as taken on its disc. A number
// <= 0 is ignored (unknown — handled by fill).
func (a *trackNumberAssigner) reserve(disc, num int) {
	if num <= 0 {
		return
	}
	if disc <= 0 {
		disc = 1
	}
	a.discMap(disc)[num] = true
	if num > a.maxByDisc[disc] {
		a.maxByDisc[disc] = num
	}
}

// fill returns the next free track number on the disc for an unknown-numbered
// file, never colliding with a reserved number.
func (a *trackNumberAssigner) fill(disc int) int {
	if disc <= 0 {
		disc = 1
	}
	used := a.discMap(disc)
	next := a.maxByDisc[disc] + 1
	for used[next] {
		next++
	}
	used[next] = true
	a.maxByDisc[disc] = next
	return next
}
