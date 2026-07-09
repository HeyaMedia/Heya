package subsonic

import (
	"strings"
	"testing"
)

// TestManifestCoversSpec: every endpoint in the spec list must be
// consciously triaged, and the manifest must not invent endpoints.
func TestManifestCoversSpec(t *testing.T) {
	spec := map[string]bool{}
	for _, ep := range specEndpoints {
		if spec[ep.Name] {
			t.Errorf("spec lists %s twice", ep.Name)
		}
		spec[ep.Name] = true
		if _, ok := manifest[ep.Name]; !ok {
			t.Errorf("spec endpoint not triaged in manifest: %s", ep.Name)
		}
	}
	for name := range manifest {
		if !spec[name] {
			t.Errorf("manifest entry not present in spec list (typo?): %s", name)
		}
	}
}

// TestImplementedRoutesRegistered: implemented/stubbed manifest entries
// must have a registered route, and every registered route must be claimed
// implemented/stubbed.
func TestImplementedRoutesRegistered(t *testing.T) {
	var s Server
	routes := s.buildRoutes()

	for name, status := range manifest {
		key := strings.ToLower(name)
		switch status {
		case opImplemented, opStubbed:
			if _, ok := routes[key]; !ok {
				t.Errorf("manifest says %s is implemented/stubbed but no route is registered", name)
			}
		case opUnsupported:
			if _, ok := routes[key]; ok {
				t.Errorf("route %s is registered but manifest says unsupported — update the manifest", name)
			}
		}
	}

	claimed := map[string]bool{}
	for name, status := range manifest {
		if status == opImplemented || status == opStubbed {
			claimed[strings.ToLower(name)] = true
		}
	}
	for key := range routes {
		if !claimed[key] {
			t.Errorf("registered route has no implemented/stubbed manifest entry: %s", key)
		}
	}
}

// TestCoverageReport prints the scoreboard under -v; always passes.
func TestCoverageReport(t *testing.T) {
	counts := map[opStatus]int{}
	for _, status := range manifest {
		counts[status]++
	}
	t.Logf("subsonic api coverage: implemented=%d stubbed=%d unsupported=%d total=%d",
		counts[opImplemented], counts[opStubbed], counts[opUnsupported], len(manifest))
}
