package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
)

type subtitleTrack struct {
	Index             int    `json:"index"`
	Language          string `json:"language"`
	Codec             string `json:"codec"`
	Title             string `json:"title"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
	// Delivery: "external" (download as VTT/ASS), "burn-in" (must transcode
	// the video with -vf subtitles=...), or "unsupported".
	Delivery string `json:"delivery"`
}

func handleGetSubtitle(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stream index")
			return
		}

		if app.TranscoderCache() == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		file, err := app.GetLibraryFileByRef(r.Context(), r.PathValue("file_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		subCodec := ""
		for _, s := range info.Streams {
			if s.CodecType == "subtitle" && s.Index == index {
				subCodec = s.CodecName
				break
			}
		}

		// Bitmap subtitles (PGS / dvb / dvd_subtitle) cannot be served as
		// text — they have to be burned into the video by the transcoder.
		// Tell the client so it can re-request playback with burn_sub set.
		switch transcoder.SubtitleDeliveryFor(subCodec) {
		case transcoder.SubDeliveryBurnIn:
			w.Header().Set("X-Heya-Subtitle-Delivery", "burn-in")
			writeError(w, http.StatusUnsupportedMediaType, "subtitle codec requires burn-in: "+subCodec)
			return
		case transcoder.SubDeliveryUnsupported:
			w.Header().Set("X-Heya-Subtitle-Delivery", "unsupported")
			writeError(w, http.StatusUnsupportedMediaType, "subtitle codec not supported: "+subCodec)
			return
		}

		isASS := subCodec == "ass" || subCodec == "ssa"
		ext := ".vtt"
		outputCodec := "webvtt"
		contentType := "text/vtt; charset=utf-8"
		if isASS {
			ext = ".ass"
			outputCodec = "ass"
			contentType = "text/x-ssa; charset=utf-8"
		}

		cacheKey := fmt.Sprintf("sub_%d_%d", file.ID, index)
		subPath := filepath.Join(app.TranscoderCache().SegmentDir(cacheKey), "subtitle"+ext)

		if _, err := os.Stat(subPath); err != nil {
			var extractErr error
			if vfs.IsSMBPath(file.Path) {
				extractErr = extractSubtitleSMB(r.Context(), file.Path, index, subPath, outputCodec)
			} else {
				extractErr = transcoder.ExtractSubtitlesAs(r.Context(), file.Path, index, subPath, outputCodec)
			}
			if extractErr != nil {
				writeError(w, http.StatusInternalServerError, "subtitle extraction failed: "+extractErr.Error())
				return
			}
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, subPath)
	}
}

func subtitleDeliveryString(d transcoder.SubtitleDelivery) string {
	switch d {
	case transcoder.SubDeliveryExternal:
		return "external"
	case transcoder.SubDeliveryBurnIn:
		return "burn-in"
	default:
		return "unsupported"
	}
}

func extractSubtitleSMB(ctx context.Context, smbPath string, streamIndex int, output string, codec string) error {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		return fmt.Errorf("invalid smb path: %s", smbPath)
	}

	source, err := vfs.Open(smbPath[:lastSlash])
	if err != nil {
		return fmt.Errorf("open smb dir: %w", err)
	}
	defer source.Close()

	f, err := source.FS.Open(smbPath[lastSlash+1:])
	if err != nil {
		return fmt.Errorf("open smb file: %w", err)
	}
	defer f.Close()

	return transcoder.ExtractSubtitlesFromReaderAs(ctx, f.(io.Reader), streamIndex, output, codec)
}
