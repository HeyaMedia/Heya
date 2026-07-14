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
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// dev-proxy is the stable dev front door. It does exactly one thing: own the
// user-facing port (:8080) and reverse-proxy to the two hot-reloading
// downstreams:
//
//	/api/*       ──► HEYA_DEV_BACKEND (default :3050, run by air)
//	/jellyfin/*  ──► HEYA_DEV_BACKEND
//	/subsonic/*  ──► HEYA_DEV_BACKEND
//	/*           ──► HEYA_DEV_PROXY   (default :3000, Nuxt/Vite)
//
// Because this process never holds business logic, it doesn't restart when
// you edit Go or Vue — in-flight WS/HMR sockets survive air rebuilds.
// Tailscale and remote access are production-only subsystems (`heya serve`
// without --dev-backend) and deliberately have no dev-proxy presence.
var devProxyCmd = &cobra.Command{
	Use:   "dev-proxy",
	Short: "Dev front door: reverse-proxy Nuxt + the API on one stable port",
	Long: "Stable dev front door used by `make dev`. Reverse-proxies /api/* and /jellyfin/* to the air-run backend " +
		"and every other path to the Nuxt dev server, so the browser-facing port never flaps during rebuilds.",
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
		mux.Handle("/jellyfin", apiProxy)
		mux.Handle("/jellyfin/", apiProxy)
		mux.Handle("/subsonic", apiProxy)
		mux.Handle("/subsonic/", apiProxy)
		mux.Handle("/", webProxy)

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
				Msg("dev-proxy front door up")
			if err := lanSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				log.Fatal().Err(err).Msg("dev-proxy server error")
			}
		}()

		<-sigCtx.Done()
		log.Info().Msg("dev-proxy shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		_ = lanSrv.Shutdown(shutdownCtx)

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
