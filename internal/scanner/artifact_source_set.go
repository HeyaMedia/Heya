package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const artifactSourceSetWalkTimeout = 30 * time.Second

// sourceSetArtifact protects the negative space of an analysis artifact. The
// ordinary inventory snapshot proves that files which existed are unchanged;
// this digest additionally proves that no new identity source appeared while
// search/fetch/apply was parked.
type sourceSetArtifact struct {
	Roots []sourceSetRootArtifact `json:"roots,omitempty"`
}

type sourceSetRootArtifact struct {
	Root      string                       `json:"root"`
	RelStarts []string                     `json:"rel_starts"`
	Generated []sourceSetGeneratedArtifact `json:"generated,omitempty"`
	Count     int                          `json:"count"`
	SHA256    string                       `json:"sha256"`
}

type sourceSetGeneratedArtifact struct {
	RelPath string `json:"rel_path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

func sourceSetFromInventory(inv Inventory, scopePaths []string) sourceSetArtifact {
	out := sourceSetArtifact{Roots: make([]sourceSetRootArtifact, 0, len(inv.Roots))}
	scopes := normalizedScopeDirs(scopePaths)
	for _, root := range inv.Roots {
		relStarts := []string{"."}
		if len(scopes) > 0 {
			relStarts = scopeRelPathsForRoot(root.Root, scopes)
			if len(relStarts) == 0 {
				continue
			}
		}
		count, digest := digestRelevantSourceFiles(root.Files)
		generated := make([]sourceSetGeneratedArtifact, 0)
		for _, file := range root.Files {
			if !file.Generated || file.SourceSHA256 == "" {
				continue
			}
			generated = append(generated, sourceSetGeneratedArtifact{
				RelPath: filepath.ToSlash(filepath.Clean(file.RelPath)),
				Size:    file.Size,
				SHA256:  file.SourceSHA256,
			})
		}
		sort.Slice(generated, func(i, j int) bool { return generated[i].RelPath < generated[j].RelPath })
		out.Roots = append(out.Roots, sourceSetRootArtifact{
			Root:      root.Root,
			RelStarts: relStarts,
			Generated: generated,
			Count:     count,
			SHA256:    digest,
		})
	}
	sort.Slice(out.Roots, func(i, j int) bool {
		if out.Roots[i].Root != out.Roots[j].Root {
			return out.Roots[i].Root < out.Roots[j].Root
		}
		return strings.Join(out.Roots[i].RelStarts, "\x00") < strings.Join(out.Roots[j].RelStarts, "\x00")
	})
	return out
}

func validateArtifactSourceSet(ctx context.Context, db *pgxpool.Pool, expected sourceSetArtifact) error {
	if len(expected.Roots) == 0 {
		return &ArtifactReplayError{Reason: "artifact source-set guard is missing"}
	}
	ctx, cancel := context.WithTimeout(ctx, artifactSourceSetWalkTimeout)
	defer cancel()
	emit := NewEventSink(Event{})
	for _, root := range expected.Roots {
		if strings.TrimSpace(root.Root) == "" || len(root.RelStarts) == 0 || len(root.SHA256) != sha256.Size*2 {
			return &ArtifactReplayError{Reason: "artifact source-set guard is invalid", Path: root.Root}
		}
		scopes := make([]string, 0, len(root.RelStarts))
		for _, relStart := range root.RelStarts {
			if relStart == "." {
				scopes = nil
				break
			}
			scopes = append(scopes, filepath.Join(root.Root, relStart))
		}
		var (
			observed Inventory
			err      error
		)
		if len(scopes) == 0 {
			observed, err = WalkInventory(ctx, []string{root.Root}, emit)
		} else {
			observed, err = WalkInventoryScoped(ctx, []string{root.Root}, scopes, emit)
		}
		if err != nil {
			return &ArtifactReplayError{Reason: "source set cannot be inspected", Path: root.Root}
		}
		// Even without a provenance DB this hashes every sidecar. With a DB it
		// additionally excludes only exact locked Heya publications.
		if _, err := markGeneratedSidecars(ctx, db, 0, &observed); err != nil {
			return &ArtifactReplayError{Reason: "generated source set cannot be verified", Path: root.Root}
		}
		applyArtifactGeneratedSignatures(&observed, root.Generated)
		var files []InventoryFile
		for _, observedRoot := range observed.Roots {
			files = append(files, observedRoot.Files...)
		}
		count, digest := digestRelevantSourceFiles(files)
		if count != root.Count || digest != root.SHA256 {
			return &ArtifactReplayError{Reason: "identity-relevant source set changed", Path: root.Root}
		}
	}
	return nil
}

func applyArtifactGeneratedSignatures(inv *Inventory, signatures []sourceSetGeneratedArtifact) {
	if inv == nil || len(signatures) == 0 {
		return
	}
	expected := make(map[string]sourceSetGeneratedArtifact, len(signatures))
	for _, signature := range signatures {
		expected[signature.RelPath] = signature
	}
	for rootIndex := range inv.Roots {
		for fileIndex := range inv.Roots[rootIndex].Files {
			file := &inv.Roots[rootIndex].Files[fileIndex]
			signature, ok := expected[filepath.ToSlash(filepath.Clean(file.RelPath))]
			if ok && file.Size == signature.Size && file.SourceSHA256 == signature.SHA256 {
				file.Generated = true
			}
		}
	}
}

func digestRelevantSourceFiles(files []InventoryFile) (int, string) {
	entries := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Generated || !isArtifactIdentitySource(file.Class) {
			continue
		}
		relPath := filepath.ToSlash(filepath.Clean(file.RelPath))
		entry := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s",
			file.Class, relPath, file.Size, file.MTime.UTC().UnixMicro(), file.SourceSHA256)
		if _, duplicate := seen[entry]; duplicate {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	hasher := sha256.New()
	for _, entry := range entries {
		_, _ = fmt.Fprintln(hasher, entry)
	}
	return len(entries), hex.EncodeToString(hasher.Sum(nil))
}

func isArtifactIdentitySource(class FileClass) bool {
	return class == ClassPrimaryMedia || class == ClassNFO || class == ClassPlexmatch || class == ClassArtwork
}
