package server

import (
	"encoding/json"
	"net/http"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
)

type transcodeStatusResponse struct {
	Available    bool   `json:"available"`
	HWAccel      string `json:"hw_accel"`
	HWAccelLabel string `json:"hw_accel_label"`
	EncoderH264  string `json:"encoder_h264"`
	EncoderHEVC  string `json:"encoder_hevc"`
	CacheDir     string `json:"cache_dir"`
	CacheMaxGB   int    `json:"cache_max_gb"`
	CacheSizeMB  int64  `json:"cache_size_mb"`
	CacheItems   int    `json:"cache_items"`
	ActiveJobs   int    `json:"active_jobs"`
	ConfigMode   string `json:"config_mode"`
}

func handleGetTranscodeStatus(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := transcodeStatusResponse{
			Available:  transcoder.IsFFmpegAvailable(),
			ConfigMode: app.ConfigSnapshot().HWAccel,
			CacheDir:   app.ConfigSnapshot().TranscodeCacheDir,
			CacheMaxGB: app.ConfigSnapshot().TranscodeCacheMaxGB,
		}

		if app.TranscoderSessions() != nil {
			hw := app.TranscoderSessions().HWAccel()
			resp.HWAccel = string(hw.Type)
			resp.HWAccelLabel = hwAccelLabel(hw.Type)
			resp.EncoderH264 = hw.EncoderH264
			resp.EncoderHEVC = hw.EncoderHEVC
		} else {
			resp.HWAccel = "none"
			resp.HWAccelLabel = "Disabled"
		}

		if app.TranscoderCache() != nil {
			stats := app.TranscoderCache().Stats()
			resp.CacheSizeMB = stats.TotalSize / (1024 * 1024)
			resp.CacheItems = stats.ItemCount
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

type transcodeSettingsRequest struct {
	HWAccel    string `json:"hw_accel"`
	CacheMaxGB int    `json:"cache_max_gb"`
}

func handleUpdateTranscodeSettings(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req transcodeSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		validAccel := map[string]bool{
			"auto": true, "none": true, "vaapi": true,
			"qsv": true, "nvenc": true, "videotoolbox": true,
		}
		if req.HWAccel != "" && !validAccel[req.HWAccel] {
			writeError(w, http.StatusBadRequest, "invalid hw_accel value")
			return
		}

		if req.HWAccel != "" {
			app.ConfigSnapshot().HWAccel = req.HWAccel
		}
		if req.CacheMaxGB > 0 {
			app.ConfigSnapshot().TranscodeCacheMaxGB = req.CacheMaxGB
		}

		if path := config.FindConfigFile(); path != "" {
			fc := app.ConfigSnapshot().ToFileConfig()
			config.SaveFile(path, fc)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleClearTranscodeCache(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.TranscoderCache() == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}
		if err := app.TranscoderCache().Clear(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
	}
}

func hwAccelLabel(t transcoder.HwAccelType) string {
	switch t {
	case transcoder.HwAccelNVENC:
		return "NVIDIA NVENC"
	case transcoder.HwAccelVAAPI:
		return "VA-API"
	case transcoder.HwAccelQSV:
		return "Intel Quick Sync"
	case transcoder.HwAccelVideoToolbox:
		return "Apple VideoToolbox"
	case transcoder.HwAccelNone:
		return "CPU (Software)"
	default:
		return "Unknown"
	}
}
