package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
)

type playbackDecision struct {
	Action    string `json:"action"`
	Profile   string `json:"profile"`
	Reason    string `json:"reason"`
	CopyVideo bool   `json:"copy_video"`
	CopyAudio bool   `json:"copy_audio"`
}

type streamInfoResponse struct {
	Container string           `json:"container"`
	Duration  float64          `json:"duration"`
	Size      int64            `json:"size"`
	BitRate   int64            `json:"bit_rate"`
	Playback  playbackDecision `json:"playback"`
	Video     []videoStream    `json:"video"`
	Audio     []audioStream    `json:"audio"`
	Subtitle  []subStream      `json:"subtitle"`
}

type videoStream struct {
	Index          int    `json:"index"`
	Codec          string `json:"codec"`
	CodecLong      string `json:"codec_long"`
	Profile        string `json:"profile,omitempty"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	PixFmt         string `json:"pix_fmt,omitempty"`
	HDR            bool   `json:"hdr"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`
	BitRate        string `json:"bit_rate,omitempty"`
	IsDefault      bool   `json:"is_default"`
}

type audioStream struct {
	Index         int    `json:"index"`
	Codec         string `json:"codec"`
	CodecLong     string `json:"codec_long"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	SampleRate    string `json:"sample_rate,omitempty"`
	BitRate       string `json:"bit_rate,omitempty"`
	Language      string `json:"language"`
	Title         string `json:"title,omitempty"`
	IsDefault     bool   `json:"is_default"`
}

type subStream struct {
	Index             int    `json:"index"`
	Codec             string `json:"codec"`
	Language          string `json:"language"`
	Title             string `json:"title,omitempty"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
}

func handleGetStreamInfo(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		caps := parseClientCaps(r)

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		writeJSON(w, http.StatusOK, buildStreamInfoResponse(info, caps, file.Path))
	}
}

func parseClientCaps(r *http.Request) transcoder.ClientCapabilities {
	caps := transcoder.DefaultClientCaps
	q := r.URL.Query()
	if q.Get("supports_hevc") == "1" {
		caps.SupportsHEVC = true
	}
	if q.Get("supports_av1") == "1" {
		caps.SupportsAV1 = true
	}
	if q.Get("supports_flac") == "1" {
		caps.SupportsFLAC = true
	}
	if q.Get("supports_opus") == "1" {
		caps.SupportsOpus = true
	}
	if q.Get("supports_mkv") == "1" {
		caps.SupportsMKV = true
	}
	if q.Get("supports_webm") == "1" {
		caps.SupportsWebM = true
	}
	return caps
}

func buildStreamInfoResponse(info worker.MediaInfo, caps transcoder.ClientCapabilities, filePath string) streamInfoResponse {
	tInfo := workerToTranscoderInfo(&info)
	plan := transcoder.Decide(&tInfo, caps)

	if plan.Action == transcoder.ActionDirectPlay && vfs.IsSMBPath(filePath) {
		plan = transcoder.PlaybackPlan{Action: transcoder.ActionRemux, Profile: "remux", Reason: "remote file requires HLS delivery"}
	}

	resp := streamInfoResponse{
		Container: info.Container,
		Duration:  info.Duration,
		Size:      info.Size,
		BitRate:   info.BitRate,
		Playback: playbackDecision{
			Action:    string(plan.Action),
			Profile:   plan.Profile,
			Reason:    plan.Reason,
			CopyVideo: plan.CopyVideo,
			CopyAudio: plan.CopyAudio,
		},
	}

	for _, s := range info.Streams {
		isDefault := s.Disposition != nil && s.Disposition.Default == 1

		switch s.CodecType {
		case "video":
			resp.Video = append(resp.Video, videoStream{
				Index:          s.Index,
				Codec:          s.CodecName,
				CodecLong:      s.CodecLongName,
				Profile:        s.Profile,
				Width:          s.Width,
				Height:         s.Height,
				PixFmt:         s.PixFmt,
				HDR:            isHDR(s),
				ColorTransfer:  s.ColorTransfer,
				ColorPrimaries: s.ColorPrimaries,
				ColorSpace:     s.ColorSpace,
				BitRate:        s.BitRate,
				IsDefault:      isDefault,
			})

		case "audio":
			resp.Audio = append(resp.Audio, audioStream{
				Index:         s.Index,
				Codec:         s.CodecName,
				CodecLong:     s.CodecLongName,
				Channels:      s.Channels,
				ChannelLayout: s.ChannelLayout,
				SampleRate:    s.SampleRate,
				BitRate:       s.BitRate,
				Language:      s.Tags["language"],
				Title:         s.Tags["title"],
				IsDefault:     isDefault,
			})

		case "subtitle":
			isForced := s.Disposition != nil && s.Disposition.Forced == 1
			isHI := s.Disposition != nil && s.Disposition.HearingImpaired == 1
			resp.Subtitle = append(resp.Subtitle, subStream{
				Index:             s.Index,
				Codec:             s.CodecName,
				Language:          s.Tags["language"],
				Title:             s.Tags["title"],
				IsDefault:         isDefault,
				IsForced:          isForced,
				IsHearingImpaired: isHI,
			})
		}
	}

	if resp.Video == nil {
		resp.Video = []videoStream{}
	}
	if resp.Audio == nil {
		resp.Audio = []audioStream{}
	}
	if resp.Subtitle == nil {
		resp.Subtitle = []subStream{}
	}

	return resp
}

func isHDR(s worker.StreamInfo) bool {
	switch s.ColorTransfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	return false
}
