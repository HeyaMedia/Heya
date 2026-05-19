package cmd

import (
	"os"
	"time"

	"github.com/karbowiak/kura/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var cfg *config.Config

var rootCmd = &cobra.Command{
	Use:   "kura",
	Short: "Kura — a self-hosted media server",
	Long:  "Kura is a self-hosted media server for movies, TV series, music, books, and more.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cfg = config.Load()

		level, err := zerolog.ParseLevel(cfg.LogLevel)
		if err != nil {
			level = zerolog.InfoLevel
		}
		zerolog.SetGlobalLevel(level)

		if cfg.LogFormat == "console" {
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(libraryCmd)
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(jobCmd)
	rootCmd.AddCommand(mediaCmd)
}
