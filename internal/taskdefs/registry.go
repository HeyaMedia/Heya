package taskdefs

type Definition struct {
	ID          string
	KickoffKind string
	WorkKinds   []string
	Synthetic   bool
	// Pump marks kickoffs that stay active for the whole run (snooze loop
	// topping up bounded work batches until the backlog drains) instead of
	// fanning out everything in one shot. The scheduler's max-runtime
	// enforcement leaves a pump kickoff itself alone — the pump checks the
	// window on every wake and winds the run down gracefully, stamping the
	// scheduled_tasks row on the way out.
	Pump bool
}

var definitions = []Definition{
	{ID: "scan_libraries", KickoffKind: "kickoff_library_scan", WorkKinds: []string{"process_scan", "search_metadata", "fetch_metadata", "apply_metadata", "ffprobe", "scan_keyframes", "enrich_media_item", "detect_local_assets"}},
	{ID: "refresh_stale_items", KickoffKind: "kickoff_refresh_stale", WorkKinds: []string{"enrich_media_item", "detect_local_assets"}},
	{ID: "scan_music_loudness", KickoffKind: "kickoff_music_loudness", WorkKinds: []string{"scan_track_loudness", "scan_album_loudness"}, Pump: true},
	{ID: "scan_music_fingerprint", KickoffKind: "kickoff_music_fingerprint", WorkKinds: []string{"scan_track_fingerprint"}, Pump: true},
	{ID: "scan_media_segments", KickoffKind: "kickoff_media_segments", WorkKinds: []string{"scan_media_segments_file"}, Pump: true},
	{ID: "detect_media_segments", KickoffKind: "kickoff_detect_segments", WorkKinds: []string{"detect_segments_season", "detect_segments_movie"}, Pump: true},
	{ID: "generate_trickplay", KickoffKind: "kickoff_trickplay", WorkKinds: []string{"trickplay_file"}},
	{ID: "generate_thumbnails", KickoffKind: "kickoff_thumbnails", WorkKinds: []string{"thumbnail_extra"}},
	{ID: "analyze_music_facets", KickoffKind: "kickoff_sonic_analysis", WorkKinds: []string{"analyze_track_facets", "refresh_artist_centroids", "refresh_album_centroids"}, Pump: true},
	{ID: "cleanup_scanner_artifacts", KickoffKind: "cleanup_scanner_artifacts"},
	{ID: "embed_recommendations", KickoffKind: "kickoff_embed_recommendations"},
	{ID: "sync_music_services", KickoffKind: "kickoff_music_services_sync", WorkKinds: []string{"kickoff_listen_import", "import_listens_batch"}},

	{ID: "transcoding", WorkKinds: []string{"transcode"}, Synthetic: true},
	{ID: "artwork", WorkKinds: []string{"download_image", "fetch_artwork", "save_images"}, Synthetic: true},
	{ID: "nfo_writes", WorkKinds: []string{"save_nfo", "save_music_nfo"}, Synthetic: true},
	{ID: "external_lookups", WorkKinds: []string{"person_fetch", "ratings_fetch"}, Synthetic: true},
	{ID: "refresh_actions", WorkKinds: []string{"force_refresh_metadata", "force_refresh_images"}, Synthetic: true},
	{ID: "cleanup", WorkKinds: []string{"soft_delete"}, Synthetic: true},
}

func All() []Definition {
	out := make([]Definition, len(definitions))
	copy(out, definitions)
	return out
}

func Scheduled() []Definition {
	out := make([]Definition, 0, len(definitions))
	for _, def := range definitions {
		if !def.Synthetic {
			out = append(out, def)
		}
	}
	return out
}

func ByID(id string) (Definition, bool) {
	for _, def := range definitions {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

func TaskKinds(id string) []string {
	def, ok := ByID(id)
	if !ok {
		return nil
	}
	kinds := make([]string, 0, len(def.WorkKinds)+1)
	if def.KickoffKind != "" {
		kinds = append(kinds, def.KickoffKind)
	}
	kinds = append(kinds, def.WorkKinds...)
	return kinds
}

func WorkToTask() map[string]string {
	out := map[string]string{}
	owners := map[string]string{}
	shared := map[string]bool{}
	for _, def := range definitions {
		for _, kind := range TaskKinds(def.ID) {
			if owner, exists := owners[kind]; exists {
				if owner != def.ID {
					shared[kind] = true
				}
				continue
			}
			owners[kind] = def.ID
		}
	}
	for kind, owner := range owners {
		if !shared[kind] {
			out[kind] = owner
		}
	}
	return out
}

func TaskOwnsKind(taskID, kind string) bool {
	if taskID == "" || kind == "" {
		return false
	}
	for _, taskKind := range TaskKinds(taskID) {
		if taskKind == kind {
			return true
		}
	}
	return false
}
