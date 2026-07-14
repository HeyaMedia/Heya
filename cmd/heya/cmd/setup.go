package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"charm.land/huh/v2"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/karbowiak/heya/migrations"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Guided first-time configuration",
	Long:  "Interactive wizard to configure Heya: writes ./.env, applies migrations, creates the admin user, and optionally adds a first library.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ui.IsInteractive() {
			ui.Error("Setup requires an interactive terminal. Run 'heya setup' directly.")
			return nil
		}

		fmt.Print(ui.Banner())
		fmt.Println()
		ui.Header("Setup Wizard")
		fmt.Println()

		// Step 1: Infrastructure prompts. Seeded with the currently-resolved
		// config (env > .env > defaults), so the user just hits Enter to
		// accept what's already loaded.
		databaseURL := cfg.DatabaseURL.Value
		host := cfg.Host.Value
		port := cfg.Port.Value
		logLevel := cfg.LogLevel.Value
		dataDir := cfg.DataDir.Value
		heyaMetadataURL := cfg.HeyaMetadataURL.Value

		err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Database URL").
					Description("PostgreSQL connection string").
					Value(&databaseURL),
				huh.NewInput().
					Title("Host").
					Value(&host),
				huh.NewInput().
					Title("Port").
					Value(&port),
				huh.NewSelect[string]().
					Title("Log level").
					Options(
						huh.NewOption("debug", "debug"),
						huh.NewOption("info", "info"),
						huh.NewOption("warn", "warn"),
						huh.NewOption("error", "error"),
					).
					Value(&logLevel),
				huh.NewInput().
					Title("Data dir").
					Value(&dataDir),
				huh.NewInput().
					Title("HeyaMetadata URL").
					Description("Canonical V2 metadata service").
					Value(&heyaMetadataURL),
			),
		).Run()
		if err != nil {
			return err
		}

		ctx := context.Background()
		ui.Printf("  Testing connection... ")
		pool, err := database.Connect(ctx, databaseURL)
		if err != nil {
			ui.Error("connection failed: %v", err)
			return fmt.Errorf("cannot connect to database")
		}
		pool.Close()
		ui.Success("Connected")

		// Step 2: Run migrations.
		fmt.Println()
		goose.SetBaseFS(migrations.FS)
		db, err := sql.Open("pgx", databaseURL)
		if err != nil {
			return err
		}
		defer db.Close()

		current, _ := goose.GetDBVersion(db)
		ui.Info("Current version", fmt.Sprintf("%d", current))

		pending, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
		if err == nil && int64(len(pending)) > current {
			var runMigrations bool
			huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Apply %d pending migrations?", int64(len(pending))-current)).
						Value(&runMigrations),
				),
			).Run()
			if runMigrations {
				if err := goose.Up(db, "."); err != nil {
					ui.Error("migration failed: %v", err)
				} else {
					ui.Success("Migrations applied")
				}
			}
		} else {
			ui.Success("Database schema is up to date")
		}

		// Step 3: Create admin user. Build a one-shot service.App against
		// an inline config so we don't touch the global cfg.
		fmt.Println()
		pool, _ = database.Connect(ctx, databaseURL)
		defer pool.Close()
		q := sqlc.New(pool)

		bootCfg := &config.Config{
			DatabaseURL:     config.Field[string]{Value: databaseURL, Source: config.SourceEnv, EnvVar: "HEYA_DATABASE_URL"},
			Host:            config.Field[string]{Value: host, Source: config.SourceEnv, EnvVar: "HEYA_HOST"},
			Port:            config.Field[string]{Value: port, Source: config.SourceEnv, EnvVar: "HEYA_PORT"},
			LogLevel:        config.Field[string]{Value: logLevel, Source: config.SourceEnv, EnvVar: "HEYA_LOG_LEVEL"},
			LogFormat:       config.Field[string]{Value: cfg.LogFormat.Value, Source: cfg.LogFormat.Source, EnvVar: cfg.LogFormat.EnvVar},
			DataDir:         config.Field[string]{Value: dataDir, Source: config.SourceEnv, EnvVar: "HEYA_DATA_DIR"},
			HeyaMetadataURL: config.Field[string]{Value: heyaMetadataURL, Source: config.SourceEnv, EnvVar: "HEYA_METADATA_URL"},
		}

		userCount, _ := q.CountUsers(ctx)
		var adminUsername, adminEmail, adminPassword string
		if userCount > 0 {
			users, _ := q.ListUsers(ctx)
			ui.Success("Admin user exists: %s", users[0].Username)
		} else {
			adminUsername = "admin"
			adminEmail = "admin@localhost"

			err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Admin username").
						Value(&adminUsername),
					huh.NewInput().
						Title("Admin email").
						Value(&adminEmail),
					huh.NewInput().
						Title("Admin password").
						EchoMode(huh.EchoModePassword).
						Value(&adminPassword),
				),
			).Run()
			if err != nil {
				return err
			}

			app, err := service.New(ctx, bootCfg)
			if err != nil {
				return err
			}
			_, err = app.CreateUser(ctx, adminUsername, adminEmail, adminPassword, true)
			app.Close()
			if err != nil {
				ui.Error("failed to create user: %v", err)
			} else {
				ui.Success("Created admin user: %s", adminUsername)
			}
		}

		// Step 4: Optional first library.
		fmt.Println()
		libs, _ := q.ListLibraries(ctx)
		if len(libs) > 0 {
			ui.Success("Libraries exist (%d total)", len(libs))
			for _, lib := range libs {
				ui.Info(lib.Name, string(lib.MediaType))
			}
		} else {
			var addLibrary bool
			huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Add your first media library?").
						Value(&addLibrary),
				),
			).Run()

			if addLibrary {
				var libName, libType, libPath string
				libName = "Movies"

				err := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("Library name").
							Value(&libName),
						huh.NewSelect[string]().
							Title("Media type").
							Options(
								huh.NewOption("Movies", "movie"),
								huh.NewOption("TV Shows", "tv"),
								huh.NewOption("Music", "music"),
								huh.NewOption("Books", "book"),
							).
							Value(&libType),
						huh.NewInput().
							Title("Path to media files").
							Description("Local path or smb://user:pass@host/share/path").
							Value(&libPath),
					),
				).Run()
				if err != nil {
					return err
				}

				app, err := service.New(ctx, bootCfg)
				if err != nil {
					return err
				}
				mt, _ := service.ParseMediaType(libType)
				users, _ := q.ListUsers(ctx)
				if len(users) > 0 {
					_, err = app.CreateLibrary(ctx, libName, mt, []string{libPath}, users[0].ID, nil)
					if err != nil {
						ui.Error("failed to create library: %v", err)
					} else {
						ui.Success("Created library: %s (%s)", libName, libType)
					}
				}
				app.Close()
			}
		}

		// Step 5: Write .env. Skip values that match the built-in default
		// so the file stays as small as possible.
		fmt.Println()
		var writeEnv bool
		envPath := "./.env"
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Write infra settings to %s?", envPath)).
					Description("Only non-default values are written. .env values are loaded automatically by heya serve.").
					Value(&writeEnv),
			),
		).Run()

		if writeEnv {
			if err := writeDotEnv(envPath, map[string]string{
				"HEYA_DATABASE_URL": databaseURL,
				"HEYA_HOST":         host,
				"HEYA_PORT":         port,
				"HEYA_LOG_LEVEL":    logLevel,
				"HEYA_DATA_DIR":     dataDir,
				"HEYA_METADATA_URL": heyaMetadataURL,
			}); err != nil {
				ui.Error("failed to write %s: %v", envPath, err)
			} else {
				ui.Success("Wrote %s", envPath)
			}
		}

		fmt.Println()
		ui.Header("Setup Complete")
		ui.Println("  Start the server:  " + ui.Primary("heya serve"))
		ui.Println("  Open the UI:       " + ui.Primary(fmt.Sprintf("http://%s:%s/settings", host, port)))
		ui.Println("  Scan a library:    " + ui.Primary("heya library scan --all"))
		fmt.Println()

		return nil
	},
}

