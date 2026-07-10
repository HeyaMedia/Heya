package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/vfs"
)

type FileClass string

const (
	ClassPrimaryMedia FileClass = "primary_media"
	ClassExtraMedia   FileClass = "extra_media"
	ClassNFO          FileClass = "nfo"
	ClassPlexmatch    FileClass = "plexmatch"
	ClassArtwork      FileClass = "artwork"
	ClassSubtitle     FileClass = "subtitle"
	ClassLyrics       FileClass = "lyrics"
	ClassJunk         FileClass = "junk"
	ClassUnknown      FileClass = "unknown"
)

type InventoryFile struct {
	Root      string
	Path      string
	RelPath   string
	Name      string
	Ext       string
	Class     FileClass
	Kind      string
	AssetType string
	Size      int64
	MTime     time.Time
}

type InventoryRoot struct {
	Root  string
	FS    fs.FS
	Files []InventoryFile
}

type Inventory struct {
	Roots []InventoryRoot
}

type InventoryObserver struct {
	OnFile func(InventoryFile)
}

func WalkInventory(ctx context.Context, roots []string, emit Emitter) (Inventory, error) {
	return WalkInventoryWithObserver(ctx, roots, emit, nil)
}

func WalkInventoryWithObserver(ctx context.Context, roots []string, emit Emitter, observer *InventoryObserver) (Inventory, error) {
	return walkInventory(ctx, roots, nil, emit, observer)
}

func WalkInventoryScoped(ctx context.Context, roots []string, scopes []string, emit Emitter) (Inventory, error) {
	return WalkInventoryScopedWithObserver(ctx, roots, scopes, emit, nil)
}

func WalkInventoryScopedWithObserver(ctx context.Context, roots []string, scopes []string, emit Emitter, observer *InventoryObserver) (Inventory, error) {
	return walkInventory(ctx, roots, normalizedScopeDirs(scopes), emit, observer)
}

func walkInventory(ctx context.Context, roots []string, scopes []string, emit Emitter, observer *InventoryObserver) (Inventory, error) {
	var inv Inventory
	for _, root := range roots {
		relStarts := []string{"."}
		if len(scopes) > 0 {
			relStarts = scopeRelPathsForRoot(root, scopes)
			if len(relStarts) == 0 {
				continue
			}
		}

		data := map[string]any{}
		if len(scopes) > 0 {
			data["scopes"] = len(relStarts)
		}
		emit.Emit(Event{Event: "root.enter", Root: root, Data: data})
		source, err := vfs.Open(root)
		if err != nil {
			emit.Emit(Event{Event: "root.error", Severity: SeverityWarn, Root: root, Message: err.Error()})
			return inv, err
		}

		rootInv := InventoryRoot{Root: root, FS: source.FS}
		isSMB := vfs.IsSMBPath(root)
		seen := map[string]bool{}
		for _, relStart := range relStarts {
			if len(scopes) > 0 {
				if _, statErr := fs.Stat(source.FS, relStart); statErr != nil {
					emit.Emit(Event{Event: "walk.error", Severity: SeverityWarn, Root: root, RelPath: relStart, Message: statErr.Error()})
					continue
				}
			}
			err = fs.WalkDir(source.FS, relStart, func(relPath string, d fs.DirEntry, err error) error {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				if err != nil {
					emit.Emit(Event{Event: "walk.error", Severity: SeverityWarn, Root: root, RelPath: relPath, Message: err.Error()})
					return err
				}

				if d.IsDir() {
					if relPath == "." {
						return nil
					}
					name := d.Name()
					switch {
					case strings.HasPrefix(name, "."):
						emit.Emit(Event{Event: "dir.ignored", Root: root, RelPath: relPath, Kind: "directory", Reason: "hidden_directory"})
						return fs.SkipDir
					case mediafile.IsSkipDir(name):
						emit.Emit(Event{Event: "dir.ignored", Root: root, RelPath: relPath, Kind: "directory", Reason: "system_directory"})
						return fs.SkipDir
					case mediafile.IsExtrasDir(name):
						emit.Emit(Event{Event: "dir.extra", Root: root, RelPath: relPath, Kind: "directory", Reason: mediafile.ExtraTypeFromDir(name)})
						return nil
					default:
						return nil
					}
				}

				file := classifyFile(source.FS, root, relPath, d, isSMB)
				if seen[file.Path] {
					return nil
				}
				seen[file.Path] = true
				switch file.Class {
				case ClassJunk:
					emit.Emit(Event{Event: "file.ignored", Root: root, Path: file.Path, RelPath: file.RelPath, Kind: string(file.Class), Reason: "junk_file"})
					return nil
				case ClassUnknown:
					return nil
				default:
					rootInv.Files = append(rootInv.Files, file)
					if observer != nil && observer.OnFile != nil {
						observer.OnFile(file)
					}
					data := map[string]any{"class": string(file.Class)}
					if file.Kind != "" {
						data["kind"] = file.Kind
					}
					if file.AssetType != "" {
						data["asset_type"] = file.AssetType
					}
					if file.Size > 0 {
						data["size"] = file.Size
					}
					emit.Emit(Event{Event: "file.classified", Root: root, Path: file.Path, RelPath: file.RelPath, Kind: string(file.Class), Data: data})
					return nil
				}
			})
			if err != nil {
				break
			}
		}
		closeErr := source.Close()
		if err != nil {
			return inv, err
		}
		if closeErr != nil {
			return inv, closeErr
		}
		sort.Slice(rootInv.Files, func(i, j int) bool {
			return rootInv.Files[i].RelPath < rootInv.Files[j].RelPath
		})
		inv.Roots = append(inv.Roots, rootInv)
		emit.Emit(Event{Event: "root.complete", Root: root, Data: map[string]any{"files": len(rootInv.Files)}})
	}
	return inv, nil
}

