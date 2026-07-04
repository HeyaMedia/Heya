package transcoder

import (
	"bufio"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Keyframes struct {
	IFrames  []float64 `json:"iframes"`
	Duration float64   `json:"duration"`
}

func ExtractKeyframes(ctx context.Context, filePath string) (*Keyframes, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0",
		"-i",
		filePath,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var iframes []float64
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) < 2 {
			continue
		}
		if !strings.HasPrefix(parts[1], "K") {
			continue
		}
		ts, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			continue
		}
		iframes = append(iframes, ts)
	}

	cmd.Wait()

	var duration float64
	if len(iframes) > 0 {
		duration = iframes[len(iframes)-1]
	}

	durationCmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		"-i",
		filePath,
	)
	if dOut, err := durationCmd.Output(); err == nil {
		if d, err := strconv.ParseFloat(strings.TrimSpace(string(dOut)), 64); err == nil {
			duration = d
		}
	}

	return &Keyframes{IFrames: iframes, Duration: duration}, nil
}

func KeyframesToSegmentTimes(kf *Keyframes, minDuration float64) []float64 {
	if kf == nil || len(kf.IFrames) == 0 {
		return nil
	}
	if minDuration <= 0 {
		minDuration = 4.0
	}

	var times []float64
	lastCut := 0.0

	for _, ts := range kf.IFrames {
		if ts <= 0 {
			continue
		}
		if ts-lastCut >= minDuration {
			times = append(times, ts)
			lastCut = ts
		}
	}

	return times
}
