package jellyfin

import (
	"net/http"
	"strings"
)

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

// Library-structure mutations. Heya libraries are managed through Heya's own
// settings — the Jellyfin surface never creates, renames, or deletes them.
// Statuses mirror upstream's validation order exactly (blank names 400,
// unknown names/ids 404) and answer 403 where upstream would have mutated,
// so a client sees "not allowed", never a lying 204.

// blankParam reports whether a required name-like parameter is missing or
// whitespace-only (upstream's ArgumentNullException.ThrowIfNullOrWhiteSpace).
func blankParam(v string) bool { return strings.TrimSpace(v) == "" }

func (s *Server) libraryByName(r *http.Request, name string) bool {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		return false
	}
	for _, lib := range libs {
		if lib.Name == name {
			return true
		}
	}
	return false
}

// POST /Library/VirtualFolders — library creation is Heya-managed: 403.
func (s *Server) handleAddVirtualFolder(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusForbidden)
}

// DELETE /Library/VirtualFolders?name=
func (s *Server) handleDeleteVirtualFolder(w http.ResponseWriter, r *http.Request, _ Params) {
	name := queryCI(r, "name")
	if blankParam(name) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.libraryByName(r, name) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// POST /Library/VirtualFolders/Name?name=&newName=
func (s *Server) handleRenameVirtualFolder(w http.ResponseWriter, r *http.Request, _ Params) {
	name, newName := queryCI(r, "name"), queryCI(r, "newName")
	if blankParam(name) || blankParam(newName) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.libraryByName(r, name) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// POST /Library/VirtualFolders/LibraryOptions — {Id, LibraryOptions}.
func (s *Server) handleUpdateLibraryOptions(w http.ResponseWriter, r *http.Request, _ Params) {
	var body struct {
		ID string `json:"Id"`
	}
	if err := decodeJSON(r, &body); err != nil || body.ID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if id, err := DecodeIDKind(body.ID, KindLibrary); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	} else if _, err := s.app.GetLibrary(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// POST /Library/VirtualFolders/Paths — {Name, PathInfo}.
func (s *Server) handleAddMediaPath(w http.ResponseWriter, r *http.Request, _ Params) {
	var body struct {
		Name string `json:"Name"`
	}
	if err := decodeJSON(r, &body); err != nil || blankParam(body.Name) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Upstream 404s when the on-disk path doesn't exist; path additions are
	// Heya-managed regardless, so an unknown library or path is 404 and an
	// existing one would be 403 — a foreign path never exists here.
	w.WriteHeader(http.StatusNotFound)
}

// POST /Library/VirtualFolders/Paths/Update — {Name, PathInfo}.
func (s *Server) handleUpdateMediaPath(w http.ResponseWriter, r *http.Request, _ Params) {
	var body struct {
		Name string `json:"Name"`
	}
	if err := decodeJSON(r, &body); err != nil || blankParam(body.Name) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.libraryByName(r, body.Name) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// DELETE /Library/VirtualFolders/Paths?name=&path=
func (s *Server) handleRemoveMediaPath(w http.ResponseWriter, r *http.Request, _ Params) {
	if blankParam(queryCI(r, "name")) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.libraryByName(r, queryCI(r, "name")) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusForbidden)
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
