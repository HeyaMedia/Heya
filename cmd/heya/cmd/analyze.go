package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/pgvector/pgvector-go"
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

		pending, err := q.CountPendingAnalysis(ctx, sqlc.CountPendingAnalysisParams{
			MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
			AnalyzerVersion:    version,
		})
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
until pending == 0 or the context is cancelled.

This command runs end-to-end in-process — independent of River. Use
'heya queue process' to drain whatever the scheduled kickoff has
already fanned out into River.`,
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

		q := sqlc.New(app.DBPool())
		total, _ := q.CountPendingAnalysis(ctx, sqlc.CountPendingAnalysisParams{
			MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
			AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
		})
		if total == 0 {
			fmt.Println("nothing to analyze.")
			return nil
		}
		fmt.Printf("analyzing %d tracks (--once: %v)...\n", total, once)

		if err := analyzer.Load(ctx); err != nil {
			return fmt.Errorf("analyzer load: %w", err)
		}
		defer analyzer.Unload()

		affectedArtists := map[int64]struct{}{}
		affectedAlbums := map[int64]struct{}{}
		processed, failed := 0, 0
		currentVersion := sonicanalysis.AnalyzerVersion

		for ctx.Err() == nil {
			next, err := q.NextTrackForAnalysis(ctx, sqlc.NextTrackForAnalysisParams{
				MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
				AnalyzerVersion:    currentVersion,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			if err != nil {
				return fmt.Errorf("next track: %w", err)
			}
			facets, analyzeErr := analyzer.Analyze(ctx, next.FilePath)
			if analyzeErr != nil {
				log.Warn().Err(analyzeErr).Int64("track_id", next.ID).Msg("analyze failed")
				// Stub-write so we don't re-pick this track.
				_ = q.UpsertTrackFacets(ctx, sqlc.UpsertTrackFacetsParams{
					TrackID:         next.ID,
					AnalyzerVersion: currentVersion,
				})
				failed++
				continue
			}
			if err := persistCLIFacets(ctx, q, next.ID, facets, currentVersion); err != nil {
				log.Warn().Err(err).Int64("track_id", next.ID).Msg("persist failed")
				failed++
				continue
			}
			affectedArtists[next.ArtistID] = struct{}{}
			affectedAlbums[next.AlbumID] = struct{}{}
			processed++
			if once {
				break
			}
		}

		for artistID := range affectedArtists {
			_ = q.RefreshArtistCentroid(ctx, artistID)
		}
		for albumID := range affectedAlbums {
			_ = q.RefreshAlbumCentroid(ctx, albumID)
		}

		fmt.Printf("done. completed=%d failed=%d total=%d\n", processed, failed, processed+failed)
		return nil
	},
}

// persistCLIFacets writes one track_facets row from the analyze run
// CLI. Mirrors what AnalyzeTrackFacetsWorker does on the server side —
// kept here so the CLI doesn't drag in a River client dependency just
// to call the worker.
func persistCLIFacets(ctx context.Context, q *sqlc.Queries, trackID int64, f *sonicanalysis.Facets, currentVersion int32) error {
	topGenresJSON, err := json.Marshal(f.TopGenres)
	if err != nil {
		return fmt.Errorf("marshal top_genres: %w", err)
	}
	moodTagsJSON, err := json.Marshal(f.MoodTags)
	if err != nil {
		return fmt.Errorf("marshal mood_tags: %w", err)
	}
	return q.UpsertTrackFacets(ctx, sqlc.UpsertTrackFacetsParams{
		TrackID:          trackID,
		TrackEmbedding:   pgvector.NewVector(f.TrackEmbed),
		ArtistEmbedding:  pgvector.NewVector(f.ArtistEmbed),
		ReleaseEmbedding: pgvector.NewVector(f.ReleaseEmbed),
		TextEmbedding:    pgvector.NewVector(f.TextEmbed),
		Bpm:              pgtype.Float4{Float32: float32(f.BPM), Valid: true},
		BpmConfidence:    pgtype.Float4{Float32: float32(f.BPMConfidence), Valid: true},
		KeyRoot:          pgtype.Int2{Int16: int16(f.Key.Root), Valid: true},
		KeyMode:          pgtype.Int2{Int16: int16(f.Key.Mode), Valid: true},
		KeyClarity:       pgtype.Float4{Float32: float32(f.KeyClarity), Valid: true},
		TopGenres:        topGenresJSON,
		MoodTags:         moodTagsJSON,
		Waveform:         f.Waveform,
		AnalyzerVersion:  currentVersion,
	})
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
