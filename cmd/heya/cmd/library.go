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
	"github.com/karbowiak/heya/internal/scanner"
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

		lib, err := app.CreateLibrary(ctx, name, mt, paths, users[0].ID)
		if err != nil {
			return err
		}

		ui.Success("Created library: %s (id=%d)", lib.Name, lib.ID)
		ui.Info("Type", ui.MediaBadge(string(lib.MediaType)))
		ui.Info("Paths", strings.Join(lib.Paths, ", "))
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
		scanOnly, _ := cmd.Flags().GetBool("scan-only")
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

		opts := scanner.ScanOptions{ForceRescan: force}

		for _, lib := range libs {
			ui.Header(fmt.Sprintf("Scanning %s", lib.Name))
			ui.Info("Library", fmt.Sprintf("%s (id=%d)", lib.Name, lib.ID))
			ui.Info("Type", ui.MediaBadge(string(lib.MediaType)))

			scanResult, err := app.ScanLibrary(ctx, lib.ID, opts)
			if err != nil {
				ui.Error("scan failed: %v", err)
				continue
			}
			ui.Success("Scan complete")
			ui.Info("Discovered", strconv.Itoa(scanResult.Discovered))
			ui.Info("New", strconv.Itoa(scanResult.New))
			ui.Info("Unchanged", strconv.Itoa(scanResult.Unchanged))
			ui.Info("Deleted", strconv.Itoa(scanResult.Deleted))

			if scanOnly {
				continue
			}

			count, err := app.EnqueuePendingFiles(ctx, lib.ID)
			if err != nil {
				ui.Error("enqueue failed: %v", err)
				continue
			}
			ui.Success("Enqueued %d files for processing", count)
		}

		if !scanOnly {
			fmt.Println()
			ui.Println(ui.Dim("Run 'heya queue process' to process now, or jobs will be picked up by 'heya serve' / 'heya dev'."))
		}

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

	libraryScanCmd.Flags().Int64("id", 0, "Library ID to scan")
	libraryScanCmd.Flags().String("name", "", "Library name to scan")
	libraryScanCmd.Flags().Bool("all", false, "Scan all libraries")
	libraryScanCmd.Flags().Bool("scan-only", false, "Only discover files, don't enqueue processing")
	libraryScanCmd.Flags().Bool("force", false, "Force re-scan all files")

	libraryRemoveCmd.Flags().Int64("id", 0, "Library ID to remove")

	libraryFilesCmd.Flags().Int64("id", 0, "Library ID")
	libraryFilesCmd.Flags().String("media", "", "Filter by media type")
	libraryFilesCmd.Flags().String("status", "", "Filter by status")

	libraryStatsCmd.Flags().Int64("id", 0, "Library ID")

	libraryWatchCmd.Flags().Int64("id", 0, "Library ID")

	libraryCmd.AddCommand(libraryAddCmd)
	libraryCmd.AddCommand(libraryListCmd)
	libraryCmd.AddCommand(libraryScanCmd)
	libraryCmd.AddCommand(libraryRemoveCmd)
	libraryCmd.AddCommand(libraryFilesCmd)
	libraryCmd.AddCommand(libraryStatsCmd)
	libraryCmd.AddCommand(libraryWatchCmd)
}
