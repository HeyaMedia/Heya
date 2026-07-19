package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/spf13/cobra"
)

var libraryCmd = &cobra.Command{
	Use:   "library",
	Short: "Manage media libraries",
	Long:  "Add, list, scan, and remove media libraries.",
}

var libraryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new library",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		mediaTypeStr, _ := cmd.Flags().GetString("type")
		paths, _ := cmd.Flags().GetStringSlice("path")

		if name == "" || mediaTypeStr == "" || len(paths) == 0 {
			return fmt.Errorf("--name, --type, and --path are required")
		}

		mt, err := service.ParseMediaType(mediaTypeStr)
		if err != nil {
			return err
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			q := sqlc.New(app.DBPool())
			users, err := q.ListUsers(ctx)
			if err != nil || len(users) == 0 {
				return fmt.Errorf("no users exist — create a user first with `heya user create`")
			}

			settings := settingsFromFlags(cmd, mediaTypeStr)

			lib, err := app.CreateLibrary(ctx, name, mt, paths, users[0].ID, settings)
			if err != nil {
				return err
			}

			ui.Success("Created library: %s (id=%d)", lib.Name, lib.ID)
			ui.Info("Type", ui.MediaBadge(string(lib.MediaType)))
			ui.Info("Paths", strings.Join(secrettext.RedactStrings(lib.Paths), ", "))
			printLibrarySettings(lib)
			return nil
		})
	},
}

var libraryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all libraries",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			libs, err := app.ListLibraries(ctx)
			if err != nil {
				return err
			}

			if ui.JSONMode {
				for i := range libs {
					libs[i].Paths = secrettext.RedactStrings(libs[i].Paths)
				}
				return ui.OutputJSON(libs)
			}

			if len(libs) == 0 {
				ui.Warn("No libraries found. Run 'heya library add' to create one.")
				return nil
			}

			t := ui.NewTable("ID", "NAME", "TYPE", "PATHS")
			for _, lib := range libs {
				t.AddRow(
					strconv.FormatInt(lib.ID, 10),
					lib.Name,
					ui.MediaBadge(string(lib.MediaType)),
					strings.Join(secrettext.RedactStrings(lib.Paths), ", "),
				)
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run the library scanner",
	Long:  "Runs the scanner synchronously and emits every observed fact and plan decision. Writes only when --apply is set.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		all, _ := cmd.Flags().GetBool("all")
		apply, _ := cmd.Flags().GetBool("apply")
		fetch, _ := cmd.Flags().GetBool("fetch")
		jsonl, _ := cmd.Flags().GetBool("jsonl")
		materialize, _ := cmd.Flags().GetBool("materialize")
		report, _ := cmd.Flags().GetBool("report")
		search, _ := cmd.Flags().GetBool("search")
		if apply {
			materialize = true
		}
		if materialize {
			fetch = true
		}
		if fetch {
			search = true
		}

		if id == 0 && !all {
			return fmt.Errorf("--id or --all is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			var libs []sqlc.Library
			if all {
				var err error
				libs, err = app.ListLibraries(ctx)
				if err != nil {
					return err
				}
			} else {
				lib, err := app.GetLibrary(ctx, id)
				if err != nil {
					return err
				}
				libs = []sqlc.Library{lib}
			}
			for _, lib := range libs {
				if len(libs) > 1 {
					ui.Header(fmt.Sprintf("Scanning %s", lib.Name))
				}
				opts := scanner.NormalizeOptions(scanner.Options{
					Apply:              apply,
					ApplyDB:            app.DBPool(),
					JSONL:              jsonl,
					PersistenceDB:      app.DBPool(),
					PersistScan:        true,
					Report:             report,
					FetchPreview:       fetch,
					MaterializePreview: materialize,
					RemoteSearch:       search,
					BookFetcher:        app.Metadata(),
					BookMaterializer:   scanner.NewSQLBookMaterializeStore(app.DBPool()),
					BookSearcher:       app.Metadata(),
					MovieSearcher:      app.Metadata(),
					MovieFetcher:       app.Metadata(),
					MovieMaterializer:  scanner.NewSQLMovieMaterializeStore(app.DBPool()),
					MusicProbe:         mediaprobe.Probe,
					MusicFetcher:       app.Metadata(),
					MusicMaterializer:  scanner.NewSQLMusicMaterializeStore(app.DBPool()),
					MusicSearcher:      app.Metadata(),
					TVFetcher:          app.Metadata(),
					TVMaterializer:     scanner.NewSQLTVMaterializeStore(app.DBPool()),
					TVSearcher:         app.Metadata(),
				})
				run := scanner.NewLibraryRun(lib, opts, cmd.OutOrStdout())
				if err := run.Run(ctx, scanner.PhaseAnalyze); err != nil {
					return err
				}
				if search {
					if err := run.Run(ctx, scanner.PhaseSearch); err != nil {
						return err
					}
				}
				if fetch {
					if err := run.Run(ctx, scanner.PhaseFetch); err != nil {
						return err
					}
				}
				if materialize {
					if err := run.Run(ctx, scanner.PhaseMaterialize); err != nil {
						return err
					}
				}
				if apply {
					if err := run.Run(ctx, scanner.PhaseApply); err != nil {
						return err
					}
				}
				_, err := run.Finish(ctx)
				if err != nil {
					return err
				}
			}
			return nil
		})
	},
}

var libraryRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a library",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteLibrary(ctx, id); err != nil {
				return err
			}

			ui.Success("Deleted library: id=%d", id)
			return nil
		})
	},
}

var libraryRematchReviewsCmd = &cobra.Command{
	Use:     "rematch-reviews",
	Aliases: []string{"rematch-music-reviews"},
	Short:   "Rematch review rows using current, source-valid analysis",
	Long: "Replays a retained local analysis only when it was produced by the current scanner rules " +
		"and every captured source file is unchanged. Stale revisions, removed sidecars, and replaced " +
		"media are sent through fresh scoped analysis instead of resurrecting old evidence.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		limit, _ := cmd.Flags().GetInt32("limit")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}
		if limit <= 0 {
			limit = 10000
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			q := sqlc.New(app.DBPool())
			lib, err := q.GetLibraryByID(ctx, id)
			if err != nil {
				return err
			}
			switch lib.MediaType {
			case sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeAnime, sqlc.MediaTypeMusic, sqlc.MediaTypeBook:
			default:
				return fmt.Errorf("library %d has unsupported scanner media type %s", id, lib.MediaType)
			}
			rows, err := q.ListScannerReviewsForRematch(ctx, sqlc.ListScannerReviewsForRematchParams{
				LibraryID: id,
				MediaType: lib.MediaType,
				RowLimit:  limit,
			})
			if err != nil {
				return err
			}
			staleByScope := make(map[string]bool)
			reanalysisScopes := make(map[string][]string)
			for _, row := range rows {
				scopeKey := strings.Join(row.ScopePaths, "\x00")
				if staleByScope[scopeKey] {
					continue
				}
				artifact, artifactErr := q.GetScannerEntityArtifact(ctx, row.AnalysisArtifactID)
				if artifactErr != nil {
					return fmt.Errorf("load retained analysis artifact %d: %w", row.AnalysisArtifactID, artifactErr)
				}
				if replayErr := scanner.ValidateScannerAnalysisArtifactReplayWithDB(ctx, app.DBPool(), artifact); replayErr != nil {
					staleByScope[scopeKey] = true
					reanalysisScopes[scopeKey] = row.ScopePaths
					continue
				}
			}

			reanalyzed := 0
			for _, scopePaths := range reanalysisScopes {
				if err := worker.EnqueueProcessLibraryScan(ctx, app.RiverClient(), app.DBPool(), worker.ProcessLibraryScanArgs{
					LibraryID: lib.ID, MediaType: lib.MediaType, ScopePaths: scopePaths, Force: true,
				}, worker.PriorityMatch, "rematch_reviews_stale_analysis"); err != nil {
					return fmt.Errorf("enqueue fresh scanner analysis after %d scopes: %w", reanalyzed, err)
				}
				reanalyzed++
			}

			enqueued := 0
			for _, row := range rows {
				scopeKey := strings.Join(row.ScopePaths, "\x00")
				if staleByScope[scopeKey] {
					continue
				}
				if err := worker.EnqueueSearchLibraryMetadata(ctx, app.RiverClient(), app.DBPool(), worker.SearchLibraryMetadataArgs{
					LibraryID:          row.LibraryID,
					MediaType:          lib.MediaType,
					ScopePaths:         row.ScopePaths,
					ScannerEntityID:    row.ScannerEntityID,
					AnalysisArtifactID: row.AnalysisArtifactID,
					Force:              true,
				}, worker.PriorityMatch, ""); err != nil {
					return fmt.Errorf("enqueue scanner entity %d after %d rows: %w", row.ScannerEntityID, enqueued, err)
				}
				enqueued++
			}
			ui.Success("Enqueued %d review rematches and %d fresh scope analyses for %s", enqueued, reanalyzed, lib.Name)
			return nil
		})
	},
}

