package decision

import (
	"sort"
	"time"

	"github.com/karbowiak/heya/internal/parser/video"
)

// rankCandidates orders accepted candidates for one unit, best first, using
// the domain-specific arr comparer order:
//
//	movie (Radarr): quality(+revision) → format score → indexer priority →
//	                age (continuous, newer better) → size distance
//	tv (Sonarr):    quality(+revision) → format score → season-pack/episode
//	                coverage → indexer priority → age buckets → size distance
//	music (Lidarr): quality → format score → indexer priority → age → size
//
// Protocol comparison is omitted on purpose: this instance is usenet-only.
func rankCandidates(target Target, unit *Unit, accepted []*CandidateResult) {
	sort.SliceStable(accepted, func(i, j int) bool {
		a, b := accepted[i], accepted[j]

		// Quality: lower ladder position = better.
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		// Revision preference within equal quality.
		if target.Profile.PreferProperRepack {
			if a.Attrs.RevisionVersion != b.Attrs.RevisionVersion {
				return a.Attrs.RevisionVersion > b.Attrs.RevisionVersion
			}
			if a.Attrs.RevisionReal != b.Attrs.RevisionReal {
				return a.Attrs.RevisionReal > b.Attrs.RevisionReal
			}
		}
		// Custom-format score.
		if a.FormatScore != b.FormatScore {
			return a.FormatScore > b.FormatScore
		}
		// TV coverage sits before indexer priority (Sonarr order).
		if target.Domain == "tv" {
			if c := compareTVCoverage(target, a, b); c != 0 {
				return c < 0
			}
		}
		// Indexer priority: lower = preferred.
		if a.Input.IndexerPriority != b.Input.IndexerPriority {
			return a.Input.IndexerPriority < b.Input.IndexerPriority
		}
		// Age.
		if c := compareAge(target.Domain, a.Input.PublishDate, b.Input.PublishDate); c != 0 {
			return c < 0
		}
		// Size distance to preferred.
		if c := compareSizePreference(target, a, b); c != 0 {
			return c < 0
		}
		// Deterministic final tie-break.
		if a.Input.IndexerID != b.Input.IndexerID {
			return a.Input.IndexerID < b.Input.IndexerID
		}
		return a.Input.Index < b.Input.Index
	})
}

// compareTVCoverage prefers full-season packs, then — for standard series —
// tighter releases (fewer episodes, lower first episode); anime batches
// order the opposite way (more episodes preferred). Negative = a first.
func compareTVCoverage(target Target, a, b *CandidateResult) int {
	aShow := video.FilenameParseShow(a.Input.Title)
	bShow := video.FilenameParseShow(b.Input.Title)

	aFull, bFull := boolToInt(aShow.FullSeason), boolToInt(bShow.FullSeason)
	if aFull != bFull {
		return bFull - aFull
	}
	aCount, bCount := len(aShow.EpisodeNumbers), len(bShow.EpisodeNumbers)
	if aCount != bCount {
		if target.Anime {
			return bCount - aCount
		}
		return aCount - bCount
	}
	aFirst, bFirst := firstEpisode(aShow.EpisodeNumbers), firstEpisode(bShow.EpisodeNumbers)
	if aFirst != bFirst {
		return aFirst - bFirst
	}
	return 0
}

// compareAge: TV uses Sonarr's buckets (under an hour, under a day, under a
// week, older — older collapsed together); movies/music compare
// continuously, newer first. Zero publish dates sort last.
func compareAge(domain string, a, b time.Time) int {
	if a.IsZero() && b.IsZero() {
		return 0
	}
	if a.IsZero() {
		return 1
	}
	if b.IsZero() {
		return -1
	}
	if domain == "tv" {
		ab, bb := ageBucket(a), ageBucket(b)
		return ab - bb
	}
	if a.After(b) {
		return -1
	}
	if b.After(a) {
		return 1
	}
	return 0
}

func ageBucket(t time.Time) int {
	age := time.Since(t)
	switch {
	case age < time.Hour:
		return 0
	case age <= 24*time.Hour:
		return 1
	case age <= 7*24*time.Hour:
		return 2
	default:
		return 3
	}
}

// compareSizePreference prefers the candidate whose size sits closest to
// the profile's preferred size for its quality (runtime-scaled). Candidates
// without size data or a size definition compare equal.
func compareSizePreference(target Target, a, b *CandidateResult) int {
	if target.RuntimeMinutes <= 0 {
		return 0
	}
	da, oka := sizeDistance(target, a)
	db, okb := sizeDistance(target, b)
	if !oka || !okb {
		return 0
	}
	switch {
	case da < db:
		return -1
	case db < da:
		return 1
	default:
		return 0
	}
}

func sizeDistance(target Target, cand *CandidateResult) (float64, bool) {
	if cand.Input.SizeBytes <= 0 {
		return 0, false
	}
	def, ok := target.Profile.SizeDefs[cand.QualityKey]
	if !ok || def.PreferredMBPerMin <= 0 {
		return 0, false
	}
	sizeMB := float64(cand.Input.SizeBytes) / (1024 * 1024)
	preferred := def.PreferredMBPerMin * float64(target.RuntimeMinutes)
	d := sizeMB - preferred
	if d < 0 {
		d = -d
	}
	return d, true
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func firstEpisode(nums []int) int {
	if len(nums) == 0 {
		return 1 << 30
	}
	min := nums[0]
	for _, n := range nums[1:] {
		if n < min {
			min = n
		}
	}
	return min
}
