package server

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/karbowiak/heya/web"
	"github.com/rs/zerolog/log"
)

func spaHandler() http.Handler {
	if devURL := os.Getenv("HEYA_DEV_PROXY"); devURL != "" {
		target, err := url.Parse(devURL)
		if err != nil {
			log.Warn().Err(err).Str("url", devURL).Msg("invalid HEYA_DEV_PROXY, falling back to embedded dist")
		} else {
			log.Info().Str("upstream", devURL).Msg("dev mode: proxying SPA requests to Nuxt")
			proxy := httputil.NewSingleHostReverseProxy(target)
			origDirector := proxy.Director
			proxy.Director = func(r *http.Request) {
				origDirector(r)
				r.Host = target.Host
			}
			proxy.ModifyResponse = func(resp *http.Response) error {
				ct := resp.Header.Get("Content-Type")
				if ct == "" {
					p := resp.Request.URL.Path
					dot := strings.LastIndex(p, ".")
					if dot >= 0 {
						ext := p[dot:]
						if q := strings.Index(ext, "?"); q >= 0 {
							ext = ext[:q]
						}
						switch ext {
						case ".js", ".mjs":
							resp.Header.Set("Content-Type", "text/javascript")
						case ".css":
							resp.Header.Set("Content-Type", "text/css")
						case ".wasm":
							resp.Header.Set("Content-Type", "application/wasm")
						case ".json", ".map":
							resp.Header.Set("Content-Type", "application/json")
						case ".woff2":
							resp.Header.Set("Content-Type", "font/woff2")
						case ".vue", ".ts", ".tsx", ".jsx":
							resp.Header.Set("Content-Type", "text/javascript")
						}
					}
				}
				return nil
			}
			return proxy
		}
	}

	fsys := web.DistFS

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := fsys.Open(path)
		if err == nil {
			f.Close()
			http.FileServerFS(fsys).ServeHTTP(w, r)
			return
		}

		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}
