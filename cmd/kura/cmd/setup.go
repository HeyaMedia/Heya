package cmd

import (
	"context"
	"database/sql"
	"fmt"

	"charm.land/huh/v2"
	"github.com/karbowiak/kura/internal/config"
	"github.com/karbowiak/kura/internal/database"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/service"
	"github.com/karbowiak/kura/internal/ui"
	"github.com/karbowiak/kura/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Guided first-time configuration",
	Long:  "Interactive wizard to configure Kura: database, admin user, first library, and API keys.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ui.IsInteractive() {
			ui.Error("Setup requires an interactive terminal. Run 'kura setup' directly.")
			return nil
		}

		fmt.Print(ui.Banner())
		fmt.Println()
		ui.Header("Setup Wizard")
		fmt.Println()

		fc := &config.FileConfig{
			DatabaseURL: cfg.DatabaseURL,
			Host:        cfg.Host,
			Port:        cfg.Port,
			LogLevel:    cfg.LogLevel,
			LogFormat:   cfg.LogFormat,
			TMDBToken:   cfg.TMDBToken,
			DataDir:     cfg.DataDir,
		}

		// Step 1: Database connection
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Database URL").
					Description("PostgreSQL connection string").
					Value(&fc.DatabaseURL),
			),
		).Run()
		if err != nil {
			return err
		}

		ctx := context.Background()
		ui.Printf("  Testing connection... ")
		pool, err := database.Connect(ctx, fc.DatabaseURL)
		if err != nil {
			ui.Error("connection failed: %v", err)
			return fmt.Errorf("cannot connect to database")
		}
		pool.Close()
		ui.Success("Connected")

		// Step 2: Run migrations
		fmt.Println()
		goose.SetBaseFS(migrations.FS)
		db, err := sql.Open("pgx", fc.DatabaseURL)
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

		// Step 3: Create admin user
		fmt.Println()
		pool, _ = database.Connect(ctx, fc.DatabaseURL)
		defer pool.Close()
		q := sqlc.New(pool)

		userCount, _ := q.CountUsers(ctx)
		if userCount > 0 {
			users, _ := q.ListUsers(ctx)
			ui.Success("Admin user exists: %s", users[0].Username)
		} else {
			var username, email, password string
			username = "admin"
			email = "admin@localhost"

			err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Admin username").
						Value(&username),
					huh.NewInput().
						Title("Admin email").
						Value(&email),
					huh.NewInput().
						Title("Admin password").
						EchoMode(huh.EchoModePassword).
						Value(&password),
				),
			).Run()
			if err != nil {
				return err
			}

			app, err := service.New(ctx, config.MergeFileWithEnv(fc))
			if err != nil {
				return err
			}
			_, err = app.CreateUser(ctx, username, email, password, true)
			app.Close()
			if err != nil {
				ui.Error("failed to create user: %v", err)
			} else {
				ui.Success("Created admin user: %s", username)
			}
		}

		// Step 4: Add first library
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

				app, err := service.New(ctx, config.MergeFileWithEnv(fc))
				if err != nil {
					return err
				}
				mt, _ := service.ParseMediaType(libType)
				users, _ := q.ListUsers(ctx)
				if len(users) > 0 {
					_, err = app.CreateLibrary(ctx, libName, mt, []string{libPath}, users[0].ID)
					if err != nil {
						ui.Error("failed to create library: %v", err)
					} else {
						ui.Success("Created library: %s (%s)", libName, libType)
					}
				}
				app.Close()
			}
		}

		// Step 5: TMDB token
		fmt.Println()
		if fc.TMDBToken != "" {
			ui.Success("TMDB API token configured")
		} else {
			ui.Println(ui.Dim("  TMDB token enables movie/TV metadata. Get one at https://www.themoviedb.org/settings/api"))
			huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("TMDB API read access token (optional, press Enter to skip)").
						Value(&fc.TMDBToken),
				),
			).Run()
			if fc.TMDBToken != "" {
				ui.Success("TMDB token saved")
			}
		}

		// Step 6: Save config
		fmt.Println()
		var saveConfig bool
		savePath := "./kura.yaml"
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Save configuration to kura.yaml?").
					Value(&saveConfig),
			),
		).Run()

		if saveConfig {
			if err := config.SaveFile(savePath, fc); err != nil {
				ui.Error("failed to save config: %v", err)
			} else {
				ui.Success("Configuration saved to %s", savePath)
			}
		}

		fmt.Println()
		ui.Header("Setup Complete")
		ui.Println("  Start the server:  " + ui.Primary("kura serve"))
		ui.Println("  Scan a library:    " + ui.Primary("kura library scan --all"))
		ui.Println("  View dashboard:    " + ui.Primary("kura dashboard"))
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
