package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/mediaprobe"
)

// ManagerStreamView is one stream of an on-disk file, ffprobe-derived —
// everything the probe knows, not just the headline codec.
type ManagerStreamView struct {
	Kind         string `json:"kind" doc:"video | audio | subtitle | other"`
	Codec        string `json:"codec,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Width        int32  `json:"width,omitempty"`
	Height       int32  `json:"height,omitempty"`
	FrameRate    string `json:"frame_rate,omitempty" doc:"e.g. 23.976"`
	BitDepth     int32  `json:"bit_depth,omitempty"`
	HDR          string `json:"hdr,omitempty" doc:"Dolby Vision / HDR10 / HLG"`
	Channels     int32  `json:"channels,omitempty"`
	Layout       string `json:"layout,omitempty" doc:"5.1(side), stereo, ..."`
	SampleRateHz int32  `json:"sample_rate_hz,omitempty"`
	Language     string `json:"language,omitempty"`
	Title        string `json:"title,omitempty"`
	BitrateKbps  int32  `json:"bitrate_kbps,omitempty"`
	Default      bool   `json:"default,omitempty"`
	Forced       bool   `json:"forced,omitempty"`
}

// ManagerFileDetailView is the full picture of one library file: the
// absolute path and every probed stream. Fetched lazily when a file row
// expands — the blobs are too heavy to ride along on list endpoints.
type ManagerFileDetailView struct {
	ID          int64               `json:"id"`
	Path        string              `json:"path" doc:"Absolute on-disk path"`
	SizeBytes   int64               `json:"size_bytes"`
	Container   string              `json:"container,omitempty"`
	DurationSec int32               `json:"duration_sec,omitempty"`
	BitrateKbps int32               `json:"bitrate_kbps,omitempty" doc:"Overall container bitrate"`
	AddedAt     string              `json:"added_at"`
	Streams     []ManagerStreamView `json:"streams"`
}

// ManagerFile returns full probe detail for one library file.
func (a *App) ManagerFile(ctx context.Context, fileID int64) (*ManagerFileDetailView, error) {
	var (
		view    ManagerFileDetailView
		created time.Time
		blob    []byte
	)
	err := a.db.QueryRow(ctx, `
		SELECT lf.id, lf.path, lf.size, lf.created_at, COALESCE(lf.media_info, '{}'::jsonb)
		FROM library_files lf
		WHERE lf.id = $1 AND lf.deleted_at IS NULL`, fileID).
		Scan(&view.ID, &view.Path, &view.SizeBytes, &created, &blob)
	if err != nil {
		return nil, fmt.Errorf("manager file %d: %w", fileID, err)
	}
	view.AddedAt = created.UTC().Format(time.RFC3339)
	view.Streams = []ManagerStreamView{}

	info, perr := mediaprobe.Parse(blob)
	if perr != nil || info == nil {
		return &view, nil // unprobed file: path + size still answer
	}
	view.Container = containerLabel(info.Container, view.Path)
	view.DurationSec = int32(info.Duration)
	view.BitrateKbps = int32(info.BitRate / 1000)
	for i := range info.Streams {
		s := &info.Streams[i]
		// Embedded cover art is a video stream to ffprobe — noise here.
		if s.Disposition != nil && s.Disposition.AttachedPic == 1 {
			continue
		}
		view.Streams = append(view.Streams, streamView(s))
	}
	return &view, nil
}

func streamView(s *mediaprobe.StreamInfo) ManagerStreamView {
	out := ManagerStreamView{
		Kind:    s.CodecType,
		Codec:   s.CodecName,
		Profile: s.Profile,
	}
	switch s.CodecType {
	case "video":
		out.Width, out.Height = int32(s.Width), int32(s.Height)
		out.FrameRate = frameRateLabel(s.AvgFrameRate, s.RFrameRate)
		out.BitDepth = videoBitDepth(s)
		out.HDR = hdrLabel(s)
	case "audio":
		out.Channels = int32(s.Channels)
		out.Layout = s.ChannelLayout
		if out.Layout == "" && s.Channels > 0 {
			out.Layout = channelLayoutLabel(int32(s.Channels))
		}
		out.SampleRateHz = int32(mediaprobe.ParseFloatString(s.SampleRate))
		out.BitDepth = int32(mediaprobe.ParseIntString(s.BitsPerRawSample))
	case "subtitle":
	default:
		out.Kind = "other"
	}
	out.BitrateKbps = int32(mediaprobe.ParseFloatString(s.BitRate) / 1000)
	if s.Tags != nil {
		if lang := s.Tags["language"]; lang != "" && lang != "und" {
			out.Language = lang
		}
		out.Title = s.Tags["title"]
	}
	if s.Disposition != nil {
		out.Default = s.Disposition.Default == 1
		out.Forced = s.Disposition.Forced == 1
	}
	return out
}

// containerLabel prefers the file extension over ffprobe's format_name
// ("matroska,webm" reads worse than "mkv").
func containerLabel(formatName, path string) string {
	if dot := strings.LastIndex(path, "."); dot >= 0 && dot < len(path)-1 {
		ext := strings.ToLower(path[dot+1:])
		if len(ext) <= 5 {
			return ext
		}
	}
	if head, _, found := strings.Cut(formatName, ","); found {
		return head
	}
	return formatName
}

func frameRateLabel(avg, r string) string {
	src := avg
	if src == "" || src == "0/0" {
		src = r
	}
	num, den, found := strings.Cut(src, "/")
	if !found {
		return src
	}
	n := mediaprobe.ParseFloatString(num)
	d := mediaprobe.ParseFloatString(den)
	if n <= 0 || d <= 0 {
		return ""
	}
	fps := n / d
	label := strconv.FormatFloat(fps, 'f', 3, 64)
	label = strings.TrimRight(strings.TrimRight(label, "0"), ".")
	return label
}

func videoBitDepth(s *mediaprobe.StreamInfo) int32 {
	if depth := mediaprobe.ParseIntString(s.BitsPerRawSample); depth > 0 {
		return int32(depth)
	}
	switch {
	case strings.Contains(s.PixFmt, "12"):
		return 12
	case strings.Contains(s.PixFmt, "10"):
		return 10
	case s.PixFmt != "":
		return 8
	}
	return 0
}

func hdrLabel(s *mediaprobe.StreamInfo) string {
	for _, sd := range s.SideDataList {
		if sd.DvProfile > 0 {
			return fmt.Sprintf("Dolby Vision P%d", sd.DvProfile)
		}
	}
	switch s.ColorTransfer {
	case "smpte2084":
		return "HDR10"
	case "arib-std-b67":
		return "HLG"
	}
	return ""
}
