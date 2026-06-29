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
		path = trimSMBTrailingSlash(path)
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
		path = trimSMBTrailingSlash(path)
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
			p = strings.Trim(p, "/")
			if p == "" {
				continue
			}
			result += "/" + p
		}
		return result
	}
	return filepath.Join(parts...)
}

func RedactPath(path string) string {
	if !IsSMBPath(path) {
		return path
	}
	// Mask the password by slicing the authority only — never re-parse the
	// whole URL, or a literal '#'/'?' in the path would be mangled (see
	// ParseSMBURL). The path tail is kept verbatim.
	rest := path[len("smb://"):]
	authority := rest
	tail := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority = rest[:i]
		tail = rest[i:] // keeps the leading '/'
	}
	at := strings.LastIndexByte(authority, '@')
	if at < 0 {
		return path // no credentials to redact
	}
	userinfo := authority[:at]
	if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
		userinfo = userinfo[:colon+1] + "xxxxx"
	}
	return "smb://" + userinfo + authority[at:] + tail
}

func trimSMBTrailingSlash(path string) string {
	for len(path) > len("smb://") && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
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
	rest, ok := strings.CutPrefix(rawURL, "smb://")
	if !ok {
		return nil, fmt.Errorf("expected smb:// scheme: %q", rawURL)
	}

	// Split the authority (user:pass@host:port) from the path at the first
	// slash, then keep the path verbatim. We deliberately do NOT run the path
	// through url.Parse: SMB filenames routinely contain '#', '?', '%', spaces
	// and other bytes that url.Parse treats as fragment/query delimiters and
	// silently truncates (a literal '#' in a directory name turned the path
	// into a fragment and lost everything after it). Paths are stored verbatim
	// by the scanner, so we read them back verbatim.
	authority := rest
	rawPath := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority = rest[:i]
		rawPath = rest[i+1:]
	}

	// Only the authority gets URL semantics — host/port splitting (incl. IPv6)
	// and percent-decoding of credentials.
	u, err := url.Parse("smb://" + authority)
	if err != nil {
		return nil, fmt.Errorf("invalid SMB URL: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("SMB URL must include a host: smb://host/share[/path]")
	}
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

	parts := strings.SplitN(rawPath, "/", 2)
	if parts[0] == "" {
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
