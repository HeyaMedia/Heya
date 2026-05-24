package matcher

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

// TestProbeAltTitlesSmoke is a minimal one-query probe to validate that
// the new HeyaMedia search response shape (numeric external_ids + the
// alt_titles[] field) decodes correctly and that romaji queries now
// resolve via alt_titles matching. Single query so it doesn't hammer
// HeyaMedia (which currently can't handle the full corpus reliably).
func TestProbeAltTitlesSmoke(t *testing.T) {
	if !portOpen("localhost:3030") {
		t.Skip("heya.media not reachable")
	}

	heya := heyamedia.NewHeyaProvider(heyamedia.NewClient("http://localhost:3030"))

	hits, err := heya.Search(context.Background(), metadata.KindTV, metadata.SearchQuery{
		Title: "Shingeki no Kyojin",
		Year:  "2013",
	})
	if err != nil {
		t.Fatalf("search error (decode probably broke): %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for Shingeki no Kyojin")
	}

	top := hits[0]
	t.Logf("top hit: %s (%s)", top.Title, top.Year)
	t.Logf("alt_titles: %d entries", len(top.AltTitles))
	t.Logf("external_ids: %v", top.ExternalIDs)

	if len(top.AltTitles) == 0 {
		t.Error("alt_titles should be non-empty for Attack on Titan — HeyaMedia field missing or decode broken")
	}
	foundRomaji := false
	for _, alt := range top.AltTitles {
		if alt == "Shingeki no Kyojin" {
			foundRomaji = true
			break
		}
	}
	if !foundRomaji {
		t.Errorf("expected 'Shingeki no Kyojin' in alt_titles; got %v", top.AltTitles)
	}

	if top.ExternalIDs["tmdb"] == "" {
		t.Errorf("expected external_ids[tmdb] populated (number→string coerce); got %v", top.ExternalIDs)
	}

	score := scoreBestTitle("Shingeki no Kyojin", top, "2013")
	threshold := autoMatchThresholdFor(top, DefaultOptions().AutoMatchThreshold)
	t.Logf("score=%.3f threshold=%.3f verdict=%s", score, threshold, verdictFor(score, threshold))
	if score < threshold {
		t.Errorf("Shingeki no Kyojin should now ACCEPT via alt_titles match; got score=%.3f threshold=%.3f", score, threshold)
	}
}

func verdictFor(score, threshold float64) string {
	if score >= threshold {
		return "ACCEPT"
	}
	return "REJECT"
}
