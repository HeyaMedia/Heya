package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/jellyfin"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// dev-proxy is the stable dev front door. It owns the user-facing LAN port
// (:8080) and — when the --dev-backend server tells it to — the tailnet node,
// reverse-proxying everything to the two hot-reloading downstreams:
//
//	/api/*  ──► HEYA_DEV_BACKEND (default :3050, run by air)
//	/*      ──► HEYA_DEV_PROXY   (default :3000, Nuxt/Vite)
//
// Because this process never holds business logic, it doesn't restart when you
// edit Go or Vue — so the tailnet node and any in-flight WS/HMR socket survive
// air rebuilds. tsnet here is the exact production code path (tsnetwrap), just
// fronting a proxy handler instead of the embedded app. The backend drives
// enable/disable over the control socket (see serve --dev-backend).
var devProxyCmd = &cobra.Command{
	Use:   "dev-proxy",
	Short: "Dev front door: reverse-proxy Nuxt + the API and own the tailnet node",
	Long: "Stable dev front door used by `make dev`. Reverse-proxies /api/* to the air-run backend " +
		"and everything else to the Nuxt dev server, while owning the LAN listener and the Tailscale " +
		"node so neither flaps when air rebuilds the backend.",
	RunE: func(cmd *cobra.Command, args []string) error {
		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		backendURL, err := url.Parse(envOr("HEYA_DEV_BACKEND", "http://localhost:3050"))
		if err != nil {
			return err
		}
		nuxtURL, err := url.Parse(envOr("HEYA_DEV_PROXY", "http://localhost:3000"))
		if err != nil {
			return err
		}

		// stdlib ReverseProxy handles WebSocket upgrades natively (Go 1.12+),
		// so /api/ws and Vite's HMR socket both pass through unchanged. A
		// downstream that's mid-rebuild yields a clean 502 instead of killing
		// the front door — the browser retries once air is back.
		apiProxy := httputil.NewSingleHostReverseProxy(backendURL)
		apiProxy.FlushInterval = -1 // flush immediately for SSE / streamed bodies
		apiProxy.ErrorHandler = upstreamErrorHandler("api", backendURL)
		webProxy := httputil.NewSingleHostReverseProxy(nuxtURL)
		webProxy.ErrorHandler = upstreamErrorHandler("web", nuxtURL)

		mux := http.NewServeMux()
		mux.Handle("/api/", apiProxy)
		// Jellyfin-compatible surface: exact route-table matches (and only
		// those — /search etc. stay with Nuxt) go to the Go backend. See
		// internal/jellyfin.ClaimsPath.
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if jellyfin.ClaimsPath(r.URL.Path) {
				apiProxy.ServeHTTP(w, r)
				return
			}
			webProxy.ServeHTTP(w, r)
		}))

		// tsnet via the production wrapper, fronting the proxy handler. We
		// only construct the node here; the backend opens/closes its listeners
		// over the control socket so the DB-backed toggle stays authoritative.
		tsLogger := log.With().Str("subsystem", "tailscale").Logger()
		tsServer := tsnetwrap.New(mux, tsLogger, func(st tsnetwrap.Status) {
			tsLogger.Debug().Str("backend_state", st.BackendState).Bool("running", st.Running).Msg("tailscale status")
		})

		// Control socket for serve --dev-backend to drive enable/disable.
		socketPath := devTSControlSocket()
		if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
			return err
		}
		_ = os.Remove(socketPath) // clear a stale socket from a crashed run
		ctlLn, err := net.Listen("unix", socketPath)
		if err != nil {
			return err
		}
		ctlSrv := &http.Server{Handler: tsnetwrap.ControlHandler(tsServer), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := ctlSrv.Serve(ctlLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				tsLogger.Warn().Err(err).Msg("tailscale control server stopped")
			}
		}()

		// LAN front door.
		ln, err := reuseAddrListen(cfg.Addr())
		if err != nil {
			return err
		}
		lanSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			log.Info().
				Str("addr", cfg.Addr()).
				Str("api", backendURL.String()).
				Str("nuxt", nuxtURL.String()).
				Str("ts_control", socketPath).
				Msg("dev-proxy front door up")
			if err := lanSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				log.Fatal().Err(err).Msg("dev-proxy server error")
			}
		}()

		<-sigCtx.Done()
		log.Info().Msg("dev-proxy shutting down")

		// Hard backstop — tsnet teardown can hang; mirror serve.go's 8s cap.
		go func() {
			<-time.After(8 * time.Second)
			log.Warn().Msg("dev-proxy shutdown took >8s, forcing exit")
			os.Exit(1)
		}()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		_ = lanSrv.Shutdown(shutdownCtx)
		_ = ctlSrv.Shutdown(shutdownCtx)
		_ = tsServer.Close() // flush state dir cleanly so the next start isn't slow
		_ = os.Remove(socketPath)

		log.Info().Msg("dev-proxy clean shutdown complete")
		return nil
	},
}

// upstreamErrorHandler returns a ReverseProxy ErrorHandler that maps an
// unreachable downstream (typically mid-rebuild) to a quiet 502 rather than a
// stack-traced 500. The front door stays up; the client retries.
func upstreamErrorHandler(name string, target *url.URL) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		log.Debug().Err(err).Str("upstream", name).Str("target", target.String()).Str("path", r.URL.Path).Msg("dev-proxy upstream unavailable")
		w.WriteHeader(http.StatusBadGateway)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func init() {
	rootCmd.AddCommand(devProxyCmd)
}
