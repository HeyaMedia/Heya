package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/ingestv2"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
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
			ui.Info("Paths", strings.Join(lib.Paths, ", "))
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
					strings.Join(lib.Paths, ", "),
				)
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan a library for media files",
	Long:  "Discovers files and enqueues them for processing. Use 'heya queue process' to run the queue, or 'heya serve' will process automatically.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

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
				ui.Header(fmt.Sprintf("Scanning %s", lib.Name))
				ui.Info("Library", fmt.Sprintf("%s (id=%d)", lib.Name, lib.ID))
				ui.Info("Type", ui.MediaBadge(string(lib.MediaType)))

				app.EnqueueScanLibrary(lib.ID, force)
				ui.Success("Scan enqueued")
			}

			fmt.Println()
			ui.Println(ui.Dim("Jobs will be processed by 'heya serve', or run 'heya queue process'."))

			return nil
		})
	},
}

var libraryScanV2Cmd = &cobra.Command{
	Use:   "scan-v2",
	Short: "Run the experimental v2 library scanner",
	Long:  "Runs the experimental ingest v2 scanner synchronously and emits every observed fact and plan decision. Writes only when --apply is set.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
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

		if id == 0 {
			return fmt.Errorf("--id is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			lib, err := app.GetLibrary(ctx, id)
			if err != nil {
				return err
			}
			_, err = ingestv2.RunLibrary(ctx, lib, ingestv2.Options{
				Apply:              apply,
				ApplyDB:            app.DBPool(),
				JSONL:              jsonl,
				PersistenceDB:      app.DBPool(),
				PersistScan:        true,
				Report:             report,
				FetchPreview:       fetch,
				MaterializePreview: materialize,
				RemoteSearch:       search,
				MovieSearcher:      app.Metadata(),
				MovieFetcher:       app.Metadata(),
				MovieMaterializer:  ingestv2.NewSQLMovieMaterializeStore(app.DBPool()),
				TVFetcher:          app.Metadata(),
				TVMaterializer:     ingestv2.NewSQLTVMaterializeStore(app.DBPool()),
				TVSearcher:         app.Metadata(),
			}, cmd.OutOrStdout())
			return err
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
			status := app.WatcherManager().Status()
			if len(status) == 0 {
				ui.Warn("No active watchers. Start the server with 'heya serve' to enable file watching.")
				return nil
			}

			t := ui.NewTable("LIBRARY", "PATH")
			for id, path := range status {
				t.AddRow(strconv.FormatInt(id, 10), path)
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
			ui.Info("Paths", strings.Join(lib.Paths, ", "))
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
	libraryAddCmd.Flags().StringSlice("path", nil, "Filesystem paths to watch")
	addSettingsFlags(libraryAddCmd)

	libraryScanCmd.Flags().Int64("id", 0, "Library ID to scan")
	libraryScanCmd.Flags().String("name", "", "Library name to scan")
	libraryScanCmd.Flags().Bool("all", false, "Scan all libraries")
	libraryScanCmd.Flags().Bool("force", false, "Force re-scan all files")

	libraryScanV2Cmd.Flags().Int64("id", 0, "Library ID to scan")
	libraryScanV2Cmd.Flags().Bool("apply", false, "Apply the v2 materialization plan to the database")
	libraryScanV2Cmd.Flags().Bool("fetch", false, "Fetch selected metadata details and report what would be applied without writing")
	libraryScanV2Cmd.Flags().Bool("jsonl", false, "Emit one JSON event per line")
	libraryScanV2Cmd.Flags().Bool("materialize", false, "Preview media item, movie row, and file writes without applying them")
	libraryScanV2Cmd.Flags().Bool("report", false, "Emit a compact review report instead of the event stream")
	libraryScanV2Cmd.Flags().Bool("search", false, "Search heya.media for candidate matches without fetching metadata")

	libraryRemoveCmd.Flags().Int64("id", 0, "Library ID to remove")

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
	libraryCmd.AddCommand(libraryScanV2Cmd)
	libraryCmd.AddCommand(libraryRemoveCmd)
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
	cmd.Flags().Bool("fetch-ratings", true, "Fetch external ratings (IMDb, TMDB, etc.) from heya.media")
	cmd.Flags().Bool("save-nfo", false, "Write NFO files to media directory")
	cmd.Flags().Bool("save-images", false, "Write images to media directory")
}
