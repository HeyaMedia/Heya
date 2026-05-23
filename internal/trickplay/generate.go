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
)

func GenerateSprites(ctx context.Context, filePath string, duration float64, outDir string) (int, error) {
	if duration <= 0 {
		return 0, nil
	}

	vttPath := filepath.Join(outDir, "index.vtt")
	if _, err := os.Stat(vttPath); err == nil {
		return 0, nil
	}

	interval := 5.0
	if duration > 7200 {
		interval = 10.0
	}

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

	os.MkdirAll(outDir, 0755)

	spriteCount := int(math.Ceil(float64(len(tiles)) / float64(TilesPerSheet)))
	var vtt strings.Builder
	vtt.WriteString("WEBVTT\n\n")

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

			tileGlobal := startTile + i
			startTime := float64(tileGlobal) * interval
			endTime := startTime + interval
			if endTime > duration {
				endTime = duration
			}

			spriteName := fmt.Sprintf("sprite_%d.jpg", spriteIdx)
			x := col * TileW
			y := row * TileH

			vtt.WriteString(fmt.Sprintf("%s --> %s\n", formatVTTTime(startTime), formatVTTTime(endTime)))
			vtt.WriteString(fmt.Sprintf("%s#xywh=%d,%d,%d,%d\n\n", spriteName, x, y, TileW, TileH))
		}

		spritePath := filepath.Join(outDir, fmt.Sprintf("sprite_%d.jpg", spriteIdx))
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

	if err := os.WriteFile(vttPath, []byte(vtt.String()), 0644); err != nil {
		return 0, fmt.Errorf("write vtt: %w", err)
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
