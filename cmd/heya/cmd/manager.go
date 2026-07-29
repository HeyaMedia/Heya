package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

// heya manager — the acquisition subsystem's CLI surface (CLI-first rule:
// everything the /manager UI can do, this can do).

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Manage the acquisition pipeline (indexers, download clients, quality profiles)",
}

// ── Indexers ─────────────────────────────────────────────────────────────

var managerIndexerCmd = &cobra.Command{
	Use:   "indexer",
	Short: "Manage release indexers",
}

var managerIndexerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List indexers",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			views, err := app.ListManagerIndexers(ctx)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(views)
			}
			table := ui.NewTable("ID", "Name", "Kind", "Protocol", "Enabled", "Priority", "Last test")
			var addRow func(view service.ManagerIndexerView, indent string)
			addRow = func(view service.ManagerIndexerView, indent string) {
				test := "never"
				if view.LastTestAt != nil {
					if view.LastTestOK {
						test = "ok"
					} else {
						test = "failed: " + view.LastTestError
					}
				}
				table.AddRow(
					strconv.FormatInt(view.ID, 10),
					indent+view.Name,
					view.Kind,
					view.Protocol,
					strconv.FormatBool(view.Enabled),
					strconv.Itoa(int(view.Priority)),
					test,
				)
				for _, child := range view.Children {
					addRow(child, indent+"  ")
				}
			}
			for _, view := range views {
				addRow(view, "")
			}
			table.Render()
			return nil
		})
	},
}

var managerIndexerAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an indexer (prowlarr app connection, or a torznab/newznab endpoint)",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		kind, _ := cmd.Flags().GetString("kind")
		baseURL, _ := cmd.Flags().GetString("url")
		apiKey, _ := cmd.Flags().GetString("api-key")
		protocol, _ := cmd.Flags().GetString("protocol")
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.CreateManagerIndexer(ctx, service.ManagerIndexerInput{
				Name:     name,
				Kind:     kind,
				BaseURL:  baseURL,
				APIKey:   apiKey,
				Protocol: protocol,
			})
			if err != nil {
				return err
			}
			ui.Success("Added indexer %s (id=%d)", view.Name, view.ID)
			result, err := app.TestManagerIndexer(ctx, view.ID)
			if err != nil {
				return err
			}
			if result.OK {
				ui.Success("Connection test: %s", result.Detail)
			} else {
				ui.Warn("Connection test failed: %s", result.Error)
			}
			return nil
		})
	},
}

var managerIndexerTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Test an indexer connection (and sync Prowlarr children)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.TestManagerIndexer(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(result)
			}
			if result.OK {
				ui.Success("%s", result.Detail)
			} else {
				ui.Error("%s", result.Error)
			}
			return nil
		})
	},
}

var managerIndexerRemoveCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove an indexer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteManagerIndexer(ctx, id); err != nil {
				return err
			}
			ui.Success("Removed indexer %d", id)
			return nil
		})
	},
}

// ── Download clients ─────────────────────────────────────────────────────

var managerClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Manage download clients",
}

var managerClientListCmd = &cobra.Command{
	Use:   "list",
	Short: "List download clients",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			views, err := app.ListManagerDownloadClients(ctx)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(views)
			}
			table := ui.NewTable("ID", "Name", "Kind", "URL", "Category", "Enabled", "Last test")
			for _, view := range views {
				test := "never"
				if view.LastTestAt != nil {
					if view.LastTestOK {
						test = "ok"
					} else {
						test = "failed: " + view.LastTestError
					}
				}
				table.AddRow(
					strconv.FormatInt(view.ID, 10),
					view.Name,
					view.Kind,
					view.BaseURL,
					view.Category,
					strconv.FormatBool(view.Enabled),
					test,
				)
			}
			table.Render()
			return nil
		})
	},
}

var managerClientAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a download client (sabnzbd)",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		kind, _ := cmd.Flags().GetString("kind")
		baseURL, _ := cmd.Flags().GetString("url")
		apiKey, _ := cmd.Flags().GetString("api-key")
		category, _ := cmd.Flags().GetString("category")
		mapRemote, _ := cmd.Flags().GetString("map-remote")
		mapLocal, _ := cmd.Flags().GetString("map-local")
		input := service.ManagerDownloadClientInput{
			Name:     name,
			Kind:     kind,
			BaseURL:  baseURL,
			APIKey:   apiKey,
			Category: category,
		}
		if mapRemote != "" || mapLocal != "" {
			input.PathMappings = []service.ManagerPathMapping{{Remote: mapRemote, Local: mapLocal}}
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.CreateManagerDownloadClient(ctx, input)
			if err != nil {
				return err
			}
			ui.Success("Added download client %s (id=%d)", view.Name, view.ID)
			result, err := app.TestManagerDownloadClient(ctx, view.ID)
			if err != nil {
				return err
			}
			if result.OK {
				ui.Success("Connection test: %s", result.Detail)
			} else {
				ui.Warn("Connection test failed: %s", result.Error)
			}
			return nil
		})
	},
}

var managerClientTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Test a download client connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.TestManagerDownloadClient(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(result)
			}
			if result.OK {
				ui.Success("%s", result.Detail)
			} else {
				ui.Error("%s", result.Error)
			}
			return nil
		})
	},
}

var managerClientRemoveCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove a download client",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteManagerDownloadClient(ctx, id); err != nil {
				return err
			}
			ui.Success("Removed download client %d", id)
			return nil
		})
	},
}

// ── Quality profiles ─────────────────────────────────────────────────────

var managerProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage quality profiles",
}

var managerProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quality profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			views, err := app.ListManagerQualityProfiles(ctx)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(views)
			}
			table := ui.NewTable("ID", "Name", "Domain", "Cutoff", "Qualities", "In use")
			for _, view := range views {
				allowed := 0
				for _, item := range view.Items {
					if item.Allowed {
						allowed++
					}
				}
				table.AddRow(
					strconv.FormatInt(view.ID, 10),
					view.Name,
					view.Domain,
					view.Cutoff,
					fmt.Sprintf("%d allowed / %d", allowed, len(view.Items)),
					strconv.FormatInt(view.InUseCount, 10),
				)
			}
			table.Render()
			return nil
		})
	},
}

func init() {
	managerIndexerAddCmd.Flags().String("name", "", "Display name")
	managerIndexerAddCmd.Flags().String("kind", "prowlarr", "prowlarr | torznab | newznab")
	managerIndexerAddCmd.Flags().String("url", "", "Base URL (prowlarr app URL, or the torznab api endpoint)")
	managerIndexerAddCmd.Flags().String("api-key", "", "API key")
	managerIndexerAddCmd.Flags().String("protocol", "", "usenet | torrent (torznab/newznab only)")

	managerClientAddCmd.Flags().String("name", "", "Display name")
	managerClientAddCmd.Flags().String("kind", "sabnzbd", "sabnzbd")
	managerClientAddCmd.Flags().String("url", "", "Base URL")
	managerClientAddCmd.Flags().String("api-key", "", "API key")
	managerClientAddCmd.Flags().String("category", "heya", "Download category")
	managerClientAddCmd.Flags().String("map-remote", "", "Path prefix as the client reports it (e.g. /storage)")
	managerClientAddCmd.Flags().String("map-local", "", "Same prefix as this process sees it (e.g. /Volumes/Storage)")

	managerIndexerCmd.AddCommand(managerIndexerListCmd, managerIndexerAddCmd, managerIndexerTestCmd, managerIndexerRemoveCmd)
	managerClientCmd.AddCommand(managerClientListCmd, managerClientAddCmd, managerClientTestCmd, managerClientRemoveCmd)
	managerProfileCmd.AddCommand(managerProfileListCmd)
	managerCmd.AddCommand(managerIndexerCmd, managerClientCmd, managerProfileCmd)
	rootCmd.AddCommand(managerCmd)
}
