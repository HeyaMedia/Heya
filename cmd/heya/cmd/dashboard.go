package cmd

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/karbowiak/heya/internal/dashboard"
	"github.com/karbowiak/heya/internal/localtls"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Live server dashboard (TUI)",
	Long:  "Full-screen interactive dashboard showing server status, libraries, queue, and watchers.",
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL, _ := cmd.Flags().GetString("server")
		token, _ := cmd.Flags().GetString("token")
		var client *dashboard.Client

		if token == "" {
			if !ui.IsInteractive() {
				return fmt.Errorf("--token is required in non-interactive mode")
			}

			var username, password string
			username = "admin"

			err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Server URL").
						Value(&serverURL),
					huh.NewInput().
						Title("Username").
						Value(&username),
					huh.NewInput().
						Title("Password").
						EchoMode(huh.EchoModePassword).
						Value(&password),
				),
			).Run()
			if err != nil {
				return err
			}

			client = dashboard.NewClientWithHTTP(serverURL, "", localtls.Client(cfg.DataDir.Value, 5*time.Second))
			if err := client.Login(context.Background(), username, password); err != nil {
				ui.Error("Login failed: %v", err)
				return err
			}
		} else {
			client = dashboard.NewClientWithHTTP(serverURL, token, localtls.Client(cfg.DataDir.Value, 5*time.Second))
		}

		m := dashboard.NewWithClient(client)
		p := tea.NewProgram(m)
		_, err := p.Run()
		return err
	},
}

func init() {
	dashboardCmd.Flags().String("server", "https://localhost:8080", "Heya server URL")
	dashboardCmd.Flags().String("token", "", "Auth token (skip login prompt)")

	rootCmd.AddCommand(dashboardCmd)
}
