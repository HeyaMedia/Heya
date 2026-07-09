package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
)

const (
	scanArtifactKindSearch = "search_result"
	scanArtifactKindFetch  = "fetch_result"
	scanArtifactSchemaV1   = int32(1)
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
	SchemaVersion int               `json:"schema_version"`
	ScopePaths    []string          `json:"scope_paths,omitempty"`
	Inventory     inventoryArtifact `json:"inventory"`
	Result        Result            `json:"result"`
}

type inventoryArtifact struct {
	Roots []inventoryRootArtifact `json:"roots"`
}

type inventoryRootArtifact struct {
	Root  string          `json:"root"`
	Files []InventoryFile `json:"files,omitempty"`
}

func PersistSearchArtifact(ctx context.Context, db *pgxpool.Pool, scanRunID int64, opts Options, result Result) error {
	return persistResultArtifact(ctx, db, scanRunID, scanArtifactKindSearch, opts, result)
}

func PersistFetchArtifact(ctx context.Context, db *pgxpool.Pool, scanRunID int64, opts Options, result Result) error {
	return persistResultArtifact(ctx, db, scanRunID, scanArtifactKindFetch, opts, result)
}

func persistResultArtifact(ctx context.Context, db *pgxpool.Pool, scanRunID int64, kind string, opts Options, result Result) error {
	if db == nil || scanRunID == 0 {
		return nil
	}
	data, err := marshalResultArtifact(kind, opts, result)
	if err != nil {
		return err
	}
	if _, err := sqlc.New(db).UpsertScanRunArtifact(ctx, sqlc.UpsertScanRunArtifactParams{
		ScanRunID:     scanRunID,
		Kind:          kind,
		ScopeKey:      scannerScopeKey(opts.ScopePaths),
		SchemaVersion: scanArtifactSchemaV1,
		Data:          data,
	}); err != nil {
		return fmt.Errorf("persist scanner %s artifact: %w", kind, err)
	}
	return nil
}

func LoadSearchArtifact(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, scanRunID int64) (Result, bool, error) {
	return loadResultArtifact(ctx, db, lib, opts, scanRunID, scanArtifactKindSearch)
}

func LoadFetchArtifact(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, scanRunID int64) (Result, bool, error) {
	return loadResultArtifact(ctx, db, lib, opts, scanRunID, scanArtifactKindFetch)
}

func loadResultArtifact(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, scanRunID int64, kind string) (Result, bool, error) {
	if db == nil {
		return Result{}, false, nil
	}
	q := sqlc.New(db)
	scopeKey := scannerScopeKey(opts.ScopePaths)
	var (
		row sqlc.ScanRunArtifact
		err error
	)
	if scanRunID > 0 {
		row, err = q.GetScanRunArtifact(ctx, sqlc.GetScanRunArtifactParams{
			ScanRunID: scanRunID,
			Kind:      kind,
			ScopeKey:  scopeKey,
		})
	} else {
		row, err = q.GetLatestScanRunArtifactByLibrary(ctx, sqlc.GetLatestScanRunArtifactByLibraryParams{
			LibraryID: lib.ID,
			MediaType: lib.MediaType,
			Kind:      kind,
			ScopeKey:  scopeKey,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{}, false, nil
		}
		return Result{}, false, fmt.Errorf("load scanner %s artifact: %w", kind, err)
	}
	result, err := unmarshalResultArtifact(kind, row.Data)
	if err != nil {
		return Result{}, false, err
	}
	result = filterResultToScopes(result, opts.ScopePaths, nil)
	return result, true, nil
}

func marshalSearchArtifact(opts Options, result Result) ([]byte, error) {
	return marshalResultArtifact(scanArtifactKindSearch, opts, result)
}

func marshalFetchArtifact(opts Options, result Result) ([]byte, error) {
	return marshalResultArtifact(scanArtifactKindFetch, opts, result)
}

func marshalResultArtifact(kind string, opts Options, result Result) ([]byte, error) {
	result = filterResultToScopes(result, opts.ScopePaths, nil)
	artifact := scanRunArtifact{
		SchemaVersion: int(scanArtifactSchemaV1),
		ScopePaths:    normalizedScopePaths(opts.ScopePaths),
		Inventory:     inventoryToArtifact(result.Inventory),
		Result:        result,
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

func acceptedSearchNeedsFetch(accepted bool, providerID string) bool {
	return accepted && strings.TrimSpace(providerID) != ""
}

func movieFetchCoverage(items []MovieFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) == "" && item.Detail == nil {
			continue
		}
		out[searchFetchKey(item.Key, item.ProviderID)] = true
	}
	return out
}

func bookFetchCoverage(items []BookFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) == "" && item.Detail == nil {
			continue
		}
		out[searchFetchKey(item.Key, item.ProviderID)] = true
	}
	return out
}

func musicFetchCoverage(items []MusicFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) == "" && item.Detail == nil {
			continue
		}
		out[searchFetchKey(item.Key, item.ProviderID)] = true
	}
	return out
}

func tvFetchCoverage(items []TVFetchPreview) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Error) == "" && item.Detail == nil {
			continue
		}
		out[searchFetchKey(item.Key, item.ProviderID)] = true
		for _, key := range item.Keys {
			out[searchFetchKey(key, item.ProviderID)] = true
		}
	}
	return out
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
