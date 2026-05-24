package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var transcodeCmd = &cobra.Command{
	Use:   "transcode",
	Short: "FFmpeg transcoding utilities",
}

var transcodeProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Run ffprobe on a file",
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, _ := cmd.Flags().GetString("file")
		if filePath == "" {
			return fmt.Errorf("--file is required")
		}

		if !transcoder.IsFFprobeAvailable() {
			return fmt.Errorf("ffprobe is not installed")
		}

		out, err := exec.Command("ffprobe",
			"-v", "quiet",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			filePath,
		).Output()
		if err != nil {
			return fmt.Errorf("ffprobe failed: %w", err)
		}

		if ui.JSONMode {
			fmt.Println(string(out))
			return nil
		}

		var probe struct {
			Format struct {
				Filename   string `json:"filename"`
				FormatName string `json:"format_name"`
				Duration   string `json:"duration"`
				Size       string `json:"size"`
				BitRate    string `json:"bit_rate"`
			} `json:"format"`
			Streams []struct {
				Index         int    `json:"index"`
				CodecName     string `json:"codec_name"`
				CodecType     string `json:"codec_type"`
				Width         int    `json:"width"`
				Height        int    `json:"height"`
				SampleRate    string `json:"sample_rate"`
				Channels      int    `json:"channels"`
				ChannelLayout string `json:"channel_layout"`
			} `json:"streams"`
		}
		json.Unmarshal(out, &probe)

		ui.Header("File Info")
		ui.Info("File", probe.Format.Filename)
		ui.Info("Format", probe.Format.FormatName)
		ui.Info("Duration", probe.Format.Duration+"s")
		ui.Info("Size", probe.Format.Size+" bytes")
		ui.Info("Bitrate", probe.Format.BitRate)

		fmt.Println()
		ui.Header("Streams")
		t := ui.NewTable("#", "TYPE", "CODEC", "DETAILS")
		for _, s := range probe.Streams {
			detail := ""
			if s.CodecType == "video" {
				detail = fmt.Sprintf("%dx%d", s.Width, s.Height)
			} else if s.CodecType == "audio" {
				detail = fmt.Sprintf("%s %dch", s.SampleRate, s.Channels)
			}
			t.AddRow(
				fmt.Sprintf("%d", s.Index),
				s.CodecType,
				s.CodecName,
				detail,
			)
		}
		fmt.Println(t.Render())
		return nil
	},
}

var transcodeTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test transcode a file",
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, _ := cmd.Flags().GetString("file")
		profileName, _ := cmd.Flags().GetString("profile")

		if filePath == "" {
			return fmt.Errorf("--file is required")
		}
		if !transcoder.IsFFmpegAvailable() {
			return fmt.Errorf("ffmpeg is not installed")
		}

		profile, ok := transcoder.GetProfile(profileName)
		if !ok {
			return fmt.Errorf("unknown profile: %s", profileName)
		}

		tmpDir := fmt.Sprintf("/tmp/heya-transcode-test-%d", time.Now().UnixNano())

		ui.Header("Transcode Test")
		ui.Info("File", filePath)
		ui.Info("Profile", profileName)
		ui.Info("Output", tmpDir)

		err := transcoder.TranscodeToHLS(context.Background(), filePath, tmpDir, profile)
		if err != nil {
			ui.Error("Transcode failed: %s", err)
			return err
		}

		ui.Success("Transcode complete!")
		return nil
	},
}

var transcodeCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage transcode cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		showStats, _ := cmd.Flags().GetBool("stats")
		doClear, _ := cmd.Flags().GetBool("clear")

		cache := transcoder.NewCacheManager(cfg.TranscodeCacheDir.Value, cfg.TranscodeCacheMaxGB.Value)

		if doClear {
			if err := cache.Clear(); err != nil {
				return err
			}
			ui.Success("Cache cleared")
			return nil
		}

		if showStats {
			stats := cache.Stats()
			ui.Header("Transcode Cache")
			ui.Info("Location", cfg.TranscodeCacheDir.Value)
			ui.Info("Items", fmt.Sprintf("%d", stats.ItemCount))
			ui.Info("Size", fmt.Sprintf("%.2f GB", float64(stats.TotalSize)/(1024*1024*1024)))
			ui.Info("Max Size", fmt.Sprintf("%d GB", stats.MaxSizeGB))
			return nil
		}

		return cmd.Help()
	},
}

func init() {
	transcodeProbeCmd.Flags().String("file", "", "File to probe")
	transcodeTestCmd.Flags().String("file", "", "File to transcode")
	transcodeTestCmd.Flags().String("profile", "1080p", "Transcode profile")
	transcodeCacheCmd.Flags().Bool("stats", false, "Show cache statistics")
	transcodeCacheCmd.Flags().Bool("clear", false, "Clear the cache")

	transcodeCmd.AddCommand(transcodeProbeCmd)
	transcodeCmd.AddCommand(transcodeTestCmd)
	transcodeCmd.AddCommand(transcodeCacheCmd)
}
