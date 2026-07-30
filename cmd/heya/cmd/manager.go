package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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

var managerProfileAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a quality profile from the domain's canonical ladder",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		domain, _ := cmd.Flags().GetString("domain")
		cutoff, _ := cmd.Flags().GetString("cutoff")
		language, _ := cmd.Flags().GetString("language")
		return withApp(func(ctx context.Context, app *service.App) error {
			items := app.ManagerQualityLadders()[domain]
			if len(items) == 0 {
				return fmt.Errorf("domain must be movie, tv, music, or book")
			}
			if cutoff == "" {
				for _, item := range items {
					if item.Allowed {
						cutoff = item.Quality
						break
					}
				}
			}
			view, err := app.CreateManagerQualityProfile(ctx, service.ManagerQualityProfileInput{
				Name:     name,
				Domain:   domain,
				Items:    items,
				Cutoff:   cutoff,
				Language: language,
			})
			if err != nil {
				return err
			}
			ui.Success("Created quality profile %q (id %d) — tune it in the UI or via the API", view.Name, view.ID)
			return nil
		})
	},
}

var managerProfileCloneCmd = &cobra.Command{
	Use:   "clone <id>",
	Short: "Duplicate a quality profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.CloneManagerQualityProfile(ctx, id)
			if err != nil {
				return err
			}
			ui.Success("Cloned into %q (id %d)", view.Name, view.ID)
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

// ── Library lens ─────────────────────────────────────────────────────────

var managerLibraryCmd = &cobra.Command{
	Use:   "library",
	Short: "Managed view over a library: completeness, monitoring, profiles",
}

var managerLibraryItemsCmd = &cobra.Command{
	Use:   "items <library id>",
	Short: "List a library's items with completeness and monitoring state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		libraryID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid library id %q", args[0])
		}
		search, _ := cmd.Flags().GetString("search")
		monitored, _ := cmd.Flags().GetString("monitored")
		fileState, _ := cmd.Flags().GetString("file-state")
		status, _ := cmd.Flags().GetString("status")
		profile, _ := cmd.Flags().GetString("profile")
		sort, _ := cmd.Flags().GetString("sort")
		dir, _ := cmd.Flags().GetString("dir")
		page, _ := cmd.Flags().GetInt("page")
		perPage, _ := cmd.Flags().GetInt("per-page")
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.ManagerLibraryItems(ctx, libraryID, service.ManagerLibraryItemsParams{
				Search:    search,
				Monitored: monitored,
				FileState: fileState,
				Status:    status,
				Profile:   profile,
				Sort:      sort,
				Dir:       dir,
				Page:      page,
				PerPage:   perPage,
			})
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(result)
			}
			ui.Info("Library", fmt.Sprintf("%s (%s) — %d items, %d monitored, %d with gaps, %s on disk",
				result.Library.Name, result.Library.MediaType,
				result.Stats.Items, result.Stats.Monitored, result.Stats.ItemsMissing,
				humanBytes(result.Stats.SizeOnDisk)))
			table := ui.NewTable("ID", "Title", "Year", "Status", "Mon", "Have", "Missing", "Size")
			for _, item := range result.Items {
				mon := ""
				if item.Monitored {
					mon = "✓"
				}
				table.AddRow(
					strconv.FormatInt(item.ID, 10),
					item.Title,
					item.Year,
					item.Status,
					mon,
					fmt.Sprintf("%d/%d", item.HaveCount, item.TotalCount),
					strconv.Itoa(int(item.MissingCount)),
					humanBytes(item.SizeOnDisk),
				)
			}
			fmt.Println(table.Render())
			ui.Info("Page", fmt.Sprintf("%d of %d matching items (page %d, %d per page)",
				len(result.Items), result.Total, result.Page, result.PerPage))
			return nil
		})
	},
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "—"
}

