//go:build embed_frontend

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distDir embed.FS

var DistFS, _ = fs.Sub(distDir, "dist")
