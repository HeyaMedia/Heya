package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"
	"os"

	"github.com/karbowiak/kura/internal/service"
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		user, err := app.CreateUser(ctx, username, email, password, isAdmin)
		if err != nil {
			return err
		}

		role := "user"
		if user.IsAdmin {
			role = "admin"
		}
		fmt.Printf("Created %s: %s (id=%d)\n", role, user.Username, user.ID)
		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		users, err := app.ListUsers(ctx)
		if err != nil {
			return err
		}

		if len(users) == 0 {
			fmt.Println("No users found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tUSERNAME\tEMAIL\tADMIN\n")
		for _, u := range users {
			fmt.Fprintf(w, "%d\t%s\t%s\t%v\n", u.ID, u.Username, u.Email, u.IsAdmin)
		}
		return w.Flush()
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

		ctx := context.Background()
		app, err := service.New(ctx, cfg)
		if err != nil {
			return err
		}
		defer app.Close()

		if err := app.DeleteUser(ctx, username); err != nil {
			return err
		}

		fmt.Printf("Deleted user: %s\n", username)
		return nil
	},
}

func init() {
	userCreateCmd.Flags().String("username", "", "Username for the new user")
	userCreateCmd.Flags().String("email", "", "Email for the new user")
	userCreateCmd.Flags().String("password", "", "Password for the new user")
	userCreateCmd.Flags().Bool("admin", false, "Grant admin privileges")

	userDeleteCmd.Flags().String("username", "", "Username to delete")

	userCmd.AddCommand(userCreateCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userDeleteCmd)
}
