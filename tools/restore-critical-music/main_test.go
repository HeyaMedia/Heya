package main

import "testing"

func TestCompareIdentityPrefersContentHash(t *testing.T) {
	current := fileRef{Size: 100, ContentHash: "current"}

	if got := compareIdentity(fileIdentity{Size: 100, ContentHash: "backup"}, current); got != identityMismatch {
		t.Fatalf("hash mismatch with equal size = %v, want identityMismatch", got)
	}
	if got := compareIdentity(fileIdentity{Size: 99, ContentHash: "current"}, current); got != identityMatched {
		t.Fatalf("matching hash with unequal size = %v, want identityMatched", got)
	}
}

func TestCompareIdentityFallsBackToSize(t *testing.T) {
	current := fileRef{Size: 100}

	if got := compareIdentity(fileIdentity{Size: 100}, current); got != identityMatched {
		t.Fatalf("equal size = %v, want identityMatched", got)
	}
	if got := compareIdentity(fileIdentity{Size: 99}, current); got != identityMismatch {
		t.Fatalf("unequal size = %v, want identityMismatch", got)
	}
	if got := compareIdentity(fileIdentity{}, current); got != identityUnknown {
		t.Fatalf("missing backup identity = %v, want identityUnknown", got)
	}
}

func TestFacetDecodeIncludesPathIdentity(t *testing.T) {
	data := []byte(`{"paths":[{"path_key":"/music/track.flac","size":123,"content_hash":"abc"}],"track":{"file_path":"/music/track.flac"},"facet":{"track_embedding":null,"artist_embedding":null,"release_embedding":null,"text_embedding":null,"bpm":null,"bpm_confidence":null,"key_root":null,"key_mode":null,"key_clarity":null,"top_genres":null,"mood_tags":null,"waveform":null,"analyzed_at":"2026-07-07T00:00:00Z","analyzer_version":1}}`)
	var row facetExport
	if err := row.decodeBackup(data); err != nil {
		t.Fatal(err)
	}
	if len(row.Paths) != 1 || row.Paths[0].Size != 123 || row.Paths[0].ContentHash != "abc" {
		t.Fatalf("decoded path = %#v", row.Paths)
	}
	if row.Track.FilePath != "/music/track.flac" {
		t.Fatalf("decoded preferred file = %q", row.Track.FilePath)
	}
}

func TestTrackFileDecodeIncludesFileIdentity(t *testing.T) {
	data := []byte(`{"path_key":"/music/track.flac","library_file":{"size":123,"content_hash":"abc"},"track_file":{"integrated_lufs":-12.3,"true_peak_db":null,"loudness_range_db":null,"sample_peak_db":null,"loudness_analyzed_at":null,"intro_end_ms":null,"outro_start_ms":null,"fade_start_ms":null,"silence_start_ms":null,"boundaries_analyzed_at":null,"chromaprint":null,"chromaprint_algorithm":null,"chromaprint_duration_secs":null,"fingerprinted_at":null}}`)
	var row trackFileExport
	if err := row.decodeBackup(data); err != nil {
		t.Fatal(err)
	}
	if row.LibraryFile.Size != 123 || row.LibraryFile.ContentHash != "abc" {
		t.Fatalf("decoded identity = %#v", row.LibraryFile)
	}
}

func TestMatchEntityRejectsChangedFilesBeforeAmbiguity(t *testing.T) {
	paths := []pathExport{
		{PathKey: "/music/a.flac", fileIdentity: fileIdentity{Size: 100, ContentHash: "same"}},
		{PathKey: "/music/b.flac", fileIdentity: fileIdentity{Size: 200, ContentHash: "old"}},
	}
	refs := map[string]fileRef{
		"/music/a.flac": {TrackID: 1, Size: 100, ContentHash: "same"},
		"/music/b.flac": {TrackID: 2, Size: 200, ContentHash: "new"},
	}

	got := matchEntity(paths, refs, func(ref fileRef) int64 { return ref.TrackID })
	if got.ID != 1 || len(got.IDs) != 1 || !got.IdentityMismatch {
		t.Fatalf("match = %#v", got)
	}
}

func TestResolveEntityByPreferredPath(t *testing.T) {
	paths := []pathExport{
		{PathKey: "/music/a.flac", fileIdentity: fileIdentity{Size: 100, ContentHash: "a"}},
		{PathKey: "/music/a.m4a", fileIdentity: fileIdentity{Size: 50, ContentHash: "b"}},
	}
	refs := map[string]fileRef{
		"/music/a.flac": {TrackID: 1, Size: 100, ContentHash: "a"},
		"/music/a.m4a":  {TrackID: 2, Size: 50, ContentHash: "b"},
	}
	got := matchEntity(paths, refs, func(ref fileRef) int64 { return ref.TrackID })
	resolveEntityByPreferredPath(&got, "/music/a.flac", paths, refs, func(ref fileRef) int64 { return ref.TrackID })
	if got.ID != 1 || len(got.IDs) != 2 {
		t.Fatalf("resolved match = %#v", got)
	}
}
