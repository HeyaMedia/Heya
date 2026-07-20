package cmd

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display current configuration with sources",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration with sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		sources := cfg.Sources()
		values := configValues(cfg)

		if ui.JSONMode {
			payload := make(map[string]map[string]any, len(sources))
			for k, s := range sources {
				payload[k] = map[string]any{
					"value":   values[k],
					"source":  s.Source,
					"env_var": s.EnvVar,
				}
			}
			return ui.OutputJSON(payload)
		}

		ui.Header("Configuration")
		fmt.Println()

		keys := make([]string, 0, len(sources))
		for k := range sources {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		t := ui.NewTable("KEY", "VALUE", "SOURCE", "ENV VAR")
		for _, k := range keys {
			src := sources[k]
			envVar := src.EnvVar
			if envVar == "" {
				envVar = "—"
			}
			t.AddRow(k, values[k], ui.Dim(string(src.Source)), ui.Dim(envVar))
		}
		fmt.Println(t.Render())
		return nil
	},
}

func configValues(c *config.Config) map[string]string {
	return map[string]string{
		"security.enable_registration": strconv.FormatBool(c.EnableRegistration.Value),
		"security.waf_mode":            c.WAFMode.Value,
		"security.trusted_networks":    c.TrustedNetworks.Value,
		"infra.database_url":           c.DatabaseURL.Value,
		"infra.host":                   c.Host.Value,
		"infra.port":                   c.Port.Value,
		"infra.log_level":              c.LogLevel.Value,
		"infra.log_format":             c.LogFormat.Value,
		"infra.data_dir":               c.DataDir.Value,
		"infra.heya_metadata_url":      c.HeyaMetadataURL.Value,
		"transcoder.hwaccel":           c.HWAccel.Value,
		"transcoder.cache_dir":         c.TranscodeCacheDir.Value,
		"transcoder.cache_max_gb":      strconv.Itoa(c.TranscodeCacheMaxGB.Value),
		"jellyfin.enabled":             strconv.FormatBool(c.Jellyfin.Enabled.Value),
		"tailscale.enabled":            strconv.FormatBool(c.Tailscale.Enabled.Value),
		"tailscale.hostname":           c.Tailscale.Hostname.Value,
		"tailscale.state_dir":          c.Tailscale.StateDir.Value,
		"tailscale.https":              strconv.FormatBool(c.Tailscale.HTTPS.Value),
		"tailscale.funnel":             strconv.FormatBool(c.Tailscale.Funnel.Value),
	}
}

func init() {
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
}
