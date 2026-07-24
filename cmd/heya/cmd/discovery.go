package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/discovery"
	"github.com/spf13/cobra"
)

// heya discovery — inspect and drive the LAN (mDNS/DNS-SD) advertisement.
// status/enable/disable are thin clients over the authenticated API (the
// advertiser lives in the serve process). browse is the exception: it runs a
// real multicast query from THIS machine, which is the only way to answer
// "can a client actually see the server from here?" — the question every
// discovery bug reduces to.
var discoveryCmd = &cobra.Command{
	Use:     "discovery",
	Short:   "LAN discovery — advertise this server over mDNS so clients find it",
	Aliases: []string{"mdns"},
}

var (
	discoveryJSON        bool
	discoveryBrowseWait  time.Duration
	discoveryBrowseAll   bool
	discoveryServiceType string
)

var discoveryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what this server is advertising",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return discoveryCall(cmd.Context(), http.MethodGet, "/api/discovery/status")
	},
}

var discoveryEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Start advertising on the local network",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return discoveryToggle(cmd.Context(), true)
	},
}

var discoveryDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop advertising on the local network",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return discoveryToggle(cmd.Context(), false)
	},
}

var discoveryNameCmd = &cobra.Command{
	Use:   "name <name>",
	Short: "Set the name clients display (empty argument restores the hostname)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var name string
		if len(args) == 1 {
			name = args[0]
		}
		cur, err := discoveryRequest(cmd.Context(), http.MethodGet, "/api/discovery/status", nil)
		if err != nil {
			return err
		}
		var st discoveryStatusResponse
		if err := json.Unmarshal(cur, &st); err != nil {
			return fmt.Errorf("parsing current config: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{"enabled": st.Config.Enabled, "name": name})
		return discoveryWrite(cmd.Context(), payload)
	},
}

var discoveryBrowseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Browse the local network for Heya servers (what a client would see)",
	Long: "Runs an mDNS query from this machine and prints every Heya server that answers.\n" +
		"This is the check that matters: it proves multicast actually reaches you, which\n" +
		"`heya discovery status` (a view from inside the server) cannot.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return discoveryBrowse(cmd.Context())
	},
}

func init() {
	discoveryCmd.PersistentFlags().BoolVar(&discoveryJSON, "json", false, "Print the raw JSON response")
	// Shares apiBaseURL with `heya api` — same server-resolution story.
	discoveryCmd.PersistentFlags().StringVar(&apiBaseURL, "base", envOr("HEYA_API_BASE_URL", "https://localhost:8080"), "Server base URL")
	discoveryBrowseCmd.Flags().DurationVar(&discoveryBrowseWait, "wait", 4*time.Second, "How long to listen for answers")
	discoveryBrowseCmd.Flags().BoolVar(&discoveryBrowseAll, "all", false, "Print every TXT key, not just the interesting ones")
	discoveryBrowseCmd.Flags().StringVar(&discoveryServiceType, "service", discovery.ServiceType, "DNS-SD service type to browse")
	discoveryCmd.AddCommand(discoveryStatusCmd, discoveryEnableCmd, discoveryDisableCmd, discoveryNameCmd, discoveryBrowseCmd)
	rootCmd.AddCommand(discoveryCmd)
}

