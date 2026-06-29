package taskdefs

type Definition struct {
	ID          string
	KickoffKind string
	WorkKinds   []string
	Synthetic   bool
}

var definitions = []Definition{
	{ID: "scan_libraries", KickoffKind: "kickoff_library_scan", WorkKinds: []string{"process_file", "ffprobe", "metadata_match", "enrich_media_item", "detect_local_assets", "scan_track_loudness", "scan_album_loudness"}},
	{ID: "refresh_stale_items", KickoffKind: "kickoff_refresh_stale", WorkKinds: []string{"enrich_media_item", "detect_local_assets"}},
	{ID: "scan_music_loudness", KickoffKind: "kickoff_music_loudness", WorkKinds: []string{"scan_track_loudness", "scan_album_loudness"}},
	{ID: "generate_trickplay", KickoffKind: "kickoff_trickplay", WorkKinds: []string{"trickplay_file"}},
	{ID: "generate_thumbnails", KickoffKind: "kickoff_thumbnails", WorkKinds: []string{"thumbnail_extra"}},
	{ID: "analyze_music_facets", KickoffKind: "kickoff_sonic_analysis", WorkKinds: []string{"analyze_track_facets", "refresh_artist_centroids", "refresh_album_centroids"}},

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

func KindsByTask() map[string][]string {
	out := make(map[string][]string, len(definitions))
	for _, def := range definitions {
		out[def.ID] = TaskKinds(def.ID)
	}
	return out
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

func TaskIDByKickoffKind(kind string) (string, bool) {
	for _, def := range definitions {
		if def.KickoffKind == kind {
			return def.ID, true
		}
	}
	return "", false
}
