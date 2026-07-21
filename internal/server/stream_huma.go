package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
)

// registerStreamRoutes covers the JSON read-side of streaming: probe info,
// transcode progress, subtitle track list. Binary streaming endpoints
// (HLS playlists/segments, direct stream, subtitle file body, trickplay
// sprites/VTT) stay on the stdlib mux — see streaming_handlers.go.
func registerStreamRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/stream/{file_id}/info", "stream-info", "Probe + transcode plan for a file", "Streaming")),
		func(ctx context.Context, in *struct {
			FileID string `path:"file_id" maxLength:"64"`
			// The full client-caps query is built by useClientCaps on the FE.
			// Decoded with parseClientCapsFromQuery — we don't bind individual
			// fields because Huma can't bind unknown query params.
			SupportsHEVC     bool `query:"supports_hevc"`
			SupportsAV1      bool `query:"supports_av1"`
			SupportsFLAC     bool `query:"supports_flac"`
			SupportsOpus     bool `query:"supports_opus"`
			SupportsAC3      bool `query:"supports_ac3"`
			SupportsEAC3     bool `query:"supports_eac3"`
			SupportsMKV      bool `query:"supports_mkv"`
			SupportsWebM     bool `query:"supports_webm"`
			SupportsHDR      bool `query:"supports_hdr"`
			SupportsHDR10    bool `query:"supports_hdr10"`
			SupportsHLG      bool `query:"supports_hlg"`
			SupportsDoVi     bool `query:"supports_dovi"`
			SupportsHEVCHev1 bool `query:"supports_hevc_hev1"`
		}) (*JSONOutput[streamInfoResponse], error) {
			// Force a probe if this file has never been ffprobed. The FE fetches
			// this endpoint on player mount and gates direct-play vs HLS on the
			// returned plan, so it's the natural choke point — without media_info
			// Decide() would blindly fall back to a 1080p transcode.
			fileID, ok := app.ResolveLibraryFileID(ctx, in.FileID)
			if !ok {
				return nil, huma.Error404NotFound("file not found")
			}
			file, err := app.EnsureFileProbed(ctx, fileID)
			if err != nil {
				return nil, huma.Error404NotFound("file not found")
			}
			caps := transcoder.DefaultClientCaps
			caps.SupportsHEVC = in.SupportsHEVC
			caps.SupportsAV1 = in.SupportsAV1
			caps.SupportsFLAC = in.SupportsFLAC
			caps.SupportsOpus = in.SupportsOpus
			caps.SupportsAC3 = in.SupportsAC3
			caps.SupportsEAC3 = in.SupportsEAC3
			caps.SupportsMKV = in.SupportsMKV
			caps.SupportsWebM = in.SupportsWebM
			caps.SupportsHDR = in.SupportsHDR
			caps.SupportsHDR10 = in.SupportsHDR10
			caps.SupportsHLG = in.SupportsHLG
			caps.SupportsDoVi = in.SupportsDoVi
			caps.SupportsHEVCHev1 = in.SupportsHEVCHev1

			var info mediaprobe.MediaInfo
			if len(file.MediaInfo) > 0 {
				_ = json.Unmarshal(file.MediaInfo, &info)
			}
			resp := buildStreamInfoResponse(info, caps, file.Path, file.LibraryID)
			// 60s is the sweet spot here: probe results are stable for a given
			// file, but client caps from the query string can change as the FE
			// re-detects on browser update — short max-age keeps the cache key
			// from going stale across an upgrade.
			return cachedJSON(resp, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/stream/{file_id}/transcode-status", "stream-transcode-status", "Live ffmpeg session telemetry", "Streaming")),
		func(ctx context.Context, in *struct {
			FileID  string `path:"file_id" maxLength:"64"`
			Audio   int    `query:"audio" minimum:"0" doc:"Zero-based audio track used by the HLS session"`
			Session string `query:"sid" maxLength:"128" doc:"Playback session id carried by the HLS manifest"`
		}) (*JSONOutput[transcodeProgressResponse], error) {
			fileID, ok := app.ResolveLibraryFileID(ctx, in.FileID)
			if !ok {
				return noStoreJSON(transcodeProgressResponse{Active: false, State: "idle"}), nil
			}
			sessions := app.TranscoderSessions()
			if sessions == nil {
				return noStoreJSON(transcodeProgressResponse{Active: false}), nil
			}
			sess := sessions.GetExistingSession(fileID, in.Audio, in.Session)
			if sess == nil {
				return noStoreJSON(transcodeProgressResponse{Active: false, State: "idle"}), nil
			}
			head := sess.HeadSnapshot()
			stats := sess.ProgressSnapshot()
			running, state := transcodeSessionState(head, stats)
			resp := transcodeProgressResponse{
				Active:           true,
				Running:          running,
				State:            state,
				HeadStopReason:   string(head.StopReason),
				SessionKey:       sess.Key,
				TotalSegments:    sess.TotalSegs,
				ReadySegments:    sess.ReadySegmentCount(),
				HeadStartSegment: head.StartSeg,
				HeadCurrentSeg:   head.CurrentSeg,
				LastRequestedSeg: sess.LastRequestedSegment(),
				LeadCapSeconds:   transcoder.LeadCapSeconds,
				Frame:            stats.Frame,
				FPS:              stats.FPS,
				BitrateKbps:      stats.Bitrate,
				TotalSizeBytes:   stats.TotalSize,
				OutTimeSeconds:   stats.OutTimeSeconds,
				Speed:            stats.Speed,
				DupFrames:        stats.DupFrames,
				DropFrames:       stats.DropFrames,
			}
			if !stats.StartedAt.IsZero() {
				resp.StartedAtUnixMs = stats.StartedAt.UnixMilli()
				resp.ElapsedSeconds = time.Since(stats.StartedAt).Seconds()
			}
			if !stats.UpdatedAt.IsZero() {
				resp.UpdatedAtUnixMs = stats.UpdatedAt.UnixMilli()
				resp.LastUpdateAgoMs = time.Since(stats.UpdatedAt).Milliseconds()
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/stream/{file_id}/subtitles", "list-subtitles", "Subtitle tracks for a file", "Streaming")),
		func(ctx context.Context, in *struct {
			FileID string `path:"file_id" maxLength:"64"`
		}) (*JSONOutput[[]subtitleTrack], error) {
			file, err := app.GetLibraryFileByRef(ctx, in.FileID)
			if err != nil {
				return nil, huma.Error404NotFound("file not found")
			}
			var info mediaprobe.MediaInfo
			if len(file.MediaInfo) > 0 {
				_ = json.Unmarshal(file.MediaInfo, &info)
			}
			tracks := make([]subtitleTrack, 0, 4)
			for _, s := range info.Streams {
				if s.CodecType != "subtitle" {
					continue
				}
				track := subtitleTrack{
					Index:    s.Index,
					Language: s.Tags["language"],
					Codec:    s.CodecName,
					Title:    s.Tags["title"],
					Delivery: subtitleDeliveryString(transcoder.SubtitleDeliveryFor(s.CodecName)),
				}
				if s.Disposition != nil {
					track.IsDefault = s.Disposition.Default == 1
					track.IsForced = s.Disposition.Forced == 1
					track.IsHearingImpaired = s.Disposition.HearingImpaired == 1
				}
				tracks = append(tracks, track)
			}
			return cachedJSON(tracks, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/stream/{file_id}/segments", "stream-segments", "Skip segments (intro/recap/credits markers) for a file", "Streaming")),
		func(ctx context.Context, in *struct {
			FileID string `path:"file_id" maxLength:"64"`
		}) (*JSONOutput[fileSegmentsResponse], error) {
			fileID, ok := app.ResolveLibraryFileID(ctx, in.FileID)
			if !ok {
				return nil, huma.Error404NotFound("file not found")
			}
			segments, err := app.ListFileSegments(ctx, fileID)
			if err != nil {
				return nil, huma.Error500InternalServerError("segments lookup failed")
			}
			// Segments only change when the pump re-fetches (days apart) or
			// on manual edit — short client cache keeps player mounts cheap
			// without hiding edits for long.
			return cachedJSON(fileSegmentsResponse{Segments: segments}, 300), nil
		})
}

// fileSegmentsResponse wraps the segment list so the schema has a named
// object at the top level (matches the other stream JSON responses).
type fileSegmentsResponse struct {
	Segments []service.FileSegment `json:"segments"`
}
