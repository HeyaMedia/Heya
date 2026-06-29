package vfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSMBURLBasic(t *testing.T) {
	cfg, err := ParseSMBURL("smb://myserver/myshare")
	require.NoError(t, err)
	assert.Equal(t, "myserver", cfg.Host)
	assert.Equal(t, "445", cfg.Port)
	assert.Equal(t, "", cfg.Username)
	assert.Equal(t, "", cfg.Password)
	assert.Equal(t, "myshare", cfg.Share)
	assert.Equal(t, "", cfg.Path)
}

func TestParseSMBURLWithCreds(t *testing.T) {
	cfg, err := ParseSMBURL("smb://user:pass@host/share/sub/path")
	require.NoError(t, err)
	assert.Equal(t, "user", cfg.Username)
	assert.Equal(t, "pass", cfg.Password)
	assert.Equal(t, "host", cfg.Host)
	assert.Equal(t, "share", cfg.Share)
	assert.Equal(t, "sub/path", cfg.Path)
}

func TestParseSMBURLWithPort(t *testing.T) {
	cfg, err := ParseSMBURL("smb://host:1234/share")
	require.NoError(t, err)
	assert.Equal(t, "1234", cfg.Port)
}

// SMB filenames legitimately contain '#', '?', '$', '!', '%' and spaces.
// url.Parse treats '#'/'?' as fragment/query delimiters and would truncate the
// path — these must survive verbatim. Regression for the "$#-! My Dad Says"
// directory that collapsed to "$" and dropped the rest of the path.
func TestParseSMBURLSpecialChars(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantShare string
		wantPath  string
	}{
		{
			name:      "hash and bang in dir and file",
			input:     "smb://guest:pass@host/storage/TV/Foreign/$#-! My Dad Says (2010)/Season 01/$#-! My Dad Says - S01E06.mkv",
			wantShare: "storage",
			wantPath:  "TV/Foreign/$#-! My Dad Says (2010)/Season 01/$#-! My Dad Says - S01E06.mkv",
		},
		{
			name:      "question mark",
			input:     "smb://host/share/Who Knows? (2020)/file.mkv",
			wantShare: "share",
			wantPath:  "Who Knows? (2020)/file.mkv",
		},
		{
			name:      "literal percent is not decoded",
			input:     "smb://host/share/50% Off/file.mkv",
			wantShare: "share",
			wantPath:  "50% Off/file.mkv",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseSMBURL(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.wantShare, cfg.Share)
			assert.Equal(t, tt.wantPath, cfg.Path)
		})
	}
}

func TestParseSMBURLErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no share", "smb://host/"},
		{"no host", "smb:///share"},
		{"wrong scheme", "http://host/share"},
		{"empty path", "smb://host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSMBURL(tt.input)
			assert.Error(t, err)
		})
	}
}

func TestIsSMBPath(t *testing.T) {
	assert.True(t, IsSMBPath("smb://host/share"))
	assert.False(t, IsSMBPath("/local/path"))
	assert.False(t, IsSMBPath("./relative"))
}

func TestSMBPathHelpers(t *testing.T) {
	assert.Equal(t, "smb://host/share/Movies", Dir("smb://host/share/Movies/Film.mkv"))
	assert.Equal(t, "Film.mkv", Base("smb://host/share/Movies/Film.mkv"))
	assert.Equal(t, "smb://host/share", Dir("smb://host/share/Movies/"))
	assert.Equal(t, "Movies", Base("smb://host/share/Movies/"))
	assert.Equal(t, "smb://host/share/Movies/Film.mkv", Join("smb://host/share/", "/Movies/", "Film.mkv"))
	assert.Equal(t, "smb://host/share/Movies", Join("smb://host/share", "", "Movies"))
}

func TestRedactPath(t *testing.T) {
	assert.Equal(t, "/local/path", RedactPath("/local/path"))
	assert.Equal(t, "smb://host/share", RedactPath("smb://host/share"))
	assert.Equal(t, "smb://user:xxxxx@host/share/sub/path", RedactPath("smb://user:pass@host/share/sub/path"))
	assert.Equal(t, "smb://user:xxxxx@host/share", RedactPath("smb://user:pa%24%24@host/share"))
	assert.Equal(t, "smb://user@host/share", RedactPath("smb://user@host/share"))
	// A literal '#' in the path must not be re-parsed as a URL fragment and lost.
	assert.Equal(t,
		"smb://guest:xxxxx@host/storage/TV/Foreign/$#-! My Dad Says (2010)/Season 01/ep.mkv",
		RedactPath("smb://guest:pass@host/storage/TV/Foreign/$#-! My Dad Says (2010)/Season 01/ep.mkv"))
}

func TestOpenLocalValid(t *testing.T) {
	dir := t.TempDir()
	src, err := openLocal(dir)
	require.NoError(t, err)
	assert.NotNil(t, src.FS)
	assert.Equal(t, dir, src.RootPath)
	assert.NoError(t, src.Close())
}

func TestOpenLocalNotExist(t *testing.T) {
	_, err := openLocal("/nonexistent/path/12345")
	assert.Error(t, err)
}

func TestOpenLocalFileNotDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("hi"), 0o644))

	_, err := openLocal(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}
