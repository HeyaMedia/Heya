package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyametadata"
)

const (
	scanArtifactKindAnalyze = "analysis_result"
	scanArtifactKindSearch  = "search_result"
	scanArtifactKindFetch   = "fetch_result"
	scanArtifactSchemaV1    = int32(1)
	// PipelineRevision is semantic compatibility, not JSON wire compatibility.
	// Bump it whenever analysis/grouping rules change in a way that makes a
	// retained analysis unsafe to replay through a newer matcher.
	scanArtifactPipelineRevision = 4
	// PostgreSQL jsonb has a hard 256 MiB limit for the total size of array
	// elements. Keep every artifact comfortably below that boundary; scanner
	// entity artifacts are the durable hand-off for the worker pipeline.
	maxScanRunArtifactBytes = 64 << 20
)

// ArtifactTooLargeError is returned before a scanner artifact reaches
// PostgreSQL. It is permanent for the input and should not be retried unchanged.
type ArtifactTooLargeError struct {
	Kind  string
	Size  int
	Limit int
}

func (e *ArtifactTooLargeError) Error() string {
	return fmt.Sprintf("scanner %s artifact is %d bytes, exceeding the %d-byte safety limit; split the scan into owner scopes or use per-entity artifacts", e.Kind, e.Size, e.Limit)
}

type scanRunArtifact struct {
	SchemaVersion    int               `json:"schema_version"`
	PipelineRevision int               `json:"pipeline_revision,omitempty"`
	ScopePaths       []string          `json:"scope_paths,omitempty"`
	SourceSet        sourceSetArtifact `json:"source_set"`
	Inventory        inventoryArtifact `json:"inventory"`
	Result           Result            `json:"result"`
}

type inventoryArtifact struct {
	Roots []inventoryRootArtifact `json:"roots"`
}

type inventoryRootArtifact struct {
	Root  string          `json:"root"`
	Files []InventoryFile `json:"files,omitempty"`
}

func marshalSearchArtifact(opts Options, result Result) ([]byte, error) {
	return marshalResultArtifact(scanArtifactKindSearch, opts, result)
}

func marshalFetchArtifact(opts Options, result Result) ([]byte, error) {
	return marshalResultArtifact(scanArtifactKindFetch, opts, result)
}

func marshalResultArtifact(kind string, opts Options, result Result) ([]byte, error) {
	result = filterResultToScopes(result, opts.ScopePaths, nil)
	sourceSet := result.artifactSourceSet
	if len(sourceSet.Roots) == 0 {
		sourceSet = sourceSetFromInventory(result.Inventory, opts.ScopePaths)
	}
	artifact := scanRunArtifact{
		SchemaVersion:    int(scanArtifactSchemaV1),
		PipelineRevision: scanArtifactPipelineRevision,
		ScopePaths:       normalizedScopePaths(opts.ScopePaths),
		SourceSet:        sourceSet,
		Inventory:        inventoryToArtifact(result.Inventory),
		Result:           result,
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		return nil, fmt.Errorf("marshal scanner %s artifact: %w", kind, err)
	}
	if err := validateResultArtifactSize(kind, data, maxScanRunArtifactBytes); err != nil {
		return nil, err
	}
	return data, nil
}

func validateResultArtifactSize(kind string, data []byte, limit int) error {
	if limit > 0 && len(data) > limit {
		return &ArtifactTooLargeError{Kind: kind, Size: len(data), Limit: limit}
	}
	return nil
}

func unmarshalSearchArtifact(data []byte) (Result, error) {
	return unmarshalResultArtifact(scanArtifactKindSearch, data)
}

func unmarshalFetchArtifact(data []byte) (Result, error) {
	return unmarshalResultArtifact(scanArtifactKindFetch, data)
}

func unmarshalResultArtifact(kind string, data []byte) (Result, error) {
	var artifact scanRunArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return Result{}, fmt.Errorf("unmarshal scanner %s artifact: %w", kind, err)
	}
	if artifact.SchemaVersion != int(scanArtifactSchemaV1) {
		return Result{}, fmt.Errorf("unsupported scanner %s artifact schema version %d", kind, artifact.SchemaVersion)
	}
	result := artifact.Result
	result.Inventory = artifact.Inventory.toInventory()
	result.artifactSourceSet = artifact.SourceSet
	return result, nil
}