// writeDotEnv writes the given values as a minimal .env, skipping any value
// that matches the built-in default. Preserves a stable key ordering so the
// file diff is deterministic on re-run.
func writeDotEnv(path string, vals map[string]string) error {
	// G101: the postgres URL below is the documented default for the local
	// docker-compose setup, not a real credential. Both halves are literally
	// the string "heya".
	defaults := map[string]string{ //nolint:gosec // G101: default-value table, not credentials
		"HEYA_DATABASE_URL": "postgres://heya:heya@localhost:5440/heya?sslmode=disable",
		"HEYA_HOST":         "0.0.0.0",
		"HEYA_PORT":         "8080",
		"HEYA_LOG_LEVEL":    "info",
		"HEYA_DATA_DIR":     "./data",
		"HEYA_METADATA_URL": "http://localhost:3030",
	}

	keys := make([]string, 0, len(vals))
	for k, v := range vals {
		if v == "" || v == defaults[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# Generated by `heya setup` — edit freely. See .env.example for every supported key.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, vals[k])
	}
	// .env contains DB URLs + similar config the user may want to read with a
	// non-root shell account; 0o600 would force sudo to inspect it. Keep 0o644.
	return os.WriteFile(path, []byte(b.String()), 0o644) //nolint:gosec // G306: .env is user-readable on purpose
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
