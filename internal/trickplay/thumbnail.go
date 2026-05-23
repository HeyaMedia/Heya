package trickplay

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"time"

	"github.com/rs/zerolog/log"
)

func init() {
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
}

func ExtractThumbnail(ctx context.Context, filePath string, durationMs int32, outPath string) error {
	seekPcts := []float64{0.10, 0.20, 0.30}
	for _, pct := range seekPcts {
		seekTime := 5.0
		if durationMs > 0 {
			seekTime = float64(durationMs) * pct / 1000.0
		}

		if err := extractFrame(ctx, filePath, seekTime, outPath); err != nil {
			log.Warn().Err(err).Str("file", filePath).Float64("seek", seekTime).Msg("frame extraction failed")
			continue
		}

		if isBlackFrame(outPath) {
			os.Remove(outPath)
			continue
		}

		return nil
	}

	if _, err := os.Stat(outPath); err != nil {
		return fmt.Errorf("no usable frame extracted")
	}
	return nil
}

func extractFrame(ctx context.Context, input string, seekTime float64, output string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-nostats", "-loglevel", "warning",
		"-ss", fmt.Sprintf("%.3f", seekTime),
		"-i", input,
		"-vframes", "1",
		"-vf", "scale=480:-2",
		"-q:v", "4",
		"-y", output,
	)
	return cmd.Run()
}

func isBlackFrame(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	img, err := jpeg.Decode(f)
	if err != nil {
		return true
	}

	bounds := img.Bounds()
	totalBrightness := 0
	samples := 0
	stepX := max((bounds.Dx())/10, 1)
	stepY := max((bounds.Dy())/10, 1)

	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			lum := (299*int(r>>8) + 587*int(g>>8) + 114*int(b>>8)) / 1000
			totalBrightness += lum
			samples++
		}
	}

	if samples == 0 {
		return true
	}
	return totalBrightness/samples < 20
}
