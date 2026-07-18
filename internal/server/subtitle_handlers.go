package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"golang.org/x/sync/singleflight"
)

var subtitleExtractions singleflight.Group

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
	return handleGetSubtitleAs(app, false)
}

// handleGetSubtitleAs optionally normalizes every external text subtitle to
// WebVTT. Browsers keep ASS/SSA for AkariSub rendering; Google's Default
// Media Receiver accepts the same track reliably when Heya exposes WebVTT.
func handleGetSubtitleAs(app *service.App, forceWebVTT bool) http.HandlerFunc {
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

		var info mediaprobe.MediaInfo
		if len(file.MediaInfo) > 0 {
			_ = json.Unmarshal(file.MediaInfo, &info)
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

		isASS := !forceWebVTT && (subCodec == "ass" || subCodec == "ssa")
		ext := ".vtt"
		outputCodec := "webvtt"
		contentType := "text/vtt; charset=utf-8"
		if isASS {
			ext = ".ass"
			outputCodec = "ass"
			contentType = "text/x-ssa; charset=utf-8"
		}

		cacheKey := fmt.Sprintf("sub_%d_%d", file.ID, index)
		lease, err := app.TranscoderCache().ReserveSegmentFile(cacheKey, "subtitle"+ext)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot reserve subtitle cache")
			return
		}
		defer lease.Release()
		subPath := lease.Path()

		extractErr := ensureCachedSubtitle(r.Context(), app.LifetimeContext(), subPath, func(ctx context.Context) error {
			// The shared producer owns its own pin. The request lease above may be
			// released when the singleflight leader disconnects while another
			// request is still waiting for the app-owned extraction to finish.
			producerLease, err := app.TranscoderCache().ReserveSegmentFile(cacheKey, "subtitle"+ext)
			if err != nil {
				return err
			}
			defer producerLease.Release()
			producerPath := producerLease.Path()
			return transcoder.ExtractSubtitlesAs(ctx, file.Path, index, producerPath, outputCodec)
		})
		if extractErr != nil {
			// The client has already gone away; avoid attempting a second response
			// after cancellation. A joined request can retry independently if the
			// singleflight leader was the request that cancelled.
			if r.Context().Err() != nil {
				return
			}
			writeError(w, http.StatusInternalServerError, "subtitle extraction failed")
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, subPath)
	}
}

// ensureCachedSubtitle coalesces concurrent requests for one cache artifact.
// The extractor publishes atomically, so a successful os.Stat always denotes
// a complete subtitle rather than an ffmpeg file still being written. Waiting
// callers may leave on request cancellation without interrupting another
// request that already owns the extraction.
func ensureCachedSubtitle(waitCtx, workCtx context.Context, path string, extract func(context.Context) error) error {
	if waitCtx == nil {
		return errors.New("ensure cached subtitle: nil context")
	}
	if err := waitCtx.Err(); err != nil {
		return err
	}
	if workCtx == nil {
		return errors.New("ensure cached subtitle: nil work context")
	}
	if extract == nil {
		return errors.New("ensure cached subtitle: nil extractor")
	}
	ready, err := cachedSubtitleReady(path)
	if err != nil || ready {
		return err
	}

	result := subtitleExtractions.DoChan(path, func() (any, error) {
		// Another request may have published while this caller was joining the
		// group. Recheck inside the singleflight before starting ffmpeg.
		ready, err := cachedSubtitleReady(path)
		if err != nil || ready {
			return nil, err
		}
		if err := workCtx.Err(); err != nil {
			return nil, err
		}
		return nil, extract(workCtx)
	})

	select {
	case <-waitCtx.Done():
		return waitCtx.Err()
	case completed := <-result:
		return completed.Err
	}
}

func cachedSubtitleReady(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("subtitle cache target is not a regular file")
		}
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
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