// discoveryStatusResponse mirrors /api/discovery/status — only the fields the
// CLI renders; --json prints everything untouched.
type discoveryStatusResponse struct {
	Available bool `json:"available"`
	Config    struct {
		Enabled     bool   `json:"enabled"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Port        int    `json:"port"`
		Host        string `json:"host"`
		Addresses   string `json:"addresses"`
		Interfaces  string `json:"interfaces"`
	} `json:"config"`
	Status *struct {
		Advertising bool     `json:"advertising"`
		Instance    string   `json:"instance"`
		ServiceType string   `json:"service_type"`
		Domain      string   `json:"domain"`
		Hostname    string   `json:"hostname"`
		Port        int      `json:"port"`
		TXT         []string `json:"txt"`
		Interfaces  []string `json:"interfaces"`
		Addresses   []string `json:"addresses"`
		StartedAt   string   `json:"started_at"`
		LastError   string   `json:"last_error"`
	} `json:"status"`
	Message string `json:"message"`
}

func discoveryCall(ctx context.Context, method, path string) error {
	body, err := discoveryRequest(ctx, method, path, nil)
	if err != nil {
		return err
	}
	if discoveryJSON {
		fmt.Println(string(body))
		return nil
	}
	var st discoveryStatusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		fmt.Println(string(body))
		return nil
	}
	printDiscoveryStatus(st)
	return nil
}

func discoveryToggle(ctx context.Context, enabled bool) error {
	cur, err := discoveryRequest(ctx, http.MethodGet, "/api/discovery/status", nil)
	if err != nil {
		return err
	}
	var st discoveryStatusResponse
	if err := json.Unmarshal(cur, &st); err != nil {
		return fmt.Errorf("parsing current config: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"enabled": enabled, "name": st.Config.Name})
	return discoveryWrite(ctx, payload)
}

func discoveryWrite(ctx context.Context, payload []byte) error {
	resp, err := discoveryRequest(ctx, http.MethodPut, "/api/discovery/config", payload)
	if err != nil {
		return err
	}
	if discoveryJSON {
		fmt.Println(string(resp))
		return nil
	}
	var st discoveryStatusResponse
	if err := json.Unmarshal(resp, &st); err != nil {
		fmt.Println(string(resp))
		return nil
	}
	printDiscoveryStatus(st)
	return nil
}

func discoveryRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return remoteRequest(ctx, method, path, body)
}

func printDiscoveryStatus(st discoveryStatusResponse) {
	if !st.Available {
		fmt.Println("lan discovery: unavailable —", st.Message)
		return
	}
	fmt.Printf("enabled:    %v\n", st.Config.Enabled)
	fmt.Printf("name:       %s", st.Config.DisplayName)
	if st.Config.Name == "" {
		fmt.Print("  (from hostname)")
	}
	fmt.Println()
	if st.Status == nil {
		return
	}
	s := st.Status
	if !s.Advertising {
		if s.LastError != "" {
			fmt.Printf("state:      NOT advertising — %s\n", s.LastError)
		} else {
			fmt.Println("state:      not advertising")
		}
		return
	}
	fmt.Printf("service:    %s.%s%s\n", s.Instance, s.ServiceType, strings.TrimSuffix("."+s.Domain, "."))
	fmt.Printf("host:       %s:%d\n", s.Hostname, s.Port)
	if len(s.Addresses) > 0 {
		fmt.Printf("addresses:  %s\n", strings.Join(s.Addresses, ", "))
	}
	if len(s.Interfaces) > 0 {
		fmt.Printf("interfaces: %s\n", strings.Join(s.Interfaces, ", "))
	}
	for i, entry := range s.TXT {
		label := "txt:"
		if i > 0 {
			label = "    "
		}
		fmt.Printf("%-11s %s\n", label, entry)
	}
	if s.StartedAt != "" {
		fmt.Printf("since:      %s\n", s.StartedAt)
	}
	if s.LastError != "" {
		fmt.Printf("last error: %s\n", s.LastError)
	}
}

// discoveryBrowse runs one multicast browse window and prints what answered.
// Deliberately local: no API token, no running Heya needed.
func discoveryBrowse(ctx context.Context) error {
	found, err := discovery.Browse(ctx, discoveryServiceType, discoveryBrowseWait)
	if err != nil {
		return fmt.Errorf("browsing %s: %w", discoveryServiceType, err)
	}

	if discoveryJSON {
		out, err := json.MarshalIndent(found, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	if len(found) == 0 {
		fmt.Printf("no %s servers answered within %s\n", discoveryServiceType, discoveryBrowseWait)
		fmt.Println("if a server should be here: check that it has discovery enabled, that it")
		fmt.Println("shares this L2 segment (mDNS does not cross subnets or most VPNs), and that")
		fmt.Println("a container deployment is on host networking or has HEYA_DISCOVERY_ADDRESSES set.")
		return nil
	}

	for i, entry := range found {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s\n", entry.Instance)
		fmt.Printf("  url:      %s\n", entry.URL())
		fmt.Printf("  host:     %s:%d\n", entry.Hostname, entry.Port)
		for _, ip := range entry.IPv4 {
			fmt.Printf("  ipv4:     %s\n", ip)
		}
		for _, ip := range entry.IPv6 {
			fmt.Printf("  ipv6:     %s\n", ip)
		}
		if id := entry.ID(); id != "" {
			fmt.Printf("  id:       %s\n", id)
		}
		if ver := entry.TXT["ver"]; ver != "" {
			fmt.Printf("  version:  %s\n", ver)
		}
		if remote := entry.TXT["remote"]; remote != "" {
			fmt.Printf("  remote:   %s\n", remote)
		}
		if discoveryBrowseAll {
			for _, raw := range entry.RawTXT {
				fmt.Printf("  txt:      %s\n", raw)
			}
		}
	}
	return nil
}
