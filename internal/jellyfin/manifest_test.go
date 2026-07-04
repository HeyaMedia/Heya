package jellyfin

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

//go:embed spec/jellyfin-openapi-10.11.11.json.gz
var specGz []byte

// specOperations parses the vendored spec into "METHOD /Path" keys.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(specGz))
	if err != nil {
		t.Fatalf("open vendored spec: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read vendored spec: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse vendored spec: %v", err)
	}
	ops := make(map[string]bool)
	for path, methods := range doc.Paths {
		for m := range methods {
			switch m {
			case "get", "post", "put", "delete", "head":
				ops[strings.ToUpper(m)+" "+path] = true
			}
		}
	}
	return ops
}

// TestManifestCoversSpec: every operation in the vendored Jellyfin spec must
// be consciously triaged. Adding a spec version bump without re-triaging
// fails here — that's the point.
func TestManifestCoversSpec(t *testing.T) {
	ops := specOperations(t)
	if len(ops) == 0 {
		t.Fatal("vendored spec parsed to zero operations")
	}
	for op := range ops {
		if _, ok := manifest[op]; !ok {
			t.Errorf("spec operation not triaged in manifest: %s", op)
		}
	}
	for op := range manifest {
		if !ops[op] {
			t.Errorf("manifest entry not present in vendored spec (typo?): %s", op)
		}
	}
}

// TestImplementedRoutesRegistered: implemented/stubbed manifest entries must
// have a registered route, and every registered route must be either in the
// manifest as implemented/stubbed or an acknowledged extra.
func TestImplementedRoutesRegistered(t *testing.T) {
	var s Server
	registered := make(map[string]bool)
	for _, pat := range s.buildRouter().patterns() {
		registered[pat] = true
	}

	for op, e := range manifest {
		switch e.Status {
		case opImplemented, opStubbed:
			if !registered[op] {
				t.Errorf("manifest says %s is implemented/stubbed but no route is registered", op)
			}
		case opPlanned, opOutOfScope:
			if registered[op] {
				t.Errorf("route %s is registered but manifest still says planned/out-of-scope — update the manifest", op)
			}
		}
	}
	for pat := range registered {
		if extraRoutes[pat] {
			continue
		}
		if e, ok := manifest[pat]; !ok {
			t.Errorf("registered route has no manifest entry and is not an extra: %s", pat)
		} else if e.Status != opImplemented && e.Status != opStubbed {
			t.Errorf("registered route %s has manifest status %d", pat, e.Status)
		}
	}
}

// TestCoverageReport prints the scoreboard under -v; always passes.
func TestCoverageReport(t *testing.T) {
	counts := map[opStatus]int{}
	byTag := map[string][4]int{}
	for _, e := range manifest {
		counts[e.Status]++
		c := byTag[e.Tag]
		c[e.Status]++
		byTag[e.Tag] = c
	}
	t.Logf("jellyfin api coverage: implemented=%d stubbed=%d planned=%d out_of_scope=%d total=%d",
		counts[opImplemented], counts[opStubbed], counts[opPlanned], counts[opOutOfScope], len(manifest))
}