var managerLibraryShowCmd = &cobra.Command{
	Use:   "show <media item id>",
	Short: "Arr-style item detail: completeness, files, seasons, or albums",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid media item id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			detail, err := app.GetManagerMediaDetail(ctx, id)
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(detail)
			}
			item := detail.Item
			ui.Info("Title", fmt.Sprintf("%s (%s)", item.Title, item.Year))
			ui.Info("Library", fmt.Sprintf("%s (%s)", detail.Library.Name, detail.Library.MediaType))
			ui.Info("State", fmt.Sprintf("monitored=%v profile=%s status=%s", item.Monitored, orDash(detail.ProfileName), orDash(item.Status)))
			ui.Info("Disk", fmt.Sprintf("%s in %d file(s) at %s", humanBytes(item.SizeOnDisk), item.FileCount, orDash(detail.Path)))
			ui.Info("Have", fmt.Sprintf("%d/%d (%d missing)", item.HaveCount, item.TotalCount, item.MissingCount))
			switch {
			case len(detail.Seasons) > 0:
				table := ui.NewTable("Season", "Aired", "Have", "Size")
				for _, s := range detail.Seasons {
					table.AddRow(strconv.Itoa(int(s.Number)), strconv.Itoa(int(s.Aired)), strconv.Itoa(int(s.Have)), humanBytes(s.SizeOnDisk))
				}
				fmt.Println(table.Render())
			case len(detail.Albums) > 0:
				table := ui.NewTable("Album", "Type", "Released", "Tracks", "Size")
				for _, al := range detail.Albums {
					table.AddRow(al.Title, al.Type, firstNonEmpty(al.ReleaseDate, al.Year), fmt.Sprintf("%d/%d", al.TracksHave, al.TracksTotal), humanBytes(al.SizeBytes))
				}
				fmt.Println(table.Render())
			case len(detail.Files) > 0:
				table := ui.NewTable("File", "Kind", "Quality", "Codecs", "Size")
				for _, f := range detail.Files {
					table.AddRow(f.Path, f.Kind, f.Quality, strings.TrimSpace(f.VideoCodec+" "+f.AudioCodec), humanBytes(f.SizeBytes))
				}
				fmt.Println(table.Render())
			}
			return nil
		})
	},
}

var managerLibraryAlbumCmd = &cobra.Command{
	Use:   "album <album id | d<discography id>>",
	Short: "Album detail: tracklist with per-track file and quality state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			detail, err := app.GetManagerAlbumDetail(ctx, args[0])
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(detail)
			}
			al := detail.Album
			ui.Info("Album", fmt.Sprintf("%s — %s (%s)", detail.ArtistName, al.Title, firstNonEmpty(al.ReleaseDate, al.Year)))
			ui.Info("Type", al.Type)
			ui.Info("Have", fmt.Sprintf("%d/%d tracks · %s", al.TracksHave, al.TracksTotal, humanBytes(al.SizeBytes)))
			if len(detail.Tracks) > 0 {
				table := ui.NewTable("#", "Title", "Length", "Quality", "Size")
				for _, track := range detail.Tracks {
					state := track.Quality
					if !track.HasFile {
						state = "MISSING"
					}
					table.AddRow(
						fmt.Sprintf("%d-%02d", track.Disc, track.Number),
						track.Title,
						fmt.Sprintf("%d:%02d", track.Duration/60, track.Duration%60),
						state,
						humanBytes(track.SizeBytes),
					)
				}
				fmt.Println(table.Render())
			}
			if len(detail.Editions) > 0 {
				ui.Info("Editions", fmt.Sprintf("%d sibling edition(s)", len(detail.Editions)))
			}
			return nil
		})
	},
}

var managerLibraryRefreshCmd = &cobra.Command{
	Use:   "refresh <media item id>",
	Short: "Refresh an artist's metadata inline (incl. discography) — no worker needed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid media item id %q", args[0])
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.RefreshMusicArtistNow(ctx, id)
			if err != nil {
				return err
			}
			if result.Skipped {
				ui.Info("Result", "skipped (identity guard or no upstream record)")
				return nil
			}
			ui.Success("Refreshed: %d albums matched, %d updated", result.AlbumsMatched, result.AlbumsUpdated)
			return nil
		})
	},
}

