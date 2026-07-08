package scanner

func filterResultToScopes(result Result, scopes []string, emit Emitter) Result {
	if len(normalizedScopeDirs(scopes)) == 0 {
		return result
	}

	result.Inventory = FilterInventoryToScopes(result.Inventory, scopes, emit)
	relPaths := inventoryRelPathSet(result.Inventory)
	if len(relPaths) == 0 {
		return Result{Inventory: result.Inventory}
	}

	result = filterMovieResultToRelPaths(result, relPaths)
	result = filterTVResultToRelPaths(result, relPaths)
	result = filterMusicResultToRelPaths(result, relPaths)
	result = filterBookResultToRelPaths(result, relPaths)
	return result
}

func filterResultToIdentityKey(result Result, key string) Result {
	if key == "" {
		return result
	}
	relPaths := resultRelPathsForIdentityKey(result, key)
	if len(relPaths) > 0 {
		result.Inventory = filterInventoryToRelPaths(result.Inventory, relPaths)
		result = filterMovieResultToRelPaths(result, relPaths)
		result = filterTVResultToRelPaths(result, relPaths)
		result = filterMusicResultToRelPaths(result, relPaths)
		result = filterBookResultToRelPaths(result, relPaths)
	}

	keys := map[string]bool{key: true}
	result.MovieSearch = filterMovieSearchToKeys(result.MovieSearch, keys)
	result.MovieMetadata = filterMovieMetadataToKeys(result.MovieMetadata, keys)
	result.MovieApply = filterMovieApplyToKeys(result.MovieApply, keys)

	result.TVSearch = filterTVSearchToKeys(result.TVSearch, keys)
	result.TVMetadata = filterTVMetadataToKeys(result.TVMetadata, keys)
	result.TVApply = filterTVApplyToKeys(result.TVApply, keys)

	result.MusicSearch = filterMusicSearchToKeys(result.MusicSearch, keys)
	result.MusicApply = filterMusicApplyToKeys(result.MusicApply, keys)

	result.BookSearch = filterBookSearchToKeys(result.BookSearch, keys)
	result.BookMetadata = filterBookMetadataToKeys(result.BookMetadata, keys)
	result.BookApply = filterBookApplyToKeys(result.BookApply, keys)
	return result
}

func resultRelPathsForIdentityKey(result Result, key string) map[string]bool {
	relPaths := map[string]bool{}
	for _, match := range result.MovieMatches {
		if match.Key != key {
			continue
		}
		addStringsToSet(relPaths, match.Files)
		for _, asset := range match.Assets {
			relPaths[asset.RelPath] = true
		}
		addStringsToSet(relPaths, match.NFOs)
	}
	for _, match := range result.TVMatches {
		if match.Key != key {
			continue
		}
		addStringsToSet(relPaths, match.Files)
		addStringsToSet(relPaths, match.Subtitles)
		addStringsToSet(relPaths, match.NFOs)
		addStringsToSet(relPaths, match.Plexmatches)
		for _, asset := range match.Assets {
			relPaths[asset.RelPath] = true
		}
		for _, plan := range match.Plans {
			addStringsToSet(relPaths, plan.Files)
			addStringsToSet(relPaths, plan.Subtitles)
			if plan.NFO != "" {
				relPaths[plan.NFO] = true
			}
			if plan.Plexmatch != "" {
				relPaths[plan.Plexmatch] = true
			}
			for _, asset := range plan.Assets {
				relPaths[asset.RelPath] = true
			}
		}
	}
	for _, artist := range result.MusicArtists {
		if artist.Key != key {
			continue
		}
		addStringsToSet(relPaths, artist.Files)
		for _, album := range artist.Albums {
			addStringsToSet(relPaths, album.Files)
			addStringsToSet(relPaths, album.NFOs)
			for _, track := range album.Tracks {
				if track.RelPath != "" {
					relPaths[track.RelPath] = true
				}
			}
		}
	}
	for _, plan := range result.BookPlans {
		if plan.Key != key {
			continue
		}
		addStringsToSet(relPaths, plan.Files)
		for _, asset := range plan.Assets {
			relPaths[asset.RelPath] = true
		}
	}
	return relPaths
}