func scopeRelPathsForRoot(root string, scopes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, scope := range scopes {
		rel, ok := scopeRelPathForRoot(root, scope)
		if !ok || seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func scopeRelPathForRoot(root, scope string) (string, bool) {
	root = strings.TrimRight(strings.TrimSpace(root), "/")
	scope = strings.TrimRight(strings.TrimSpace(scope), "/")
	if root == "" || scope == "" {
		return "", false
	}
	if strings.Contains(root, "://") || strings.Contains(scope, "://") {
		if scope != root && !strings.HasPrefix(scope, root+"/") {
			return "", false
		}
		rel := strings.TrimPrefix(scope, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return ".", true
		}
		return rel, true
	}
	rel, err := filepath.Rel(root, scope)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func FilterInventoryToScopes(inv Inventory, scopes []string, emit Emitter) Inventory {
	scopeDirs := normalizedScopeDirs(scopes)
	if len(scopeDirs) == 0 {
		return inv
	}

	var out Inventory
	totalBefore := 0
	totalAfter := 0
	for _, root := range inv.Roots {
		filtered := InventoryRoot{Root: root.Root, FS: root.FS}
		totalBefore += len(root.Files)
		for _, file := range root.Files {
			if inventoryFileInAnyScope(file, scopeDirs) {
				filtered.Files = append(filtered.Files, file)
			}
		}
		totalAfter += len(filtered.Files)
		out.Roots = append(out.Roots, filtered)
	}
	if emit != nil {
		emit.Emit(Event{
			Event: "scope.filtered",
			Data: map[string]any{
				"scopes": len(scopeDirs),
				"before": totalBefore,
				"after":  totalAfter,
			},
		})
	}
	return out
}

func normalizedScopeDirs(scopes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = normalizeScopeDir(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func normalizeScopeDir(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	if strings.Contains(scope, "://") {
		scope = strings.TrimRight(scope, "/")
		if scopePathLooksLikeFile(scope) {
			if idx := strings.LastIndex(scope, "/"); idx > strings.Index(scope, "://")+2 {
				scope = scope[:idx]
			}
		}
		return strings.TrimRight(scope, "/")
	}

	scope = filepath.Clean(scope)
	if info, err := os.Stat(scope); err == nil {
		if info.IsDir() {
			return scope
		}
		return scope
	}
	if scopePathLooksLikeFile(scope) {
		scope = filepath.Dir(scope)
	}
	return scope
}

func scopePathLooksLikeFile(scope string) bool {
	base := filepath.Base(scope)
	if strings.EqualFold(base, ".plexmatch") || isCanonicalNFO(strings.ToLower(base)) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(base))
	return parser.IsMediaExtension(ext) ||
		mediafile.IsImageExt(ext) ||
		mediafile.IsSubtitleExt(ext) ||
		mediafile.IsLyricsExt(ext) ||
		ext == ".nfo"
}

func inventoryPathInAnyScope(filePath string, scopes []string) bool {
	for _, scope := range scopes {
		if inventoryPathInScope(filePath, scope) {
			return true
		}
	}
	return false
}

func inventoryFileInAnyScope(file InventoryFile, scopes []string) bool {
	return inventoryPathInAnyScope(file.Path, scopes) || inventoryPathInAnyScope(file.RelPath, scopes)
}

func inventoryPathInScope(filePath, scope string) bool {
	if strings.Contains(filePath, "://") || strings.Contains(scope, "://") {
		filePath = strings.TrimRight(filePath, "/")
		scope = strings.TrimRight(scope, "/")
		return filePath == scope || strings.HasPrefix(filePath, scope+"/")
	}

	filePath = filepath.Clean(filePath)
	scope = filepath.Clean(scope)
	rel, err := filepath.Rel(scope, filePath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func classifyFile(fsys fs.FS, root, relPath string, d fs.DirEntry, isSMB bool) InventoryFile {
	name := d.Name()
	ext := strings.ToLower(filepath.Ext(name))
	class := ClassUnknown
	kind := ""
	assetType := ""

	switch {
	case mediafile.IsJunkFile(name):
		class = ClassJunk
	case strings.EqualFold(name, ".plexmatch"):
		class = ClassPlexmatch
	case isCanonicalNFO(name):
		class = ClassNFO
		kind, _, _ = nfo.CanonicalNFO(strings.ToLower(name))
	case mediafile.IsImageExt(ext):
		if at := imageAssetType(name); at != "" {
			class = ClassArtwork
			assetType = at
		}
	case mediafile.IsSubtitleExt(ext):
		class = ClassSubtitle
	case mediafile.IsLyricsExt(ext):
		class = ClassLyrics
	case parser.IsMediaExtension(ext) && mediafile.ExtraTypeFromPath(relPath) != "":
		class = ClassExtraMedia
		kind = mediafile.ExtraTypeFromPath(relPath)
	case parser.IsMediaExtension(ext):
		class = ClassPrimaryMedia
	}

	var fullPath string
	if isSMB {
		fullPath = vfs.Join(root, relPath)
	} else {
		fullPath = filepath.Join(root, relPath)
	}
	info, _ := d.Info()
	// Symlinked media must stat the TARGET, not the link: d.Info() is an
	// lstat, but a loose-file scope's scoped walk stats its root with fs.Stat
	// (which follows links), and it's the content's size/mtime that the
	// change detector should track. Mixing the two made symlinked files read
	// as "changed" on every scan, forever.
	if d.Type()&fs.ModeSymlink != 0 {
		if followed, err := fs.Stat(fsys, relPath); err == nil {
			info = followed
		}
	}
	var size int64
	var mtime time.Time
	if info != nil {
		size = info.Size()
		mtime = info.ModTime()
	}
	return InventoryFile{
		Root:      root,
		Path:      fullPath,
		RelPath:   relPath,
		Name:      name,
		Ext:       ext,
		Class:     class,
		Kind:      kind,
		AssetType: assetType,
		Size:      size,
		MTime:     mtime,
	}
}

func isCanonicalNFO(name string) bool {
	_, _, ok := nfo.CanonicalNFO(strings.ToLower(name))
	return ok
}

var (
	numberedBackdropRE = regexp.MustCompile(`^backdrop\d*$`)
	seasonPosterRE     = regexp.MustCompile(`^season(?:\d+|specials|all)-poster$`)
)

func imageAssetType(name string) string {
	base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	switch {
	case base == "poster" || base == "folder" || base == "cover" || base == "primary":
		return "poster"
	case base == "fanart" || base == "backdrop" || numberedBackdropRE.MatchString(base):
		return "backdrop"
	case base == "banner":
		return "banner"
	case base == "clearart" || base == "art":
		return "art"
	case base == "cdart" || base == "disc" || base == "discart":
		return "disc"
	case base == "clearlogo" || base == "logo":
		return "logo"
	case base == "landscape" || base == "thumb" || strings.HasSuffix(base, "-thumb"):
		return "thumb"
	case seasonPosterRE.MatchString(base):
		return "season_poster"
	default:
		return ""
	}
}
