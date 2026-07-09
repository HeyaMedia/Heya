package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// `heya cast` — control server-side playback to network receivers.
//
// All subcommands drive the *running server's* /api/cast/* surface (the
// `heya api` token/login machinery is reused): a playback session must
// live in the serve process, not in a CLI process that exits. This is
// the one place CLI-first means "CLI as first API client" rather than
// "CLI linking the service layer".

var castVolumeFlag int
var castToFlag string

var castCmd = &cobra.Command{
	Use:   "cast",
	Short: "Cast music to network receivers (AirPlay)",
}

var castDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List discovered cast devices",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var out struct {
			Items []castDeviceJSON `json:"items"`
		}
		if err := castAPI(cmd.Context(), http.MethodGet, "/api/cast/devices", nil, &out); err != nil {
			return err
		}
		if len(out.Items) == 0 {
			fmt.Println("no devices discovered (yet) — is casting enabled and the server on the same LAN?")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "NAME\tMODEL\tADDRESS\tID")
		for _, d := range out.Items {
			model := strings.TrimSpace(d.Manufacturer + " " + d.Model)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s:%d\t%s\n", d.Name, model, d.Addr, d.Port, d.ID)
		}
		return w.Flush()
	},
}

var castPlayCmd = &cobra.Command{
	Use:   "play <track-id>",
	Short: "Play a track on a device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		trackID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("track id must be numeric, got %q", args[0])
		}
		dev, err := castResolveDevice(cmd.Context(), castToFlag)
		if err != nil {
			return err
		}
		body := map[string]any{"device_id": dev.ID, "track_id": trackID, "volume": castVolumeFlag}
		var snap castSessionJSON
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/cast/sessions", body, &snap); err != nil {
			return err
		}
		fmt.Printf("casting to %s (session %s)\n", dev.Name, snap.ID)
		return nil
	},
}

var castStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active cast sessions",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var out struct {
			Items []castSessionJSON `json:"items"`
		}
		if err := castAPI(cmd.Context(), http.MethodGet, "/api/cast/sessions", nil, &out); err != nil {
			return err
		}
		if len(out.Items) == 0 {
			fmt.Println("no active cast sessions")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "DEVICE\tSTATE\tTRACK\tPOSITION\tVOL\tSESSION")
		for _, s := range out.Items {
			track := s.Title
			if s.Artist != "" {
				track = s.Artist + " – " + s.Title
			}
			pos := fmt.Sprintf("%s / %s", castFmtSec(int(s.PositionSec)), castFmtSec(s.DurationSec))
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", s.DeviceName, s.State, track, pos, s.Volume, s.ID)
		}
		return w.Flush()
	},
}

func castControlCmd(verb, short string) *cobra.Command {
	return &cobra.Command{
		Use:   verb,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := castResolveSession(cmd.Context(), castToFlag)
			if err != nil {
				return err
			}
			var snap castSessionJSON
			if err := castAPI(cmd.Context(), http.MethodPost, "/api/cast/sessions/"+s.ID+"/"+verb, nil, &snap); err != nil {
				return err
			}
			fmt.Printf("%s: %s (%s)\n", verb, snap.DeviceName, snap.State)
			return nil
		},
	}
}

var castSeekCmd = &cobra.Command{
	Use:   "seek <seconds>",
	Short: "Seek to an absolute position in the current track",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sec, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("seconds must be numeric, got %q", args[0])
		}
		s, err := castResolveSession(cmd.Context(), castToFlag)
		if err != nil {
			return err
		}
		var snap castSessionJSON
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/cast/sessions/"+s.ID+"/seek",
			map[string]any{"seconds": sec}, &snap); err != nil {
			return err
		}
		fmt.Printf("seek: %s → %s\n", snap.DeviceName, castFmtSec(sec))
		return nil
	},
}

var castVolCmd = &cobra.Command{
	Use:   "volume <0-100>",
	Short: "Set the session's device volume",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		level, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("volume must be numeric, got %q", args[0])
		}
		s, err := castResolveSession(cmd.Context(), castToFlag)
		if err != nil {
			return err
		}
		var snap castSessionJSON
		if err := castAPI(cmd.Context(), http.MethodPost, "/api/cast/sessions/"+s.ID+"/volume",
			map[string]any{"level": level}, &snap); err != nil {
			return err
		}
		fmt.Printf("volume: %s → %d\n", snap.DeviceName, snap.Volume)
		return nil
	},
}

