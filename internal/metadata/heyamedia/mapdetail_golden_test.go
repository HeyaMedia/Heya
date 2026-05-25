package heyamedia

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	gen "github.com/karbowiak/heya/clients/heyamedia"
)

// updateGolden regenerates the .golden.json files instead of asserting
// against them. Use after an intentional MediaDetail field-mapping change:
//
//	go test -run TestMapDetail.*_Golden ./internal/metadata/heyamedia -update-golden
//
// Then commit the regenerated fixtures and diff them against the previous
// version to confirm only the expected fields moved.
var updateGolden = flag.Bool("update-golden", false, "regenerate the .golden.json files")

// runMapDetailGolden is the shared body of the per-kind golden tests. It
// decodes a saved heya.media response into the typed wire layer the
// production mapper consumes, runs the mapper, and snapshots the
// MediaDetail output as JSON for byte-level comparison against the
// committed golden file.
//
// The point of this isn't catching API drift — the integration tests do
// that. It's catching mapper drift: a refactor that silently drops
// ArtistTopTracks or renames a JSON tag would slip past the integration
// tests (which mostly assert non-zero counts) but show up here as a diff.
func runMapDetailGolden(t *testing.T, fixture, golden string, mapFn func(t *testing.T, data []byte) any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}

	got, err := json.MarshalIndent(mapFn(t, data), "", "  ")
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}

	goldenPath := filepath.Join("testdata", golden)
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
		t.Logf("wrote %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-golden to create)", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("mapDetail diverged from %s.\nRun with -update-golden to refresh, then `git diff` the new file to confirm only intended fields changed.", golden)
	}
}

// Per-kind fixture decoders feed the typed DocBody through the
// post-refactor mappers. Golden files snapshot the resulting
// metadata.MediaDetail; any future field-mapping change must either
// preserve the golden output or be re-snapshotted with -update-golden.

func mapDocFromArtistFixture(t *testing.T, data []byte) any {
	t.Helper()
	var body gen.ArtistDocBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode ArtistDocBody: %v", err)
	}
	return mapArtistDoc(&body)
}

func mapDocFromMovieFixture(t *testing.T, data []byte) any {
	t.Helper()
	var body gen.MovieDocBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode MovieDocBody: %v", err)
	}
	return mapMovieDoc(&body)
}

func mapDocFromTvFixture(t *testing.T, data []byte) any {
	t.Helper()
	var body gen.TVDocBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode TVDocBody: %v", err)
	}
	return mapTvDoc(&body)
}

// Reuse the same golden files: the assertion is that the new mappers
// reproduce the legacy mapDetail's output byte-for-byte.

func TestMapArtistDoc_Golden(t *testing.T) {
	runMapDetailGolden(t, "artist_ado.json", "artist_ado.detail.golden.json", mapDocFromArtistFixture)
}

func TestMapMovieDoc_Golden(t *testing.T) {
	runMapDetailGolden(t, "movie_fightclub.json", "movie_fightclub.detail.golden.json", mapDocFromMovieFixture)
}

func TestMapTvDoc_Golden(t *testing.T) {
	runMapDetailGolden(t, "tv_elfenlied.json", "tv_elfenlied.detail.golden.json", mapDocFromTvFixture)
}

// Person path returns the legacy HeyaPersonResponse shape (person_worker.go
// consumes it directly), so its golden file is a different schema from
// MediaDetail. Snapshot the full mapped response to lock the contract.

func mapDocFromPersonFixture(t *testing.T, data []byte) any {
	t.Helper()
	var body gen.PersonDocBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode PersonDocBody: %v", err)
	}
	return mapPersonDoc(&body)
}

func TestMapPersonDoc_Golden(t *testing.T) {
	runMapDetailGolden(t, "person_brad_pitt.json", "person_brad_pitt.detail.golden.json", mapDocFromPersonFixture)
}
