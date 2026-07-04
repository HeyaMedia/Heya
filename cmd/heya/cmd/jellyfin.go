package cmd

import (
	"fmt"

	"github.com/karbowiak/heya/internal/jellyfin"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var jellyfinCmd = &cobra.Command{
	Use:   "jellyfin",
	Short: "Jellyfin-compatible API tools",
}

var jellyfinCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Show spec coverage of the Jellyfin-compatible API",
	Long: "Triage state of every operation in the vendored Jellyfin OpenAPI spec: " +
		"implemented (real behavior), stubbed (correct 'feature off' answers), " +
		"planned (future work), out_of_scope (no Heya equivalent).",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries := jellyfin.CoverageReport()

		if ui.JSONMode {
			return ui.OutputJSON(entries)
		}

		counts := map[jellyfin.CoverageStatus]int{}
		for _, e := range entries {
			counts[e.Status]++
		}
		ui.Header("Jellyfin API coverage")
		fmt.Printf("\nimplemented %d · stubbed %d · planned %d · out of scope %d · total %d\n\n",
			counts[jellyfin.CoverageImplemented], counts[jellyfin.CoverageStubbed],
			counts[jellyfin.CoveragePlanned], counts[jellyfin.CoverageOutOfScope], len(entries))

		verbose, _ := cmd.Flags().GetBool("all")
		t := ui.NewTable("STATUS", "TAG", "OPERATION")
		for _, e := range entries {
			if !verbose && e.Status != jellyfin.CoverageImplemented && e.Status != jellyfin.CoverageStubbed {
				continue
			}
			t.AddRow(string(e.Status), e.Tag, e.Operation)
		}
		fmt.Println(t.Render())
		if !verbose {
			fmt.Println(ui.Dim("(--all to include planned / out-of-scope operations)"))
		}
		return nil
	},
}

func init() {
	jellyfinCoverageCmd.Flags().Bool("all", false, "include planned and out-of-scope operations")
	jellyfinCmd.AddCommand(jellyfinCoverageCmd)
	rootCmd.AddCommand(jellyfinCmd)
}
