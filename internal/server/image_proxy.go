package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/karbowiak/heya/internal/config"
	"github.com/rs/zerolog/log"
)

// newPassiveImageProxy returns a reverse proxy that forwards an inbound image
// request verbatim to another Heya instance's identical route, or nil when
// proxying isn't wanted. It is only active in passive mode — where this process
// is a guest on a borrowed (production) DB whose image files live on the source
// server's disk, not locally — and only when HEYA_IMAGE_PROXY_URL names that
// source. The public image endpoints are unauthenticated (browsers can't attach
// Authorization to <img>), so there are no credentials to forward.
func newPassiveImageProxy(cfg *config.Config) *httputil.ReverseProxy {
	// cfg is nil for the zero-valued App used in operation-contract tests.
	if cfg == nil || !cfg.PassiveMode.Value || cfg.ImageProxyURL.Value == "" {
		return nil
	}
	base, err := url.Parse(cfg.ImageProxyURL.Value)
	if err != nil || base.Scheme == "" || base.Host == "" {
		log.Error().Str("url", cfg.ImageProxyURL.Value).
			Msg("HEYA_IMAGE_PROXY_URL is not a valid absolute URL; serving images locally")
		return nil
	}
	log.Info().Str("upstream", base.String()).
		Msg("passive mode: proxying image bytes to upstream Heya instance")
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// base has no path, so SetURL keeps the inbound path+query as-is —
			// the upstream serves the exact same routes. Out.Host targets the
			// upstream vhost so its TLS cert (MagicDNS) validates.
			pr.SetURL(base)
			pr.Out.Host = base.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn().Err(err).Str("path", r.URL.Path).Msg("image proxy upstream failed")
			http.Error(w, "image proxy upstream failed", http.StatusBadGateway)
		},
	}
}

// proxiedImage serves the request from the upstream proxy when one is
// configured, otherwise falls back to the local byte handler. With proxy nil
// (the normal, non-passive case) it's exactly the local handler — zero cost.
func proxiedImage(proxy *httputil.ReverseProxy, local http.HandlerFunc) http.HandlerFunc {
	if proxy == nil {
		return local
	}
	return proxy.ServeHTTP
}
