package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		q := sqlc.New(app.DB)
		users, err := q.ListUsers(ctx)
		if err != nil || len(users) == 0 {
			return fmt.Errorf("no users exist — create a user first with `kura user create`")
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
	},
}

var libraryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all libraries",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

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
	},
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan a library for media files",
	Long:  "Discovers files and enqueues them for processing. Use 'heya queue process' to run the queue, or 'heya serve'/'heya dev' will process automatically.",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		all, _ := cmd.Flags().GetBool("all")
		force, _ := cmd.Flags().GetBool("force")

		if id == 0 && !all {
			return fmt.Errorf("--id or --all is required")
		}

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		var libs []sqlc.Library
		if all {
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

			if err := app.EnqueueScanLibrary(ctx, lib.ID, force); err != nil {
				ui.Error("enqueue failed: %v", err)
				continue
			}
			ui.Success("Scan enqueued")
		}

		fmt.Println()
		ui.Println(ui.Dim("Jobs will be processed by 'heya serve' / 'heya dev', or run 'heya queue process'."))

		return nil
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		if err := app.DeleteLibrary(ctx, id); err != nil {
			return err
		}

		ui.Success("Deleted library: id=%d", id)
		return nil
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

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
	},
}

var libraryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Show watcher status",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		status := app.Watcher.Status()
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

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
	if cmd.Flags().Changed("metadata-providers") {
		v, _ := cmd.Flags().GetString("metadata-providers")
		s.MetadataProviders = splitComma(v)
	}
	if cmd.Flags().Changed("artwork-providers") {
		v, _ := cmd.Flags().GetString("artwork-providers")
		s.ArtworkProviders = splitComma(v)
	}
	if cmd.Flags().Changed("ratings-providers") {
		v, _ := cmd.Flags().GetString("ratings-providers")
		s.RatingsProviders = splitComma(v)
	}
	if cmd.Flags().Changed("watch") {
		s.Watch, _ = cmd.Flags().GetBool("watch")
	}
	if cmd.Flags().Changed("auto-collections") {
		s.AutoCollections, _ = cmd.Flags().GetBool("auto-collections")
	}
	if cmd.Flags().Changed("metadata-refresh") {
		s.MetadataRefreshDays, _ = cmd.Flags().GetInt("metadata-refresh")
	}
	if cmd.Flags().Changed("save-nfo") {
		s.SaveNFO, _ = cmd.Flags().GetBool("save-nfo")
	}
	if cmd.Flags().Changed("save-images") {
		s.SaveImages, _ = cmd.Flags().GetBool("save-images")
	}
	return s
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func printLibrarySettings(lib sqlc.Library) {
	s := metadata.ParseSettings(lib.Settings)
	fmt.Println()
	ui.Info("Settings", "")
	if len(s.MetadataProviders) > 0 {
		ui.Info("  Metadata providers", strings.Join(s.MetadataProviders, ", "))
	}
	if len(s.ArtworkProviders) > 0 {
		ui.Info("  Artwork providers", strings.Join(s.ArtworkProviders, ", "))
	}
	if len(s.RatingsProviders) > 0 {
		ui.Info("  Ratings providers", strings.Join(s.RatingsProviders, ", "))
	}
	if s.PreferredLanguage != "" {
		ui.Info("  Language", s.PreferredLanguage)
	}
	if s.PreferredCountry != "" {
		ui.Info("  Country", s.PreferredCountry)
	}
	ui.Info("  Watch", strconv.FormatBool(s.Watch))
	ui.Info("  Auto collections", strconv.FormatBool(s.AutoCollections))
	if s.MetadataRefreshDays > 0 {
		ui.Info("  Metadata refresh", fmt.Sprintf("every %d days", s.MetadataRefreshDays))
	} else {
		ui.Info("  Metadata refresh", "never")
	}
	ui.Info("  Save NFO", strconv.FormatBool(s.SaveNFO))
	ui.Info("  Save images", strconv.FormatBool(s.SaveImages))
}

func runInteractiveResolve(ctx context.Context, app *service.App, libraryID int64) {
	q := sqlc.New(app.DB)
	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Status:    sqlc.FileStatusUnmatched,
		Limit:     100,
		Offset:    0,
	})
	if err != nil || len(files) == 0 {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	total := len(files)

	for i, f := range files {
		candidates, err := app.ListMatchCandidates(ctx, f.ID)
		if err != nil || len(candidates) == 0 {
			continue
		}

		fmt.Printf("\n[%d/%d] %s\n", i+1, total, filepath.Base(f.Path))
		if f.ErrorMessage != "" {
			fmt.Printf("  Note: %s\n", f.ErrorMessage)
		}
		fmt.Println("  Candidates:")
		for j, c := range candidates {
			fmt.Printf("    %d. %s", j+1, c.Title)
			if c.Year != "" {
				fmt.Printf(" (%s)", c.Year)
			}
			fmt.Printf(" — %s %s — confidence: %.2f\n", c.ProviderName, c.ProviderID, numericToFloat(c.Confidence))
		}

		fmt.Printf("  [1-%d] select, [s]kip, [q]uit: ", len(candidates))
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "q" {
			fmt.Println("Quitting interactive mode.")
			return
		}
		if input == "s" || input == "" {
			continue
		}

		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(candidates) {
			fmt.Println("  Invalid selection, skipping.")
			continue
		}

		chosen := candidates[n-1]
		if err := app.ResolveMatch(ctx, f.ID, chosen.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  Error resolving: %v\n", err)
		} else {
			fmt.Printf("  Matched to: %s\n", chosen.Title)
		}
	}
}

func numericToFloat(n pgtype.Numeric) float64 {
	f, _ := n.Float64Value()
	return f.Float64
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
	cmd.Flags().String("metadata-providers", "", "Comma-separated metadata providers (e.g. tmdb,tvdb,anidb)")
	cmd.Flags().String("artwork-providers", "", "Comma-separated artwork providers (e.g. tmdb,fanart.tv)")
	cmd.Flags().String("ratings-providers", "", "Comma-separated ratings providers (e.g. omdb)")
	cmd.Flags().Bool("watch", false, "Enable filesystem watching")
	cmd.Flags().Bool("auto-collections", false, "Automatically add to collections (movies)")
	cmd.Flags().Int("metadata-refresh", 0, "Auto-refresh metadata every N days (0=never)")
	cmd.Flags().Bool("save-nfo", false, "Write NFO files to media directory")
	cmd.Flags().Bool("save-images", false, "Write images to media directory")
}
