package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/web"
)

func spaHandler() http.Handler {
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
