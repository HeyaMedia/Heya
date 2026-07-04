package jellyfin

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
)

// Display preferences, filters, similar items, lyrics, session listing —
// the long tail jellyfin-web and Finamp poke at after login.

// --- Display preferences ---

// displayPreferencesDto is what jellyfin-web reads before rendering its home
// screen. We persist whatever the client posts (keyed per user+client+id in
// system_settings) and serve it back; first hit gets workable defaults.
type displayPreferencesDto struct {
	ID                 string            `json:"Id"`
	SortBy             string            `json:"SortBy"`
	RememberIndexing   bool              `json:"RememberIndexing"`
	PrimaryImageHeight int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int               `json:"PrimaryImageWidth"`
	CustomPrefs        map[string]string `json:"CustomPrefs"`
	ScrollDirection    string            `json:"ScrollDirection"`
	ShowBackdrop       bool              `json:"ShowBackdrop"`
	RememberSorting    bool              `json:"RememberSorting"`
	SortOrder          string            `json:"SortOrder"`
	ShowSidebar        bool              `json:"ShowSidebar"`
	Client             string            `json:"Client"`
}

func displayPrefsKey(userID int64, client, id string) string {
	return fmt.Sprintf("jellyfin.dp.%d.%s.%s", userID, strings.ToLower(client), strings.ToLower(id))
}

// GET /DisplayPreferences/{displayPreferencesId}
func (s *Server) handleGetDisplayPreferences(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	client := queryCI(r, "client")
	if raw, err := s.app.GetSystemSetting(r.Context(), displayPrefsKey(u.ID, client, p["displayPreferencesId"])); err == nil && len(raw) > 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}
	writeJSON(w, http.StatusOK, displayPreferencesDto{
		ID:                 p["displayPreferencesId"],
		SortBy:             "SortName",
		PrimaryImageHeight: 250,
		PrimaryImageWidth:  250,
		CustomPrefs:        map[string]string{},
		ScrollDirection:    "Horizontal",
		ShowBackdrop:       true,
		SortOrder:          "Ascending",
		Client:             client,
	})
}

