package jellyfin

import (
	"net/http"
	"runtime"
	"strings"
)

// jellyfinVersion is the server version advertised to clients. 10.11.x is
// the deliberate target: it's the last stable line with the dynamic-HLS API
// surface (12.0 removed those endpoints) and the baseline every shipping
// client is tested against. Feature-gating in clients keys off this value.
const (
	jellyfinVersion = "10.11.11"
	productName     = "Jellyfin Server"
)

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Darwin"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

func (s *Server) publicInfo(r *http.Request) publicSystemInfo {
	return publicSystemInfo{
		LocalAddress:           requestBaseURL(r),
		ServerName:             s.serverName(),
		Version:                jellyfinVersion,
		ProductName:            productName,
		OperatingSystem:        osName(),
		ID:                     s.serverID(r),
		StartupWizardCompleted: true,
	}
}

// requestBaseURL returns the client-facing Jellyfin base, including Heya's
// required mount prefix. Forwarding headers keep discovery correct when Heya
// is placed behind a conventional TLS-terminating reverse proxy.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); v != "" {
		scheme = v
	}
	host := r.Host
	if v := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); v != "" {
		host = v
	}
	base := scheme + "://" + host
	requestPath, _, _ := strings.Cut(r.RequestURI, "?")
	requestPath = strings.ToLower(requestPath)
	if requestPath == "/jellyfin" || strings.HasPrefix(requestPath, "/jellyfin/") {
		base += "/jellyfin"
	}
	return base
}

func firstForwardedValue(value string) string {
	value, _, _ = strings.Cut(value, ",")
	return strings.TrimSpace(value)
}

// GET /System/Info/Public — the discovery endpoint. Every client validates a
// candidate server URL by fetching this anonymously; ProductName and a
// GUID-shaped Id are what make them believe.
func (s *Server) handleSystemInfoPublic(w http.ResponseWriter, r *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, s.publicInfo(r))
}

// GET /System/Info (authenticated) — the full variant. Paths are advertised
// as Heya's actual data dir; nothing reads them remotely, but jellyfin-web's
// dashboard renders them.
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request, _ Params) {
	dataDir := ""
	if cfg := s.app.ConfigSnapshot(); cfg != nil {
		dataDir = cfg.DataDir.Value
	}
	writeJSON(w, http.StatusOK, systemInfo{
		publicSystemInfo:           s.publicInfo(r),
		OperatingSystemDisplayName: osName(),
		SupportsLibraryMonitor:     true,
		WebSocketPortNumber:        0,
		ProgramDataPath:            dataDir,
		CachePath:                  dataDir,
		LogPath:                    dataDir,
		InternalMetadataPath:       dataDir,
		TranscodingTempPath:        dataDir,
		SystemArchitecture:         runtime.GOARCH,
	})
}

// GET|POST /System/Ping — returns the product name as a JSON string.
func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, productName)
}

// GET /Branding/Configuration — anonymous; jellyfin-web fetches it before
// rendering the login page.
func (s *Server) handleBrandingConfiguration(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, brandingConfiguration{})
}

// GET /Branding/Css(.css) — no custom CSS; Jellyfin returns 204 in that case.
func (s *Server) handleBrandingCss(w http.ResponseWriter, _ *http.Request, _ Params) {
	w.Header().Set("Content-Type", "text/css")
	w.WriteHeader(http.StatusNoContent)
}

// GET /QuickConnect/Enabled — QuickConnect is off (Phase 3 candidate).
// Clients show/hide their QC login button off this boolean.
func (s *Server) handleQuickConnectEnabled(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, false)
}

func (s *Server) serverName() string {
	if h, err := hostname(); err == nil && h != "" {
		return h
	}
	return "Heya"
}