func filterInventoryToRelPaths(inv Inventory, relPaths map[string]bool) Inventory {
	out := Inventory{Roots: make([]InventoryRoot, 0, len(inv.Roots))}
	for _, root := range inv.Roots {
		next := InventoryRoot{Root: root.Root}
		for _, file := range root.Files {
			if relPaths[file.RelPath] {
				next.Files = append(next.Files, file)
			}
		}
		if len(next.Files) > 0 {
			out.Roots = append(out.Roots, next)
		}
	}
	return out
}

func addStringsToSet(set map[string]bool, items []string) {
	for _, item := range items {
		if item != "" {
			set[item] = true
		}
	}
}

func inventoryRelPathSet(inv Inventory) map[string]bool {
	out := map[string]bool{}
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if file.RelPath != "" {
				out[file.RelPath] = true
			}
		}
	}
	return out
}

func filterMovieResultToRelPaths(result Result, relPaths map[string]bool) Result {
	result.Movies = filterMoviePlansToRelPaths(result.Movies, relPaths)

	keys := map[string]bool{}
	result.MovieMatches = filterMovieMatchesToRelPaths(result.MovieMatches, relPaths, keys)
	result.MovieSearch = filterMovieSearchToKeys(result.MovieSearch, keys)
	result.MovieMaterialize = filterMovieMaterializeToRelPaths(result.MovieMaterialize, relPaths, keys)
	result.MovieApply = filterMovieApplyToKeys(result.MovieApply, keys)
	result.MovieMetadata = filterMovieMetadataToKeys(result.MovieMetadata, keys)
	return result
}

func filterMoviePlansToRelPaths(items []MoviePlan, relPaths map[string]bool) []MoviePlan {
	out := make([]MoviePlan, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Parts = filterMoviePartsToRelPaths(item.Parts, relPaths)
		item.Assets = filterMovieAssetsToRelPaths(item.Assets, relPaths)
		if !relPaths[item.NFO] {
			item.NFO = ""
		}
		if len(item.Files) > 0 {
			out = append(out, item)
		}
	}
	return out
}