var libraryReanalyzeScopesCmd = &cobra.Command{
	Use:   "reanalyze-scopes",
	Short: "Enqueue fresh scanner analysis for specific library directories",
	Long: "Invalidates no unrelated work: every --scope is queued as its own scanner unit, " +
		"so the resulting generation replaces the prior entity set for exactly that directory.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		scopes, _ := cmd.Flags().GetStringSlice("scope")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}
		if len(scopes) == 0 {
			return fmt.Errorf("at least one --scope is required")
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			lib, err := app.GetLibrary(ctx, id)
			if err != nil {
				return err
			}
			queued := 0
			seen := map[string]bool{}
			for _, value := range scopes {
				scope := filepath.Clean(strings.TrimSpace(value))
				if !filepath.IsAbs(scope) {
					return fmt.Errorf("scope must be an absolute path: %q", value)
				}
				inside, root := libraryScopeRoot(lib.Paths, scope)
				if !inside {
					return fmt.Errorf("scope is outside library %d roots: %q", id, scope)
				}
				if scope == root {
					return fmt.Errorf("scope is a complete library root; use the ordinary library scan instead: %q", scope)
				}
				if seen[scope] {
					continue
				}
				seen[scope] = true
				if err := worker.EnqueueProcessLibraryScan(ctx, app.RiverClient(), app.DBPool(), worker.ProcessLibraryScanArgs{
					LibraryID: lib.ID, MediaType: lib.MediaType, ScopePaths: []string{scope}, Force: true,
				}, worker.PriorityMatch, "manual_scope_reanalysis"); err != nil {
					return fmt.Errorf("enqueue scope %q after %d scopes: %w", scope, queued, err)
				}
				queued++
			}
			ui.Success("Enqueued %d fresh scope analyses for %s", queued, lib.Name)
			return nil
		})
	},
}

func libraryScopeRoot(roots []string, scope string) (bool, string) {
	for _, value := range roots {
		root := filepath.Clean(strings.TrimSpace(value))
		if !filepath.IsAbs(root) {
			continue
		}
		rel, err := filepath.Rel(root, scope)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true, root
		}
	}
	return false, ""
}

