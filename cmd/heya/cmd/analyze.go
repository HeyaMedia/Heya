package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Sonic analysis pipeline controls",
	Long:  "Inspect, trigger, and reset the per-track ML/DSP music analysis pipeline.",
}

// modelsDir is the canonical on-disk location for downloaded ONNX
// models. Derived from cfg.DataDir; not user-tunable beyond DataDir.
func modelsDir() string { return cfg.DataDir.Value + "/models" }

// withApp constructs a service.App so the CLI shares the same
// settings + DB-backed plumbing as the running server. Caller is
// responsible for app.Close() (or the process exit cleans it up).
func withApp(ctx context.Context) (*service.App, error) {
	return service.New(ctx, cfg)
}

var analyzeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show analyzer + fetcher state and pending count",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		db, err := database.Connect(ctx, cfg.DatabaseURL.Value)
		if err != nil {
			return err
		}
		defer db.Close()
		q := sqlc.New(db)

		app, err := withApp(ctx)
		if err != nil {
			return err
		}
		defer app.Close()
		settings := app.SonicAnalysisSettings(ctx)
		version := sonicanalysis.AnalyzerVersion

		pending, err := q.CountPendingAnalysis(ctx, version)
		if err != nil {
			return fmt.Errorf("count pending: %w", err)
		}
		analyzed, _ := q.CountAnalyzedTracks(ctx, version)

		fmt.Printf("enabled                       : %v\n", settings.Enabled)
		fmt.Printf("models_dir                    : %s\n", modelsDir())
		fmt.Printf("accelerator                   : %s\n", settings.Accelerator)
		fmt.Printf("analyzer_version              : %d\n", version)
		fmt.Printf("tracks analyzed (this version): %d\n", analyzed)
		fmt.Printf("tracks pending analysis       : %d\n", pending)

		fetcher := sonicanalysis.NewModelFetcher(modelsDir(), "")
		fmt.Printf("models on disk                : %v\n", fetcher.AllPresent())
		return nil
	},
}

var analyzeFetchModelsCmd = &cobra.Command{
	Use:   "fetch-models",
	Short: "Download missing model files (blocking)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		fetcher := sonicanalysis.NewModelFetcher(modelsDir(), "")

		done := make(chan error, 1)
		go func() { done <- fetcher.Run(ctx) }()

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case err := <-done:
				if err != nil {
					return err
				}
				fmt.Println("all models present.")
				return nil
			case <-ticker.C:
				if p := fetcher.Progress(); p != nil {
					fmt.Printf("  %s (%d/%d files, %.1f MB / %.1f MB)\n",
						p.CurrentFile,
						p.FilesDone, p.FilesTotal,
						float64(p.BytesDone)/(1<<20),
						float64(p.BytesTotal)/(1<<20),
					)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	},
}

var analyzeRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run one analyzer pass now (ignores window)",
	Long: `Loads models, processes pending tracks one at a time, refreshes
centroids, then unloads. Honors --once for a single track, or runs
until pending == 0 or the context is cancelled.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		once, _ := cmd.Flags().GetBool("once")

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		app, err := withApp(ctx)
		if err != nil {
			return err
		}
		defer app.Close()

		settings := app.SonicAnalysisSettings(ctx)
		analyzer := sonicanalysis.NewAnalyzer(sonicanalysis.Config{
			ModelsDir:   modelsDir(),
			Accelerator: sonicanalysis.Accelerator(settings.Accelerator),
		})

		task := &scheduler.AnalyzeMusicTask{
			DB:       app.DBPool(),
			Analyzer: analyzer,
		}

		total, _ := task.CountPending(ctx)
		if total == 0 {
			fmt.Println("nothing to analyze.")
			return nil
		}
		if once {
			total = 1
		}
		fmt.Printf("analyzing %d tracks...\n", total)

		progress := scheduler.NewProgressTracker(scheduler.TaskAnalyzeMusicFacets, total)

		runCtx := ctx
		if once {
			var stop context.CancelFunc
			runCtx, stop = context.WithCancel(ctx)
			go func() {
				for {
					snap := progress.Snapshot()
					if snap.Completed+snap.Failed >= 1 {
						stop()
						return
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(100 * time.Millisecond):
					}
				}
			}()
			defer stop()
		}

		err = task.Run(runCtx, progress)
		snap := progress.Snapshot()
		fmt.Printf("done. completed=%d failed=%d total=%d\n",
			snap.Completed-snap.Failed, snap.Failed, snap.Total)
		return err
	},
}

var analyzeResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Force re-analysis of all (or one library's) tracks",
	RunE: func(cmd *cobra.Command, args []string) error {
		libraryID, _ := cmd.Flags().GetInt64("library")
		ctx := context.Background()
		db, err := database.Connect(ctx, cfg.DatabaseURL.Value)
		if err != nil {
			return err
		}
		defer db.Close()
		q := sqlc.New(db)
		if libraryID > 0 {
			if err := q.ResetTrackFacetsVersionForLibrary(ctx, libraryID); err != nil {
				return err
			}
			fmt.Printf("reset analyzer_version for library %d.\n", libraryID)
		} else {
			if err := q.ResetTrackFacetsVersion(ctx); err != nil {
				return err
			}
			fmt.Println("reset analyzer_version library-wide.")
		}
		return nil
	},
}

var analyzeWarmupCmd = &cobra.Command{
	Use:   "warmup",
	Short: "Load every model + run a smoke-test inference",
	Long:  "Loads the analyzer end-to-end and runs analyze() on a sample file to prove the full pipeline works. Useful after a fresh model fetch on a new machine.",
	RunE: func(cmd *cobra.Command, args []string) error {
		samplePath, _ := cmd.Flags().GetString("sample")
		if samplePath == "" {
			return fmt.Errorf("--sample <path-to-audio-file> is required")
		}
		abs, err := filepath.Abs(samplePath)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		app, err := withApp(ctx)
		if err != nil {
			return err
		}
		defer app.Close()
		settings := app.SonicAnalysisSettings(ctx)

		analyzer := sonicanalysis.NewAnalyzer(sonicanalysis.Config{
			ModelsDir:   modelsDir(),
			Accelerator: sonicanalysis.Accelerator(settings.Accelerator),
		})
		fmt.Println("loading models...")
		if err := analyzer.Load(ctx); err != nil {
			return err
		}
		defer analyzer.Unload()

		fmt.Printf("analyzing %s...\n", abs)
		facets, err := analyzer.Analyze(ctx, abs)
		if err != nil {
			return err
		}
		fmt.Printf("OK. bpm=%.1f (conf=%.2f), key=%s, top genre=%s, elapsed_ms=%d\n",
			facets.BPM, facets.BPMConfidence,
			facets.Key.String(),
			func() string {
				if len(facets.TopGenres) > 0 {
					return facets.TopGenres[0].Name
				}
				return ""
			}(),
			facets.ElapsedMs,
		)
		log.Info().Msg("warmup complete")
		return nil
	},
}

func init() {
	analyzeRunCmd.Flags().Bool("once", false, "Process a single track then exit")
	analyzeResetCmd.Flags().Int64("library", 0, "Reset only this library (default: all)")
	analyzeWarmupCmd.Flags().String("sample", "", "Audio file to run end-to-end against")

	analyzeCmd.AddCommand(analyzeStatusCmd)
	analyzeCmd.AddCommand(analyzeFetchModelsCmd)
	analyzeCmd.AddCommand(analyzeRunCmd)
	analyzeCmd.AddCommand(analyzeResetCmd)
	analyzeCmd.AddCommand(analyzeWarmupCmd)
	rootCmd.AddCommand(analyzeCmd)
}
