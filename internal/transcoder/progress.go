package transcoder

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// ProgressStats captures the live state of an ffmpeg encode head. ffmpeg emits
// progress as key=value blocks every ~1s via `-progress pipe:N`. Each block
// ends with either `progress=continue` (encoding) or `progress=end` (done).
//
// All fields are guarded by the embedded mutex via the accessor methods on
// TranscodeSession; the parser writes directly under that lock.
type ProgressStats struct {
	// Running reports whether ffmpeg is currently encoding. Goes false on
	// progress=end or when the process exits.
	Running bool `json:"running"`

	// Frame is the latest output frame number.
	Frame int64 `json:"frame"`

	// FPS is the instantaneous encoding rate (output frames per second).
	FPS float64 `json:"fps"`

	// Bitrate is the average output bitrate of the running stream
	// (kbits/s as reported by ffmpeg — we store the raw float).
	Bitrate float64 `json:"bitrate_kbps"`

	// TotalSize is bytes of output written so far across all segments.
	TotalSize int64 `json:"total_size_bytes"`

	// OutTimeSeconds is the timestamp of the latest written output frame
	// (i.e. how much encoded video we've produced from the input timeline).
	OutTimeSeconds float64 `json:"out_time_seconds"`

	// Speed is the wall-clock speed multiplier (1.0x means real-time).
	Speed float64 `json:"speed"`

	// DupFrames / DropFrames are encoder-internal frame stats.
	DupFrames  int64 `json:"dup_frames"`
	DropFrames int64 `json:"drop_frames"`

	// UpdatedAt is the time of the latest progress block received.
	UpdatedAt time.Time `json:"updated_at"`

	// StartedAt is when the current head started — used for ETA / elapsed
	// calculations on the consumer side.
	StartedAt time.Time `json:"started_at"`
}

// progressReader consumes the read-end of the -progress pipe and pushes
// updates into `set` on each completed block. The reader returns when EOF is
// reached (ffmpeg closes the pipe on exit) or `r` returns an error.
//
// ffmpeg's block format (one line per kv, block terminated by `progress=...`):
//
//	frame=120
//	fps=23.98
//	stream_0_0_q=23.0
//	bitrate=2500.0kbits/s
//	total_size=12345
//	out_time_us=5000000
//	out_time=00:00:05.000000
//	dup_frames=0
//	drop_frames=0
//	speed=1.05x
//	progress=continue
func progressReader(r io.Reader, started time.Time, set func(func(*ProgressStats))) {
	scanner := bufio.NewScanner(r)
	cur := ProgressStats{StartedAt: started, Running: true}

	for scanner.Scan() {
		line := scanner.Text()
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := line[:eq]
		val := strings.TrimSpace(line[eq+1:])

		switch key {
		case "frame":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.Frame = n
			}
		case "fps":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				cur.FPS = f
			}
		case "bitrate":
			// "2500.0kbits/s" or "N/A"
			if v := strings.TrimSuffix(val, "kbits/s"); v != val {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					cur.Bitrate = f
				}
			}
		case "total_size":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.TotalSize = n
			}
		case "out_time_us", "out_time_ms":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				// ffmpeg's `out_time_ms` is misnamed — it's actually
				// microseconds in older builds. Both keys live in microseconds.
				cur.OutTimeSeconds = float64(n) / 1_000_000.0
			}
		case "dup_frames":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.DupFrames = n
			}
		case "drop_frames":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.DropFrames = n
			}
		case "speed":
			// "1.05x" — strip the trailing x.
			if v := strings.TrimSuffix(val, "x"); v != val {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					cur.Speed = f
				}
			}
		case "progress":
			cur.UpdatedAt = time.Now()
			if val == "end" {
				cur.Running = false
			}
			snapshot := cur
			set(func(p *ProgressStats) { *p = snapshot })
			if val == "end" {
				return
			}
		}
	}
}