// POST /DisplayPreferences/{displayPreferencesId}
func (s *Server) handleSetDisplayPreferences(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	client := queryCI(r, "client")
	var raw json.RawMessage
	if err := decodeJSON(r, &raw); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := s.app.SetSystemSetting(r.Context(), displayPrefsKey(u.ID, client, p["displayPreferencesId"]), []byte(raw)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Filters ---

type queryFiltersLegacy struct {
	Genres          []string `json:"Genres"`
	Tags            []string `json:"Tags"`
	OfficialRatings []string `json:"OfficialRatings"`
	Years           []int    `json:"Years"`
}

type queryFilters struct {
	Genres []nameGuidPair `json:"Genres"`
	Tags   []string       `json:"Tags"`
}

func (s *Server) genreNames(r *http.Request) []string {
	rows, err := s.app.ListGenres(r.Context())
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if name, ok := row.Genre.(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

// GET /Items/Filters
func (s *Server) handleItemFilters(w http.ResponseWriter, r *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, queryFiltersLegacy{
		Genres:          s.genreNames(r),
		Tags:            []string{},
		OfficialRatings: []string{},
		Years:           []int{},
	})
}

// GET /Items/Filters2
func (s *Server) handleItemFilters2(w http.ResponseWriter, r *http.Request, _ Params) {
	names := s.genreNames(r)
	genres := make([]nameGuidPair, 0, len(names))
	for _, n := range names {
		genres = append(genres, nameGuidPair{Name: n, ID: EncodeID(KindGenre, hashName(n))})
	}
	writeJSON(w, http.StatusOK, queryFilters{Genres: genres, Tags: []string{}})
}

// --- Similar ---

// GET /Items/{itemId}/Similar (also mounted for /Shows /Movies /Albums
// /Artists /Trailers variants). Backed by media_recommendations rows that
// resolved to local library items; remote-only recommendations are useless
// to a client browsing this server.
func (s *Server) handleSimilar(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	limit, _ := strconv.ParseInt(queryCI(r, "limit"), 10, 32)
	if limit <= 0 || limit > 100 {
		limit = 12
	}

	id, err := DecodeIDKind(p["itemId"], KindItem)
	if err != nil {
		writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
		return
	}
	localIDs, err := s.app.JFSimilarLocalItemIDs(r.Context(), id, int32(limit))
	if err != nil || len(localIDs) == 0 {
		writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
		return
	}
	res, err := s.queryByIDs(r.Context(), u.ID, s.serverID(r), itemsRequest{ids: encodeItemIDs(localIDs)})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func encodeItemIDs(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, EncodeID(KindItem, id))
	}
	return out
}

// --- Lyrics ---

type lyricDto struct {
	Metadata lyricMetadata `json:"Metadata"`
	Lyrics   []lyricLine   `json:"Lyrics"`
}

type lyricMetadata struct {
	IsSynced bool `json:"IsSynced"`
}

type lyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"` // ticks
}

var lrcTimestamp = regexp.MustCompile(`^\[(\d+):(\d{2})(?:[.:](\d{1,3}))?\](.*)$`)

// GET /Audio/{itemId}/Lyrics — serves the track's sidecar lyrics, parsing
// LRC timestamps into synced-lyric ticks (Finamp renders these live).
func (s *Server) handleLyrics(w http.ResponseWriter, r *http.Request, p Params) {
	trackID, err := DecodeIDKind(p["itemId"], KindTrack)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path, ok := s.lyricsPath(r, trackID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path) //nolint:gosec // path comes from track_files rows, not request input
	if err != nil {
		http.NotFound(w, r)
		return
	}

	dto := lyricDto{Lyrics: []lyricLine{}}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if m := lrcTimestamp.FindStringSubmatch(line); m != nil {
			mins, _ := strconv.Atoi(m[1])
			secs, _ := strconv.Atoi(m[2])
			frac := 0
			if m[3] != "" {
				// Normalize centiseconds/milliseconds to ms.
				padded := m[3] + strings.Repeat("0", 3-len(m[3]))
				frac, _ = strconv.Atoi(padded)
			}
			text := strings.TrimSpace(m[4])
			if text == "" {
				continue
			}
			start := (int64(mins)*60+int64(secs))*ticksPerSecond + int64(frac)*10_000
			dto.Metadata.IsSynced = true
			dto.Lyrics = append(dto.Lyrics, lyricLine{Text: text, Start: &start})
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue // LRC metadata tags ([ar:], [ti:], ...)
		}
		dto.Lyrics = append(dto.Lyrics, lyricLine{Text: line})
	}
	if len(dto.Lyrics) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) lyricsPath(r *http.Request, trackID int64) (string, bool) {
	files, err := s.app.ListTrackFiles(r.Context(), trackID)
	if err != nil {
		return "", false
	}
	for _, tf := range files {
		if tf.LyricsPath != "" {
			return tf.LyricsPath, true
		}
	}
	return "", false
}

// --- Sessions ---

// GET /Sessions — live playback sessions. Admins see everything (matches
// Heya's own /api/sessions/active), users see their own.
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	store := s.app.Sessions()
	serverID := s.serverID(r)
	out := []sessionInfo{}
	if store == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	list := store.List()
	for _, sess := range list {
		if !u.IsAdmin && sess.UserID != u.ID {
			continue
		}
		out = append(out, sessionInfo{
			PlayState: playerStateInfo{
				CanSeek:       true,
				IsPaused:      sess.Paused,
				RepeatMode:    "RepeatNone",
				PlaybackOrder: "Default",
			},
			AdditionalUsers:          []any{},
			Capabilities:             clientCapabilities{PlayableMediaTypes: []string{}, SupportedCommands: []string{}},
			RemoteEndPoint:           sess.ClientIP,
			PlayableMediaTypes:       []string{},
			ID:                       sess.SessionID,
			UserID:                   EncodeID(KindUser, sess.UserID),
			UserName:                 sess.Username,
			Client:                   sess.ClientUserAgent,
			LastActivityDate:         sess.LastHeartbeatAt,
			DeviceName:               sess.ClientUserAgent,
			DeviceID:                 sess.SessionID,
			IsActive:                 true,
			ServerID:                 serverID,
			SupportedCommands:        []string{},
			NowPlayingQueue:          []any{},
			NowPlayingQueueFullItems: []any{},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /Sessions/Capabilities and /Capabilities/Full — clients register what
// they can do (remote control etc.). Heya doesn't drive remote control yet,
// so acknowledging is the correct minimal behavior.
func (s *Server) handleSessionsCapabilities(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNoContent)
}

// POST /Sessions/Viewing — "user is looking at item X" telemetry. Ack.
func (s *Server) handleSessionsViewing(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNoContent)
}
