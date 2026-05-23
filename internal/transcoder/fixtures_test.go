package transcoder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Fixture loaders for the Jellyfin-style profile × media decision matrix.
//
// Profile JSONs live under testdata/profiles/ and deserialize into
// ClientCapabilities. Media JSONs live under testdata/mediainfo/ and
// deserialize into MediaInfo. Both are cached on first read.

var (
	profileCache = map[string]ClientCapabilities{}
	profileMu    sync.Mutex
	mediaCache   = map[string]MediaInfo{}
	mediaMu      sync.Mutex
)

func loadProfile(t *testing.T, name string) ClientCapabilities {
	t.Helper()
	profileMu.Lock()
	defer profileMu.Unlock()
	if cached, ok := profileCache[name]; ok {
		return cached
	}
	path := filepath.Join("testdata", "profiles", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile %s: %v", name, err)
	}
	var caps ClientCapabilities
	if err := json.Unmarshal(data, &caps); err != nil {
		t.Fatalf("parse profile %s: %v", name, err)
	}
	profileCache[name] = caps
	return caps
}

func loadMediaInfo(t *testing.T, name string) MediaInfo {
	t.Helper()
	mediaMu.Lock()
	defer mediaMu.Unlock()
	if cached, ok := mediaCache[name]; ok {
		return cached
	}
	path := filepath.Join("testdata", "mediainfo", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mediainfo %s: %v", name, err)
	}
	var info MediaInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("parse mediainfo %s: %v", name, err)
	}
	mediaCache[name] = info
	return info
}