func filterMoviePartsToRelPaths(items []MoviePartPlan, relPaths map[string]bool) []MoviePartPlan {
	out := make([]MoviePartPlan, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterMovieAssetsToRelPaths(items []MovieAssetPlan, relPaths map[string]bool) []MovieAssetPlan {
	out := make([]MovieAssetPlan, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterMovieMatchesToRelPaths(items []MovieMatch, relPaths map[string]bool, keys map[string]bool) []MovieMatch {
	out := make([]MovieMatch, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Assets = filterMovieAssetsToRelPaths(item.Assets, relPaths)
		item.NFOs = filterStringsToSet(item.NFOs, relPaths)
		if len(item.Files) == 0 {
			continue
		}
		keys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterMovieSearchToKeys(items []MovieSearchMatch, keys map[string]bool) []MovieSearchMatch {
	out := make([]MovieSearchMatch, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterMovieMetadataToKeys(items []MovieFetchPreview, keys map[string]bool) []MovieFetchPreview {
	out := make([]MovieFetchPreview, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterMovieMaterializeToRelPaths(items []MovieMaterializePreview, relPaths map[string]bool, keys map[string]bool) []MovieMaterializePreview {
	out := make([]MovieMaterializePreview, 0, len(items))
	for _, item := range items {
		item.FileActions = filterMovieFileActionsToRelPaths(item.FileActions, relPaths)
		if len(item.FileActions) == 0 && !keys[item.Key] {
			continue
		}
		keys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterMovieFileActionsToRelPaths(items []MovieMaterializeFileAction, relPaths map[string]bool) []MovieMaterializeFileAction {
	out := make([]MovieMaterializeFileAction, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterMovieApplyToKeys(items []MovieApplyResult, keys map[string]bool) []MovieApplyResult {
	out := make([]MovieApplyResult, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterTVResultToRelPaths(result Result, relPaths map[string]bool) Result {
	result.TVPlans = filterTVPlansToRelPaths(result.TVPlans, relPaths)

	keys := map[string]bool{}
	result.TVMatches = filterTVMatchesToRelPaths(result.TVMatches, relPaths, keys)
	result.TVSearch = filterTVSearchToKeys(result.TVSearch, keys)
	result.TVMaterialize = filterTVMaterializeToRelPaths(result.TVMaterialize, relPaths, keys)
	result.TVApply = filterTVApplyToKeys(result.TVApply, keys)
	result.TVMetadata = filterTVMetadataToKeys(result.TVMetadata, keys)
	return result
}

func filterTVPlansToRelPaths(items []TVPlan, relPaths map[string]bool) []TVPlan {
	out := make([]TVPlan, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Assets = filterTVAssetsToRelPaths(item.Assets, relPaths)
		item.Subtitles = filterStringsToSet(item.Subtitles, relPaths)
		if !relPaths[item.NFO] {
			item.NFO = ""
		}
		if !relPaths[item.Plexmatch] {
			item.Plexmatch = ""
		}
		if len(item.Files) > 0 {
			out = append(out, item)
		}
	}
	return out
}

func filterTVAssetsToRelPaths(items []TVAssetPlan, relPaths map[string]bool) []TVAssetPlan {
	out := make([]TVAssetPlan, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterTVMatchesToRelPaths(items []TVMatch, relPaths map[string]bool, keys map[string]bool) []TVMatch {
	out := make([]TVMatch, 0, len(items))
	for _, item := range items {
		item.Plans = filterTVPlansToRelPaths(item.Plans, relPaths)
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Assets = filterTVAssetsToRelPaths(item.Assets, relPaths)
		item.Subtitles = filterStringsToSet(item.Subtitles, relPaths)
		item.NFOs = filterStringsToSet(item.NFOs, relPaths)
		item.Plexmatches = filterStringsToSet(item.Plexmatches, relPaths)
		if len(item.Files) == 0 && len(item.Plans) == 0 {
			continue
		}
		keys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterTVSearchToKeys(items []TVSearchMatch, keys map[string]bool) []TVSearchMatch {
	out := make([]TVSearchMatch, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterTVMetadataToKeys(items []TVFetchPreview, keys map[string]bool) []TVFetchPreview {
	out := make([]TVFetchPreview, 0, len(items))
	for _, item := range items {
		if keys[item.Key] || anyStringInSet(item.Keys, keys) {
			out = append(out, item)
		}
	}
	return out
}

func filterTVMaterializeToRelPaths(items []TVMaterializePreview, relPaths map[string]bool, keys map[string]bool) []TVMaterializePreview {
	out := make([]TVMaterializePreview, 0, len(items))
	for _, item := range items {
		item.FileActions = filterMovieFileActionsToRelPaths(item.FileActions, relPaths)
		if len(item.FileActions) == 0 && !keys[item.Key] && !anyStringInSet(item.Keys, keys) {
			continue
		}
		keys[item.Key] = true
		for _, key := range item.Keys {
			keys[key] = true
		}
		out = append(out, item)
	}
	return out
}

func filterTVApplyToKeys(items []TVApplyResult, keys map[string]bool) []TVApplyResult {
	out := make([]TVApplyResult, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterMusicResultToRelPaths(result Result, relPaths map[string]bool) Result {
	artistKeys := map[string]bool{}
	albumKeys := map[string]bool{}

	result.MusicTracks = filterMusicTracksToRelPaths(result.MusicTracks, relPaths)
	result.MusicAlbums = filterMusicAlbumsToRelPaths(result.MusicAlbums, relPaths, albumKeys)
	result.MusicArtists = filterMusicArtistsToRelPaths(result.MusicArtists, relPaths, artistKeys, albumKeys)
	result.MusicSearch = filterMusicSearchToKeys(result.MusicSearch, artistKeys)
	result.MusicMetadata = filterMusicMetadataToKeys(result.MusicMetadata, artistKeys, albumKeys, relPaths)
	result.MusicMaterialize = filterMusicMaterializeToRelPaths(result.MusicMaterialize, relPaths, artistKeys, albumKeys)
	result.MusicApply = filterMusicApplyToKeys(result.MusicApply, artistKeys)
	return result
}

func filterMusicTracksToRelPaths(items []MusicTrackPlan, relPaths map[string]bool) []MusicTrackPlan {
	out := make([]MusicTrackPlan, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterMusicAlbumsToRelPaths(items []MusicAlbumPlan, relPaths map[string]bool, albumKeys map[string]bool) []MusicAlbumPlan {
	out := make([]MusicAlbumPlan, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Tracks = filterMusicTracksToRelPaths(item.Tracks, relPaths)
		item.NFOs = filterStringsToSet(item.NFOs, relPaths)
		if len(item.Files) == 0 && len(item.Tracks) == 0 {
			continue
		}
		albumKeys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterMusicArtistsToRelPaths(items []MusicArtistPlan, relPaths map[string]bool, artistKeys map[string]bool, albumKeys map[string]bool) []MusicArtistPlan {
	out := make([]MusicArtistPlan, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Albums = filterMusicAlbumsToRelPaths(item.Albums, relPaths, albumKeys)
		if len(item.Files) == 0 && len(item.Albums) == 0 {
			continue
		}
		artistKeys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterMusicSearchToKeys(items []MusicSearchMatch, keys map[string]bool) []MusicSearchMatch {
	out := make([]MusicSearchMatch, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterMusicMetadataToKeys(items []MusicFetchPreview, keys, albumKeys, relPaths map[string]bool) []MusicFetchPreview {
	out := make([]MusicFetchPreview, 0, len(items))
	for _, item := range items {
		if !keys[item.Key] {
			continue
		}
		item.AlbumMappings = filterMusicAlbumFetchMatchesToScopes(item.AlbumMappings, albumKeys, relPaths)
		item.LocalAlbums = len(item.AlbumMappings)
		item.MappedAlbums = countMappedMusicAlbumMappings(item.AlbumMappings)
		item.LocalTracks = countLocalMusicMappingTracks(item.AlbumMappings)
		item.MappedTracks = countMappedMusicMappingTracks(item.AlbumMappings)
		out = append(out, item)
	}
	return out
}

func filterMusicAlbumFetchMatchesToScopes(items []MusicAlbumFetchMatch, albumKeys, relPaths map[string]bool) []MusicAlbumFetchMatch {
	out := make([]MusicAlbumFetchMatch, 0, len(items))
	for _, item := range items {
		item.TrackMappings = filterMusicTrackFetchMatchesToRelPaths(item.TrackMappings, relPaths)
		if !albumKeys[item.Key] && len(item.TrackMappings) == 0 {
			continue
		}
		item.LocalTracks = len(item.TrackMappings)
		item.MappedTracks = countMappedMusicTrackMatches(item.TrackMappings)
		out = append(out, item)
	}
	return out
}

func filterMusicTrackFetchMatchesToRelPaths(items []MusicTrackFetchMatch, relPaths map[string]bool) []MusicTrackFetchMatch {
	out := make([]MusicTrackFetchMatch, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterMusicMaterializeToRelPaths(items []MusicMaterializePreview, relPaths map[string]bool, artistKeys, albumKeys map[string]bool) []MusicMaterializePreview {
	out := make([]MusicMaterializePreview, 0, len(items))
	for _, item := range items {
		item.FileActions = filterMovieFileActionsToRelPaths(item.FileActions, relPaths)
		item.AlbumMappings = filterMusicAlbumFetchMatchesToScopes(item.AlbumMappings, albumKeys, relPaths)
		item.AlbumActions = filterMusicAlbumActionsToKeys(item.AlbumActions, albumKeys)
		if len(item.FileActions) == 0 && len(item.AlbumMappings) == 0 && !artistKeys[item.Key] {
			continue
		}
		artistKeys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterMusicAlbumActionsToKeys(items []MusicMaterializeAlbumAction, keys map[string]bool) []MusicMaterializeAlbumAction {
	out := make([]MusicMaterializeAlbumAction, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterMusicApplyToKeys(items []MusicApplyResult, keys map[string]bool) []MusicApplyResult {
	out := make([]MusicApplyResult, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func countMappedMusicAlbumMappings(items []MusicAlbumFetchMatch) int {
	n := 0
	for _, item := range items {
		if item.RemoteAlbum != "" {
			n++
		}
	}
	return n
}

func countLocalMusicMappingTracks(items []MusicAlbumFetchMatch) int {
	n := 0
	for _, item := range items {
		n += item.LocalTracks
	}
	return n
}

func countMappedMusicMappingTracks(items []MusicAlbumFetchMatch) int {
	n := 0
	for _, item := range items {
		n += item.MappedTracks
	}
	return n
}

func countMappedMusicTrackMatches(items []MusicTrackFetchMatch) int {
	n := 0
	for _, item := range items {
		if item.Matched {
			n++
		}
	}
	return n
}

func filterBookResultToRelPaths(result Result, relPaths map[string]bool) Result {
	keys := map[string]bool{}
	result.BookPlans = filterBookPlansToRelPaths(result.BookPlans, relPaths, keys)
	result.BookSearch = filterBookSearchToKeys(result.BookSearch, keys)
	result.BookMaterialize = filterBookMaterializeToRelPaths(result.BookMaterialize, relPaths, keys)
	result.BookApply = filterBookApplyToKeys(result.BookApply, keys)
	result.BookMetadata = filterBookMetadataToKeys(result.BookMetadata, keys)
	return result
}

func filterBookPlansToRelPaths(items []BookPlan, relPaths map[string]bool, keys map[string]bool) []BookPlan {
	out := make([]BookPlan, 0, len(items))
	for _, item := range items {
		item.Files = filterStringsToSet(item.Files, relPaths)
		item.Assets = filterBookAssetsToRelPaths(item.Assets, relPaths)
		if len(item.Files) == 0 {
			continue
		}
		keys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterBookAssetsToRelPaths(items []BookAssetPlan, relPaths map[string]bool) []BookAssetPlan {
	out := make([]BookAssetPlan, 0, len(items))
	for _, item := range items {
		if relPaths[item.RelPath] {
			out = append(out, item)
		}
	}
	return out
}

func filterBookSearchToKeys(items []BookSearchMatch, keys map[string]bool) []BookSearchMatch {
	out := make([]BookSearchMatch, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterBookMetadataToKeys(items []BookFetchPreview, keys map[string]bool) []BookFetchPreview {
	out := make([]BookFetchPreview, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterBookMaterializeToRelPaths(items []BookMaterializePreview, relPaths map[string]bool, keys map[string]bool) []BookMaterializePreview {
	out := make([]BookMaterializePreview, 0, len(items))
	for _, item := range items {
		item.FileActions = filterMovieFileActionsToRelPaths(item.FileActions, relPaths)
		if len(item.FileActions) == 0 && !keys[item.Key] {
			continue
		}
		keys[item.Key] = true
		out = append(out, item)
	}
	return out
}

func filterBookApplyToKeys(items []BookApplyResult, keys map[string]bool) []BookApplyResult {
	out := make([]BookApplyResult, 0, len(items))
	for _, item := range items {
		if keys[item.Key] {
			out = append(out, item)
		}
	}
	return out
}

func filterStringsToSet(items []string, set map[string]bool) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if set[item] {
			out = append(out, item)
		}
	}
	return out
}

func anyStringInSet(items []string, set map[string]bool) bool {
	for _, item := range items {
		if set[item] {
			return true
		}
	}
	return false
}
