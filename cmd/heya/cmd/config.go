package cmd

import (
	"fmt"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and edit configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration with sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		sources := cfg.Sources()

		if ui.JSONMode {
			return ui.OutputJSON(map[string]any{
				"config":  cfg.ToFileConfig(),
				"sources": sources,
			})
		}

		ui.Header("Configuration")
		fmt.Println()

		fields := []struct {
			key string
			val string
		}{
			{"database_url", cfg.DatabaseURL},
			{"host", cfg.Host},
			{"port", cfg.Port},
			{"log_level", cfg.LogLevel},
			{"log_format", cfg.LogFormat},
			{"tmdb_api_token", maskToken(cfg.TMDBToken)},
			{"data_dir", cfg.DataDir},
		}

		t := ui.NewTable("KEY", "VALUE", "SOURCE")
		for _, f := range fields {
			source := sources[f.key]
			t.AddRow(f.key, f.val, ui.Dim(source))
		}
		fmt.Println(t.Render())
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show which config file is loaded",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := config.FindConfigFile()
		if path == "" {
			ui.Warn("No config file found. Run 'heya setup' or 'heya config init' to create one.")
		} else {
			ui.Success("Config file: %s", path)
		}
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "./heya.yaml"
		if existing := config.FindConfigFile(); existing != "" {
			ui.Warn("Config file already exists: %s", existing)
			return nil
		}

		fc := cfg.ToFileConfig()
		if err := config.SaveFile(path, fc); err != nil {
			return err
		}
		ui.Success("Created config file: %s", path)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Update a config file value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		value := args[1]

		path := config.FindConfigFile()
		if path == "" {
			path = "./heya.yaml"
		}

		var fc *config.FileConfig
		if existing, err := config.LoadFile(path); err == nil {
			fc = existing
		} else {
			fc = &config.FileConfig{}
		}

		switch key {
		case "database_url":
			fc.DatabaseURL = value
		case "host":
			fc.Host = value
		case "port":
			fc.Port = value
		case "log_level":
			fc.LogLevel = value
		case "log_format":
			fc.LogFormat = value
		case "tmdb_api_token":
			fc.TMDBToken = value
		case "data_dir":
			fc.DataDir = value
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}

		if err := config.SaveFile(path, fc); err != nil {
			return err
		}
		ui.Success("Set %s in %s", key, path)
		return nil
	},
}

func maskToken(s string) string {
	if len(s) <= 10 {
		return s
	}
	return s[:8] + "..." + s[len(s)-4:]
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)

	rootCmd.AddCommand(configCmd)
}
