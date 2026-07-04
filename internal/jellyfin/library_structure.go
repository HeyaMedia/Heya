package jellyfin

import "net/http"

// GET /Library/VirtualFolders — the physical library structure. Infuse
// fetches this during add-server to enumerate libraries + their paths;
// without it (a 404), Infuse aborts with a generic error. Real Jellyfin
// gates this on admin, but we accept any authenticated user: it only exposes
// library names + already-sanitized paths, and being lenient avoids a 403
// breaking a non-admin client's add flow.

type virtualFolderInfo struct {
	Name               string         `json:"Name"`
	Locations          []string       `json:"Locations"`
	CollectionType     string         `json:"CollectionType"`
	LibraryOptions     libraryOptions `json:"LibraryOptions"`
	ItemID             string         `json:"ItemId"`
	PrimaryImageItemID string         `json:"PrimaryImageItemId"`
	RefreshStatus      string         `json:"RefreshStatus"`
}

type pathInfo struct {
	Path string `json:"Path"`
}

// libraryOptions mirrors the subset of Jellyfin's LibraryOptions that clients
// read. Values are sensible constants — Heya's real per-library tuning lives
// in its own settings, not exposed through the Jellyfin surface.
type libraryOptions struct {
	Enabled                                 bool       `json:"Enabled"`
	EnablePhotos                            bool       `json:"EnablePhotos"`
	EnableRealtimeMonitor                   bool       `json:"EnableRealtimeMonitor"`
	EnableChapterImageExtraction            bool       `json:"EnableChapterImageExtraction"`
	EnableTrickplayImageExtraction          bool       `json:"EnableTrickplayImageExtraction"`
	PathInfos                               []pathInfo `json:"PathInfos"`
	SaveLocalMetadata                       bool       `json:"SaveLocalMetadata"`
	EnableInternetProviders                 bool       `json:"EnableInternetProviders"`
	EnableAutomaticSeriesGrouping           bool       `json:"EnableAutomaticSeriesGrouping"`
	EnableEmbeddedTitles                    bool       `json:"EnableEmbeddedTitles"`
	EnableEmbeddedEpisodeInfos              bool       `json:"EnableEmbeddedEpisodeInfos"`
	AutomaticRefreshIntervalDays            int        `json:"AutomaticRefreshIntervalDays"`
	PreferredMetadataLanguage               string     `json:"PreferredMetadataLanguage"`
	MetadataCountryCode                     string     `json:"MetadataCountryCode"`
	SeasonZeroDisplayName                   string     `json:"SeasonZeroDisplayName"`
	MetadataSavers                          []string   `json:"MetadataSavers"`
	DisabledLocalMetadataReaders            []string   `json:"DisabledLocalMetadataReaders"`
	LocalMetadataReaderOrder                []string   `json:"LocalMetadataReaderOrder"`
	DisabledSubtitleFetchers                []string   `json:"DisabledSubtitleFetchers"`
	SubtitleFetcherOrder                    []string   `json:"SubtitleFetcherOrder"`
	SkipSubtitlesIfEmbeddedSubtitlesPresent bool       `json:"SkipSubtitlesIfEmbeddedSubtitlesPresent"`
	SkipSubtitlesIfAudioTrackMatches        bool       `json:"SkipSubtitlesIfAudioTrackMatches"`
	SubtitleDownloadLanguages               []string   `json:"SubtitleDownloadLanguages"`
	RequirePerfectSubtitleMatch             bool       `json:"RequirePerfectSubtitleMatch"`
	SaveSubtitlesWithMedia                  bool       `json:"SaveSubtitlesWithMedia"`
	TypeOptions                             []any      `json:"TypeOptions"`
}

func (s *Server) handleVirtualFolders(w http.ResponseWriter, r *http.Request, _ Params) {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	out := make([]virtualFolderInfo, 0, len(libs))
	for _, lib := range libs {
		id := EncodeID(KindLibrary, lib.ID)
		locations := make([]string, 0, len(lib.Paths))
		pathInfos := make([]pathInfo, 0, len(lib.Paths))
		for _, p := range lib.Paths {
			sp := sanitizePath(p)
			locations = append(locations, sp)
			pathInfos = append(pathInfos, pathInfo{Path: sp})
		}
		out = append(out, virtualFolderInfo{
			Name:               lib.Name,
			Locations:          locations,
			CollectionType:     collectionType(lib.MediaType),
			ItemID:             id,
			PrimaryImageItemID: id,
			RefreshStatus:      "Idle",
			LibraryOptions: libraryOptions{
				Enabled:                      true,
				EnablePhotos:                 true,
				EnableRealtimeMonitor:        true,
				PathInfos:                    pathInfos,
				EnableInternetProviders:      true,
				AutomaticRefreshIntervalDays: 30,
				PreferredMetadataLanguage:    "en",
				MetadataCountryCode:          "US",
				SeasonZeroDisplayName:        "Specials",
				MetadataSavers:               []string{},
				DisabledLocalMetadataReaders: []string{},
				LocalMetadataReaderOrder:     []string{},
				DisabledSubtitleFetchers:     []string{},
				SubtitleFetcherOrder:         []string{},
				SubtitleDownloadLanguages:    []string{},
				TypeOptions:                  []any{},
			},
		})
	}
	writeJSON(w, http.StatusOK, out)
}
