package cmd

import (
	"fmt"

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
		fmt.Println("library add: not yet implemented (Phase 5)")
		return nil
	},
}

var libraryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all libraries",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library list: not yet implemented (Phase 5)")
		return nil
	},
}

var libraryScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan a library for media files",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library scan: not yet implemented (Phase 6)")
		return nil
	},
}

var libraryRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a library",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library remove: not yet implemented (Phase 5)")
		return nil
	},
}

var libraryFilesCmd = &cobra.Command{
	Use:   "files",
	Short: "List files in a library",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library files: not yet implemented (Phase 6)")
		return nil
	},
}

var libraryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show library statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("library stats: not yet implemented (Phase 6)")
		return nil
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

func init() {
	libraryAddCmd.Flags().String("name", "", "Library name")
	libraryAddCmd.Flags().String("type", "", "Media type (movie, tv, music, book)")
	libraryAddCmd.Flags().StringSlice("path", nil, "Filesystem paths to watch")

	libraryScanCmd.Flags().Int64("id", 0, "Library ID to scan")
	libraryScanCmd.Flags().String("name", "", "Library name to scan")
	libraryScanCmd.Flags().Bool("all", false, "Scan all libraries")

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
