package jellyfin

import (
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	json "github.com/goccy/go-json"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/trickplay"
)

// Heya's trickplay sidecars are already Jellyfin-shaped: 320×180 tiles in
// 10×10 sprite sheets, one sheet per {n}.jpg under
// "{file}.trickplay/320 - 10x10/". The Jellyfin contract is the same data
// keyed differently — BaseItemDto.Trickplay advertises the geometry per
// MediaSource, and clients fetch sheets from
// /Videos/{itemId}/Trickplay/{width}/{index}.jpg.

// trickplayInfo mirrors the SDK model; every field is required client-side.
type trickplayInfo struct {
	Width          int `json:"Width"`
	Height         int `json:"Height"`
	TileWidth      int `json:"TileWidth"`
	TileHeight     int `json:"TileHeight"`
	ThumbnailCount int `json:"ThumbnailCount"`
	Interval       int `json:"Interval"` // ms between thumbnails
	Bandwidth      int `json:"Bandwidth"`
}

// trickplayMap builds the Trickplay advertisement for one media source, or
// nil when no sprites exist (trickplay is per-library opt-in). One stat per
// detail request — only called from single-item hydration, never list pages.
func trickplayMap(sourceID string, file sqlc.LibraryFile) map[string]any {
	var info struct {
		Duration float64 `json:"duration"`
	}
	if len(file.MediaInfo) == 0 || json.Unmarshal(file.MediaInfo, &info) != nil || info.Duration <= 0 {
		return nil
	}
	gridDir := trickplay.GridDir(trickplay.SidecarDir(file.Path))
	st, err := os.Stat(filepath.Join(gridDir, trickplay.SpriteName(0)))
	if err != nil {
		return nil
	}

	interval := trickplay.IntervalForDuration(info.Duration)
	count := int(math.Ceil(info.Duration / interval))
	if count < 1 {
		return nil
	}
	// Peak bitrate to fetch one sheet within its playback window; clients
	// use it to pick a resolution tier (we only have the one).
	sheetSeconds := interval * float64(min(count, trickplay.TilesPerSheet))
	bandwidth := max(1, int(float64(st.Size())*8/sheetSeconds))

	return map[string]any{
		sourceID: map[string]trickplayInfo{
			strconv.Itoa(trickplay.TileW): {
				Width:          trickplay.TileW,
				Height:         trickplay.TileH,
				TileWidth:      trickplay.Cols,
				TileHeight:     trickplay.Rows,
				ThumbnailCount: count,
				Interval:       int(interval * 1000),
				Bandwidth:      bandwidth,
			},
		},
	}
}

// GET /Videos/{itemId}/Trickplay/{width}/{index}.jpg — one sprite sheet.
// The width segment is advisory: 320 is the only tier Heya generates, so any
// requested width serves it (matching how upstream falls back to the closest
// generated tier rather than 404ing).
func (s *Server) handleTrickplayTile(w http.ResponseWriter, r *http.Request, p Params) {
	idx, err := strconv.Atoi(p["index"])
	if err != nil || idx < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok || (target.entityType != "movie" && target.entityType != "episode") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	spritePath := filepath.Join(
		trickplay.GridDir(trickplay.SidecarDir(target.file.Path)),
		trickplay.SpriteName(idx),
	)
	if _, err := os.Stat(spritePath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, spritePath)
}
