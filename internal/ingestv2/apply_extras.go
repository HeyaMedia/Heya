package ingestv2

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

func applyMovieLocalExtras(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID int64, match MovieMatch, _ map[string][]InventoryFile, inv Inventory) (int, error) {
	dirs := map[string]bool{}
	for _, relPath := range match.Files {
		dir := cleanRelDir(filepath.Dir(relPath))
		if dir != "." {
			dirs[dir] = true
		}
	}
	return applyLocalExtraLinks(ctx, q, libraryID, mediaItemID, inventoryExtraFilesInDirs(inv, dirs))
}

func applyTVLocalExtras(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID int64, match TVMatch, _ map[string][]InventoryFile, inv Inventory) (int, error) {
	dirs := map[string]bool{}
	for _, plan := range match.Plans {
		for _, relPath := range plan.Files {
			dir := cleanRelDir(filepath.Dir(relPath))
			if isSeasonDir(filepath.Base(dir)) {
				dir = cleanRelDir(filepath.Dir(dir))
			}
			if dir != "." {
				dirs[dir] = true
			}
		}
		if plan.NFO != "" {
			dir := cleanRelDir(filepath.Dir(plan.NFO))
			if dir != "." {
				dirs[dir] = true
			}
		}
		if plan.Plexmatch != "" {
			dir := cleanRelDir(filepath.Dir(plan.Plexmatch))
			if dir != "." {
				dirs[dir] = true
			}
		}
	}
	return applyLocalExtraLinks(ctx, q, libraryID, mediaItemID, inventoryExtraFilesInDirs(inv, dirs))
}

func applyLocalExtraLinks(ctx context.Context, q *sqlc.Queries, libraryID, mediaItemID int64, extras []InventoryFile) (int, error) {
	created := 0
	for _, extra := range extras {
		file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   libraryID,
			Path:        extra.Path,
			Size:        extra.Size,
			Mtime:       pgtype.Timestamptz{Time: extra.MTime, Valid: !extra.MTime.IsZero()},
			ParseResult: extraLibraryFileParseResult(extra),
			Status:      sqlc.FileStatusPending,
		})
		if err != nil {
			return created, err
		}
		if err := q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:          file.ID,
			Status:      sqlc.FileStatusMatched,
			MediaItemID: pgInt8(mediaItemID),
		}); err != nil {
			return created, err
		}
		if err := q.DeleteLibraryFileLinksByFile(ctx, file.ID); err != nil {
			return created, err
		}
		if _, err := q.CreateLibraryFileExtraLink(ctx, sqlc.CreateLibraryFileExtraLinkParams{
			LibraryFileID: file.ID,
			MediaItemID:   mediaItemID,
			ExtraType:     normalizeExtraLinkType(extra.Kind),
			Title:         extraLinkTitle(extra),
			Source:        "scanner_v2",
			Confidence:    1,
			Metadata: mustJSONBytes(map[string]any{
				"scanner":  "ingestv2",
				"class":    string(extra.Class),
				"kind":     extra.Kind,
				"rel_path": extra.RelPath,
			}),
		}); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func inventoryExtraFilesInDirs(inv Inventory, dirs map[string]bool) []InventoryFile {
	if len(dirs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []InventoryFile
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if file.Class != ClassExtraMedia {
				continue
			}
			for dir := range dirs {
				if !relPathBelongsToDir(file.RelPath, dir) {
					continue
				}
				key := file.Path
				if key == "" {
					key = file.RelPath
				}
				if seen[key] {
					break
				}
				seen[key] = true
				out = append(out, file)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RelPath < out[j].RelPath
	})
	return out
}

func relPathBelongsToDir(relPath, dir string) bool {
	relPath = cleanRelDir(relPath)
	dir = cleanRelDir(dir)
	if dir == "." {
		return filepath.Dir(relPath) == "."
	}
	return relPath == dir || strings.HasPrefix(relPath, dir+string(filepath.Separator)) || strings.HasPrefix(relPath, dir+"/")
}

func cleanRelDir(dir string) string {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" {
		return "."
	}
	return dir
}

func normalizeExtraLinkType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "behindthescenes":
		return "behind_the_scenes"
	case "deleted":
		return "deleted_scene"
	case "featurette", "interview", "other", "sample", "scene", "short", "teaser", "trailer":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "other"
	}
}

func extraLinkTitle(file InventoryFile) string {
	base := strings.TrimSuffix(filepath.Base(file.RelPath), filepath.Ext(file.RelPath))
	base = strings.NewReplacer(".", " ", "_", " ").Replace(base)
	base = strings.TrimSpace(base)
	if base == "" {
		return normalizeExtraLinkType(file.Kind)
	}
	return strings.Join(strings.Fields(base), " ")
}

func extraLibraryFileParseResult(file InventoryFile) []byte {
	return mustJSONBytes(map[string]any{
		"scanner": "ingestv2",
		"extra": map[string]any{
			"type":     normalizeExtraLinkType(file.Kind),
			"kind":     file.Kind,
			"rel_path": file.RelPath,
		},
	})
}
