package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/karbowiak/heya/internal/safelog"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var tailscaleCmd = &cobra.Command{
	Use:   "tailscale",
	Short: "Manage Tailscale (tsnet) integration",
	Long:  "Inspect Tailscale node state or wipe the saved tailnet identity. Run with heya serve not running — both would race for the same state dir.",
}

var tailscaleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Tailscale node status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !cfg.Tailscale.Enabled.Value {
			fmt.Println("Tailscale: disabled (toggle on in Settings → Tailscale, or set HEYA_TAILSCALE_ENABLED=true)")
			return nil
		}

		ts := newOneShotTailscale()
		defer func() { _ = ts.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if err := ts.Enable(ctx, oneShotConfig()); err != nil {
			return err
		}

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
		if !cfg.Tailscale.Enabled.Value {
			return fmt.Errorf("tailscale is disabled (set HEYA_TAILSCALE_ENABLED=true)")
		}
		ts := newOneShotTailscale()
		defer func() { _ = ts.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ts.Enable(ctx, oneShotConfig()); err != nil {
			return err
		}
		if err := ts.Logout(ctx); err != nil {
			return err
		}
		fmt.Println("Logged out. The next 'heya serve' will require a fresh auth flow.")
		return nil
	},
}

func newOneShotTailscale() *tsnetwrap.Server {
	logger := zerolog.New(safelog.Redact(os.Stderr)).With().Timestamp().Logger()
	return tsnetwrap.New(logger, nil, nil, nil)
}

func oneShotConfig() tsnetwrap.Config {
	return tsnetwrap.Config{
		Enabled:  true,
		Hostname: cfg.Tailscale.Hostname.Value,
		AuthKey:  cfg.Tailscale.AuthKey.Value,
		StateDir: cfg.Tailscale.StateDir.Value,
		HTTPS:    cfg.Tailscale.HTTPS.Value,
		Funnel:   cfg.Tailscale.Funnel.Value,
	}
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func init() {
	tailscaleStatusCmd.Flags().Bool("json", false, "JSON output")
	tailscaleCmd.AddCommand(tailscaleStatusCmd)
	tailscaleCmd.AddCommand(tailscaleLogoutCmd)
	rootCmd.AddCommand(tailscaleCmd)
}
