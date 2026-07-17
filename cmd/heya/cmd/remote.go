package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

// heya remote — drive the running server's remote-access subsystem over the
// authenticated HTTP API (same token cache as `heya api`). The state machine
// lives in the serve process, so unlike `heya tailscale` there is no
// one-shot local mode: these commands are thin API clients on purpose.
var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Direct remote access (UPnP + certificates + reachability)",
}

var remoteJSON bool

var remoteStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show remote access status",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return remoteCall(cmd.Context(), http.MethodGet, "/api/remote/status")
	},
}

var remoteCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Re-run the outside-in reachability check",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return remoteCall(cmd.Context(), http.MethodPost, "/api/remote/check")
	},
}

var remoteEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable remote access",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return remoteToggle(cmd.Context(), true)
	},
}

var remoteDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable remote access (and unmap the router port)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return remoteToggle(cmd.Context(), false)
	},
}

func init() {
	remoteCmd.PersistentFlags().BoolVar(&remoteJSON, "json", false, "Print the raw JSON response")
	// Shares apiBaseURL with `heya api` — same server-resolution story.
	remoteCmd.PersistentFlags().StringVar(&apiBaseURL, "base", envOr("HEYA_API_BASE_URL", "https://localhost:8080"), "Server base URL")
	remoteCmd.AddCommand(remoteStatusCmd, remoteCheckCmd, remoteEnableCmd, remoteDisableCmd)
	rootCmd.AddCommand(remoteCmd)
}

// remoteStatusResponse mirrors the /api/remote/status body — only the fields
// the CLI renders; --json prints everything untouched.
type remoteStatusResponse struct {
	Available bool `json:"available"`
	Config    struct {
		Enabled     bool   `json:"enabled"`
		Port        int    `json:"port"`
		ACMEEmail   string `json:"acme_email"`
		DNSProvider string `json:"dns_provider"`
		TokenSet    bool   `json:"token_set"`
		Domain      string `json:"domain"`
		Subdomain   string `json:"subdomain"`
	} `json:"config"`
	Status *struct {
		Phase            string `json:"phase"`
		Detail           string `json:"detail"`
		Port             int    `json:"port"`
		LANIP            string `json:"lan_ip"`
		RouterExternalIP string `json:"router_external_ip"`
		ObservedIP       string `json:"observed_ip"`
		CGNAT            bool   `json:"cgnat"`
		UPnP             struct {
			Available bool   `json:"available"`
			Error     string `json:"error"`
		} `json:"upnp"`
		DNS struct {
			Provider string `json:"provider"`
			WANHost  string `json:"wan_host"`
			LANHost  string `json:"lan_host"`
			Error    string `json:"error"`
		} `json:"dns"`
		Cert struct {
			Mode    string   `json:"mode"`
			Issuing bool     `json:"issuing"`
			SANs    []string `json:"sans"`
			Expiry  string   `json:"expiry"`
			Error   string   `json:"error"`
		} `json:"cert"`
		LastCheckAt string `json:"last_check_at"`
		RemoteURL   string `json:"remote_url"`
		LANURL      string `json:"lan_url"`
	} `json:"status"`
	Message string `json:"message"`
}

func remoteCall(ctx context.Context, method, path string) error {
	body, err := remoteRequest(ctx, method, path, nil)
	if err != nil {
		return err
	}
	if remoteJSON {
		fmt.Println(string(body))
		return nil
	}
	var st remoteStatusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		fmt.Println(string(body))
		return nil
	}
	printRemoteStatus(st)
	return nil
}

func remoteToggle(ctx context.Context, enabled bool) error {
	cur, err := remoteRequest(ctx, http.MethodGet, "/api/remote/status", nil)
	if err != nil {
		return err
	}
	var st remoteStatusResponse
	if err := json.Unmarshal(cur, &st); err != nil {
		return fmt.Errorf("parsing current config: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"enabled":      enabled,
		"port":         st.Config.Port,
		"acme_email":   st.Config.ACMEEmail,
		"dns_provider": st.Config.DNSProvider,
		"domain":       st.Config.Domain,
		"subdomain":    st.Config.Subdomain,
		// dns_token omitted: empty keeps the stored token.
	})
	resp, err := remoteRequest(ctx, http.MethodPut, "/api/remote/config", payload)
	if err != nil {
		return err
	}
	if remoteJSON {
		fmt.Println(string(resp))
		return nil
	}
	if enabled {
		fmt.Println("enabling — follow progress with `heya remote status`")
	} else {
		fmt.Println("disabling")
	}
	return nil
}

func remoteRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	fullURL, err := buildAPIURL(apiBaseURL, path, nil)
	if err != nil {
		return nil, err
	}
	token, err := obtainAPIToken(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := doAPIRequest(ctx, method, fullURL, token, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintln(os.Stderr, string(data))
		return nil, fmt.Errorf("%s %s → %d", method, path, resp.StatusCode)
	}
	return data, nil
}

func printRemoteStatus(st remoteStatusResponse) {
	if !st.Available {
		fmt.Println("remote access: unavailable —", st.Message)
		return
	}
	fmt.Printf("enabled:    %v\n", st.Config.Enabled)
	if st.Status == nil {
		return
	}
	s := st.Status
	fmt.Printf("phase:      %s\n", s.Phase)
	if s.Detail != "" {
		fmt.Printf("detail:     %s\n", s.Detail)
	}
	fmt.Printf("port:       %d\n", s.Port)
	fmt.Printf("lan ip:     %s\n", s.LANIP)
	if s.RouterExternalIP != "" {
		fmt.Printf("router ip:  %s\n", s.RouterExternalIP)
	}
	if s.ObservedIP != "" {
		fmt.Printf("public ip:  %s\n", s.ObservedIP)
	}
	if s.CGNAT {
		fmt.Println("cgnat:      YES — port forwarding cannot work; use Tailscale")
	}
	if !s.UPnP.Available && s.UPnP.Error != "" {
		fmt.Printf("upnp:       unavailable (%s)\n", s.UPnP.Error)
	}
	if s.DNS.Provider != "" {
		fmt.Printf("dns:        %s", s.DNS.Provider)
		if s.DNS.Error != "" {
			fmt.Printf(" — ERROR: %s", s.DNS.Error)
		}
		fmt.Println()
	}
	certLine := s.Cert.Mode
	if s.Cert.Issuing {
		certLine += " (issuing…)"
	}
	if s.Cert.Expiry != "" {
		certLine += " expires " + s.Cert.Expiry
	}
	if s.Cert.Error != "" {
		certLine += " — ERROR: " + s.Cert.Error
	}
	fmt.Printf("cert:       %s\n", certLine)
	if s.RemoteURL != "" {
		fmt.Printf("remote url: %s\n", s.RemoteURL)
	}
	if s.LANURL != "" {
		fmt.Printf("lan url:    %s\n", s.LANURL)
	}
	if s.LastCheckAt != "" {
		fmt.Printf("checked:    %s\n", s.LastCheckAt)
	}
}
