package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Inspect background jobs",
	Long:  "List and inspect River background job status.",
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("job list: not yet implemented (Phase 7)")
		return nil
	},
}

var jobStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show job status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("job status: not yet implemented (Phase 7)")
		return nil
	},
}

func init() {
	jobListCmd.Flags().String("status", "", "Filter by status (pending, running, completed, failed)")

	jobStatusCmd.Flags().Int64("id", 0, "Job ID")

	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobStatusCmd)
}
