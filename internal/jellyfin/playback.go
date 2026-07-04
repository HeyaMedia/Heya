package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sessions"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// Playback: negotiation, byte delivery, and playstate reporting.
//
// The delivery trick that keeps this thin: the client's api_key IS a Heya
// session token, and Heya's native stream endpoints accept ?token= on an
// allowlist (/api/stream/*, /api/music/tracks/*). So PlaybackInfo hands out
// TranscodingUrl / subtitle DeliveryUrls that point straight at the native
// HLS + subtitle stack — sessions, segment pacing, hwaccel, idle-kill all
// reused with zero duplication. Only the URLs clients construct THEMSELVES
// (/Videos/{id}/stream, /Audio/{id}/universal) get real handlers here.

// playTarget is a resolved playable entity.
type playTarget struct {
	entityType string // movie | episode | track
	entityID   int64  // media_item id | tv_episodes id | tracks id

	mediaItemID int64 // owning media_item (series item for episodes)
	title       string
	subtitle    string
	mediaType   string // heya media_type string for the session store

	file       sqlc.LibraryFile
	runtimeMin int32

	seasonNumber  int32
	episodeNumber int32
	episodeTitle  string
	artistName    string
	albumTitle    string
	trackDuration int32
	trackFileID   int64
}

// resolvePlayTarget maps a Jellyfin item id onto the file Heya would play.
func (s *Server) resolvePlayTarget(ctx context.Context, itemID string) (playTarget, bool) {
	kind, id, err := DecodeID(itemID)
	if err != nil {
		return playTarget{}, false
	}

	switch kind {
	case KindItem:
		rows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{
			MediaType: sqlc.MediaTypeMovie, OnlyIds: []int64{id}, Lim: 1,
		})
		if err != nil || len(rows) == 0 {
			return playTarget{}, false
		}
		file, ok, err := s.app.JFMovieFileID(ctx, id)
		if err != nil || !ok {
			return playTarget{}, false
		}
		return playTarget{
			entityType:  "movie",
			entityID:    id,
			mediaItemID: id,
			title:       rows[0].Title,
			mediaType:   "movie",
			file:        file,
			runtimeMin:  rows[0].MovieRuntimeMinutes.Int32,
		}, true

	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 {
			return playTarget{}, false
		}
		ep := rows[0]
		fileID, ok, err := s.app.JFEpisodeFileID(ctx, ep.SeriesMediaItemID, ep.SeasonNumber, ep.EpisodeNumber)
		if err != nil || !ok {
			return playTarget{}, false
		}
		file, err := s.app.GetLibraryFile(ctx, fileID)
		if err != nil {
			return playTarget{}, false
		}
		return playTarget{
			entityType:    "episode",
			entityID:      ep.ID,
			mediaItemID:   ep.SeriesMediaItemID,
			title:         ep.SeriesTitle,
			subtitle:      episodeSubtitle(ep),
			mediaType:     "tv",
			file:          file,
			runtimeMin:    ep.RuntimeMinutes,
			seasonNumber:  ep.SeasonNumber,
			episodeNumber: ep.EpisodeNumber,
			episodeTitle:  ep.Title,
		}, true

	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 {
			return playTarget{}, false
		}
		tr := rows[0]
		t := playTarget{
			entityType:    "track",
			entityID:      tr.ID,
			mediaItemID:   tr.ArtistMediaItemID,
			title:         tr.Title,
			subtitle:      tr.ArtistName + " — " + tr.AlbumTitle,
			mediaType:     "music",
			artistName:    tr.ArtistName,
			albumTitle:    tr.AlbumTitle,
			trackDuration: tr.Duration,
		}
		if tr.BestFileID > 0 {
			t.trackFileID = tr.BestFileID
			if tf, err := s.app.GetTrackFile(ctx, tr.BestFileID); err == nil {
				if lf, err := s.app.GetLibraryFile(ctx, tf.LibraryFileID); err == nil {
					t.file = lf
				}
			}
		}
		return t, t.file.Path != ""
	}
	return playTarget{}, false
}

func episodeSubtitle(ep sqlc.JFListEpisodesRow) string {
	s := "S" + pad2(ep.SeasonNumber) + "E" + pad2(ep.EpisodeNumber)
	if ep.Title != "" {
		s += " · " + ep.Title
	}
	return s
}