func inventoryToArtifact(inv Inventory) inventoryArtifact {
	out := inventoryArtifact{Roots: make([]inventoryRootArtifact, 0, len(inv.Roots))}
	for _, root := range inv.Roots {
		files := make([]InventoryFile, len(root.Files))
		copy(files, root.Files)
		out.Roots = append(out.Roots, inventoryRootArtifact{
			Root:  root.Root,
			Files: files,
		})
	}
	return out
}

func (a inventoryArtifact) toInventory() Inventory {
	out := Inventory{Roots: make([]InventoryRoot, 0, len(a.Roots))}
	for _, root := range a.Roots {
		files := make([]InventoryFile, len(root.Files))
		copy(files, root.Files)
		out.Roots = append(out.Roots, InventoryRoot{
			Root:  root.Root,
			Files: files,
		})
	}
	return out
}

func fetchMetadataCoversAcceptedSearch(result Result, lib sqlc.Library) bool {
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		metadata := movieFetchCoverage(result.MovieMetadata)
		for _, match := range result.MovieSearch {
			if acceptedSearchNeedsFetch(match.Accepted, match.ProviderID) && !metadata[searchFetchKey(match.Key, match.ProviderID)] {
				return false
			}
		}
	case lib.MediaType == sqlc.MediaTypeBook:
		metadata := bookFetchCoverage(result.BookMetadata)
		for _, match := range result.BookSearch {
			if acceptedSearchNeedsFetch(match.Accepted, match.ProviderID) && !metadata[searchFetchKey(match.Key, match.ProviderID)] {
				return false
			}
		}
	case lib.MediaType == sqlc.MediaTypeMusic:
		metadata := musicFetchCoverage(result.MusicMetadata)
		for _, match := range result.MusicSearch {
			if acceptedSearchNeedsFetch(match.Accepted, match.ProviderID) && !metadata[searchFetchKey(match.Key, match.ProviderID)] {
				return false
			}
		}
	case mediatype.IsTVLike(lib.MediaType):
		metadata := tvFetchCoverage(result.TVMetadata)
		for _, match := range result.TVSearch {
			if acceptedSearchNeedsFetch(match.Accepted, match.ProviderID) && !metadata[searchFetchKey(match.Key, match.ProviderID)] {
				return false
			}
		}
	}
	return true
}

// retainFetchMetadataForAcceptedSearch removes detail fetched for a provider
// that a newer manual decision no longer accepts. Coverage alone is
// insufficient when the accepted set becomes empty: without pruning, stale
// detail survives into materialization even though the overlaid search row is
// rejected.
func retainFetchMetadataForAcceptedSearch(result *Result, lib sqlc.Library) {
	if result == nil {
		return
	}
	allowed := map[string]bool{}
	addAllowed := func(key, providerID string) {
		if strings.TrimSpace(providerID) != "" {
			allowed[searchFetchKey(key, providerID)] = true
		}
	}
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		for _, match := range result.MovieSearch {
			if match.Accepted {
				addAllowed(match.Key, match.ProviderID)
			}
		}
		kept := result.MovieMetadata[:0]
		for _, item := range result.MovieMetadata {
			if fetchPreviewAllowed(allowed, item.Key, item.ProviderID, "", item.Detail) {
				kept = append(kept, item)
			}
		}
		result.MovieMetadata = kept
	case lib.MediaType == sqlc.MediaTypeBook:
		for _, match := range result.BookSearch {
			if match.Accepted {
				addAllowed(match.Key, match.ProviderID)
			}
		}
		kept := result.BookMetadata[:0]
		for _, item := range result.BookMetadata {
			if fetchPreviewAllowed(allowed, item.Key, item.ProviderID, "", item.Detail) {
				kept = append(kept, item)
			}
		}
		result.BookMetadata = kept
	case lib.MediaType == sqlc.MediaTypeMusic:
		for _, match := range result.MusicSearch {
			if match.Accepted {
				addAllowed(match.Key, match.ProviderID)
			}
		}
		kept := result.MusicMetadata[:0]
		for _, item := range result.MusicMetadata {
			if fetchPreviewAllowed(allowed, item.Key, item.ProviderID, item.SearchProviderID, item.Detail) {
				kept = append(kept, item)
			}
		}
		result.MusicMetadata = kept
	case mediatype.IsTVLike(lib.MediaType):
		for _, match := range result.TVSearch {
			if match.Accepted {
				addAllowed(match.Key, match.ProviderID)
			}
		}
		kept := result.TVMetadata[:0]
		for _, item := range result.TVMetadata {
			keys := item.Keys
			if len(keys) == 0 {
				keys = []string{item.Key}
			}
			for _, key := range keys {
				if fetchPreviewAllowed(allowed, key, item.ProviderID, "", item.Detail) {
					kept = append(kept, item)
					break
				}
			}
		}
		result.TVMetadata = kept
	}
}

