package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/service"
)

type fsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type fsBrowseResponse struct {
	Path    string    `json:"path"`
	Parent  string    `json:"parent,omitempty"`
	Entries []fsEntry `json:"entries"`
}

func handleFSBrowse(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("path")
		if dir == "" {
			if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
				dir = "/"
			} else {
				dir = "C:\\"
			}
		}

		dir = filepath.Clean(dir)

		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			writeError(w, http.StatusBadRequest, "path is not a valid directory")
			return
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			writeError(w, http.StatusForbidden, "cannot read directory")
			return
		}

		var dirs []fsEntry
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if !e.IsDir() {
				continue
			}
			dirs = append(dirs, fsEntry{
				Name:  e.Name(),
				Path:  filepath.Join(dir, e.Name()),
				IsDir: true,
			})
		}

		sort.Slice(dirs, func(i, j int) bool {
			return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
		})

		parent := ""
		if dir != "/" {
			parent = filepath.Dir(dir)
		}

		writeJSON(w, http.StatusOK, fsBrowseResponse{
			Path:    dir,
			Parent:  parent,
			Entries: dirs,
		})
	}
}
