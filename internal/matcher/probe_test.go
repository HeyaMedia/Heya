package matcher

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

// TestProbeAutoMatch is a manual benchmark for the auto-match gate: hit a
// running heya.media server (default localhost:3030) with a corpus of
// realistic filename → expected-title pairs and report the score
// distribution. Use this to tune AutoMatchThreshold without having to run a
// full library scan.
//
// Skipped when heya.media isn't reachable, so it doesn't break CI. Run
// locally with:
//
//	go test -v -run TestProbeAutoMatch ./internal/matcher/
//
// The output is intentionally noisy — read the per-case lines and decide
// whether the threshold needs to move.
func TestProbeAutoMatch(t *testing.T) {
	const heyaURL = "http://localhost:3030"
	if !portOpen("localhost:3030") {
		t.Skip("heya.media not reachable at localhost:3030 — skipping probe")
	}

	client := heyamedia.NewClient(heyaURL)
	heya := heyamedia.NewHeyaProvider(client)

	cases := []struct {
		kind         metadata.MediaKind
		title        string
		year         string
		wantContains string // substring expected in the top match's title
		note         string
	}{
		// ─── Movies ────────────────────────────────────────────────────
		{metadata.KindMovie, "Dune", "2021", "Dune", "exact-year canonical movie"},
		{metadata.KindMovie, "Inception", "2010", "Inception", "exact-year canonical movie"},
		{metadata.KindMovie, "The Matrix", "1999", "Matrix", "article-prefix"},
		{metadata.KindMovie, "Blade Runner 2049", "2017", "Blade Runner 2049", "numeric suffix"},

		// Movies — no year (title-only)
		{metadata.KindMovie, "Dune", "", "Dune", "no year — heya should still rank Dune up"},
		{metadata.KindMovie, "Inception", "", "Inception", "no year canonical"},

		// Movies — wrong year
		{metadata.KindMovie, "Dune", "2020", "Dune", "off-by-one year"},
		{metadata.KindMovie, "Dune", "1984", "Dune", "old version year"},

		// Movies — sequels / disambiguation
		{metadata.KindMovie, "Dune Part Two", "2024", "Dune", "sequel with subtitle"},
		{metadata.KindMovie, "Frozen", "2013", "Frozen", "same-title disambiguation by year (Disney)"},
		{metadata.KindMovie, "Frozen", "2010", "Frozen", "same-title disambiguation by year (indie)"},

		// Movies — rejection cases
		{metadata.KindMovie, "Some Random Garbage Title 99999", "2024", "", "no real match expected"},
		{metadata.KindMovie, "ZZZZ", "2024", "", "no real match"},

		// ─── TV series (live-action) ───────────────────────────────────
		{metadata.KindTV, "Breaking Bad", "2008", "Breaking Bad", "tv canonical"},
		{metadata.KindTV, "The Wire", "2002", "Wire", "tv with article prefix"},
		{metadata.KindTV, "Game of Thrones", "2011", "Game of Thrones", "long-running fantasy"},
		{metadata.KindTV, "True Detective", "2014", "True Detective", "anthology"},
		{metadata.KindTV, "The Office", "2005", "Office", "ambiguous title — multiple Offices exist"},
		{metadata.KindTV, "Doctor Who", "2005", "Doctor Who", "reboot run — disambiguation by year"},
		{metadata.KindTV, "Doctor Who", "1963", "Doctor Who", "classic run"},
		{metadata.KindTV, "Battlestar Galactica", "2004", "Battlestar Galactica", "reboot vs original"},
		{metadata.KindTV, "Battlestar Galactica", "1978", "Battlestar Galactica", "original"},
		{metadata.KindTV, "The Mandalorian", "2019", "Mandalorian", "modern streaming series"},

		// ─── Anime (KindTV with anime-specific titles) ─────────────────
		// Anime is media_type=tv in heya — what varies is whether the
		// returned title is the English release name or the romanized
		// Japanese. If your filenames use romaji and heya returns
		// English, expect low Levenshtein scores → REJECT verdicts.
		// This is the tuning target the user asked about.
		{metadata.KindTV, "Attack on Titan", "2013", "Attack on Titan", "anime — English title"},
		{metadata.KindTV, "Shingeki no Kyojin", "2013", "Attack on Titan", "anime — romaji query, English title expected"},
		{metadata.KindTV, "Cowboy Bebop", "1998", "Cowboy Bebop", "anime — universally same name"},
		{metadata.KindTV, "Fullmetal Alchemist: Brotherhood", "2009", "Fullmetal Alchemist", "anime — long subtitled"},
		{metadata.KindTV, "Naruto", "2002", "Naruto", "anime — short canonical"},
		{metadata.KindTV, "Boku no Hero Academia", "2016", "My Hero Academia", "anime — romaji query, English title expected"},
		{metadata.KindTV, "My Hero Academia", "2016", "My Hero Academia", "anime — English form of above"},
		{metadata.KindTV, "Spy x Family", "2022", "Spy", "anime — special-char title"},
		{metadata.KindTV, "Demon Slayer", "2019", "Demon Slayer", "anime — recent popular"},
		{metadata.KindTV, "Kimetsu no Yaiba", "2019", "Demon Slayer", "anime — romaji query, English title expected"},

		// ─── Music artists ─────────────────────────────────────────────
		// Year is the artist's start year (heya tracks this from
		// MusicBrainz). For music the matcher converts "Artist - Album"
		// in query.Title, but the probe sends bare artist strings, so
		// these test the artist-name search path directly.
		{metadata.KindMusic, "Radiohead", "1991", "Radiohead", "artist canonical"},
		{metadata.KindMusic, "The Beatles", "1960", "Beatles", "article-prefix artist"},
		{metadata.KindMusic, "Daft Punk", "1993", "Daft Punk", "artist with year"},
		{metadata.KindMusic, "Pink Floyd", "1965", "Pink Floyd", "artist canonical"},
		{metadata.KindMusic, "BTS", "2010", "BTS", "short acronym artist"},
		{metadata.KindMusic, "Ado", "", "Ado", "short Japanese stage name (year often unknown)"},
		{metadata.KindMusic, "King Gnu", "2017", "King Gnu", "Japanese band romanized"},
		{metadata.KindMusic, "Yoasobi", "2019", "Yoasobi", "Japanese duo — title case may vary"},
		{metadata.KindMusic, "Aimer", "2011", "Aimer", "single-word stage name"},
		{metadata.KindMusic, "Radiohead", "", "Radiohead", "no year — same artist"},

		// ─── Music — rejection cases ───────────────────────────────────
		{metadata.KindMusic, "AAAAA Not A Real Artist 99999", "", "", "garbage artist name"},
	}

	base := DefaultOptions().AutoMatchThreshold
	ctx := context.Background()

	t.Logf("base AutoMatchThreshold = %.2f", base)
	t.Logf("─────────────────────────────────────────────────────────────────────")
	t.Logf("%-32s %-32s %-7s %-7s %-9s %s", "QUERY", "TOP MATCH", "SCORE", "THRESH", "VERDICT", "NOTE")
	t.Logf("─────────────────────────────────────────────────────────────────────")

	for i, c := range cases {
		// Pace the queries — HeyaMedia's alt-title fan-out hits multiple
		// upstream providers per hit on cold cache, and slamming the
		// full corpus back-to-back has been observed to crash the
		// server. 400ms between requests is gentle enough to avoid
		// that and still finish the corpus quickly.
		if i > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		query := metadata.SearchQuery{Title: c.title, Year: c.year}
		hits, err := heya.Search(ctx, c.kind, query)
		// HeyaMedia is occasionally flaky during active development —
		// wait 10s and retry once on transport-level errors so a single
		// corpus run isn't ruined by a server hiccup.
		if err != nil {
			t.Logf("%-32s [transient error, waiting 10s and retrying: %v]", abbrev(c.title, 32), err)
			time.Sleep(10 * time.Second)
			hits, err = heya.Search(ctx, c.kind, query)
		}
		if err != nil {
			t.Logf("%-32s [search error: %v]", c.title, err)
			continue
		}

		if len(hits) == 0 {
			t.Logf("%-32s %-32s %-7s %-7s %-9s %s", abbrev(c.title, 32), "—", "—", "—", "NO-HITS", c.note)
			continue
		}

		for i := range hits {
			hits[i].Confidence = scoreBestTitle(c.title, hits[i], c.year)
		}
		sortByConfidence(hits)
		top := hits[0]

		threshold := autoMatchThresholdFor(top, base)
		accepted := top.Confidence >= threshold

		verdict := "REJECT"
		if accepted {
			verdict = "ACCEPT"
		}

		// Correctness check (only meaningful for cases that specify a
		// wantContains).
		correctnessNote := c.note
		if c.wantContains != "" {
			matchIsCorrect := strings.Contains(strings.ToLower(top.Title), strings.ToLower(c.wantContains))
			if accepted && !matchIsCorrect {
				correctnessNote = "⚠ accepted wrong title: " + c.note
			} else if !accepted && matchIsCorrect {
				correctnessNote = "✓ would have matched: " + c.note
			}
		} else if accepted {
			correctnessNote = "⚠ accepted unexpected match: " + c.note
		}

		t.Logf("%-32s %-32s %-7.3f %-7.3f %-9s %s",
			abbrev(c.title+" ("+c.year+")", 32),
			abbrev(top.Title+" ("+top.Year+")", 32),
			top.Confidence,
			threshold,
			verdict,
			correctnessNote,
		)
	}
	t.Logf("─────────────────────────────────────────────────────────────────────")
	t.Logf("To tune: change DefaultOptions().AutoMatchThreshold (matcher/types.go)")
	t.Logf("or the enrichedBoost in autoMatchThresholdFor (matcher/matcher.go)")
}

func portOpen(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func abbrev(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
