package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var parseCmd = &cobra.Command{
	Use:   "parse [name]",
	Short: "Parse a media filename or path",
	Long:  "Parse a release name or filesystem path and display the extracted metadata.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")

		if len(args) == 0 && path == "" {
			return fmt.Errorf("provide a release name as an argument or use --path")
		}

		if path != "" {
			fmt.Printf("parse --path %s: not yet implemented (Phase 3)\n", path)
		} else {
			fmt.Printf("parse %q: not yet implemented (Phase 3)\n", args[0])
		}
		return nil
	},
}

func init() {
	parseCmd.Flags().String("path", "", "Parse all files in a directory")
}
