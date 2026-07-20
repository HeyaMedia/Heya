package jellyfin

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Graceful stubs: endpoints clients probe that map to features Heya doesn't
// have (LiveTV tuners, plugins, channels...). Each returns exactly what a
// stock Jellyfin with that feature absent/disabled returns — a client that
// probes must conclude "feature off", never "broken server". Everything here
// carries manifest status opStubbed.

func (s *Server) stubEmptyQueryResult(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
}

func (s *Server) stubEmptyArray(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) stubNoContent(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stubNotFound(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNotFound)
}

// Startup wizard endpoints. A Heya server is configured through Heya itself,
// so from the Jellyfin surface the wizard is always complete — and upstream
// locks every /Startup route behind 401 once it is.
func (s *Server) handleStartupLocked(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusUnauthorized)
}

// GET /System/ActivityLog/Entries — Heya's activity feed isn't mapped onto
// Jellyfin's yet; an empty page is a valid answer (fresh servers have one).
func (s *Server) handleActivityLogEntries(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0})
}

// GET /web/ConfigurationPages — no plugins, no pages. /web/ConfigurationPage
// (singular, ?name=) therefore always 404s.
func (s *Server) handleConfigurationPages(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, []any{})
}

// GET /System/Endpoint — network locality hints; clients only branch UI on it.
func (s *Server) handleSystemEndpoint(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, map[string]bool{"IsLocal": false, "IsInNetwork": true})
}

// GET /LiveTv/Info — the canonical "LiveTV is off" answer.
func (s *Server) handleLiveTvInfo(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, map[string]any{
		"Services":     []any{},
		"IsEnabled":    false,
		"EnabledUsers": []string{},
	})
}

// GET /Auth/Providers and /Auth/PasswordResetProviders — the defaults every
// stock Jellyfin reports.
func (s *Server) handleAuthProviders(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, []map[string]any{{
		"Name": "Default", "Id": "Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider", "IsDefault": true,
	}})
}

func (s *Server) handlePasswordResetProviders(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, []map[string]any{{
		"Name": "Default", "Id": "Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider", "IsDefault": true,
	}})
}

// GET /Items/{itemId}/ThemeMedia (+ ThemeSongs / ThemeVideos) — jellyfin-web
// asks on every detail page.
func (s *Server) handleThemeMedia(w http.ResponseWriter, r *http.Request, _ Params) {
	empty := map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0, "OwnerId": ""}
	writeJSON(w, http.StatusOK, map[string]any{
		"ThemeVideosResult":     empty,
		"ThemeSongsResult":      empty,
		"SoundtrackSongsResult": empty,
	})
}

func (s *Server) handleThemeSongsOrVideos(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0, "OwnerId": ""})
}

// GET /UserImage — user avatar. Heya users have no profile images; upstream
// answers 404 for an avatar-less user, and clients (Wholphin's nav drawer,
// jellyfin-web's header) fall back to an initials placeholder on exactly
// that. Registered so the miss is a spec-correct 404 rather than an
// unrouted one.
func (s *Server) handleUserImage(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNotFound)
}

// GET /Videos/{itemId}/AdditionalParts — Infuse probes for multi-part films.
func (s *Server) handleAdditionalParts(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
}

// GET /Devices — device registry; Heya's equivalent lives in the sessions
// UI, and nothing breaks with an empty list.
func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, map[string]any{"Items": []any{}, "TotalRecordCount": 0, "StartIndex": 0})
}

// --- small real implementations ---

// GET /GetUtcTime — TimeSync for SyncPlay-capable clients.
func (s *Server) handleGetUtcTime(w http.ResponseWriter, _ *http.Request, _ Params) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.9999999Z")
	writeJSON(w, http.StatusOK, map[string]string{
		"RequestReceptionTime":     now,
		"ResponseTransmissionTime": now,
	})
}

// GET /Playback/BitrateTest?size= — clients measure bandwidth against this
// before picking a bitrate. Upstream validates size to [1, 100_000_000] with
// a 400 (its [Range] attribute) and defaults to 100 KiB.
func (s *Server) handleBitrateTest(w http.ResponseWriter, r *http.Request, _ Params) {
	size := int64(102400)
	if raw := queryCI(r, "size"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || parsed > 100_000_000 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		size = parsed
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	buf := make([]byte, 64<<10)
	_, _ = rand.Read(buf)
	for size > 0 {
		n := int64(len(buf))
		if n > size {
			n = size
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return
		}
		size -= n
	}
}

// GET /Items/Counts — library totals for dashboards.
func (s *Server) handleItemCounts(w http.ResponseWriter, r *http.Request, _ Params) {
	ctx := r.Context()
	counts := map[string]int64{}
	type probe struct {
		key string
		fn  func() (int64, error)
	}
	probes := []probe{
		{"MovieCount", func() (int64, error) { return s.countItems(ctx, sqlc.MediaTypeMovie) }},
		{"SeriesCount", func() (int64, error) { return s.countItems(ctx, sqlc.MediaTypeTv) }},
		{"ArtistCount", func() (int64, error) { return s.countItems(ctx, sqlc.MediaTypeMusic) }},
		{"BookCount", func() (int64, error) { return s.countItems(ctx, sqlc.MediaTypeBook) }},
	}
	for _, p := range probes {
		if n, err := p.fn(); err == nil {
			counts[p.key] = n
		}
	}
	writeJSON(w, http.StatusOK, counts)
}

// GET /Genres — the genre catalog as browsable items.
func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request, _ Params) {
	names := s.genreNames(r)
	serverID := s.serverID(r)
	items := make([]baseItemDto, 0, len(names))
	for _, n := range names {
		items = append(items, s.genreDto(n, serverID))
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items)})
}

