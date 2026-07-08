package trickplay

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	TileW         = 320
	TileH         = 180
	Cols          = 10
	Rows          = 10
	TilesPerSheet = Cols * Rows
	GridDirName   = "320 - 10x10"
)

func SidecarDir(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), filepath.Base(filePath)+".trickplay")
}

func GridDir(sidecarDir string) string {
	return filepath.Join(sidecarDir, GridDirName)
}

func SpriteName(spriteIdx int) string {
	return fmt.Sprintf("%d.jpg", spriteIdx)
}

func IntervalForDuration(duration float64) float64 {
	if duration > 7200 {
		return 10
	}
	return 5
}

func BuildVTT(duration float64, spriteExists func(int) bool) (string, error) {
	if duration <= 0 {
		return "", nil
	}
	interval := IntervalForDuration(duration)
	totalTiles := int(math.Ceil(duration / interval))
	if totalTiles < 1 {
		return "", nil
	}

	var vtt strings.Builder
	vtt.WriteString("WEBVTT\n\n")
	for tileGlobal := 0; tileGlobal < totalTiles; tileGlobal++ {
		spriteIdx := tileGlobal / TilesPerSheet
		if spriteExists != nil && !spriteExists(spriteIdx) {
			return "", fmt.Errorf("missing trickplay sprite %s", SpriteName(spriteIdx))
		}

		slot := tileGlobal % TilesPerSheet
		col := slot % Cols
		row := slot / Cols
		startTime := float64(tileGlobal) * interval
		endTime := startTime + interval
		if endTime > duration {
			endTime = duration
		}

		fmt.Fprintf(&vtt, "%s --> %s\n", formatVTTTime(startTime), formatVTTTime(endTime))
		fmt.Fprintf(&vtt, "%s#xywh=%d,%d,%d,%d\n\n", SpriteName(spriteIdx), col*TileW, row*TileH, TileW, TileH)
	}
	return vtt.String(), nil
}

func GenerateSprites(ctx context.Context, filePath string, duration float64, outDir string) (int, error) {
	if duration <= 0 {
		return 0, nil
	}

	if _, err := os.Stat(filepath.Join(GridDir(outDir), SpriteName(0))); err == nil {
		return 0, nil
	}

	interval := IntervalForDuration(duration)

	totalTiles := int(math.Ceil(duration / interval))
	if totalTiles < 1 {
		return 0, nil
	}

	tmpDir, err := os.MkdirTemp("", "trickplay-*")
	if err != nil {
		return 0, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	log.Info().
		Str("file", filePath).
		Int("tiles", totalTiles).
		Float64("interval", interval).
		Msg("generating trickplay thumbnails")

	fctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(fctx, "ffmpeg",
		"-nostats", "-loglevel", "warning",
		"-i", filePath,
		"-vf", fmt.Sprintf("fps=1/%.1f,scale=%d:%d", interval, TileW, TileH),
		"-q:v", "5",
		"-f", "image2",
		filepath.Join(tmpDir, "tile_%06d.jpg"),
	)
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("extract frames: %w", err)
	}

	tiles, _ := filepath.Glob(filepath.Join(tmpDir, "tile_*.jpg"))
	if len(tiles) == 0 {
		return 0, fmt.Errorf("no frames extracted")
	}

	sheetDir := GridDir(outDir)
	if err := os.MkdirAll(sheetDir, 0750); err != nil {
		return 0, fmt.Errorf("create trickplay dir: %w", err)
	}

	spriteCount := int(math.Ceil(float64(len(tiles)) / float64(TilesPerSheet)))

	for spriteIdx := 0; spriteIdx < spriteCount; spriteIdx++ {
		startTile := spriteIdx * TilesPerSheet
		endTile := startTile + TilesPerSheet
		if endTile > len(tiles) {
			endTile = len(tiles)
		}
		batch := tiles[startTile:endTile]

		rows := int(math.Ceil(float64(len(batch)) / float64(Cols)))
		spriteW := Cols * TileW
		spriteH := rows * TileH

		sprite := image.NewRGBA(image.Rect(0, 0, spriteW, spriteH))

		for i, tilePath := range batch {
			col := i % Cols
			row := i / Cols

			f, err := os.Open(tilePath)
			if err != nil {
				continue
			}
			img, err := jpeg.Decode(f)
			f.Close()
			if err != nil {
				continue
			}

			dst := image.Rect(
				col*TileW, row*TileH,
				(col+1)*TileW, (row+1)*TileH,
			)
			draw.Draw(sprite, dst, img, image.Point{}, draw.Src)
		}

		spritePath := filepath.Join(sheetDir, SpriteName(spriteIdx))
		sf, err := os.Create(spritePath)
		if err != nil {
			return 0, fmt.Errorf("create sprite: %w", err)
		}
		if err := jpeg.Encode(sf, sprite, &jpeg.Options{Quality: 80}); err != nil {
			sf.Close()
			return 0, fmt.Errorf("encode sprite: %w", err)
		}
		sf.Close()
	}

	log.Info().
		Str("file", filePath).
		Int("tiles", len(tiles)).
		Int("sprites", spriteCount).
		Str("out", outDir).
		Msg("trickplay generation complete")

	return len(tiles), nil
}

func formatVTTTime(seconds float64) string {
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	ms := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
