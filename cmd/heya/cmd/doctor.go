package cmd

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Collect a support bundle for diagnosing issues",
	Long: "Gathers app/config/database/library/tool/queue/storage diagnostics into one\n" +
		"report — the thing to paste when asking for help. Read-only: never writes\n" +
		"to the DB, never touches a file, never kicks off a scan. Runs the same\n" +
		"report builder as GET /api/admin/doctor, minus the in-process log tail\n" +
		"(the CLI is a short-lived process with no log history of its own).\n\n" +
		"Secrets (DB password, API keys) are redacted — safe to paste publicly.\n" +
		"Use --json (the global flag) for the full machine-readable bundle.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Force passive mode for the bootstrap: withApp normally runs
		// AutoMigrate + HEYA_ADMIN_*/HEYA_LIBRARY_* env bootstrap, which
		// would let a diagnostic tool alter the very install it's supposed
		// to observe (worst case: a newer binary run just for doctor
		// migrates the schema out from under an older running server).
		// Passive mode already gates exactly those two writes — see
		// service.New. Restored before the report builds so the config
		// section still shows the box's real infra.passive_mode.
		origPassive := cfg.PassiveMode.Value
		cfg.PassiveMode.Value = true
		return withApp(func(ctx context.Context, app *service.App) error {
			cfg.PassiveMode.Value = origPassive
			report := app.BuildDoctorReport(ctx, nil)

			if ui.JSONMode {
				return ui.OutputJSON(report)
			}

			printDoctorReport(report)
			return nil
		})
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func printDoctorReport(r service.DoctorReport) {
	ui.Header("Heya Doctor")
	ui.Info("Generated", r.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Println()

	printDoctorApp(r)
	printDoctorConfig(r)
	printDoctorDatabase(r)
	printDoctorLibraries(r)
	printDoctorTools(r)
	printDoctorQueue(r)
	printDoctorStorage(r)
	printDoctorLogs(r)
}

func printDoctorApp(r service.DoctorReport) {
	ui.Println(ui.Bold("App"))
	a := r.App
	ui.Info("Version", valueOr(a.Version, "unknown (no build info)"))
	ui.Info("Revision", valueOr(a.Revision, "unknown"))
	ui.Info("Go", a.GoVersion)
	ui.Info("OS/Arch", a.GOOS+"/"+a.GOARCH)
	ui.Info("CPUs", strconv.Itoa(a.NumCPU))
	ui.Info("Hostname", a.Hostname)
	ui.Info("PID", strconv.Itoa(a.PID))
	ui.Info("Uptime", fmt.Sprintf("%ds (process is short-lived from the CLI)", a.UptimeSeconds))
	warnIfError("app", a.Error)
	fmt.Println()
}

func printDoctorConfig(r service.DoctorReport) {
	ui.Println(ui.Bold("Config"))
	keys := make([]string, 0, len(r.Config.Fields))
	for k := range r.Config.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	t := ui.NewTable("KEY", "VALUE", "SOURCE", "ENV VAR")
	for _, k := range keys {
		f := r.Config.Fields[k]
		envVar := f.EnvVar
		if envVar == "" {
			envVar = "—"
		}
		t.AddRow(k, f.Value, ui.Dim(string(f.Source)), ui.Dim(envVar))
	}
	fmt.Println(t.Render())
	warnIfError("config", r.Config.Error)
	fmt.Println()
}

func printDoctorDatabase(r service.DoctorReport) {
	ui.Println(ui.Bold("Database"))
	d := r.Database
	ui.Info("Reachable", strconv.FormatBool(d.Reachable))
	if d.Reachable {
		ui.Info("Version", d.Version)
		ui.Info("Database", d.DatabaseName)
		ui.Info("Migration", strconv.FormatInt(d.MigrationVersion, 10))
		ui.Info("Pool", fmt.Sprintf("%d acquired / %d idle / %d max",
			d.Pool.AcquiredConnections, d.Pool.IdleConnections, d.Pool.MaxConnections))

		if len(d.RowCounts) > 0 {
			keys := make([]string, 0, len(d.RowCounts))
			for k := range d.RowCounts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			t := ui.NewTable("TABLE", "ROWS (~)")
			for _, k := range keys {
				t.AddRow(k, strconv.FormatInt(d.RowCounts[k], 10))
			}
			fmt.Println(t.Render())
		}
	}
	warnIfError("database", d.Error)
	fmt.Println()
}

func printDoctorLibraries(r service.DoctorReport) {
	ui.Println(ui.Bold("Libraries"))
	ui.Info("Missing media (global)", strconv.Itoa(r.Libraries.MissingMediaTotal))

	if len(r.Libraries.Libraries) > 0 {
		t := ui.NewTable("ID", "NAME", "TYPE", "PATHS OK", "FILES")
		for _, lib := range r.Libraries.Libraries {
			ok := 0
			for _, p := range lib.Paths {
				if p.Readable {
					ok++
				}
			}
			t.AddRow(
				strconv.FormatInt(lib.ID, 10),
				lib.Name,
				lib.MediaType,
				fmt.Sprintf("%d/%d", ok, len(lib.Paths)),
				strconv.FormatInt(lib.FileCount, 10),
			)
		}
		fmt.Println(t.Render())

		for _, lib := range r.Libraries.Libraries {
			for _, p := range lib.Paths {
				if !p.Readable {
					ui.Warn("%s: %s unreadable — %s", lib.Name, p.Path, valueOr(p.Error, "unknown error"))
				}
			}
			if lib.Error != "" {
				ui.Warn("%s: %s", lib.Name, lib.Error)
			}
		}
	}
	warnIfError("libraries", r.Libraries.Error)
	fmt.Println()
}

func printDoctorTools(r service.DoctorReport) {
	ui.Println(ui.Bold("Tools"))
	ui.Info("ffmpeg", doctorToolLine(r.Tools.FFmpeg))
	ui.Info("ffprobe", doctorToolLine(r.Tools.FFprobe))
	warnIfError("tools", r.Tools.Error)
	fmt.Println()
}

func doctorToolLine(t service.DoctorTool) string {
	if !t.Found {
		return "not found in PATH — " + valueOr(t.Error, "playback/transcoding will fail")
	}
	if t.Error != "" {
		return fmt.Sprintf("%s (version check failed: %s)", t.Path, t.Error)
	}
	return fmt.Sprintf("%s (%s)", t.Path, t.Version)
}

func printDoctorQueue(r service.DoctorReport) {
	ui.Println(ui.Bold("Queue"))
	if len(r.Queue.Counts) == 0 && r.Queue.Error == "" {
		ui.Info("Jobs", "none pending or running")
	} else if len(r.Queue.Counts) > 0 {
		t := ui.NewTable("KIND", "STATE", "COUNT")
		for _, c := range r.Queue.Counts {
			t.AddRow(c.Kind, c.State, strconv.FormatInt(c.Count, 10))
		}
		fmt.Println(t.Render())
	}
	warnIfError("queue", r.Queue.Error)
	fmt.Println()
}

func printDoctorStorage(r service.DoctorReport) {
	ui.Println(ui.Bold("Storage"))
	ui.Info("Data dir", doctorPathLine(r.Storage.DataDir))
	ui.Info("Transcode dir", doctorPathLine(r.Storage.TranscodeDir))
	if r.Storage.TranscodeItems > 0 {
		ui.Info("Transcode cache", fmt.Sprintf("%d MB across %d items", r.Storage.TranscodeUsedMB, r.Storage.TranscodeItems))
	}
	if len(r.Storage.LibraryDiskUsage) > 0 {
		t := ui.NewTable("LIBRARY ID", "PATH", "BYTES", "FILES", "SCANNED")
		for _, u := range r.Storage.LibraryDiskUsage {
			t.AddRow(
				strconv.FormatInt(u.LibraryID, 10),
				u.Path,
				strconv.FormatInt(u.Bytes, 10),
				strconv.FormatInt(u.FileCount, 10),
				u.ScannedAt.Format("2006-01-02T15:04:05Z07:00"),
			)
		}
		fmt.Println(t.Render())
	} else {
		ui.Info("Library disk usage", "no scan_library_disk run yet")
	}
	warnIfError("storage", r.Storage.Error)
	fmt.Println()
}

func doctorPathLine(p service.DoctorPathUsage) string {
	if p.Error != "" {
		return fmt.Sprintf("%s — %s", p.Path, p.Error)
	}
	if !p.Exists {
		return p.Path + " (missing)"
	}
	return fmt.Sprintf("%s (%d%% used)", p.Path, p.UsedPct)
}

func printDoctorLogs(r service.DoctorReport) {
	ui.Println(ui.Bold("Logs"))
	if r.Logs.Available {
		ui.Info("Entries", strconv.Itoa(len(r.Logs.Entries)))
	} else {
		ui.Info("Logs", r.Logs.Note)
	}
	warnIfError("logs", r.Logs.Error)
}

func warnIfError(section, err string) {
	if err != "" {
		ui.Warn("%s: %s", section, err)
	}
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