var libraryFilesCmd = &cobra.Command{
	Use:   "files",
	Short: "List files in a library",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			files, err := app.ListLibraryFiles(ctx, id, 100, 0)
			if err != nil {
				return err
			}

			if ui.JSONMode {
				return ui.OutputJSON(files)
			}

			if len(files) == 0 {
				ui.Warn("No files found. Run 'heya library scan' first.")
				return nil
			}

			t := ui.NewTable("ID", "STATUS", "PATH")
			for _, f := range files {
				t.AddRow(
					strconv.FormatInt(f.ID, 10),
					ui.StatusBadge(string(f.Status)),
					filepath.Base(f.Path),
				)
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var libraryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show library statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			stats, err := app.LibraryFileStats(ctx, id)
			if err != nil {
				return err
			}

			if ui.JSONMode {
				return ui.OutputJSON(stats)
			}

			if len(stats) == 0 {
				ui.Warn("No files in library. Run 'heya library scan' first.")
				return nil
			}

			t := ui.NewTable("STATUS", "COUNT")
			for _, s := range stats {
				t.AddRow(
					ui.StatusBadge(string(s.Status)),
					strconv.FormatInt(s.Count, 10),
				)
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var libraryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Show watcher status",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			status, err := app.WorkerRuntimeStatus(ctx)
			if err != nil {
				return err
			}
			if !status.Online(time.Now()) {
				ui.Warn("The dedicated worker is not running. Start it with 'heya worker'.")
				return nil
			}
			if len(status.Watchers) == 0 {
				ui.Warn("The worker is online but has no active filesystem watchers.")
				return nil
			}

			t := ui.NewTable("LIBRARY", "PATH")
			for _, watcher := range status.Watchers {
				t.AddRow(strconv.FormatInt(watcher.LibraryID, 10), secrettext.Redact(watcher.Path))
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var libraryInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show library details and settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			lib, err := app.GetLibrary(ctx, id)
			if err != nil {
				return err
			}

			ui.Header(lib.Name)
			ui.Info("ID", strconv.FormatInt(lib.ID, 10))
			ui.Info("Type", ui.MediaBadge(string(lib.MediaType)))
			ui.Info("Paths", strings.Join(secrettext.RedactStrings(lib.Paths), ", "))
			printLibrarySettings(lib)
			return nil
		})
	},
}

var librarySettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Update library settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			lib, err := app.GetLibrary(ctx, id)
			if err != nil {
				return err
			}

			current := metadata.ParseSettings(lib.Settings)
			updated := applySettingsFlags(cmd, current)

			lib, err = app.UpdateLibrarySettings(ctx, id, updated)
			if err != nil {
				return err
			}

			ui.Success("Updated settings for library: %s (id=%d)", lib.Name, lib.ID)
			printLibrarySettings(lib)
			return nil
		})
	},
}

func settingsFromFlags(cmd *cobra.Command, mediaType string) *metadata.LibrarySettings {
	defaults := metadata.DefaultSettings(mediaType)
	s := applySettingsFlags(cmd, defaults)
	return &s
}

func applySettingsFlags(cmd *cobra.Command, s metadata.LibrarySettings) metadata.LibrarySettings {
	if cmd.Flags().Changed("language") {
		s.PreferredLanguage, _ = cmd.Flags().GetString("language")
	}
	if cmd.Flags().Changed("country") {
		s.PreferredCountry, _ = cmd.Flags().GetString("country")
	}
	if cmd.Flags().Changed("watch") {
		s.Watch, _ = cmd.Flags().GetBool("watch")
	}
	if cmd.Flags().Changed("auto-collections") {
		s.AutoCollections, _ = cmd.Flags().GetBool("auto-collections")
	}
	if cmd.Flags().Changed("fetch-ratings") {
		s.FetchRatings, _ = cmd.Flags().GetBool("fetch-ratings")
	}
	if cmd.Flags().Changed("save-nfo") {
		s.SaveNFO, _ = cmd.Flags().GetBool("save-nfo")
	}
	if cmd.Flags().Changed("save-images") {
		s.SaveImages, _ = cmd.Flags().GetBool("save-images")
	}
	return s
}

func printLibrarySettings(lib sqlc.Library) {
	s := metadata.ParseSettings(lib.Settings)
	fmt.Println()
	ui.Info("Settings", "")
	if s.PreferredLanguage != "" {
		ui.Info("  Language", s.PreferredLanguage)
	}
	if s.PreferredCountry != "" {
		ui.Info("  Country", s.PreferredCountry)
	}
	ui.Info("  Watch", strconv.FormatBool(s.Watch))
	ui.Info("  Auto collections", strconv.FormatBool(s.AutoCollections))
	ui.Info("  Fetch ratings", strconv.FormatBool(s.FetchRatings))
	ui.Info("  Save NFO", strconv.FormatBool(s.SaveNFO))
	ui.Info("  Save images", strconv.FormatBool(s.SaveImages))
}

