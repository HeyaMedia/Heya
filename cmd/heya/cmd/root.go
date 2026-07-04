package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var cfg *config.Config

// withApp opens the service layer (connect + migrate + bootstrap), runs fn,
// and closes it — the shared preamble of nearly every CLI command.
func withApp(fn func(ctx context.Context, app *service.App) error) error {
	ctx := context.Background()
	app, err := service.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer app.Close()
	return fn(ctx, app)
}

var rootCmd = &cobra.Command{
	Use:   "heya",
	Short: "Heya — a self-hosted media server",
	Long:  "Heya is a self-hosted media server for movies, TV series, music, books, and more.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		jsonFlag, _ := cmd.Flags().GetBool("json")
		noColorFlag, _ := cmd.Flags().GetBool("no-color")
		ui.Init(jsonFlag, noColorFlag)

		cfg = config.Load()

		level, err := zerolog.ParseLevel(cfg.LogLevel.Value)
		if err != nil {
			level = zerolog.InfoLevel
		}
		zerolog.SetGlobalLevel(level)

		if cfg.LogFormat.Value == "console" {
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(ui.HelpBanner())
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().Bool("json", false, "Output in JSON format")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(libraryCmd)
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(jobCmd)
	rootCmd.AddCommand(queueCmd)
	rootCmd.AddCommand(mediaCmd)
	rootCmd.AddCommand(musicCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(transcodeCmd)
	rootCmd.AddCommand(studiosCmd)
}
