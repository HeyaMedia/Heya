package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
	Long:  "Create, list, and delete Kura users.",
}

var userCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new user",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("user create: not yet implemented (Phase 5)")
		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("user list: not yet implemented (Phase 5)")
		return nil
	},
}

var userDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a user",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("user delete: not yet implemented (Phase 5)")
		return nil
	},
}

func init() {
	userCreateCmd.Flags().String("username", "", "Username for the new user")
	userCreateCmd.Flags().String("password", "", "Password for the new user")
	userCreateCmd.Flags().Bool("admin", false, "Grant admin privileges")

	userDeleteCmd.Flags().String("username", "", "Username to delete")

	userCmd.AddCommand(userCreateCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userDeleteCmd)
}