func init() {
	libraryAddCmd.Flags().String("name", "", "Library name")
	libraryAddCmd.Flags().String("type", "", "Media type (movie, tv, music, book)")
	libraryAddCmd.Flags().StringSlice("path", nil, "Absolute filesystem paths to watch (mount network shares first)")
	addSettingsFlags(libraryAddCmd)

	libraryScanCmd.Flags().Int64("id", 0, "Library ID to scan")
	libraryScanCmd.Flags().Bool("all", false, "Scan all libraries")
	libraryScanCmd.Flags().Bool("apply", false, "Apply the materialization plan to the database")
	libraryScanCmd.Flags().Bool("fetch", false, "Fetch selected metadata details and report what would be applied without writing")
	libraryScanCmd.Flags().Bool("jsonl", false, "Emit one JSON event per line")
	libraryScanCmd.Flags().Bool("materialize", false, "Preview media item, domain row, and file writes without applying them")
	libraryScanCmd.Flags().Bool("report", false, "Emit a compact review report instead of the event stream")
	libraryScanCmd.Flags().Bool("search", false, "Search HeyaMetadata for candidate matches without fetching metadata")

	libraryRemoveCmd.Flags().Int64("id", 0, "Library ID to remove")
	libraryRematchReviewsCmd.Flags().Int64("id", 0, "Library ID")
	libraryRematchReviewsCmd.Flags().Int32("limit", 10000, "Maximum review rows to enqueue")
	libraryReanalyzeScopesCmd.Flags().Int64("id", 0, "Library ID")
	libraryReanalyzeScopesCmd.Flags().StringSlice("scope", nil, "Absolute directory to reanalyze (repeatable)")

	libraryFilesCmd.Flags().Int64("id", 0, "Library ID")
	libraryFilesCmd.Flags().String("media", "", "Filter by media type")
	libraryFilesCmd.Flags().String("status", "", "Filter by status")

	libraryStatsCmd.Flags().Int64("id", 0, "Library ID")

	libraryWatchCmd.Flags().Int64("id", 0, "Library ID")

	libraryInfoCmd.Flags().Int64("id", 0, "Library ID")

	librarySettingsCmd.Flags().Int64("id", 0, "Library ID to update")
	addSettingsFlags(librarySettingsCmd)

	libraryCmd.AddCommand(libraryAddCmd)
	libraryCmd.AddCommand(libraryListCmd)
	libraryCmd.AddCommand(libraryScanCmd)
	libraryCmd.AddCommand(libraryRemoveCmd)
	libraryCmd.AddCommand(libraryRematchReviewsCmd)
	libraryCmd.AddCommand(libraryReanalyzeScopesCmd)
	libraryCmd.AddCommand(libraryFilesCmd)
	libraryCmd.AddCommand(libraryStatsCmd)
	libraryCmd.AddCommand(libraryWatchCmd)
	libraryCmd.AddCommand(libraryInfoCmd)
	libraryCmd.AddCommand(librarySettingsCmd)
}

func addSettingsFlags(cmd *cobra.Command) {
	cmd.Flags().String("language", "", "Preferred metadata language (e.g. en)")
	cmd.Flags().String("country", "", "Preferred country/region (e.g. US)")
	cmd.Flags().Bool("watch", false, "Enable filesystem watching")
	cmd.Flags().Bool("auto-collections", false, "Automatically add to collections (movies)")
	cmd.Flags().Bool("fetch-ratings", true, "Fetch external ratings through HeyaMetadata V2")
	cmd.Flags().Bool("save-nfo", false, "Write NFO files to media directory")
	cmd.Flags().Bool("save-images", false, "Write images to media directory")
}
