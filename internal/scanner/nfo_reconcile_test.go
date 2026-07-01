package scanner

import "testing"

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
