package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/sabnzbd"
	"github.com/karbowiak/heya/internal/parser/video"
	"github.com/rs/zerolog/log"
)

// ManagerImportView reports what an import actually did: where files went,
// what the tagger matched, and the run id carrying the full record.
type ManagerImportView struct {
	RunID       int64    `json:"run_id"`
	MatchedItem int64    `json:"matched_item_id"`
	Title       string   `json:"matched_title"`
	Destination string   `json:"destination"`
	Moved       []string `json:"moved"`
	Skipped     []string `json:"skipped,omitempty"`
	ScanQueued  bool     `json:"scan_queued"`
}

// Media extensions per import domain; everything else (par2/nzb/sfv/nfo/
// sample junk) stays behind and is the user's cleanup.
var importExtensions = map[string]map[string]bool{
	"movie": {".mkv": true, ".mp4": true, ".avi": true, ".m2ts": true, ".ts": true,
		".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".sup": true},
	"tv": {".mkv": true, ".mp4": true, ".avi": true, ".m2ts": true, ".ts": true,
		".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".idx": true, ".sup": true},
	"music": {".flac": true, ".mp3": true, ".m4a": true, ".ogg": true, ".opus": true,
		".wav": true, ".alac": true, ".ape": true, ".wv": true, ".cue": true,
		".jpg": true, ".jpeg": true, ".png": true},
	"book": {".epub": true, ".azw3": true, ".mobi": true, ".pdf": true, ".cbz": true, ".cbr": true},
}

// ManagerImport moves a completed download's media files into the matched
// library item's folder and queues a scan — the first real write action in
// the manager. The download must be recognized by the auto-tagger (the
// same recognition the queue page shows); everything is recorded as a run.
func (a *App) ManagerImport(ctx context.Context, clientID int64, nzoID string) (*ManagerImportView, error) {
	q := sqlc.New(a.db)
	client, err := q.GetManagerDownloadClient(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("download client %d: %w", clientID, err)
	}
	if client.Kind != "sabnzbd" {
		return nil, fmt.Errorf("import not supported for client kind %q", client.Kind)
	}

	sab := sabnzbd.New(client.BaseUrl, client.ApiKey)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	history, err := sab.History(cctx, 200)
	cancel()
	if err != nil {
		return nil, err
	}
	var slot *sabnzbd.HistorySlot
	for i := range history {
		if history[i].NzoID == nzoID {
			slot = &history[i]
			break
		}
	}
	if slot == nil {
		return nil, fmt.Errorf("history entry %q: %w", nzoID, pgx.ErrNoRows)
	}
	if slot.Storage == "" {
		return nil, fmt.Errorf("history entry has no storage path — did the download fail?")
	}

	// Recognize through the same path the queue page uses.
	index, _, err := a.buildIdentityIndex(ctx, false)
	if err != nil {
		return nil, err
	}
	musicIdx, err := a.buildMusicQueueIndex(ctx)
	if err != nil {
		return nil, err
	}
	item := ManagerQueueItemView{
		Client: client.Name, ClientID: client.ID, NzoID: nzoID, Name: slot.Name,
		Category: slot.Category, Status: slot.Status,
		SizeMB: float64(slot.Bytes) / (1024 * 1024), History: true,
	}
	a.annotateQueueItem(ctx, q, client, index, musicIdx, &item)
	// Books aren't in the queue recognizer yet — try the containment index
	// before giving up.
	var bookRef *bookQueueRef
	if item.MatchedItemID == nil {
		bookIdx, berr := a.buildBookIdentityIndex(ctx, false)
		if berr == nil {
			if ref := bookIdx.match(slot.Name); ref != nil {
				bookRef = ref
				item.MatchedItemID = &ref.itemID
				item.MatchedTitle = ref.title
				item.MatchedLibrary = ref.libraryID
			}
		}
	}
	if item.MatchedItemID == nil {
		return nil, fmt.Errorf("release %q was not recognized — the tagger found no library match to import into", slot.Name)
	}

	var mediaType string
	if err := a.db.QueryRow(ctx, `SELECT media_type FROM media_items WHERE id = $1`,
		*item.MatchedItemID).Scan(&mediaType); err != nil {
		return nil, fmt.Errorf("matched item vanished: %w", err)
	}
	domain := managerProfileDomain(mediaType)

	destination, err := a.importDestination(ctx, *item.MatchedItemID, mediaType, item)
	if err != nil {
		return nil, err
	}

	var mappings []ManagerPathMapping
	_ = json.Unmarshal(client.PathMappings, &mappings)
	source := mapClientPath(slot.Storage, mappings)

	view := &ManagerImportView{
		MatchedItem: *item.MatchedItemID,
		Title:       item.MatchedTitle,
		Destination: destination,
	}

	// The move runs behind a hard timeout: a stale network mount must
	// degrade to an error, not wedge the request. Copy fallback for
	// cross-device moves can take a while — be generous.
	type moveResult struct {
		moved, skipped []string
		err            error
	}
	done := make(chan moveResult, 1)
	go func() {
		moved, skipped, merr := importMoveFiles(source, destination, importExtensions[domain])
		done <- moveResult{moved, skipped, merr}
	}()
	var moveErr error
	select {
	case res := <-done:
		view.Moved, view.Skipped, moveErr = res.moved, res.skipped, res.err
	case <-time.After(5 * time.Minute):
		moveErr = fmt.Errorf("import timed out — is the storage path reachable from this server?")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Record the run either way — imports are accountable actions.
	scope, _ := json.Marshal(map[string]any{
		"client": client.Name, "nzo_id": nzoID, "release": slot.Name,
		"media_item_id": *item.MatchedItemID, "title": item.MatchedTitle,
		"destination": destination, "source": source,
	})
	status := "completed"
	if moveErr != nil {
		status = "failed"
	}
	stats, _ := json.Marshal(map[string]any{
		"moved": len(view.Moved), "skipped": len(view.Skipped),
		"error": errText(moveErr),
	})
	run, rerr := q.CreateManagerRun(ctx, sqlc.CreateManagerRunParams{
		Kind: "import", Source: "api", Scope: scope,
	})
	if rerr == nil {
		view.RunID = run.ID
		_, _ = q.FinishManagerRun(ctx, sqlc.FinishManagerRunParams{
			ID: run.ID, Status: status, Stats: stats, Errors: []byte("[]"),
		})
	}
	if moveErr != nil {
		return view, moveErr
	}
	_ = bookRef

	// Hand the imported files to the normal pipeline: scan the library so
	// match/enrich runs exactly as if the files had appeared on disk.
	if len(view.Moved) > 0 {
		if serr := a.EnqueueScanLibraryDisk(ctx, item.MatchedLibrary); serr != nil {
			log.Warn().Err(serr).Int64("library", item.MatchedLibrary).Msg("import: scan enqueue failed")
		} else {
			view.ScanQueued = true
		}
	}
	a.notifyManagerChanged(ctx, "queue")
	return view, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// importDestination computes where a recognized download's files belong.
// The item's existing folder wins; fresh (fileless) items get a standard
// folder under the library's first root.
func (a *App) importDestination(ctx context.Context, itemID int64, mediaType string, item ManagerQueueItemView) (string, error) {
	var paths []string
	if err := a.db.QueryRow(ctx, `
		SELECT l.paths FROM libraries l
		JOIN media_items mi ON mi.library_id = l.id
		WHERE mi.id = $1`, itemID).Scan(&paths); err != nil {
		return "", fmt.Errorf("library root: %w", err)
	}
	if len(paths) == 0 || paths[0] == "" {
		return "", fmt.Errorf("the library has no configured path")
	}
	root := paths[0]

	var title, year string
	_ = a.db.QueryRow(ctx, `SELECT title, year FROM media_item_cards WHERE id = $1`, itemID).Scan(&title, &year)

	// Existing folder: deepest directory shared by the item's live files.
	var itemDir string
	var minPath, maxPath *string
	_ = a.db.QueryRow(ctx, `
		SELECT min(lf.path), max(lf.path) FROM library_files lf
		WHERE lf.media_item_id = $1 AND lf.deleted_at IS NULL`, itemID).Scan(&minPath, &maxPath)
	if minPath != nil && maxPath != nil {
		itemDir = commonDir(*minPath, *maxPath)
		// A degenerate common dir (library root itself) means files scatter;
		// fall back to a named folder.
		if itemDir == root || itemDir == "/" || itemDir == "" {
			itemDir = ""
		}
	}

	folderTitle := sanitizeFolderName(title)
	if year != "" && len(year) >= 4 {
		folderTitle = fmt.Sprintf("%s (%s)", folderTitle, year[:4])
	}

	switch mediaType {
	case "movie", "book":
		if itemDir != "" {
			return itemDir, nil
		}
		return filepath.Join(root, folderTitle), nil
	case "tv", "anime":
		seriesDir := itemDir
		if seriesDir == "" {
			seriesDir = filepath.Join(root, folderTitle)
		} else if strings.HasPrefix(filepath.Base(seriesDir), "Season") {
			seriesDir = filepath.Dir(seriesDir)
		}
		show := video.FilenameParseShow(item.Name)
		if len(show.Seasons) > 0 {
			return filepath.Join(seriesDir, fmt.Sprintf("Season %02d", show.Seasons[0])), nil
		}
		return seriesDir, nil
	case "music":
		artistDir := itemDir
		if artistDir == "" {
			artistDir = filepath.Join(root, sanitizeFolderName(item.MatchedTitle))
		}
		album := item.Parsed
		if idx := strings.Index(album, "—"); idx >= 0 {
			album = strings.TrimSpace(album[idx+len("—"):])
		} else {
			album = item.Name
		}
		return filepath.Join(artistDir, sanitizeFolderName(album)), nil
	default:
		return "", fmt.Errorf("import is not supported for %s items", mediaType)
	}
}

func sanitizeFolderName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "'", "<", "", ">", "", "|", "-")
	name = replacer.Replace(name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "Untitled"
	}
	return name
}

// importMoveFiles moves matching media files from the download folder into
// the destination, creating it as needed. Same-device moves are renames;
// cross-device falls back to copy+remove. Name collisions are skipped, not
// overwritten — an import must never clobber library files.
func importMoveFiles(source, destination string, wantExt map[string]bool) (moved, skipped []string, err error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, nil, fmt.Errorf("download folder not reachable: %w", err)
	}

	var candidates []string
	if info.IsDir() {
		walkErr := filepath.WalkDir(source, func(path string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil //nolint:nilerr // unreadable subpaths are skipped
			}
			candidates = append(candidates, path)
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	} else {
		candidates = []string{source}
	}

	sort.Strings(candidates)
	if err := os.MkdirAll(destination, 0o755); err != nil { //nolint:gosec // library folders are group/other-readable by design (media servers read them)
		return nil, nil, fmt.Errorf("creating destination: %w", err)
	}

	for _, path := range candidates {
		base := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(base))
		lower := strings.ToLower(base)
		if !wantExt[ext] || strings.Contains(lower, "sample") {
			skipped = append(skipped, base)
			continue
		}
		target := filepath.Join(destination, base)
		if _, serr := os.Stat(target); serr == nil {
			skipped = append(skipped, base+" (already exists)")
			continue
		}
		if merr := moveFile(path, target); merr != nil {
			return moved, skipped, fmt.Errorf("moving %s: %w", base, merr)
		}
		moved = append(moved, base)
	}
	if len(moved) == 0 {
		return moved, skipped, fmt.Errorf("no importable media files found in %s", source)
	}
	return moved, skipped, nil
}

func moveFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	// Cross-device (or exotic FS): copy then remove.
	in, err := os.Open(source) //nolint:gosec // path comes from the download client's own storage field, mapped + admin-triggered
	if err != nil {
		return err
	}
	defer in.Close()                                                          //nolint:errcheck // read side
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644) //nolint:gosec // destination derives from library config; O_EXCL forbids overwrite
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()           //nolint:errcheck,gosec // error path
		_ = os.Remove(target) // don't leave partials
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return os.Remove(source)
}
