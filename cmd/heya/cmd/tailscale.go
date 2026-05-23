package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/karbowiak/heya/internal/eventhub"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var tailscaleCmd = &cobra.Command{
	Use:   "tailscale",
	Short: "Manage Tailscale (tsnet) integration",
	Long:  "Inspect Tailscale node state, run a one-shot login, or wipe the saved tailnet identity.",
}

var tailscaleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Tailscale node status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Tailscale.Enabled {
			fmt.Println("Tailscale: disabled (set tailscale.enabled: true in heya.yaml to onboard)")
			return nil
		}

		ts := newOneShotTailscale()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if err := ts.Start(ctx); err != nil {
			return err
		}
		defer func() { _ = ts.Close() }()

		st := ts.Status()
		jsonFlag, _ := cmd.Flags().GetBool("json")
		if jsonFlag {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}

		fmt.Printf("Hostname:     %s\n", st.Hostname)
		fmt.Printf("Backend:      %s\n", st.BackendState)
		fmt.Printf("MagicDNS:     %s\n", emptyDash(st.MagicDNS))
		fmt.Printf("Tailnet IPv4: %s\n", emptyDash(st.IPv4))
		fmt.Printf("Tailnet IPv6: %s\n", emptyDash(st.IPv6))
		fmt.Printf("Cert domain:  %s\n", emptyDash(st.CertDomain))
		fmt.Printf("HTTPS:        %t\n", st.HTTPS)
		fmt.Printf("Funnel:       %t\n", st.Funnel)
		if st.LoginURL != "" {
			fmt.Printf("\nAuthentication required:\n  %s\n", st.LoginURL)
		}
		if st.LastError != "" {
			fmt.Printf("\nLast error: %s\n", st.LastError)
		}
		return nil
	},
}

var tailscaleLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear local tailnet identity (next start re-onboards)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Tailscale.Enabled {
			return fmt.Errorf("tailscale is disabled in heya.yaml")
		}
		ts := newOneShotTailscale()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ts.Start(ctx); err != nil {
			return err
		}
		defer func() { _ = ts.Close() }()
		if err := ts.Logout(ctx); err != nil {
			return err
		}
		fmt.Println("Logged out. The next 'heya serve' will require a fresh auth flow.")
		return nil
	},
}

func newOneShotTailscale() *tsnetwrap.Server {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	return tsnetwrap.New(tsnetwrap.Config{
		Hostname: cfg.Tailscale.Hostname,
		AuthKey:  cfg.Tailscale.AuthKey,
		StateDir: cfg.Tailscale.StateDir,
		HTTPS:    cfg.Tailscale.HTTPS,
		Funnel:   cfg.Tailscale.Funnel,
	}, logger, nil)
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// keep eventhub import alive for typed payloads in API responses (consumers
// of this CLI parse via the same event payload struct).
var _ = eventhub.EventTailscale

func init() {
	tailscaleStatusCmd.Flags().Bool("json", false, "JSON output")
	tailscaleCmd.AddCommand(tailscaleStatusCmd)
	tailscaleCmd.AddCommand(tailscaleLogoutCmd)
	rootCmd.AddCommand(tailscaleCmd)
}
