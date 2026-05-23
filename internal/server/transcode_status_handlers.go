package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
)

// transcodeProgressResponse surfaces live ffmpeg session telemetry for the
// nerdy-stats panel. Active is false when no session exists for the file —
// the player is direct-playing or hasn't started yet.
type transcodeProgressResponse struct {
	Active  bool `json:"active"`
	Running bool `json:"running"`
	// State is a UI-friendly classification: "running", "throttled",
	// "completed", "killed", "exited", or "idle" (no session). Lets the
	// frontend show "encoder paused — buffered ahead" without inferring
	// from booleans.
	State            string  `json:"state"`
	HeadStopReason   string  `json:"head_stop_reason,omitempty"`
	SessionKey       string  `json:"session_key,omitempty"`
	TotalSegments    int     `json:"total_segments"`
	ReadySegments    int     `json:"ready_segments"`
	HeadStartSegment int     `json:"head_start_segment"`
	HeadCurrentSeg   int     `json:"head_current_segment"`
	LastRequestedSeg int     `json:"last_requested_segment"`
	LeadCapSeconds   float64 `json:"lead_cap_seconds"`
	Frame            int64   `json:"frame"`
	FPS              float64 `json:"fps"`
	BitrateKbps      float64 `json:"bitrate_kbps"`
	TotalSizeBytes   int64   `json:"total_size_bytes"`
	OutTimeSeconds   float64 `json:"out_time_seconds"`
	Speed            float64 `json:"speed"`
	DupFrames        int64   `json:"dup_frames"`
	DropFrames       int64   `json:"drop_frames"`
	ElapsedSeconds   float64 `json:"elapsed_seconds"`
	LastUpdateAgoMs  int64   `json:"last_update_ago_ms"`
	StartedAtUnixMs  int64   `json:"started_at_unix_ms,omitempty"`
	UpdatedAtUnixMs  int64   `json:"updated_at_unix_ms,omitempty"`
}

func handleTranscodeStatus(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		sessions := app.TranscoderSessions()
		if sessions == nil {
			writeJSON(w, http.StatusOK, transcodeProgressResponse{Active: false})
			return
		}

		// We don't filter on the `sid` query parameter — there's at most one
		// session per file. The sid is used elsewhere to drive session-aware
		// caching; for status, the file is the right key.
		sess := sessions.GetExisting(fileID)
		if sess == nil {
			writeJSON(w, http.StatusOK, transcodeProgressResponse{Active: false, State: "idle"})
			return
		}

		head := sess.HeadSnapshot()
		stats := sess.ProgressSnapshot()
		running := stats.Running || head.Running

		// Derive a single UI-friendly state. Encoding is the active case;
		// otherwise the stop reason classifies why we're not encoding.
		state := "idle"
		switch {
		case running:
			state = "running"
		case head.StopReason == transcoder.StopReasonLeadCap:
			state = "throttled"
		case head.StopReason == transcoder.StopReasonCompleted:
			state = "completed"
		case head.StopReason == transcoder.StopReasonKilled:
			state = "killed"
		case head.StopReason == transcoder.StopReasonExited:
			state = "exited"
		}

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

		writeJSON(w, http.StatusOK, resp)
	}
}
