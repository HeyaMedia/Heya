package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
	Long:  "Create, list, and delete Heya users.",
}

var userCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new user",
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")
		isAdmin, _ := cmd.Flags().GetBool("admin")

		if username == "" || password == "" {
			return fmt.Errorf("--username and --password are required")
		}
		if email == "" {
			email = username + "@localhost"
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			user, err := app.CreateUser(ctx, username, email, password, isAdmin)
			if err != nil {
				return err
			}

			role := "user"
			if user.IsAdmin {
				role = "admin"
			}
			ui.Success("Created %s: %s (id=%d)", role, user.Username, user.ID)
			return nil
		})
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			users, err := app.ListUsers(ctx)
			if err != nil {
				return err
			}

			if ui.JSONMode {
				return ui.OutputJSON(users)
			}

			if len(users) == 0 {
				ui.Warn("No users found.")
				return nil
			}

			t := ui.NewTable("ID", "USERNAME", "EMAIL", "ROLE")
			for _, u := range users {
				role := "user"
				if u.IsAdmin {
					role = ui.Bold("admin")
				}
				t.AddRow(strconv.FormatInt(u.ID, 10), u.Username, u.Email, role)
			}
			fmt.Println(t.Render())
			return nil
		})
	},
}

var userDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a user",
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		if username == "" {
			return fmt.Errorf("--username is required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteUser(ctx, username); err != nil {
				return err
			}

			ui.Success("Deleted user: %s", username)
			return nil
		})
	},
}

var userResetPasswordCmd = &cobra.Command{
	Use:   "reset-password",
	Short: "Reset a user's password",
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")

		if username == "" || password == "" {
			return fmt.Errorf("--username and --password are required")
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.ResetPassword(ctx, username, password); err != nil {
				return err
			}

			ui.Success("Password reset for user: %s", username)
			return nil
		})
	},
}

func init() {
	userCreateCmd.Flags().String("username", "", "Username for the new user")
	userCreateCmd.Flags().String("email", "", "Email for the new user")
	userCreateCmd.Flags().String("password", "", "Password for the new user")
	userCreateCmd.Flags().Bool("admin", false, "Grant admin privileges")

	userDeleteCmd.Flags().String("username", "", "Username to delete")

	userResetPasswordCmd.Flags().String("username", "", "Username to reset")
	userResetPasswordCmd.Flags().String("password", "", "New password")

	userCmd.AddCommand(userCreateCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userDeleteCmd)
	userCmd.AddCommand(userResetPasswordCmd)
}
