package jellyfin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

	// Video: probe-backed source description + transcode decision. Caps are
	// derived against the file's own video codec inside videoMediaSource, so
	// a codec-scoped DeviceProfile restriction can't leak across codecs.
	src, plan, caps := s.videoMediaSource(ctx, target, token, req.DeviceProfile)
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
func (s *Server) videoMediaSource(ctx context.Context, target playTarget, token string, profile *deviceProfile) (mediaSourceInfo, transcoder.PlaybackPlan, transcoder.ClientCapabilities) {
	file, err := s.app.EnsureFileProbed(ctx, target.file.ID)
	if err != nil {
		file = target.file
	}
	return s.mediaSourceForFile(ctx, file, target.title, token, profile)
}

// mediaSourceForFile renders a MediaSourceInfo from a library file's stored
// probe data, without triggering a probe — the list-decoration path
// (fields=MediaSources over a whole episode page) must not fan out into
// per-item ffprobe runs. Callers that can afford a probe (PlaybackInfo,
// detail) go through videoMediaSource, which EnsureFileProbed's first.
func (s *Server) mediaSourceForFile(ctx context.Context, file sqlc.LibraryFile, name, token string, profile *deviceProfile) (mediaSourceInfo, transcoder.PlaybackPlan, transcoder.ClientCapabilities) {
	var info mediaprobe.MediaInfo
	if len(file.MediaInfo) > 0 {
		_ = json.Unmarshal(file.MediaInfo, &info)
	}

	caps := capsFromProfile(profile, videoCodecOf(&info))
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
		Name:                       name,
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
		// jellyfin-web's MediaSegmentManager gates its whole /MediaSegments
		// fetch on this at playback start (onPlayerPlaybackStart reads
		// state.MediaSource?.HasSegments and returns early when falsy) — real
		// Jellyfin computes the same per-item EXISTS in
		// MediaSegmentManager.HasSegments. Third-party clients (Streamyfin,
		// Findroid) call /MediaSegments unconditionally and ignore this flag,
		// but jellyfin-web needs it set to ever show a skip button.
		HasSegments: s.app.JFFileHasSegments(ctx, file.ID),
	}, plan, caps
}

// videoCodecOf returns the first video stream's codec name (lowercased).
func videoCodecOf(info *mediaprobe.MediaInfo) string {
	for _, st := range info.Streams {
		if st.CodecType == "video" {
			return strings.ToLower(st.CodecName)
		}
	}
	return ""
}

// attachVideoSource decorates a detail dto with its MediaSources, matching
// upstream's full-detail shape.
func (s *Server) attachVideoSource(ctx context.Context, dto *baseItemDto, itemID string) {
	target, ok := s.resolvePlayTarget(ctx, itemID)
	if !ok || (target.entityType != "movie" && target.entityType != "episode") {
		return
	}
	src, _, _ := s.videoMediaSource(ctx, target, TokenFrom(ctx), nil)
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
	return trackSourceInfo(target.trackFileID, target.file.Path, target.file.Size, target.title, target.trackDuration)
}

// trackSourceInfo is the shared Audio MediaSourceInfo builder — used by
// PlaybackInfo (via trackMediaSource) and by list-level decoration
// (attachTrackSources). Audio needs no probe data: clients stream through
// /Audio/{id}/universal, which negotiates on its own.
func trackSourceInfo(trackFileID int64, path string, size int64, title string, durationSeconds int32) mediaSourceInfo {
	return mediaSourceInfo{
		Protocol:             "File",
		ID:                   EncodeID(KindTrackFile, trackFileID),
		Path:                 sanitizePath(path),
		Type:                 "Default",
		Container:            containerOf(path),
		Size:                 size,
		Name:                 title,
		RunTimeTicks:         int64(durationSeconds) * ticksPerSecond,
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
	s.serveMediaFile(w, r, file.ID, file.Path)
}

// GET /Audio/{itemId}/stream and stream.{container} — direct track bytes.
func (s *Server) handleAudioStream(w http.ResponseWriter, r *http.Request, p Params) {
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok || target.entityType != "track" {
		http.NotFound(w, r)
		return
	}
	s.serveMediaFile(w, r, target.file.ID, target.file.Path)
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
		s.serveMediaFile(w, r, target.file.ID, target.file.Path)
		return
	}

	mgr := s.app.AudioSessions()
	if mgr == nil {
		// No ffmpeg — serve the original bytes and let the client cope;
		// better than a hard 500 for formats it might actually handle.
		s.serveMediaFile(w, r, target.file.ID, target.file.Path)
		return
	}
	outPath, err := mgr.EnsureAAC(r.Context(), target.trackFileID, target.file.Path, transcoder.DefaultAudioBitrateKbps)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Int64("track_file", target.trackFileID).Msg("universal audio transcode failed")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/mp4")
	http.ServeFile(w, r, outPath)
}

