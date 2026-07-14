package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/imagegen"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var imageCmd = &cobra.Command{Use: "image", Short: "Local image generation with stable-diffusion.cpp"}
var imageModel = imagegen.DefaultModel
var imageBackend = imagegen.BackendAuto
var imageDevice = "auto"
var imageMemoryMode string

var imageModelsCmd = &cobra.Command{Use: "models", Short: "List curated image models (never downloads)", RunE: func(cmd *cobra.Command, args []string) error {
	return withApp(func(ctx context.Context, app *service.App) error {
		for _, m := range app.ImageModels() {
			present := app.ImageStatus(m.ID, imageBackend).ModelPresent
			fmt.Printf("%-28s %5.1f GB  present=%-5v  %s\n", m.ID, float64(m.DownloadSize())/(1<<30), present, m.Label)
		}
		return nil
	})
}}
var imageStatusCmd = &cobra.Command{Use: "status", Short: "Show local image artifact state (never downloads)", RunE: func(cmd *cobra.Command, args []string) error {
	return withApp(func(ctx context.Context, app *service.App) error {
		st := app.ImageStatus(imageModel, imageBackend)
		if ui.JSONMode {
			return ui.OutputJSON(st)
		}
		fmt.Printf("stable-diffusion.cpp: %s\nbackend             : %s\nmodel               : %s\nruntime present     : %v\nmodel present       : %v\ndownload            : %s\n", st.Build, st.Backend, st.Model, st.RuntimePresent, st.ModelPresent, st.DownloadState)
		for _, device := range st.Devices {
			fmt.Printf("  device      %-12s %s\n", device.Name, device.Description)
		}
		if st.DeviceError != "" {
			fmt.Printf("device error        : %s\n", st.DeviceError)
		}
		for _, a := range st.Artifacts {
			suffix := "missing"
			if a.Present {
				suffix = "present"
			}
			if a.Shared {
				suffix += ", shared from LLM"
			}
			fmt.Printf("  %-10s %-8s %5.2f GiB  %s\n", a.Role, suffix, float64(a.Size)/(1<<30), a.Name)
		}
		fmt.Printf("additional download : %.2f GiB\n", float64(st.DownloadBytes)/(1<<30))
		if st.DownloadError != "" {
			fmt.Printf("download error      : %s\n", st.DownloadError)
		}
		return nil
	})
}}
var imageFetchCmd = &cobra.Command{Use: "fetch", Short: "Explicitly download the selected runtime and model", Long: "This is the only image command that accesses the network. It downloads pinned, checksummed artifacts.", RunE: func(cmd *cobra.Command, args []string) error {
	return withApp(func(ctx context.Context, app *service.App) error {
		m, ok := imagegen.ModelByID(imageModel)
		if !ok {
			return fmt.Errorf("unknown image model %q", imageModel)
		}
		fmt.Printf("Explicit download: %s (up to %.1f GB; existing shared artifacts are reused) + %s runtime.\n", m.Label, float64(m.DownloadSize())/(1<<30), imagegen.ResolveBackend(imageBackend))
		done := make(chan error, 1)
		go func() { done <- app.ImageDownloadWait(ctx, imageModel, imageBackend) }()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case err := <-done:
				if err != nil {
					return err
				}
				fmt.Println("\nimage runtime ready")
				return nil
			case <-tick.C:
				st := app.ImageStatus(imageModel, imageBackend)
				if p := st.Progress; p != nil && p.BytesTotal > 0 {
					fmt.Printf("\r%-48s %6.1f%% (%d / %d MB)   ", p.CurrentFile, float64(p.BytesDone)/float64(p.BytesTotal)*100, p.BytesDone>>20, p.BytesTotal>>20)
				}
			}
		}
	})
}}

var imageOutput, imageNegative string
var imageWidth, imageHeight, imageSteps int
var imageCFG float64
var imageSeed int64
var imageGenerateCmd = &cobra.Command{Use: "generate <prompt...>", Short: "Generate an image from already-downloaded artifacts", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return withApp(func(ctx context.Context, app *service.App) error {
		res, err := app.ImageGenerate(ctx, imagegen.Request{ModelID: imageModel, Backend: imageBackend, Device: imageDevice, MemoryMode: imageMemoryMode, Prompt: strings.Join(args, " "), NegativePrompt: imageNegative, Output: imageOutput, Width: imageWidth, Height: imageHeight, Steps: imageSteps, CFG: imageCFG, Seed: imageSeed})
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(res)
		}
		fmt.Printf("%s\n[%s · %.1fs]\n", res.Path, res.Model, float64(res.DurationMs)/1000)
		return nil
	})
}}

func init() {
	for _, c := range []*cobra.Command{imageModelsCmd, imageStatusCmd, imageFetchCmd, imageGenerateCmd} {
		c.Flags().StringVar(&imageModel, "model", imagegen.DefaultModel, "curated model id")
		c.Flags().StringVar(&imageBackend, "backend", imagegen.BackendAuto, "auto|cpu|cuda|rocm|vulkan|metal")
	}
	imageGenerateCmd.Flags().StringVarP(&imageOutput, "output", "o", "", "output PNG path")
	imageGenerateCmd.Flags().StringVar(&imageNegative, "negative", "", "negative prompt")
	imageGenerateCmd.Flags().StringVar(&imageDevice, "device", "auto", "compute device from `heya image status` (auto uses the memory mode's preferred placement)")
	imageGenerateCmd.Flags().StringVar(&imageMemoryMode, "memory-mode", "", "auto|low_vram (model-recommended mode when omitted)")
	imageGenerateCmd.Flags().IntVar(&imageWidth, "width", 0, "width (model default when zero)")
	imageGenerateCmd.Flags().IntVar(&imageHeight, "height", 0, "height (model default when zero)")
	imageGenerateCmd.Flags().IntVar(&imageSteps, "steps", 0, "sampling steps (model default when zero)")
	imageGenerateCmd.Flags().Float64Var(&imageCFG, "cfg", 0, "CFG scale (model default when zero)")
	imageGenerateCmd.Flags().Int64Var(&imageSeed, "seed", 0, "seed (random when zero)")
	imageCmd.AddCommand(imageModelsCmd, imageStatusCmd, imageFetchCmd, imageGenerateCmd)
	rootCmd.AddCommand(imageCmd)
}