var managerLibrarySetCmd = &cobra.Command{
	Use:   "set <media item id>...",
	Short: "Set monitored state / quality profile on media items",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ids := make([]int64, 0, len(args))
		for _, raw := range args {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid media item id %q", raw)
			}
			ids = append(ids, id)
		}
		input := service.ManagerMediaBulkInput{IDs: ids}
		if cmd.Flags().Changed("monitor") {
			monitor, _ := cmd.Flags().GetBool("monitor")
			input.Monitored = &monitor
		}
		if cmd.Flags().Changed("profile") {
			profileID, _ := cmd.Flags().GetInt64("profile")
			input.QualityProfileID = &profileID
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			result, err := app.UpdateManagerMedia(ctx, input)
			if err != nil {
				return err
			}
			ui.Success("Updated %d media item(s)", result.Updated)
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

// ── Calendar ─────────────────────────────────────────────────────────────

var managerCalendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "Release calendar: episodes airing, movies and albums releasing",
	RunE: func(cmd *cobra.Command, args []string) error {
		fromFlag, _ := cmd.Flags().GetString("from")
		toFlag, _ := cmd.Flags().GetString("to")
		libraries, _ := cmd.Flags().GetInt64Slice("library")
		monitored, _ := cmd.Flags().GetBool("monitored")

		now := time.Now()
		from := now.AddDate(0, 0, -7)
		to := now.AddDate(0, 0, 30)
		var err error
		if fromFlag != "" {
			if from, err = time.Parse("2006-01-02", fromFlag); err != nil {
				return fmt.Errorf("--from must be YYYY-MM-DD")
			}
		}
		if toFlag != "" {
			if to, err = time.Parse("2006-01-02", toFlag); err != nil {
				return fmt.Errorf("--to must be YYYY-MM-DD")
			}
		}

		return withApp(func(ctx context.Context, app *service.App) error {
			events, err := app.CalendarEvents(ctx, service.CalendarParams{
				From:       from,
				To:         to,
				LibraryIDs: libraries,
				Monitored:  monitored,
			})
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(events)
			}
			table := ui.NewTable("Date", "Kind", "Title", "Detail", "State")
			today := now.Format("2006-01-02")
			for _, ev := range events {
				detail := ""
				switch ev.Kind {
				case "episode":
					detail = fmt.Sprintf("S%02dE%02d · %s", ev.Season, ev.Episode, ev.EpisodeName)
				case "album":
					detail = fmt.Sprintf("%s (%s)", ev.AlbumTitle, ev.AlbumType)
				}
				state := "upcoming"
				switch {
				case ev.HasFile:
					state = "downloaded"
				case ev.Date == today:
					state = "today"
				case ev.Date < today:
					state = "missing"
				}
				table.AddRow(ev.Date, ev.Kind, ev.Title, detail, state)
			}
			fmt.Println(table.Render())
			ui.Info("Events", fmt.Sprintf("%d · %s → %s", len(events), from.Format("2006-01-02"), to.Format("2006-01-02")))
			return nil
		})
	},
}

var managerSearchCmd = &cobra.Command{
	Use:   "search <media-item-id>",
	Short: "Shadow-search a movie or TV season/episode: evaluate every candidate and show what would be grabbed and why (no download)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		itemID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("media item id must be numeric")
		}
		seasonFlag, _ := cmd.Flags().GetInt("season")
		episodeFlag, _ := cmd.Flags().GetInt64("episode-id")
		scope := service.ManagerSearchScope{}
		if cmd.Flags().Changed("season") {
			scope.Season = &seasonFlag
		}
		if cmd.Flags().Changed("episode-id") {
			scope.EpisodeID = &episodeFlag
		}
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.SearchManagerMedia(ctx, itemID, scope, "cli")
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(view)
			}
			ui.Info("Target", view.Target)
			ui.Info("Profile", view.Profile)
			ui.Info("Run", fmt.Sprintf("#%d · %s%s", view.RunID, view.Status, map[bool]string{true: " (partial)", false: ""}[view.Partial]))
			for _, idx := range view.Indexers {
				line := fmt.Sprintf("%s · %d results · %dms", idx.Status, idx.Fetched, idx.DurationMs)
				if idx.Error != "" {
					line += " · " + idx.Error
				}
				ui.Info(idx.Indexer, line)
			}
			table := ui.NewTable("✓", "Title", "Indexer", "Quality", "Score", "Size", "Why not")
			for _, cand := range view.Candidates {
				mark := ""
				switch {
				case cand.Chosen:
					mark = "★"
				case cand.Acceptable:
					mark = fmt.Sprintf("#%d", cand.SelectionRank)
				}
				whyNot := ""
				if !cand.Acceptable && len(cand.Rejections) > 0 {
					whyNot = cand.Rejections[0].Message
				}
				table.AddRow(mark, cand.Title, cand.Indexer, cand.Quality,
					fmt.Sprintf("%d", cand.FormatScore), humanBytes(cand.SizeBytes), whyNot)
			}
			fmt.Println(table.Render())
			ui.Info("Verdict", view.Verdict+map[bool]string{true: " → " + view.ChosenTitle, false: ""}[view.ChosenTitle != ""])
			return nil
		})
	},
}

var managerRSSCmd = &cobra.Command{
	Use:   "rss",
	Short: "RSS sweep controls",
}

var managerRSSRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run an RSS sweep now (dry-run: records decisions, downloads nothing)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			view, err := app.RunManagerRSS(ctx, "cli")
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(view)
			}
			for _, idx := range view.Indexers {
				line := fmt.Sprintf("%s · %s · %d releases · %dms", idx.Domain, idx.Status, idx.Fetched, idx.DurationMs)
				if idx.Error != "" {
					line += " · " + idx.Error
				}
				ui.Info(idx.Indexer, line)
			}
			ui.Info("Run", fmt.Sprintf("#%d · %s", view.RunID, view.Status))
			ui.Info("Releases", fmt.Sprintf("%d fetched · %d fresh · %d matched · %d evaluated · %d would grab",
				view.Fetched, view.Fresh, view.Matched, view.Evaluated, view.WouldGrabs))
			return nil
		})
	},
}

var managerHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "The decision ledger: what the pipeline would have done, and why",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		return withApp(func(ctx context.Context, app *service.App) error {
			page, err := app.ManagerHistory(ctx, service.ManagerHistoryParams{Limit: limit})
			if err != nil {
				return err
			}
			if ui.JSONMode {
				return ui.OutputJSON(page)
			}
			table := ui.NewTable("When", "Kind", "Target", "Verdict", "Would have grabbed")
			for _, d := range page.Decisions {
				target := d.TargetTitle
				if d.TargetYear > 0 {
					target = fmt.Sprintf("%s (%d)", d.TargetTitle, d.TargetYear)
				}
				table.AddRow(d.DecidedAt.Format("2006-01-02 15:04"), d.RunKind, target, d.Verdict, d.ChosenTitle)
			}
			fmt.Println(table.Render())
			return nil
		})
	},
}

func init() {
	managerCalendarCmd.Flags().String("from", "", "Window start (YYYY-MM-DD; default one week back)")
	managerCalendarCmd.Flags().String("to", "", "Window end (YYYY-MM-DD; default 30 days ahead)")
	managerCalendarCmd.Flags().Int64Slice("library", nil, "Restrict to library ids (repeatable)")
	managerCalendarCmd.Flags().Bool("monitored", false, "Monitored items only")
	managerHistoryCmd.Flags().Int("limit", 50, "Max decisions to list")
	managerSearchCmd.Flags().Int("season", 0, "TV: search this season's wanted episodes")
	managerSearchCmd.Flags().Int64("episode-id", 0, "TV: search one episode by its id")

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
	managerProfileAddCmd.Flags().String("name", "", "Profile name")
	managerProfileAddCmd.Flags().String("domain", "movie", "movie | tv | music | book")
	managerProfileAddCmd.Flags().String("cutoff", "", "Cutoff quality key (default: best allowed rung)")
	managerProfileAddCmd.Flags().String("language", "any", "Language gate: any | original | a language name")

	managerProfileCmd.AddCommand(managerProfileListCmd, managerProfileAddCmd, managerProfileCloneCmd, managerProfileRemoveCmd)
	managerFormatCmd.AddCommand(managerFormatListCmd, managerFormatImportCmd, managerFormatTestCmd, managerFormatRemoveCmd)

	managerLibraryItemsCmd.Flags().String("search", "", "Title substring filter")
	managerLibraryItemsCmd.Flags().String("monitored", "", "monitored | unmonitored")
	managerLibraryItemsCmd.Flags().String("file-state", "", "missing | complete")
	managerLibraryItemsCmd.Flags().String("status", "", "Metadata status filter (e.g. returning_series)")
	managerLibraryItemsCmd.Flags().String("profile", "", "Quality profile id, or 'none' for unassigned")
	managerLibraryItemsCmd.Flags().String("sort", "title", "title | year | added | size | missing | units | progress | status")
	managerLibraryItemsCmd.Flags().String("dir", "asc", "asc | desc")
	managerLibraryItemsCmd.Flags().Int("page", 1, "Page number")
	managerLibraryItemsCmd.Flags().Int("per-page", 60, "Items per page")
	managerLibrarySetCmd.Flags().Bool("monitor", false, "Monitored state to set (use --monitor=false to unmonitor)")
	managerLibrarySetCmd.Flags().Int64("profile", 0, "Quality profile id to assign (0 clears it)")
	managerLibraryCmd.AddCommand(managerLibraryItemsCmd, managerLibraryShowCmd, managerLibraryAlbumCmd, managerLibrarySetCmd, managerLibraryRefreshCmd)

	managerCmd.AddCommand(managerIndexerCmd, managerClientCmd, managerProfileCmd, managerFormatCmd, managerLibraryCmd, managerCalendarCmd, managerSearchCmd, managerHistoryCmd, managerRSSCmd)
	managerRSSCmd.AddCommand(managerRSSRunCmd)
	rootCmd.AddCommand(managerCmd)
}
