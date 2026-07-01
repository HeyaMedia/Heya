package scanner

import "testing"

// The two namespaces must not be conflated: filePrefix mirrors the walk's
// verbatim path construction (double slash for a trailing-slash SMB root),
// while rootKey is canonical so library_nfo_dirs rows keep matching across
// trailing-slash config drift — conflating them orphans recorded state and
// swallows pending NFO edits.
func TestScanPathPrefixes(t *testing.T) {
	cases := []struct {
		root       string
		isSMB      bool
		wantPrefix string
		wantKey    string
	}{
		{"smb://h/share/dir", true, "smb://h/share/dir/", "smb://h/share/dir"},
		{"smb://h/share/dir/", true, "smb://h/share/dir//", "smb://h/share/dir"},
		{"smb://h/share/dir//", true, "smb://h/share/dir///", "smb://h/share/dir"},
		{"/data/movies", false, "/data/movies/", "/data/movies"},
		{"/data/movies/", false, "/data/movies/", "/data/movies"},
	}
	for _, c := range cases {
		prefix, key := scanPathPrefixes(c.root, c.isSMB)
		if prefix != c.wantPrefix || key != c.wantKey {
			t.Errorf("scanPathPrefixes(%q, %v) = (%q, %q), want (%q, %q)",
				c.root, c.isSMB, prefix, key, c.wantPrefix, c.wantKey)
		}
	}
}

func TestCanonicalNFOKey(t *testing.T) {
	cases := map[string]string{
		"smb://h/share/dir":         "smb://h/share/dir",
		"smb://h/share/dir/":        "smb://h/share/dir",
		"smb://h/share/dir//Movie":  "smb://h/share/dir/Movie",
		"smb://h/share//dir/Movie/": "smb://h/share/dir/Movie",
		"/data/movies":              "/data/movies",
		"/data/movies//Movie A":     "/data/movies/Movie A",
		"/":                         "/",
	}
	for in, want := range cases {
		if got := canonicalNFOKey(in); got != want {
			t.Errorf("canonicalNFOKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// A rooted fileDir can reach nearestNFODir when a root prefix doesn't
// round-trip cleanly (e.g. an SMB root configured with a trailing slash).
// filepath.Dir("/") == "/" is a fixed point, so without the termination
// guard this loops forever and hangs the scan.
func TestNearestNFODirTerminatesOnRootedPaths(t *testing.T) {
	seen := map[string]nfoEntry{
		"Movie A (2024)": {name: "movie.nfo", kind: "movie"},
	}

	if got, ok := nearestNFODir("/Movie A (2024)/sub", seen); ok {
		t.Fatalf("rooted path must not resolve a relative NFO dir, got %q", got)
	}
	if got, ok := nearestNFODir("/", seen); ok {
		t.Fatalf("bare root must not resolve, got %q", got)
	}

	got, ok := nearestNFODir("Movie A (2024)/sub", seen)
	if !ok || got != "Movie A (2024)" {
		t.Fatalf("relative lookup broke: got %q ok=%v", got, ok)
	}
	if _, ok := nearestNFODir(".", map[string]nfoEntry{}); ok {
		t.Fatal("empty seen map must not resolve")
	}
}
