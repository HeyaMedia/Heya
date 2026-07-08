package scanner

import (
	"context"
	"io/fs"
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

func WalkInventory(ctx context.Context, roots []string, emit Emitter) (Inventory, error) {
	var inv Inventory
	for _, root := range roots {
		emit.Emit(Event{Event: "root.enter", Root: root})
		source, err := vfs.Open(root)
		if err != nil {
			emit.Emit(Event{Event: "root.error", Severity: SeverityWarn, Root: root, Message: err.Error()})
			return inv, err
		}

		rootInv := InventoryRoot{Root: root, FS: source.FS}
		isSMB := vfs.IsSMBPath(root)
		err = fs.WalkDir(source.FS, ".", func(relPath string, d fs.DirEntry, err error) error {
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

			file := classifyFile(root, relPath, d, isSMB)
			switch file.Class {
			case ClassJunk:
				emit.Emit(Event{Event: "file.ignored", Root: root, Path: file.Path, RelPath: file.RelPath, Kind: string(file.Class), Reason: "junk_file"})
				return nil
			case ClassUnknown:
				return nil
			default:
				rootInv.Files = append(rootInv.Files, file)
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
			if inventoryPathInAnyScope(file.Path, scopeDirs) {
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
		if filepath.Ext(scope) != "" {
			if idx := strings.LastIndex(scope, "/"); idx > strings.Index(scope, "://")+2 {
				scope = scope[:idx]
			}
		}
		return strings.TrimRight(scope, "/")
	}

	scope = filepath.Clean(scope)
	if filepath.Ext(scope) != "" {
		scope = filepath.Dir(scope)
	}
	return scope
}

func inventoryPathInAnyScope(filePath string, scopes []string) bool {
	for _, scope := range scopes {
		if inventoryPathInScope(filePath, scope) {
			return true
		}
	}
	return false
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

func classifyFile(root, relPath string, d fs.DirEntry, isSMB bool) InventoryFile {
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
