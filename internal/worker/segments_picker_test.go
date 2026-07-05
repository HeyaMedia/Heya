package worker

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

func ms(v int64) *int64 { return &v }

func TestPickSegmentsDurationGate(t *testing.T) {
	fileDur := int64(3_600_000)
	cands := []heyamedia.SegmentCandidate{
		// Exact-cut skipme candidate.
		{Type: "intro", StartMs: 229_500, EndMs: ms(246_500), DurationMs: 3_600_000, Submissions: 1, Source: "skipmedb"},
		// TheIntroDB carries no authored runtime (server-side matched).
		{Type: "intro", StartMs: 228_836, EndMs: ms(245_506), Source: "theintrodb"},
		// Different release cut — 4 minutes off, must be rejected.
		{Type: "intro", StartMs: 100_000, EndMs: ms(190_000), DurationMs: 3_360_000, Submissions: 50, Source: "skipmedb"},
	}
	picked := pickSegments(cands, fileDur)
	if len(picked) != 1 {
		t.Fatalf("want 1 intro, got %d: %+v", len(picked), picked)
	}
	if picked[0].Source != "community:skipmedb" || picked[0].StartMs != 229_500 {
		t.Errorf("exact-duration match should beat unknown-duration: %+v", picked[0])
	}
}

func TestPickSegmentsOpenEndedCredits(t *testing.T) {
	fileDur := int64(3_600_000)
	picked := pickSegments([]heyamedia.SegmentCandidate{
		{Type: "credits", StartMs: 3_431_000, Source: "theintrodb"},
	}, fileDur)
	if len(picked) != 1 {
		t.Fatalf("want 1 credits, got %d", len(picked))
	}
	if picked[0].EndMs != fileDur {
		t.Errorf("open-ended credits must materialize to file duration, got %d", picked[0].EndMs)
	}
}

func TestPickSegmentsRejectsDegenerate(t *testing.T) {
	fileDur := int64(3_600_000)
	picked := pickSegments([]heyamedia.SegmentCandidate{
		{Type: "intro", StartMs: -5, EndMs: ms(30_000), Source: "theintrodb"},              // negative start
		{Type: "intro", StartMs: 10_000, EndMs: ms(10_500), Source: "theintrodb"},          // sub-second
		{Type: "credits", StartMs: 3_700_000, EndMs: ms(3_710_000), Source: "theintrodb"},  // starts past EOF
		{Type: "credits", StartMs: 3_500_000, EndMs: ms(99_999_999), Source: "theintrodb"}, // end clamps to EOF
	}, fileDur)
	if len(picked) != 1 {
		t.Fatalf("want only the clampable credits, got %+v", picked)
	}
	if picked[0].Type != "credits" || picked[0].EndMs != fileDur {
		t.Errorf("credits end should clamp to file duration: %+v", picked[0])
	}
}

func TestPickSegmentsSubmissionsTiebreak(t *testing.T) {
	fileDur := int64(1_377_000)
	picked := pickSegments([]heyamedia.SegmentCandidate{
		{Type: "intro", StartMs: 1_039, EndMs: ms(91_039), DurationMs: 1_377_312, Source: "aniskip"},
		{Type: "intro", StartMs: 151_891, EndMs: ms(234_261), DurationMs: 1_377_000, Submissions: 10, Source: "skipmedb"},
	}, fileDur)
	if len(picked) != 1 {
		t.Fatalf("want 1 intro, got %d", len(picked))
	}
	// skipme is 0ms off, aniskip 312ms off — closeness wins before
	// submissions even enter the comparison.
	if picked[0].Source != "community:skipmedb" {
		t.Errorf("closest authored runtime should win: %+v", picked[0])
	}
}

func TestPickSegmentsMultipleCommercials(t *testing.T) {
	fileDur := int64(2_700_000)
	picked := pickSegments([]heyamedia.SegmentCandidate{
		{Type: "commercial", StartMs: 600_000, EndMs: ms(780_000), DurationMs: 2_700_000, Source: "skipmedb"},
		{Type: "commercial", StartMs: 1_500_000, EndMs: ms(1_680_000), DurationMs: 2_700_000, Source: "skipmedb"},
	}, fileDur)
	if len(picked) != 2 {
		t.Fatalf("both commercial breaks should survive, got %d: %+v", len(picked), picked)
	}
	if picked[0].StartMs != 600_000 || picked[1].StartMs != 1_500_000 {
		t.Errorf("commercials should sort by start: %+v", picked)
	}
}

func TestParseFirstEpisodeRef(t *testing.T) {
	raw := []byte(`{"parsed":{"release":{"seasons":[2],"episodes":[3,4]}}}`)
	season, episode, ok := parseFirstEpisodeRef(raw)
	if !ok || season != 2 || episode != 3 {
		t.Errorf("got s%de%d ok=%v", season, episode, ok)
	}
	if _, _, ok := parseFirstEpisodeRef([]byte(`{"parsed":{"release":{"seasons":[],"episodes":[]}}}`)); ok {
		t.Error("empty seasons/episodes must not resolve")
	}
}

func TestExternalIDStrings(t *testing.T) {
	got := externalIDStrings([]byte(`{"tmdb":"1396","tvdb":81189,"imdb":"tt0903747"}`))
	if got["tmdb"] != "1396" || got["tvdb"] != "81189" || got["imdb"] != "tt0903747" {
		t.Errorf("mixed string/number ids should normalize: %+v", got)
	}
}