func pad2(n int32) string {
	if n < 10 {
		return "0" + strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatInt(int64(n), 10)
}

// GET|POST /Items/{itemId}/PlaybackInfo
func (s *Server) handlePlaybackInfo(w http.ResponseWriter, r *http.Request, p Params) {
	ctx := r.Context()
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok {
		writeJSON(w, http.StatusOK, playbackInfoResponse{
			MediaSources: []mediaSourceInfo{},
			ErrorCode:    "NotAllowed",
		})
		return
	}

	var req playbackInfoRequest
	if r.Method == http.MethodPost {
		_ = decodeJSON(r, &req)
	}
	token := TokenFrom(ctx)
	playSessionID := newPlaySessionID()

	if target.entityType == "track" {
		writeJSON(w, http.StatusOK, playbackInfoResponse{
			MediaSources:  []mediaSourceInfo{s.trackMediaSource(target)},
			PlaySessionID: playSessionID,
		})
		return
	}

	// Video: probe-backed source description + transcode decision.
	caps := capsFromProfile(req.DeviceProfile)
	src, plan := s.videoMediaSource(ctx, target, token, caps)
	directOK := src.SupportsDirectPlay

	if !directOK && src.SupportsTranscoding {
		// Native HLS stack, authenticated by the client's own token.
		src.TranscodingURL = "/api/stream/" + strconv.FormatInt(target.file.ID, 10) +
			"/hls/master.m3u8?token=" + url.QueryEscape(token) +
			"&sid=" + playSessionID + capsQuery(caps)
		src.TranscodingSubProtocol = "hls"
		src.TranscodingContainer = "ts"
		if plan.NeedsFMP4 {
			src.TranscodingContainer = "mp4"
		}
	}

	writeJSON(w, http.StatusOK, playbackInfoResponse{
		MediaSources:  []mediaSourceInfo{src},
		PlaySessionID: playSessionID,
	})
}

// videoMediaSource builds the full MediaSourceInfo for a video target —
// shared by PlaybackInfo and by /Items/{id} detail hydration (upstream
// includes MediaSources on the detail dto and Infuse builds its playability
// decision from it).
func (s *Server) videoMediaSource(ctx context.Context, target playTarget, token string, caps transcoder.ClientCapabilities) (mediaSourceInfo, transcoder.PlaybackPlan) {
	file, err := s.app.EnsureFileProbed(ctx, target.file.ID)
	if err != nil {
		file = target.file
	}
	var info mediaprobe.MediaInfo
	if len(file.MediaInfo) > 0 {
		_ = json.Unmarshal(file.MediaInfo, &info)
	}

	tInfo := toTranscoderInfo(&info)
	plan := transcoder.Decide(&tInfo, caps)
	directOK := plan.Action == transcoder.ActionDirectPlay && !vfs.IsSMBPath(file.Path)

	streams, defAudio, defSub := buildMediaStreams(file.ID, token, &info)
	if streams == nil {
		streams = []mediaStream{}
	}

	return mediaSourceInfo{
		Protocol:                   "File",
		ID:                         EncodeID(KindFile, file.ID),
		Path:                       sanitizePath(file.Path),
		Type:                       "Default",
		Container:                  containerOf(file.Path),
		Size:                       file.Size,
		Name:                       target.title,
		ETag:                       tag32("etag-source", file.ID),
		RunTimeTicks:               int64(info.Duration * float64(ticksPerSecond)),
		SupportsDirectPlay:         directOK,
		SupportsDirectStream:       directOK,
		SupportsTranscoding:        s.app.TranscoderSessions() != nil,
		SupportsProbing:            true,
		VideoType:                  "VideoFile",
		MediaStreams:               streams,
		MediaAttachments:           []any{},
		Formats:                    []string{},
		Bitrate:                    info.BitRate,
		RequiredHTTPHeaders:        map[string]string{},
		DefaultAudioStreamIndex:    defAudio,
		DefaultSubtitleStreamIndex: defSub,
	}, plan
}

// attachVideoSource decorates a detail dto with its MediaSources, matching
// upstream's full-detail shape.
func (s *Server) attachVideoSource(ctx context.Context, dto *baseItemDto, itemID string) {
	target, ok := s.resolvePlayTarget(ctx, itemID)
	if !ok || (target.entityType != "movie" && target.entityType != "episode") {
		return
	}
	src, _ := s.videoMediaSource(ctx, target, TokenFrom(ctx), transcoder.DefaultClientCaps)
	dto.MediaSources = []mediaSourceInfo{src}
	dto.MediaStreams = src.MediaStreams
	dto.Container = src.Container
	dto.VideoType = "VideoFile"
	dto.Path = src.Path // already sanitized in videoMediaSource
	dto.Chapters = []any{}
	dto.Trickplay = map[string]any{}
	for _, ms := range src.MediaStreams {
		if ms.Type == "Video" {
			hd := ms.Height >= 720
			dto.IsHD = &hd
			dto.Width = ms.Width
			dto.Height = ms.Height
			break
		}
	}
}

func (s *Server) trackMediaSource(target playTarget) mediaSourceInfo {
	return mediaSourceInfo{
		Protocol:             "File",
		ID:                   EncodeID(KindTrackFile, target.trackFileID),
		Path:                 sanitizePath(target.file.Path),
		Type:                 "Default",
		Container:            containerOf(target.file.Path),
		Size:                 target.file.Size,
		Name:                 target.title,
		RunTimeTicks:         int64(target.trackDuration) * ticksPerSecond,
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  true,
		MediaStreams:         []mediaStream{},
		MediaAttachments:     []any{},
		Formats:              []string{},
		RequiredHTTPHeaders:  map[string]string{},
	}
}

// GET /Videos/{itemId}/stream and /Videos/{itemId}/stream.{container} —
// direct byte delivery; clients build this URL themselves for direct play.
func (s *Server) handleVideoStream(w http.ResponseWriter, r *http.Request, p Params) {
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok {
		http.NotFound(w, r)
		return
	}
	file := target.file
	// MediaSourceId, when present, must win — multi-version items.
	if msid := queryCI(r, "mediaSourceId"); msid != "" {
		if fid, err := DecodeIDKind(msid, KindFile); err == nil {
			if f, err := s.app.GetLibraryFile(r.Context(), fid); err == nil {
				file = f
			}
		}
	}
	serveMediaFile(w, r, file.Path)
}

// GET /Audio/{itemId}/stream and stream.{container} — direct track bytes.
func (s *Server) handleAudioStream(w http.ResponseWriter, r *http.Request, p Params) {
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok || target.entityType != "track" {
		http.NotFound(w, r)
		return
	}
	serveMediaFile(w, r, target.file.Path)
}

// GET|HEAD /Audio/{itemId}/universal — capability-negotiated audio. Direct
// when the client accepts the on-disk format, else on-the-fly AAC fMP4 via
// the shared audio session manager (same path Heya's own web player uses).
func (s *Server) handleAudioUniversal(w http.ResponseWriter, r *http.Request, p Params) {
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok || target.entityType != "track" {
		http.NotFound(w, r)
		return
	}

	format := containerOf(target.file.Path)
	caps := audioCapsFromContainers(queryCI(r, "container"))
	if transcoder.CanPlayDirect(format, caps) {
		serveMediaFile(w, r, target.file.Path)
		return
	}

	mgr := s.app.AudioSessions()
	if mgr == nil {
		// No ffmpeg — serve the original bytes and let the client cope;
		// better than a hard 500 for formats it might actually handle.
		serveMediaFile(w, r, target.file.Path)
		return
	}
	outPath, err := mgr.EnsureAACMP4(r.Context(), target.trackFileID, target.file.Path)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Int64("track_file", target.trackFileID).Msg("universal audio transcode failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/mp4")
	http.ServeFile(w, r, outPath)
}

// serveMediaFile range-serves a local or SMB-backed media file.
func serveMediaFile(w http.ResponseWriter, r *http.Request, path string) {
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentTypeForPath(path))
	w.Header().Set("Accept-Ranges", "bytes")

	if vfs.IsSMBPath(path) {
		serveVFS(w, r, path)
		return
	}
	f, err := os.Open(path) //nolint:gosec // G304: path comes from library_files rows, not request input
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), f)
}

