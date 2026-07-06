package service

import (
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/parser"
)

// wrapParse marshals a real parser result into the {"parsed": …} envelope the
// scanner writes to library_files.parse_result — the exact shape the episode
// mappers unmarshal. Using the real parser (not a hand-written blob) guards the
// field-name contract: if the parser renamed "absoluteEpisodes", this breaks.
func wrapParse(t *testing.T, path string) []byte {
	t.Helper()
	entry := parser.ParseStoragePath(path)
	blob, err := json.Marshal(map[string]any{"parsed": entry})
	if err != nil {
		t.Fatalf("marshal parse result: %v", err)
	}
	return blob
}

func TestBuildEpisodeFileMapAbsoluteRemap(t *testing.T) {
	// Real absolute-numbered anime file: parses to absoluteEpisodes=[24], no
	// season. The enriched catalog places absolute 24 at season 2, episode 2.
	files := []sqlc.ListEpisodeFilesRow{{
		ID:          101,
		Size:        1234,
		ParseResult: wrapParse(t, "/data/Anime/Uchuu Senkan Yamato 2 {anidb-2662}/Uchuu Senkan Yamato 2 - 24 - Life and death struggle! Two brave men.mkv"),
	}}
	absMap := map[int]SeasonEpisode{24: {Season: 2, Episode: 2}}

	got := BuildEpisodeFileMap(files, absMap)
	if entry, ok := got["s2e2"]; !ok || entry.FileID != 101 {
		t.Fatalf("expected absolute 24 remapped to s2e2 (file 101), got %#v", got)
	}
	if _, ok := got["s0e24"]; ok {
		t.Errorf("absolute file must not leak an s0e24 key: %#v", got)
	}

	// The season set must expose the resolved season, not season 0.
	seasons := BuildAvailableSeasonSet(files, absMap)
	if !seasons[2] {
		t.Errorf("expected season 2 available from remapped absolute file, got %#v", seasons)
	}
	if seasons[0] {
		t.Errorf("absolute file must not mark season 0 available, got %#v", seasons)
	}
}

func TestBuildEpisodeFileMapAbsoluteUnresolved(t *testing.T) {
	// Before enrichment there's no absolute_number catalog, so the resolver is
	// empty. The file simply produces no episode key (rather than a wrong one).
	files := []sqlc.ListEpisodeFilesRow{{
		ID:          7,
		ParseResult: wrapParse(t, "/data/Anime/Eureka Seven AO {anidb-8854}/Eureka Seven AO - 24 - The Door into Summer.mkv"),
	}}

	got := BuildEpisodeFileMap(files, nil)
	if len(got) != 0 {
		t.Errorf("unresolved absolute file must produce no keys, got %#v", got)
	}
	if seasons := BuildAvailableSeasonSet(files, nil); len(seasons) != 0 {
		t.Errorf("unresolved absolute file must claim no season, got %#v", seasons)
	}
}

func TestAbsoluteEpisodeMapExcludesSpecials(t *testing.T) {
	// Providers sometimes stamp a non-zero absolute_number on a special (season
	// 0). Such a row must never enter the resolver — otherwise a main-show
	// absolute file ("Series - 5") would remap onto the special. The SQL filters
	// it; AbsoluteEpisodeMap guards it a second time.
	rows := []sqlc.ListEpisodeAbsoluteMapRow{
		{SeasonNumber: 0, EpisodeNumber: 3, AbsoluteNumber: 5}, // a special, must drop
		{SeasonNumber: 1, EpisodeNumber: 7, AbsoluteNumber: 7}, // real, must keep
	}
	m := AbsoluteEpisodeMap(rows)
	if _, ok := m[5]; ok {
		t.Errorf("absolute 5 came from a season-0 special and must be dropped: %#v", m)
	}
	if se, ok := m[7]; !ok || se != (SeasonEpisode{Season: 1, Episode: 7}) {
		t.Errorf("absolute 7 should resolve to s1e7, got %#v (ok=%v)", m[7], ok)
	}
}

func TestBuildEpisodeFileMapSeasonZeroSpecialPreserved(t *testing.T) {
	// A genuine season-0 special (explicit S00E05) must keep its s0e5 key and
	// NOT be swept up by the absolute remap — the whole reason absolute numbers
	// live in their own field.
	files := []sqlc.ListEpisodeFilesRow{{
		ID:          9,
		ParseResult: wrapParse(t, "/data/TV/Some Show/Specials/Some Show - S00E05 - Behind the Scenes.mkv"),
	}}
	// Resolver would map absolute 5 to a main-season episode; the special must
	// ignore it because it carries a real season (0), not an absolute number.
	absMap := map[int]SeasonEpisode{5: {Season: 1, Episode: 5}}

	got := BuildEpisodeFileMap(files, absMap)
	if _, ok := got["s0e5"]; !ok {
		t.Fatalf("genuine S00E05 special must map to s0e5, got %#v", got)
	}
	if _, ok := got["s1e5"]; ok {
		t.Errorf("special must not be remapped to s1e5: %#v", got)
	}
}
