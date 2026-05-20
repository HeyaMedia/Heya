package vfs

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Source struct {
	FS       fs.FS
	RootPath string
	Close    func() error
}

func Open(path string) (*Source, error) {
	if strings.HasPrefix(path, "smb://") {
		return openSMB(path)
	}
	return openLocal(path)
}

func openLocal(path string) (*Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("local path %q: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local path %q is not a directory", path)
	}

	return &Source{
		FS:       os.DirFS(path),
		RootPath: path,
		Close:    func() error { return nil },
	}, nil
}

func IsSMBPath(path string) bool {
	return strings.HasPrefix(path, "smb://")
}

func Dir(path string) string {
	if IsSMBPath(path) {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash <= len("smb://") {
			return path
		}
		return path[:lastSlash]
	}
	return filepath.Dir(path)
}

func Base(path string) string {
	if IsSMBPath(path) {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash < 0 {
			return path
		}
		return path[lastSlash+1:]
	}
	return filepath.Base(path)
}

func Join(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	if IsSMBPath(parts[0]) {
		result := strings.TrimSuffix(parts[0], "/")
		for _, p := range parts[1:] {
			result += "/" + p
		}
		return result
	}
	return filepath.Join(parts...)
}

type SMBConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Share    string
	Path     string
}

func ParseSMBURL(rawURL string) (*SMBConfig, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid SMB URL: %w", err)
	}

	if u.Scheme != "smb" {
		return nil, fmt.Errorf("expected smb:// scheme, got %s://", u.Scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "445"
	}

	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return nil, fmt.Errorf("SMB URL must include a share name: smb://host/share[/path]")
	}

	share := parts[0]
	subPath := ""
	if len(parts) == 2 {
		subPath = parts[1]
	}

	return &SMBConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		Share:    share,
		Path:     subPath,
	}, nil
}
