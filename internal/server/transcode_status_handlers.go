package server

// transcodeProgressResponse surfaces live ffmpeg session telemetry for the
// nerdy-stats panel. Active is false when no session exists for the file —
// the player is direct-playing or hasn't started yet.
//
// The Huma handler that produces this lives in stream_huma.go.
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
