package service

import (
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanEnvLibrariesRequiresFilesystemPaths(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		want  string
		valid bool
	}{
		{name: "mounted path", path: "/mnt/media/Music", valid: true},
		{name: "relative path", path: "media/Music", want: "must be absolute"},
		{name: "legacy transport URL", path: "smb://reader:super-secret@nas/Music", want: "mount the share"},
		{name: "other transport URL", path: "https://reader:super-secret@storage.test/Music", want: "URL-style library paths are not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{
				"HEYA_LIBRARY_4_NAME":  "Music",
				"HEYA_LIBRARY_4_PATHS": tt.path,
				"HEYA_LIBRARY_4_TYPE":  "music",
			}
			libs, err := scanEnvLibrariesFrom([]string{"HEYA_LIBRARY_4_NAME=Music"}, func(key string) string {
				return values[key]
			})
			if tt.valid {
				require.NoError(t, err)
				require.Len(t, libs, 1)
				assert.Equal(t, []string{tt.path}, libs[0].Paths)
				assert.Equal(t, sqlc.MediaTypeMusic, libs[0].MediaType)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.False(t, strings.Contains(err.Error(), "super-secret"), "diagnostic leaked URL credentials: %v", err)
		})
	}
}