func init() {
	castPlayCmd.Flags().IntVar(&castVolumeFlag, "volume", 30, "Initial device volume (0-100)")
	castCmd.PersistentFlags().StringVar(&castToFlag, "to", "", "Device name (substring) or device ID")
	castCmd.AddCommand(castDevicesCmd, castPlayCmd, castStatusCmd, castSeekCmd, castVolCmd,
		castControlCmd("pause", "Pause playback"),
		castControlCmd("resume", "Resume playback"),
		castControlCmd("stop", "Stop the session"))
	rootCmd.AddCommand(castCmd)
}

type castDeviceJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
	Addr         string `json:"addr"`
	Port         int    `json:"port"`
}

type castSessionJSON struct {
	ID          string  `json:"id"`
	DeviceID    string  `json:"device_id"`
	DeviceName  string  `json:"device_name"`
	State       string  `json:"state"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	PositionSec float64 `json:"position_sec"`
	DurationSec int     `json:"duration_sec"`
	Volume      int     `json:"volume"`
}

// castResolveDevice matches --to against discovered devices: exact ID
// first, then case-insensitive name substring. Ambiguity is an error
// listing the candidates rather than a guess.
func castResolveDevice(ctx context.Context, sel string) (castDeviceJSON, error) {
	var out struct {
		Items []castDeviceJSON `json:"items"`
	}
	if err := castAPI(ctx, http.MethodGet, "/api/cast/devices", nil, &out); err != nil {
		return castDeviceJSON{}, err
	}
	if len(out.Items) == 0 {
		return castDeviceJSON{}, fmt.Errorf("no cast devices discovered")
	}
	if sel == "" {
		if len(out.Items) == 1 {
			return out.Items[0], nil
		}
		return castDeviceJSON{}, fmt.Errorf("multiple devices found — pick one with --to (see `heya cast devices`)")
	}
	var matches []castDeviceJSON
	for _, d := range out.Items {
		if d.ID == sel {
			return d, nil
		}
		if strings.Contains(strings.ToLower(d.Name), strings.ToLower(sel)) {
			matches = append(matches, d)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return castDeviceJSON{}, fmt.Errorf("no device matches %q", sel)
	default:
		names := make([]string, len(matches))
		for i, d := range matches {
			names[i] = d.Name
		}
		return castDeviceJSON{}, fmt.Errorf("%q is ambiguous: %s", sel, strings.Join(names, ", "))
	}
}

// castResolveSession picks the target session: by --to device selector
// when given, otherwise the sole active session.
func castResolveSession(ctx context.Context, sel string) (castSessionJSON, error) {
	var out struct {
		Items []castSessionJSON `json:"items"`
	}
	if err := castAPI(ctx, http.MethodGet, "/api/cast/sessions", nil, &out); err != nil {
		return castSessionJSON{}, err
	}
	if len(out.Items) == 0 {
		return castSessionJSON{}, fmt.Errorf("no active cast sessions")
	}
	if sel == "" {
		if len(out.Items) == 1 {
			return out.Items[0], nil
		}
		return castSessionJSON{}, fmt.Errorf("multiple sessions active — pick one with --to")
	}
	for _, s := range out.Items {
		if s.DeviceID == sel || strings.Contains(strings.ToLower(s.DeviceName), strings.ToLower(sel)) {
			return s, nil
		}
	}
	return castSessionJSON{}, fmt.Errorf("no session on a device matching %q", sel)
}

// castAPI is a JSON round-trip against the running server, sharing the
// `heya api` token cache + re-login machinery.
func castAPI(ctx context.Context, method, path string, body any, out any) error {
	token, err := obtainAPIToken(ctx)
	if err != nil {
		return err
	}
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	fullURL := strings.TrimRight(apiBaseURL, "/") + path
	resp, err := doAPIRequest(ctx, method, fullURL, token, raw)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized && apiToken == "" {
		_ = resp.Body.Close()
		_ = clearAPITokenCache()
		token, err = loginAndCacheAPI(ctx)
		if err != nil {
			return fmt.Errorf("re-login failed: %w", err)
		}
		resp, err = doAPIRequest(ctx, method, fullURL, token, raw)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func castFmtSec(sec int) string {
	if sec <= 0 {
		return "0:00"
	}
	d := time.Duration(sec) * time.Second
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
