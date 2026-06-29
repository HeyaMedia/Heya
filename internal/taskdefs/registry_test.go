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
	for _, kind := range []string{"enrich_media_item", "detect_local_assets", "scan_track_loudness", "scan_album_loudness"} {
		if owner, exists := workToTask[kind]; exists {
			t.Fatalf("shared kind %q resolved to %q", kind, owner)
		}
	}
	if owner := workToTask["process_file"]; owner != "scan_libraries" {
		t.Fatalf("process_file resolved to %q, want scan_libraries", owner)
	}
}

func TestTaskOwnsKind(t *testing.T) {
	for _, tc := range []struct {
		task string
		kind string
	}{
		{task: "scan_libraries", kind: "enrich_media_item"},
		{task: "refresh_stale_items", kind: "detect_local_assets"},
		{task: "scan_music_loudness", kind: "scan_track_loudness"},
	} {
		if !TaskOwnsKind(tc.task, tc.kind) {
			t.Fatalf("expected %q to own %q", tc.task, tc.kind)
		}
	}
	if TaskOwnsKind("refresh_stale_items", "process_file") {
		t.Fatal("refresh_stale_items unexpectedly owns process_file")
	}
}

func TestKickoffLookup(t *testing.T) {
	for _, def := range Scheduled() {
		id, ok := TaskIDByKickoffKind(def.KickoffKind)
		if !ok {
			t.Fatalf("kickoff kind %q did not resolve", def.KickoffKind)
		}
		if id != def.ID {
			t.Fatalf("kickoff kind %q resolved to %q, want %q", def.KickoffKind, id, def.ID)
		}
	}
}
