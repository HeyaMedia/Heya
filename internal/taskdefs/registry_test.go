package taskdefs

import "testing"

func TestRegistryHasUniqueIDsAndKinds(t *testing.T) {
	ids := map[string]bool{}
	kickoffKinds := map[string]string{}
	for _, def := range All() {
		if def.ID == "" {
			t.Fatal("task definition has empty ID")
		}
		if ids[def.ID] {
			t.Fatalf("duplicate task ID %q", def.ID)
		}
		ids[def.ID] = true

		if !def.Synthetic && def.KickoffKind == "" {
			t.Fatalf("scheduled task %q has no kickoff kind", def.ID)
		}
		if def.KickoffKind != "" {
			if owner, exists := kickoffKinds[def.KickoffKind]; exists {
				t.Fatalf("kickoff kind %q is owned by both %q and %q", def.KickoffKind, owner, def.ID)
			}
			kickoffKinds[def.KickoffKind] = def.ID
		}
	}
}

func TestWorkToTaskOmitsSharedKinds(t *testing.T) {
	workToTask := WorkToTask()
	for _, kind := range []string{"enrich_media_item", "detect_local_assets", "scan_track_fingerprint", "scan_track_loudness", "scan_album_loudness", "analyze_track_facets", "refresh_artist_centroids", "refresh_album_centroids"} {
		if owner, exists := workToTask[kind]; exists {
			t.Fatalf("shared kind %q resolved to %q", kind, owner)
		}
	}
	if owner := workToTask["ffprobe"]; owner != "scan_libraries" {
		t.Fatalf("ffprobe resolved to %q, want scan_libraries", owner)
	}
	if owner := workToTask["process_scan"]; owner != "scan_libraries" {
		t.Fatalf("process_scan resolved to %q, want scan_libraries", owner)
	}
	if owner := workToTask["search_metadata"]; owner != "scan_libraries" {
		t.Fatalf("search_metadata resolved to %q, want scan_libraries", owner)
	}
	if owner := workToTask["fetch_metadata"]; owner != "scan_libraries" {
		t.Fatalf("fetch_metadata resolved to %q, want scan_libraries", owner)
	}
	if owner := workToTask["apply_metadata"]; owner != "scan_libraries" {
		t.Fatalf("apply_metadata resolved to %q, want scan_libraries", owner)
	}
}

func TestTaskOwnsKind(t *testing.T) {
	for _, tc := range []struct {
		task string
		kind string
	}{
		{task: "scan_libraries", kind: "process_scan"},
		{task: "scan_libraries", kind: "search_metadata"},
		{task: "scan_libraries", kind: "fetch_metadata"},
		{task: "scan_libraries", kind: "apply_metadata"},
		{task: "scan_libraries", kind: "scan_keyframes"},
		{task: "scan_libraries", kind: "enrich_media_item"},
		{task: "scan_libraries", kind: "scan_track_fingerprint"},
		{task: "scan_libraries", kind: "analyze_track_facets"},
		{task: "refresh_stale_items", kind: "detect_local_assets"},
		{task: "scan_music_loudness", kind: "scan_track_loudness"},
	} {
		if !TaskOwnsKind(tc.task, tc.kind) {
			t.Fatalf("expected %q to own %q", tc.task, tc.kind)
		}
	}
	if TaskOwnsKind("refresh_stale_items", "ffprobe") {
		t.Fatal("refresh_stale_items unexpectedly owns ffprobe")
	}
}