// GET /Genres/{genreName} — one genre as a browsable item (the header a client
// loads before listing the genre's titles via /Items?GenreIds=). 404 for an
// unknown name, matching upstream; the match is case-insensitive and returns
// the catalog's canonical casing.
func (s *Server) handleGenreDetail(w http.ResponseWriter, r *http.Request, p Params) {
	name := p["genreName"]
	serverID := s.serverID(r)
	for _, known := range s.genreNames(r) {
		if strings.EqualFold(known, name) {
			writeJSON(w, http.StatusOK, s.genreDto(known, serverID))
			return
		}
	}
	http.NotFound(w, r)
}

// genreDto builds the genre pseudo-item shared by /Genres and /Genres/{name}.
func (s *Server) genreDto(name, serverID string) baseItemDto {
	dto := baseItemDto{
		Name:              name,
		ID:                EncodeID(KindGenre, hashName(name)),
		ServerID:          serverID,
		Type:              "Genre",
		MediaType:         "Unknown",
		IsFolder:          true,
		Taglines:          []string{},
		Genres:            []string{},
		LocationType:      "FileSystem",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
	}
	return dto.done()
}

// GET /Search/Hints — cross-type quick search.
func (s *Server) handleSearchHints(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	term := queryCI(r, "searchTerm")
	if term == "" {
		writeJSON(w, http.StatusOK, map[string]any{"SearchHints": []any{}, "TotalRecordCount": 0})
		return
	}
	limit, _ := strconv.ParseInt(queryCI(r, "limit"), 10, 32)
	if limit <= 0 || limit > 100 {
		limit = 24
	}

	hints := []map[string]any{}
	for _, t := range []string{"Movie", "Series", "MusicArtist", "MusicAlbum", "Audio", "Episode"} {
		res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), itemsRequest{
			types:      []string{t},
			searchTerm: term,
			recursive:  true,
			limit:      int(limit),
		})
		if err != nil {
			continue
		}
		for _, item := range res.Items {
			hints = append(hints, map[string]any{
				"ItemId":          item.ID,
				"Id":              item.ID,
				"Name":            item.Name,
				"Type":            item.Type,
				"MediaType":       item.MediaType,
				"ProductionYear":  item.ProductionYear,
				"PrimaryImageTag": item.ImageTags["Primary"],
				"Album":           item.Album,
				"AlbumArtist":     item.AlbumArtist,
				"Series":          item.SeriesName,
			})
			if int64(len(hints)) >= limit {
				break
			}
		}
		if int64(len(hints)) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"SearchHints": hints, "TotalRecordCount": len(hints)})
}

// GET /Items/{itemId}/Download and /Items/{itemId}/File — full-file
// delivery; CanDownload is advertised on video and track dtos.
func (s *Server) handleItemDownload(w http.ResponseWriter, r *http.Request, p Params) {
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.serveMediaFile(w, r, target.file.ID, target.file.Path)
}

// Subtitle delivery addressed the Jellyfin way (item + media source + stream
// index). Redirects to Heya's native extraction endpoint, which the client's
// token authorizes — same trick as TranscodingUrl.
func (s *Server) handleSubtitleStream(w http.ResponseWriter, r *http.Request, p Params) {
	msid := firstNonEmpty(p["routeMediaSourceId"], p["mediaSourceId"])
	fileID, err := DecodeIDKind(msid, KindFile)
	if err != nil {
		// Some clients echo the item id as the media source; fall back to
		// resolving the item's file.
		itemID := firstNonEmpty(p["routeItemId"], p["itemId"])
		target, ok := s.resolvePlayTarget(r.Context(), itemID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		fileID = target.file.ID
	}
	index, err := strconv.ParseInt(firstNonEmpty(p["routeIndex"], p["index"]), 10, 32)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	token := extractAuth(r).Token
	http.Redirect(w, r,
		"/api/stream/"+strconv.FormatInt(fileID, 10)+"/subtitles/"+strconv.FormatInt(index, 10)+"?token="+url.QueryEscape(token),
		http.StatusFound)
}

func (s *Server) countItems(ctx context.Context, mediaType sqlc.MediaType) (int64, error) {
	_, total, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{MediaType: mediaType, Lim: 1})
	return total, err
}
