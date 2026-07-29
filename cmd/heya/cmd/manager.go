package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

// heya manager — the acquisition subsystem's CLI surface (CLI-first rule:
// everything the /manager UI can do, this can do).

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

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
			fmt.Println(table.Render())
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

var managerIndexerStatsCmd = &cobra.Command{
	Use:   "stats <id>",
	Short: "Show live per-indexer stats for a Prowlarr connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			stats, err := app.ManagerIndexerStats(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(stats)
			}
			table := ui.NewTable("Indexer", "Queries", "RSS", "Grabs", "Failed", "Avg resp", "Health")
			for _, stat := range stats {
				health := "ok"
				if stat.DisabledTill != "" {
					health = "backoff until " + stat.DisabledTill
				}
				table.AddRow(
					stat.Name,
					strconv.Itoa(stat.Queries),
					strconv.Itoa(stat.RssQueries),
					strconv.Itoa(stat.Grabs),
					strconv.Itoa(stat.FailedQueries+stat.FailedRss+stat.FailedGrabs),
					fmt.Sprintf("%d ms", stat.AvgResponseMs),
					health,
				)
			}
			fmt.Println(table.Render())
			return nil
		})
	},
}

var managerIndexerHistoryCmd = &cobra.Command{
	Use:   "history <id>",
	Short: "Show daily query/grab activity for a Prowlarr connection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			history, err := app.ManagerIndexerHistory(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(history)
			}
			table := ui.NewTable("Date", "Queries", "Failed", "Grabs")
			for _, day := range history.Days {
				table.AddRow(day.Date, strconv.Itoa(day.Queries), strconv.Itoa(day.Failed), strconv.Itoa(day.Grabs))
			}
			fmt.Println(table.Render())
			for source, count := range history.BySource {
				ui.Info(source, strconv.Itoa(count)+" queries")
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
			fmt.Println(table.Render())
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

var managerClientActivityCmd = &cobra.Command{
	Use:   "activity <id>",
	Short: "Show a download client's live queue and transfer totals",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			activity, err := app.ManagerDownloadClientActivity(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(activity)
			}
			state := "downloading"
			if activity.Paused {
				state = "paused"
			} else if len(activity.Queue) == 0 {
				state = "idle"
			}
			ui.Info("State", state)
			ui.Info("Speed", fmt.Sprintf("%.1f MB/s", activity.SpeedKBps/1024))
			ui.Info("Disk free", fmt.Sprintf("%.1f GB", activity.DiskFreeGB))
			ui.Info("Downloaded", fmt.Sprintf("today %s · week %s · month %s · total %s",
				humanBytes(activity.DownloadedDay), humanBytes(activity.DownloadedWeek),
				humanBytes(activity.DownloadedMonth), humanBytes(activity.DownloadedTotal)))
			if len(activity.Queue) > 0 {
				table := ui.NewTable("Name", "Category", "Status", "Progress", "ETA")
				for _, item := range activity.Queue {
					table.AddRow(item.Name, item.Category, item.Status,
						fmt.Sprintf("%d%%", item.Percentage), item.TimeLeft)
				}
				fmt.Println(table.Render())
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
			table := ui.NewTable("ID", "Name", "Domain", "Cutoff", "Language", "Qualities", "In use")
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
					view.Language,
					fmt.Sprintf("%d allowed / %d", allowed, len(view.Items)),
					strconv.FormatInt(view.InUseCount, 10),
				)
			}
			fmt.Println(table.Render())
			return nil
		})
	},
}

var managerProfileRemoveCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove a quality profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteManagerQualityProfile(ctx, id); err != nil {
				return err
			}
			ui.Success("Removed quality profile %d", id)
			return nil
		})
	},
}

// ── Custom formats ───────────────────────────────────────────────────────

var managerFormatCmd = &cobra.Command{
	Use:   "format",
	Short: "Manage custom formats",
}

var managerFormatListCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom formats",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			views, err := app.ListManagerCustomFormats(ctx)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(views)
			}
			table := ui.NewTable("ID", "Name", "Domain", "Conditions", "Source", "TRaSH")
			for _, view := range views {
				trash := ""
				if view.TrashID != "" {
					trash = "yes"
				}
				table.AddRow(
					strconv.FormatInt(view.ID, 10),
					view.Name,
					view.Domain,
					strconv.Itoa(len(view.Specifications)),
					view.Source,
					trash,
				)
			}
			fmt.Println(table.Render())
			return nil
		})
	},
}

var managerFormatImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import custom formats from a running arr instance or a JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, _ := cmd.Flags().GetString("from")
		baseURL, _ := cmd.Flags().GetString("url")
		apiKey, _ := cmd.Flags().GetString("api-key")
		file, _ := cmd.Flags().GetString("file")
		withProfiles, _ := cmd.Flags().GetBool("profiles")

		input := service.ManagerFormatImportInput{
			Kind:            kind,
			BaseURL:         baseURL,
			APIKey:          apiKey,
			IncludeProfiles: withProfiles,
		}
		if file != "" {
			raw, err := os.ReadFile(file) //nolint:gosec // G304: user-supplied CLI path is the feature
			if err != nil {
				return err
			}
			input.JSON = string(raw)
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.ImportManagerCustomFormats(ctx, input)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(result)
			}
			ui.Success("Formats: %d created, %d updated, %d skipped", result.Created, result.Updated, len(result.Skipped))
			for _, name := range result.Skipped {
				ui.Warn("skipped %q (name taken by another source)", name)
			}
			for _, name := range result.ProfilesCreated {
				ui.Success("Profile imported: %s", name)
			}
			for _, name := range result.ProfilesUpdated {
				ui.Success("Profile re-synced: %s", name)
			}
			for _, name := range result.ProfilesSkipped {
				ui.Warn("profile skipped %q (name taken)", name)
			}
			for _, warning := range result.Warnings {
				ui.Warn("%s", warning)
			}
			return nil
		})
	},
}

var managerFormatTestCmd = &cobra.Command{
	Use:   "test <release title>",
	Short: "Parse a release title and show matching formats and profile scores",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tv, _ := cmd.Flags().GetBool("tv")
		sizeGB, _ := cmd.Flags().GetFloat64("size-gb")
		input := service.ManagerReleaseTestInput{
			Title:     args[0],
			TV:        tv,
			SizeBytes: int64(sizeGB * (1 << 30)),
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.TestManagerRelease(ctx, input)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(view)
			}
			ui.Info("Parsed", fmt.Sprintf("%dp %s modifier=%s group=%s type=%s",
				view.Parsed.Resolution, strings.Join(view.Parsed.Sources, "+"),
				view.Parsed.Modifier, view.Parsed.Group, view.Parsed.ReleaseType))
			if len(view.Matches) == 0 {
				ui.Info("Matches", "none")
			} else {
				names := make([]string, 0, len(view.Matches))
				for _, match := range view.Matches {
					names = append(names, match.Name)
				}
				ui.Success("Matches: %s", strings.Join(names, ", "))
			}
			table := ui.NewTable("Profile", "Score", "Min met", "Language")
			for _, profile := range view.Profiles {
				met := "yes"
				if !profile.MinMet {
					met = "NO"
				}
				lang := "ok"
				if !profile.LanguageOK {
					lang = fmt.Sprintf("REJECTED (wants %s)", profile.Language)
				}
				table.AddRow(profile.Name, strconv.FormatInt(int64(profile.Score), 10), met, lang)
			}
			fmt.Println(table.Render())
			return nil
		})
	},
}

var managerFormatRemoveCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Remove a custom format",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			if err := app.DeleteManagerCustomFormat(ctx, id); err != nil {
				return err
			}
			ui.Success("Removed custom format %d", id)
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

	managerFormatImportCmd.Flags().String("from", "", "radarr | sonarr | lidarr (source app; decides enum mapping and domain)")
	managerFormatImportCmd.Flags().String("url", "", "Base URL of the running arr instance")
	managerFormatImportCmd.Flags().String("api-key", "", "arr API key")
	managerFormatImportCmd.Flags().String("file", "", "Import from a JSON file (arr export or TRaSH guide) instead of a live instance")
	managerFormatImportCmd.Flags().Bool("profiles", false, "Also import quality profiles with their format scores (live instance only)")
	managerFormatTestCmd.Flags().Bool("tv", false, "Parse as a TV release")
	managerFormatTestCmd.Flags().Float64("size-gb", 0, "Release size in GB for size conditions")

	managerIndexerCmd.AddCommand(managerIndexerListCmd, managerIndexerAddCmd, managerIndexerTestCmd, managerIndexerStatsCmd, managerIndexerHistoryCmd, managerIndexerRemoveCmd)
	managerClientCmd.AddCommand(managerClientListCmd, managerClientAddCmd, managerClientTestCmd, managerClientActivityCmd, managerClientRemoveCmd)
	managerProfileCmd.AddCommand(managerProfileListCmd, managerProfileRemoveCmd)
	managerFormatCmd.AddCommand(managerFormatListCmd, managerFormatImportCmd, managerFormatTestCmd, managerFormatRemoveCmd)
	managerCmd.AddCommand(managerIndexerCmd, managerClientCmd, managerProfileCmd, managerFormatCmd)
	rootCmd.AddCommand(managerCmd)
}
