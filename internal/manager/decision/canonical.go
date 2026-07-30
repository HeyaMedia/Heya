package decision

// Canonical quality orderings, best-first — the full vocabulary each domain
// can produce. Used when a profile's ladder doesn't contain a quality at
// all (partial seeded ladders): a file BETTER than the profile's cutoff in
// canonical order is satisfied, not wanted — owning a 2160p disc when the
// profile wants 1080p is "above the want", never "cutoff unmet". Real arr
// profiles carry every quality (allowed flag only), which makes this
// implicit there.
var canonicalVideoOrder = []string{
	"remux-2160p", "bluray-2160p", "webdl-2160p", "webrip-2160p", "hdtv-2160p",
	"remux-1080p", "bluray-1080p", "webdl-1080p", "webrip-1080p", "hdtv-1080p",
	"bluray-720p", "webdl-720p", "webrip-720p", "hdtv-720p",
	"bluray-576p", "bluray-480p", "webdl-480p", "webrip-480p",
	"dvd", "sdtv", "br-disk", "raw-hd",
}

var canonicalMusicOrder = []string{
	"flac-24", "flac", "alac", "ape", "wavpack",
	"mp3-320", "mp3-v0", "aac-320", "mp3-256", "mp3-v2", "aac-256",
	"mp3-192", "ogg-vorbis-q10", "wav",
}

var canonicalBookOrder = []string{
	"epub", "azw3", "mobi", "pdf", "cbz", "m4b", "flac", "mp3-320", "mp3-128",
}

func canonicalOrder(domain string) []string {
	switch domain {
	case "music":
		return canonicalMusicOrder
	case "book":
		return canonicalBookOrder
	default:
		return canonicalVideoOrder
	}
}

// CanonicalRank returns the quality's position in the domain's canonical
// best-first order; found=false for unknown/empty keys.
func CanonicalRank(domain, quality string) (int, bool) {
	if quality == "" {
		return 0, false
	}
	for i, key := range canonicalOrder(domain) {
		if key == quality {
			return i, true
		}
	}
	return 0, false
}

// QualityMeetsCutoffCanonically reports whether a quality that has NO slot
// in the profile ladder still meets (or exceeds) the profile's cutoff in
// canonical order. ok=false when either side can't be ranked.
func QualityMeetsCutoffCanonically(domain string, profile *Profile, quality string) (meets, ok bool) {
	qualityRank, qok := CanonicalRank(domain, quality)
	if !qok {
		return false, false
	}
	cutoffPos, found := profile.CutoffPosition()
	if !found {
		return false, false
	}
	// Best canonical rank among the cutoff item's member qualities.
	item := profile.Items[cutoffPos]
	keys := item.Qualities
	if item.Quality != "" {
		keys = append([]string{item.Quality}, keys...)
	}
	bestCutoffRank := -1
	for _, key := range keys {
		if rank, kok := CanonicalRank(domain, key); kok && (bestCutoffRank == -1 || rank < bestCutoffRank) {
			bestCutoffRank = rank
		}
	}
	if bestCutoffRank == -1 {
		return false, false
	}
	return qualityRank <= bestCutoffRank, true
}