// serveMediaFile range-serves a local or SMB-backed media file. In passive
// mode (borrowed prod DB, media files on the prod host's disk) a file that
// isn't present locally is proxied from the upstream Heya's native
// /api/stream/{fileId} endpoint — the client's token is a session row in the
// SHARED database, so it authenticates upstream as-is. Same idea as the
// passive image proxy; without it, dev playback of prod-path media 404s.
func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request, fileID int64, path string) {
	if path == "" {
		http.NotFound(w, r)
		return
	}
	if vfs.IsSMBPath(path) {
		w.Header().Set("Content-Type", contentTypeForPath(path))
		w.Header().Set("Accept-Ranges", "bytes")
		serveVFS(w, r, path)
		return
	}
	f, err := os.Open(path) //nolint:gosec // G304: path comes from library_files rows, not request input
	if err != nil {
		if upstream := s.app.PassiveMediaUpstream(); upstream != "" && fileID > 0 {
			s.proxyUpstreamMedia(w, r, upstream, fileID)
			return
		}
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentTypeForPath(path))
	w.Header().Set("Accept-Ranges", "bytes")
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), f)
}

// passiveMediaClient streams upstream media bytes. No overall timeout — a
// direct-played film runs for hours; the request context governs lifetime.
var passiveMediaClient = &http.Client{}

func (s *Server) proxyUpstreamMedia(w http.ResponseWriter, r *http.Request, upstream string, fileID int64) {
	token := TokenFrom(r.Context())
	if token == "" {
		token = extractAuth(r).Token
	}
	u := fmt.Sprintf("%s/api/stream/%d?token=%s", upstream, fileID, url.QueryEscape(token))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	res, err := passiveMediaClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Int64("file", fileID).Str("component", "jellyfin").Msg("passive media proxy upstream failed")
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer func() { _ = res.Body.Close() }()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, res.Body)
	}
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
		if phase == playStart {
			// Jellyfin's Playing report is the earliest confirmation that audio
			// actually began. This becomes transient external now-playing only.
			if err := s.app.RecordPlayback(ctx, u.ID, service.PlaybackEvent{
				EntityType:   "track",
				EntityID:     target.entityID,
				TotalSeconds: target.trackDuration,
				Source:       "jellyfin",
			}); err != nil {
				log.Warn().Err(err).Str("component", "jellyfin").Msg("now-playing submission failed")
			}
		}
		if phase == playStopped {
			completed := target.trackDuration > 0 && pos >= (target.trackDuration*9)/10
			// A stopped/skimmed track is not a taste event. Only a completed
			// Jellyfin report becomes permanent Heya/external history.
			if completed {
				if err := s.app.RecordPlayback(ctx, u.ID, service.PlaybackEvent{
					EntityType:      "track",
					EntityID:        target.entityID,
					PositionSeconds: pos,
					TotalSeconds:    target.trackDuration,
					Completed:       true,
					Source:          "jellyfin",
				}); err != nil {
					log.Warn().Err(err).Str("component", "jellyfin").Msg("scrobble failed")
				}
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