func serveVFS(w http.ResponseWriter, r *http.Request, smbPath string) {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	source, err := vfs.Open(smbPath[:lastSlash])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() { _ = source.Close() }()
	f, err := source.FS.Open(smbPath[lastSlash+1:])
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, smbPath[lastSlash+1:], stat.ModTime(), rs)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	_, _ = io.Copy(w, f)
}

// --- playstate reporting ---

type playPhase int

const (
	playStart playPhase = iota
	playProgress
	playStopped
)

// POST /Sessions/Playing | /Progress | /Stopped
func (s *Server) handlePlaying(phase playPhase) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, _ Params) {
		u, _ := UserFrom(r.Context())
		var report playbackReport
		if err := decodeJSON(r, &report); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.applyPlaystate(r, u, report, phase)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) applyPlaystate(r *http.Request, u sqlc.User, report playbackReport, phase playPhase) {
	ctx := r.Context()
	target, ok := s.resolvePlayTarget(r.Context(), report.ItemID)
	if !ok {
		log.Debug().Str("component", "jellyfin").Str("item", report.ItemID).Msg("playstate for unresolvable item ignored")
		return
	}

	pos := int32(report.PositionTicks / ticksPerSecond)
	total := s.targetTotalSeconds(ctx, target)

	switch target.entityType {
	case "movie", "episode":
		// Same semantics as Heya's own player: every report upserts resume
		// state; completion derives from total-30s inside the service.
		_, err := s.app.UpdateWatchProgress(ctx, u.ID, target.entityType, target.entityID, pos, total)
		if err != nil {
			log.Warn().Err(err).Str("component", "jellyfin").Msg("watch progress upsert failed")
		}
	case "track":
		if phase == playStopped {
			completed := target.trackDuration > 0 && pos >= (target.trackDuration*9)/10
			if err := s.app.RecordPlayback(ctx, u.ID, service.PlaybackEvent{
				EntityType:      "track",
				EntityID:        target.entityID,
				PositionSeconds: pos,
				TotalSeconds:    target.trackDuration,
				Completed:       completed,
				Source:          "jellyfin",
			}); err != nil {
				log.Warn().Err(err).Str("component", "jellyfin").Msg("scrobble failed")
			}
		}
	}

	// Mirror into the live activity panel.
	if store := s.app.Sessions(); store != nil {
		sessionID := firstNonEmpty(report.PlaySessionID, DeviceFrom(ctx).DeviceID)
		if sessionID == "" {
			return
		}
		sessionID = "jellyfin-" + sessionID
		if phase == playStopped {
			store.EndForUser(sessionID, u.ID)
			return
		}
		device := DeviceFrom(ctx)
		store.Upsert(sessions.Session{
			SessionID:       sessionID,
			UserID:          u.ID,
			Username:        u.Username,
			FileID:          target.file.ID,
			MediaItemID:     target.mediaItemID,
			MediaTitle:      target.title,
			MediaSubtitle:   target.subtitle,
			MediaType:       target.mediaType,
			EntityType:      target.entityType,
			EntityID:        target.entityID,
			SeasonNumber:    target.seasonNumber,
			EpisodeNumber:   target.episodeNumber,
			EpisodeTitle:    target.episodeTitle,
			ArtistName:      target.artistName,
			AlbumTitle:      target.albumTitle,
			PositionSeconds: pos,
			TotalSeconds:    total,
			Paused:          report.IsPaused,
			PlaybackAction:  "direct_play",
			Container:       containerOf(target.file.Path),
			ClientUserAgent: deviceUserAgent(device, r),
			ClientIP:        clientIP(r),
			StartedAt:       time.Now(),
			LastHeartbeatAt: time.Now(),
		})
	}
}

// targetTotalSeconds prefers entity runtime metadata, falling back to the
// probed file duration (metadata runtimes are often missing on fresh rips).
func (s *Server) targetTotalSeconds(_ context.Context, target playTarget) int32 {
	if target.entityType == "track" {
		return target.trackDuration
	}
	if target.runtimeMin > 0 {
		return target.runtimeMin * 60
	}
	if len(target.file.MediaInfo) > 0 {
		var info mediaprobe.MediaInfo
		if err := json.Unmarshal(target.file.MediaInfo, &info); err == nil && info.Duration > 0 {
			return int32(info.Duration)
		}
	}
	return 0
}

// POST /Sessions/Playing/Ping — keepalive for active transcodes. The native
// session manager already idle-kills on segment inactivity; ack is enough.
func (s *Server) handlePlayingPing(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.WriteHeader(http.StatusNoContent)
}

func newPlaySessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "psid-fallback"
	}
	return hex.EncodeToString(buf)
}
