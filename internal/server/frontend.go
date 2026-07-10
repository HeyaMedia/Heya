package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/web"
)

func spaHandler() http.Handler {
	fsys := web.DistFS

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Static assets straight from the embedded FS; the shell (and any
		// client-side route) goes through the theme-injecting fallback.
		if path != "" && path != "index.html" {
			if f, err := fsys.Open(path); err == nil {
				_ = f.Close()
				http.FileServerFS(fsys).ServeHTTP(w, r)
				return
			}
		}

		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(injectTheme(index, r))
	})
}

// injectTheme stamps data-theme on <html> from the heya_theme cookie
// (written by the FE's useAppearance) so the shell's very first paint —
// before even the inline boot script runs — matches the user's theme.
// Dark is the no-attribute default, so only light/oled need injection.
func injectTheme(index []byte, r *http.Request) []byte {
	c, err := r.Cookie("heya_theme")
	if err != nil || (c.Value != "light" && c.Value != "oled") {
		return index
	}
	return bytes.Replace(index, []byte("<html"), []byte(`<html data-theme="`+c.Value+`"`), 1)
}
