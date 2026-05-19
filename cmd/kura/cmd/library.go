package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/scanner"
	"github.com/karbowiak/kura/internal/service"
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

		fmt.Printf("Created library: %s (id=%d, type=%s, paths=%s)\n",
			lib.Name, lib.ID, lib.MediaType, strings.Join(lib.Paths, ", "))
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

		if len(libs) == 0 {
			fmt.Println("No libraries found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tTYPE\tPATHS\n")
		for _, lib := range libs {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
				lib.ID, lib.Name, lib.MediaType, strings.Join(lib.Paths, ", "))
		}
		return w.Flush()
	},
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan a library for media files",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetInt64("id")
		all, _ := cmd.Flags().GetBool("all")
		scanOnly, _ := cmd.Flags().GetBool("scan-only")
		interactive, _ := cmd.Flags().GetBool("interactive")
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
			fmt.Printf("Scanning %s (id=%d, type=%s)...\n", lib.Name, lib.ID, lib.MediaType)

			scanResult, err := app.ScanLibrary(ctx, lib.ID, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				continue
			}
			fmt.Printf("  Discovered: %d, New: %d, Updated: %d, Unchanged: %d, Deleted: %d, Errors: %d\n",
				scanResult.Discovered, scanResult.New, scanResult.Updated,
				scanResult.Unchanged, scanResult.Deleted, scanResult.Errors)

			if scanOnly {
				continue
			}

			fmt.Printf("Matching %s...\n", lib.Name)
			matchResult, err := app.MatchLibrary(ctx, lib.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  match error: %v\n", err)
				continue
			}
			fmt.Printf("  Matched: %d, Unmatched: %d, Errors: %d\n",
				matchResult.Matched, matchResult.Unmatched, matchResult.Errors)

			if interactive && matchResult.Unmatched > 0 {
				runInteractiveResolve(ctx, app, lib.ID)
			}
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

		fmt.Printf("Deleted library: id=%d\n", id)
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

		if len(files) == 0 {
			fmt.Println("No files found. Run `kura library scan` first.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tSTATUS\tPATH\n")
		for _, f := range files {
			fmt.Fprintf(w, "%d\t%s\t%s\n", f.ID, f.Status, filepath.Base(f.Path))
		}
		return w.Flush()
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

		if len(stats) == 0 {
			fmt.Println("No files in library. Run `kura library scan` first.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "STATUS\tCOUNT\n")
		for _, s := range stats {
			fmt.Fprintf(w, "%s\t%d\n", s.Status, s.Count)
		}
		return w.Flush()
	},
}

var libraryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Show watcher status for a library",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library watch: not yet implemented (Phase 9)")
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
	libraryScanCmd.Flags().Bool("scan-only", false, "Only scan, don't match")
	libraryScanCmd.Flags().BoolP("interactive", "i", false, "Interactively resolve unmatched files")
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
