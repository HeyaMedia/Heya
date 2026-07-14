package matcher

import (
	"context"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
)

// TestProbeAltTitlesSmoke is a minimal one-query probe to validate that
// the HeyaMetadata search summary's alternate/original titles decode
// correctly and that romaji queries resolve without a provider-shaped
// external-ID payload. Single query so it doesn't hammer
// HeyaMetadata (which currently can't handle the full corpus reliably).
func TestProbeAltTitlesSmoke(t *testing.T) {
	if !portOpen("localhost:3030") {
		t.Skip("HeyaMetadata not reachable")
	}

	client, err := heyametadata.NewClient("http://localhost:3030", "")
	if err != nil {
		t.Fatal(err)
	}
	heya := heyametadata.NewHeyaProvider(client)

	hits, err := heya.Search(context.Background(), metadata.KindTV, metadata.SearchQuery{
		CanonicalKind: "anime",
		Title:         "Shingeki no Kyojin",
		Year:          "2013",
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
		t.Error("alt_titles should be non-empty for Attack on Titan — HeyaMetadata field missing or decode broken")
	}
	foundEnglish := false
	for _, alt := range top.AltTitles {
		if strings.Contains(strings.ToLower(alt), "attack on titan") {
			foundEnglish = true
			break
		}
	}
	if !foundEnglish {
		t.Errorf("expected 'Attack on Titan' in alt_titles; got %v", top.AltTitles)
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