func fetchPreviewAllowed(allowed map[string]bool, key, providerID, searchProviderID string, detail *metadata.MediaDetail) bool {
	if allowed[searchFetchKey(key, providerID)] || (searchProviderID != "" && allowed[searchFetchKey(key, searchProviderID)]) {
		return true
	}
	return detail != nil && strings.TrimSpace(detail.CanonicalID) != "" && allowed[searchFetchKey(key, heyametadata.EncodeEntityProviderID(detail.CanonicalID))]
}

func acceptedSearchNeedsFetch(accepted bool, providerID string) bool {
	return accepted && strings.TrimSpace(providerID) != ""
}

func movieFetchCoverage(items []MovieFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) != "" || item.Detail == nil {
			continue
		}
		addFetchCoverage(out, item.Key, item.ProviderID, item.Detail)
	}
	return out
}

func bookFetchCoverage(items []BookFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) != "" || item.Detail == nil {
			continue
		}
		addFetchCoverage(out, item.Key, item.ProviderID, item.Detail)
	}
	return out
}

func musicFetchCoverage(items []MusicFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) != "" || item.Detail == nil {
			continue
		}
		addFetchCoverage(out, item.Key, item.ProviderID, item.Detail)
		if item.SearchProviderID != "" {
			addFetchCoverage(out, item.Key, item.SearchProviderID, item.Detail)
		}
	}
	return out
}

func tvFetchCoverage(items []TVFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) != "" || item.Detail == nil {
			continue
		}
		addFetchCoverage(out, item.Key, item.ProviderID, item.Detail)
		for _, key := range item.Keys {
			addFetchCoverage(out, key, item.ProviderID, item.Detail)
		}
	}
	return out
}

func addFetchCoverage(out map[string]bool, key, providerID string, detail *metadata.MediaDetail) {
	out[searchFetchKey(key, providerID)] = true
	if detail == nil || strings.TrimSpace(detail.CanonicalID) == "" {
		return
	}
	// HeyaMetadata candidates are opaque workflow references. Resolving one
	// legitimately promotes it to a canonical entity UUID between fetch and
	// apply; both references describe the same accepted search decision.
	out[searchFetchKey(key, heyametadata.EncodeEntityProviderID(detail.CanonicalID))] = true
}

func searchFetchKey(key, providerID string) string {
	return strings.TrimSpace(key) + "\x00" + strings.TrimSpace(providerID)
}

func scannerScopeKey(scopePaths []string) string {
	paths := normalizedScopePaths(scopePaths)
	if len(paths) == 0 {
		return "all"
	}
	hash := sha256.Sum256([]byte(strings.Join(paths, "\x00")))
	return hex.EncodeToString(hash[:])
}

func normalizedScopePaths(scopePaths []string) []string {
	if len(scopePaths) == 0 {
		return nil
	}
	out := make([]string, 0, len(scopePaths))
	seen := map[string]bool{}
	for _, scope := range scopePaths {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}
