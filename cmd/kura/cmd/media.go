package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Browse and manage media items",
	Long:  "List, search, and manage matched media items.",
}

var mediaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List media items",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("media list: not yet implemented (Phase 8)")
		return nil
	},
}

var mediaInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show media item details",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("media info: not yet implemented (Phase 8)")
		return nil
	},
}

var mediaSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search media items",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("media search %q: not yet implemented (Phase 8)\n", args[0])
		return nil
	},
}

var mediaMatchCmd = &cobra.Command{
	Use:   "match",
	Short: "Trigger metadata matching for unmatched files",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("media match: not yet implemented (Phase 8)")
		return nil
	},
}

var mediaRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Re-fetch metadata for a media item",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("media refresh: not yet implemented (Phase 8)")
		return nil
	},
}

func init() {
	mediaListCmd.Flags().String("type", "", "Filter by type (movie, tv, music, book)")

	mediaInfoCmd.Flags().Int64("id", 0, "Media item ID")

	mediaMatchCmd.Flags().Int64("library-id", 0, "Library ID to match")

	mediaRefreshCmd.Flags().Int64("id", 0, "Media item ID to refresh")

	mediaCmd.AddCommand(mediaListCmd)
	mediaCmd.AddCommand(mediaInfoCmd)
	mediaCmd.AddCommand(mediaSearchCmd)
	mediaCmd.AddCommand(mediaMatchCmd)
	mediaCmd.AddCommand(mediaRefreshCmd)
}
