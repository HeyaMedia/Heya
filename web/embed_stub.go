//go:build !embed_frontend

package web

import "io/fs"

// Go-only builds and tests do not need generated frontend assets. Release
// builds opt into embed.go with -tags embed_frontend after web/dist has been
// populated by the frontend build stage.
var DistFS fs.FS = emptyFS{}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}
