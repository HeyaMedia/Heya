package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

type Scanner struct {
	db *pgxpool.Pool
	q  *sqlc.Queries
}

func New(db *pgxpool.Pool) *Scanner {
	return &Scanner{db: db, q: sqlc.New(db)}
}

// knownFile is the preloaded DB state for one library file. Loading the whole
// library in one query and doing map lookups replaces the previous one
// SELECT per walked file.
type knownFile struct {
	id           int64
	size         int64
	mtime        pgtype.Timestamptz
	deleted      bool
	hasTrickplay bool
	hasNFO       bool
}

// nfoEntry is a canonical NFO file observed during the walk. The walk gets
// mtimes for free from the directory listing, so an edited NFO is detectable
// without ever opening it.
type nfoEntry struct {
	name  string // actual on-disk entry name
	kind  string // tvshow | movie | artist | album
	mtime time.Time
	prio  int
}

// nfoDirState mirrors a library_nfo_dirs row: the NFO that was last applied
// to the files beneath dir_path.
type nfoDirState struct {
	name  string
	mtime time.Time
}

func (s *Scanner) ScanLibrary(ctx context.Context, lib sqlc.Library, opts ScanOptions) (ScanResult, error) {
	var result ScanResult
	discovered := make(map[string]bool)
	failedRoots := 0
	var firstScanErr error

	log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Str("type", string(lib.MediaType)).Int("paths", len(lib.Paths)).Msg("starting library scan")

	// Force rescan re-upserts everything during the walk, so the preload map
	// would never be consulted — skip the query.
	known := map[string]knownFile{}
	if !opts.ForceRescan {
		var err error
		known, err = s.loadKnownFiles(ctx, lib.ID)
		if err != nil {
			return result, fmt.Errorf("preloading library files: %w", err)
		}
	}
	nfoState := s.loadNFODirState(ctx, lib.ID)
	upserted := make(map[string]bool)

	for _, rootPath := range lib.Paths {
		log.Info().Str("root", vfs.RedactPath(rootPath)).Msg("scanning root path")
		if err := s.scanPath(ctx, lib.ID, rootPath, opts, &result, discovered, known, nfoState, upserted); err != nil {
			failedRoots++
			if firstScanErr == nil {
				firstScanErr = err
			}
			log.Error().Err(err).Str("path", vfs.RedactPath(rootPath)).Msg("error scanning path")
		}
	}

	if ctx.Err() != nil {
		log.Warn().Err(ctx.Err()).Msg("skipping deletion detection after cancelled scan")
	} else {
		// Local-path deletion is authoritative via os.Stat regardless of whether
		// every root opened (a removed root makes its files stat as not-exist),
		// so it always runs. SMB paths can't be stat'd here, so they're only
		// soft-deleted via the discovered-set diff when every root scanned
		// cleanly — otherwise a transient mount outage would nuke the lot.
		smbTrustworthy := failedRoots == 0
		deleted, err := s.detectDeletions(ctx, lib.ID, discovered, smbTrustworthy)
		if err != nil {
			log.Error().Err(err).Msg("error detecting deletions")
		}
		result.Deleted = deleted
	}

	log.Info().
		Int("discovered", result.Discovered).
		Int("new", result.New).
		Int("updated", result.Updated).
		Int("unchanged", result.Unchanged).
		Int("deleted", result.Deleted).
		Int("errors", result.Errors).
		Msg("scan complete")

	if err := ctx.Err(); err != nil {
		return result, err
	}
	if failedRoots > 0 {
		return result, fmt.Errorf("scan incomplete: %d root path(s) failed: %w", failedRoots, firstScanErr)
	}
	return result, nil
}

func (s *Scanner) loadKnownFiles(ctx context.Context, libraryID int64) (map[string]knownFile, error) {
	rows, err := s.q.ListLibraryFilesForScan(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]knownFile, len(rows))
	for _, r := range rows {
		known[r.Path] = knownFile{
			id:           r.ID,
			size:         r.Size,
			mtime:        r.Mtime,
			deleted:      r.DeletedAt.Valid,
			hasTrickplay: r.HasTrickplay,
			hasNFO:       r.HasNfo,
		}
	}
	return known, nil
}

// loadNFODirState is best-effort: an empty map just means every NFO dir looks
// new this scan, which records rows without re-applying anything (files that
// already carry nfo data are left alone) — no storm, self-corrects next scan.
func (s *Scanner) loadNFODirState(ctx context.Context, libraryID int64) map[string]nfoDirState {
	rows, err := s.q.ListLibraryNFODirs(ctx, libraryID)
	if err != nil {
		log.Error().Err(err).Int64("library_id", libraryID).Msg("loading NFO dir state failed; treating as empty")
		return map[string]nfoDirState{}
	}
	state := make(map[string]nfoDirState, len(rows))
	var legacy []string
	for _, r := range rows {
		key := canonicalNFOKey(r.DirPath)
		if key != r.DirPath {
			legacy = append(legacy, r.DirPath)
		}
		entry := nfoDirState{name: r.NfoName, mtime: r.Mtime.Time}
		// On a key collision keep the OLDER mtime: a stale applied-marker only
		// costs a redundant re-apply, a fresh one swallows a pending edit.
		if cur, ok := state[key]; !ok || entry.mtime.Before(cur.mtime) {
			state[key] = entry
		}
	}
	if len(legacy) > 0 {
		s.rewriteLegacyNFOKeys(ctx, libraryID, legacy, state)
	}
	return state
}

// rewriteLegacyNFOKeys persists rows that were stored under a non-canonical
// key (the brief window where keys mirrored the walk's verbatim double-slash
// form) under their canonical key, then drops the legacy rows. Without the
// rewrite a canonical-key lookup misses them, the dir reads as brand new, and
// new-dir semantics (record without re-applying) swallow any pending NFO
// edit. Crash-safe ordering: canonical upserts land before legacy deletes,
// and a failed delete just re-normalizes on the next scan.
func (s *Scanner) rewriteLegacyNFOKeys(ctx context.Context, libraryID int64, legacy []string, state map[string]nfoDirState) {
	written := make(map[string]bool, len(legacy))
	for _, old := range legacy {
		key := canonicalNFOKey(old)
		if written[key] {
			continue
		}
		e := state[key]
		if err := s.q.UpsertLibraryNFODir(ctx, sqlc.UpsertLibraryNFODirParams{
			LibraryID: libraryID,
			DirPath:   key,
			NfoName:   e.name,
			Mtime:     pgtype.Timestamptz{Time: e.mtime, Valid: true},
		}); err != nil {
			log.Warn().Err(err).Str("dir", key).Msg("rewriting legacy NFO state key failed; will retry next scan")
			return
		}
		written[key] = true
	}
	if err := s.q.DeleteLibraryNFODirs(ctx, sqlc.DeleteLibraryNFODirsParams{
		LibraryID: libraryID,
		Column2:   legacy,
	}); err != nil {
		log.Warn().Err(err).Msg("deleting legacy NFO state keys failed; will retry next scan")
		return
	}
	log.Info().Int("rows", len(legacy)).Msg("normalized legacy NFO state keys")
}

// canonicalNFOKey maps any historical library_nfo_dirs key form onto today's
// canonical one: duplicate slashes collapsed (the smb:// scheme's own double
// slash excepted) and no trailing slash.
func canonicalNFOKey(key string) string {
	if rest, ok := strings.CutPrefix(key, "smb://"); ok {
		return "smb://" + strings.TrimRight(collapseSlashes(rest), "/")
	}
	if key == "/" {
		return key
	}
	return strings.TrimRight(collapseSlashes(key), "/")
}

func collapseSlashes(s string) string {
	if !strings.Contains(s, "//") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	var prev byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' && prev == '/' {
			continue
		}
		b.WriteByte(c)
		prev = c
	}
	return b.String()
}

// nfoResolver lazily finds and parses the governing NFO for a path, memoizing
// per directory (nil entries memoize "looked, nothing there"). On an
// unchanged rescan it is never consulted, so no NFO is opened at all.
type nfoResolver struct {
	fsys  fs.FS
	cache map[string]*nfo.ParsedNFO
}

func (r *nfoResolver) dir(dir string) *nfo.ParsedNFO {
	if p, ok := r.cache[dir]; ok {
		return p
	}
	p := nfo.FindAndParse(r.fsys, dir)
	r.cache[dir] = p
	return p
}

// forPath returns the nearest ancestor directory's NFO (starting at the
// file's own dir, ending at the root). Terminates at filepath.Dir's fixed
// point ("." for relative paths, "/" for rooted ones) so a malformed input
// can't spin the loop forever.
func (r *nfoResolver) forPath(relPath string) *nfo.ParsedNFO {
	dir := filepath.Dir(relPath)
	for {
		if p := r.dir(dir); p != nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// scanPathPrefixes returns the two path namespaces the NFO reconcile needs
// for one library root.
//
// filePrefix mirrors EXACTLY how the walk builds library_files paths — SMB is
// rootPath verbatim + "/" (a trailing-slash root yields a double slash, and
// stored paths carry it too), local is filepath.Join's cleaned form. Anything
// else and TrimPrefix leaves a rooted rel path, breaking seenNFOs lookups.
//
// rootKey is the canonical root (duplicate slashes collapsed, no trailing
// slash) used for library_nfo_dirs keys. It deliberately does NOT mirror the
// walk: state keys must stay identical across slash-config drift, or a rename
// of the key namespace would make every recorded row look brand new and
// silently swallow a pending NFO edit (new-dir semantics record without
// re-applying). canonicalNFOKey is the single definition of that form —
// loadNFODirState rewrites any legacy row onto it.
func scanPathPrefixes(rootPath string, isSMB bool) (filePrefix, rootKey string) {
	if isSMB {
		return rootPath + "/", canonicalNFOKey(rootPath)
	}
	clean := filepath.Clean(rootPath)
	return clean + "/", clean
}

// nearestNFODir walks up from fileDir to the nearest directory with a seen
// NFO. Same fixed-point termination as forPath: a rooted path (possible if a
// stored path's root prefix didn't round-trip cleanly) must fail the lookup,
// not hang the scan on Dir("/") == "/".
func nearestNFODir(fileDir string, seen map[string]nfoEntry) (string, bool) {
	dir := fileDir
	for {
		if _, ok := seen[dir]; ok {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (s *Scanner) scanPath(ctx context.Context, libraryID int64, rootPath string, opts ScanOptions, result *ScanResult, discovered map[string]bool, known map[string]knownFile, nfoState map[string]nfoDirState, upserted map[string]bool) error {
	source, err := vfs.Open(rootPath)
	if err != nil {
		return err
	}
	defer source.Close()

	isSMB := vfs.IsSMBPath(rootPath)
	resolver := &nfoResolver{fsys: source.FS, cache: make(map[string]*nfo.ParsedNFO)}
	seenNFOs := make(map[string]nfoEntry) // rel dir → canonical NFO present there

	walkErr := fs.WalkDir(source.FS, ".", func(relPath string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		if err != nil {
			log.Warn().Err(err).Str("path", relPath).Msg("walk error")
			result.Errors++
			return err
		}

		if d.IsDir() {
			name := d.Name()
			nameLower := strings.ToLower(name)
			if relPath != "." {
				if strings.HasPrefix(name, ".") || mediafile.IsSkipDir(name) {
					log.Debug().Str("dir", relPath).Msg("skipping directory")
					return fs.SkipDir
				}
				if mediafile.IsExtrasDir(nameLower) {
					log.Debug().Str("dir", relPath).Msg("skipping extras directory (handled by asset detection)")
					return fs.SkipDir
				}
			}
			log.Debug().Str("dir", relPath).Msg("entering directory")
			return nil
		}

		name := d.Name()
		if mediafile.IsJunkFile(name) {
			return nil
		}

		// Canonical NFOs aren't media, but their mtime (free — it comes with
		// the directory listing) is how edits get detected without opening
		// them. Parsing stays lazy: only a new/changed file pulls it in.
		if kind, prio, ok := nfo.CanonicalNFO(strings.ToLower(name)); ok {
			if info, ierr := d.Info(); ierr == nil {
				dir := filepath.Dir(relPath)
				if cur, exists := seenNFOs[dir]; !exists || prio < cur.prio {
					seenNFOs[dir] = nfoEntry{name: name, kind: kind, mtime: info.ModTime(), prio: prio}
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !parser.IsMediaExtension(ext) {
			return nil
		}

		var fullPath string
		if isSMB {
			fullPath = rootPath + "/" + relPath
		} else {
			fullPath = filepath.Join(rootPath, relPath)
		}

		result.Discovered++
		discovered[fullPath] = true

		if opts.OnProgress != nil {
			opts.OnProgress(result.Discovered, name)
		}

		info, err := d.Info()
		if err != nil {
			result.Errors++
			return nil
		}

		size := info.Size()
		mtime := info.ModTime()

		if !opts.ForceRescan {
			if existing, ok := known[fullPath]; ok {
				if existing.deleted {
					if err := s.q.RestoreLibraryFile(ctx, existing.id); err != nil {
						log.Error().Err(err).Str("file", relPath).Msg("error restoring soft-deleted file")
						result.Errors++
						return nil
					}
					log.Info().Str("file", relPath).Msg("restored previously soft-deleted file")
					result.New++
					return nil
				}
				if existing.size == size && existing.mtime.Valid && existing.mtime.Time.Truncate(time.Microsecond).Equal(mtime.Truncate(time.Microsecond)) {
					s.syncTrickplayFlag(ctx, existing.id, fullPath, existing.hasTrickplay)
					log.Debug().Str("file", relPath).Msg("unchanged, skipping")
					result.Unchanged++
					return nil
				}
			}
		}

		parsed := parser.ParseStoragePath(relPath)

		nfoData := resolver.forPath(relPath)

		parseData := map[string]any{
			"parsed": parsed,
		}
		if nfoData != nil {
			parseData["nfo"] = nfoData
		}

		parseJSON, err := json.Marshal(parseData)
		if err != nil {
			parseJSON = []byte("{}")
		}

		// Move detection: a recently soft-deleted file with the same size is a
		// relocate *candidate*, but size alone is too weak an identity — a new,
		// coincidentally same-sized file would inherit the deleted file's
		// media link and watch history. Moves preserve the basename and/or the
		// mtime, so require one of those on top (basename preferred: a move
		// across dirs keeps the name; a rename-in-place keeps the mtime).
		if moved := s.findMovedFile(ctx, libraryID, size, fullPath, mtime); moved != nil {
			s.q.RelocateLibraryFile(ctx, sqlc.RelocateLibraryFileParams{
				ID:          moved.ID,
				Path:        fullPath,
				Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
				ParseResult: parseJSON,
			})
			log.Info().Str("from", moved.Path).Str("to", relPath).Int64("file_id", moved.ID).Msg("detected file move")
			upserted[fullPath] = true
			result.Updated++
			return nil
		}

		_, upsertErr := s.q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   libraryID,
			Path:        fullPath,
			Size:        size,
			Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
			ParseResult: parseJSON,
			Status:      sqlc.FileStatusPending,
		})
		if upsertErr != nil {
			log.Error().Err(upsertErr).Str("path", vfs.RedactPath(fullPath)).Msg("error upserting file")
			result.Errors++
			return nil
		}
		upserted[fullPath] = true

		title := ""
		if parsed.Release != nil {
			title = parsed.Release.Title
		}

		nfoTitle := ""
		if nfoData != nil {
			nfoTitle = nfoData.Title
		}

		log.Info().
			Str("file", relPath).
			Int64("size", size).
			Str("media", string(parsed.Media)).
			Str("parsed_title", title).
			Str("nfo_title", nfoTitle).
			Msg("discovered media file")

		result.New++
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// Only reconcile after a clean walk — a partial walk has an incomplete
	// seenNFOs set, and acting on it would misread missing dirs as removed
	// NFOs and re-drive files for nothing.
	s.reconcileNFOs(ctx, libraryID, rootPath, isSMB, source.FS, seenNFOs, nfoState, known, discovered, upserted, result)
	return nil
}

// reconcileNFOs applies local-metadata changes to files whose bytes did not
// change: an edited NFO (mtime/name drift vs the recorded row), an NFO added
// after its files were scanned (file lacks nfo data), or a removed NFO
// (files re-resolve to an outer NFO or to none). Affected files get their
// parse_result rebuilt and flow back through the match pipeline as pending;
// everything else stays untouched, so a no-change rescan writes nothing.
func (s *Scanner) reconcileNFOs(ctx context.Context, libraryID int64, rootPath string, isSMB bool, fsys fs.FS, seenNFOs map[string]nfoEntry, nfoState map[string]nfoDirState, known map[string]knownFile, discovered map[string]bool, upserted map[string]bool, result *ScanResult) {
	prefix, rootKey := scanPathPrefixes(rootPath, isSMB)
	keyPrefix := rootKey + "/"
	dirFull := func(relDir string) string {
		if relDir == "." {
			return rootKey
		}
		return keyPrefix + relDir
	}
	relOfDir := func(full string) string {
		if full == rootKey {
			return "."
		}
		return strings.TrimPrefix(full, keyPrefix)
	}

	// Dirs whose recorded NFO differs from what the walk saw. Postgres stores
	// µs precision, so compare µs-truncated.
	changed := make(map[string]bool)
	seenFull := make(map[string]bool, len(seenNFOs))
	for relDir, e := range seenNFOs {
		full := dirFull(relDir)
		seenFull[full] = true
		if st, ok := nfoState[full]; ok {
			if st.name != e.name || !st.mtime.Truncate(time.Microsecond).Equal(e.mtime.Truncate(time.Microsecond)) {
				changed[relDir] = true
			}
		}
	}

	// Recorded NFO dirs under this root that no longer have an NFO on disk.
	var removedFull, removedRel []string
	for full := range nfoState {
		if full != rootKey && !strings.HasPrefix(full, keyPrefix) {
			continue
		}
		if !seenFull[full] {
			removedFull = append(removedFull, full)
			removedRel = append(removedRel, relOfDir(full))
		}
	}

	// Lazy parse of a seen NFO, at most once per dir. A nil parse marks the
	// dir failed: its files are skipped (don't wipe good local metadata over
	// a broken XML) and its state row is left alone so the next scan retries.
	parseCache := make(map[string]*nfo.ParsedNFO)
	failedParse := make(map[string]bool)
	parseSeen := func(relDir string) *nfo.ParsedNFO {
		if p, ok := parseCache[relDir]; ok {
			return p
		}
		e := seenNFOs[relDir]
		path := e.name
		if relDir != "." {
			path = relDir + "/" + e.name
		}
		p := nfo.ParseFile(fsys, path, e.kind)
		parseCache[relDir] = p
		if p == nil {
			failedParse[relDir] = true
			log.Warn().Str("dir", relDir).Str("nfo", e.name).Msg("NFO changed but failed to parse; leaving files untouched")
		}
		return p
	}
	underRemoved := func(fileDir string) bool {
		for _, rr := range removedRel {
			if rr == "." || fileDir == rr || strings.HasPrefix(fileDir, rr+"/") {
				return true
			}
		}
		return false
	}

	reapplied := 0
	for fullPath, kf := range known {
		if !discovered[fullPath] || upserted[fullPath] || !strings.HasPrefix(fullPath, prefix) {
			continue
		}
		rel := strings.TrimPrefix(fullPath, prefix)
		fileDir := filepath.Dir(rel)

		gov, hasGov := nearestNFODir(fileDir, seenNFOs)
		needs := underRemoved(fileDir) || (hasGov && (changed[gov] || !kf.hasNFO))
		if !needs {
			continue
		}

		var nfoData *nfo.ParsedNFO
		if hasGov {
			nfoData = parseSeen(gov)
			if nfoData == nil {
				continue // failed parse — keep the file's existing metadata
			}
		}

		parseData := map[string]any{
			"parsed": parser.ParseStoragePath(rel),
		}
		if nfoData != nil {
			parseData["nfo"] = nfoData
		}
		parseJSON, err := json.Marshal(parseData)
		if err != nil {
			parseJSON = []byte("{}")
		}
		if err := s.q.ReapplyLibraryFileParse(ctx, sqlc.ReapplyLibraryFileParseParams{
			ID:          kf.id,
			ParseResult: parseJSON,
		}); err != nil {
			log.Error().Err(err).Str("path", vfs.RedactPath(fullPath)).Msg("error re-applying NFO metadata")
			result.Errors++
			continue
		}
		upserted[fullPath] = true
		result.Updated++
		reapplied++
	}

	// Persist observed state: new/changed rows only, so a no-change rescan
	// issues zero writes. Failed parses stay unrecorded to retry next scan.
	for relDir, e := range seenNFOs {
		if failedParse[relDir] {
			continue
		}
		full := dirFull(relDir)
		if st, ok := nfoState[full]; ok && st.name == e.name && st.mtime.Truncate(time.Microsecond).Equal(e.mtime.Truncate(time.Microsecond)) {
			continue
		}
		if err := s.q.UpsertLibraryNFODir(ctx, sqlc.UpsertLibraryNFODirParams{
			LibraryID: libraryID,
			DirPath:   full,
			NfoName:   e.name,
			Mtime:     pgtype.Timestamptz{Time: e.mtime, Valid: true},
		}); err != nil {
			log.Error().Err(err).Str("dir", relDir).Msg("error recording NFO dir state")
			continue
		}
		nfoState[full] = nfoDirState{name: e.name, mtime: e.mtime}
	}
	if len(removedFull) > 0 {
		if err := s.q.DeleteLibraryNFODirs(ctx, sqlc.DeleteLibraryNFODirsParams{
			LibraryID: libraryID,
			Column2:   removedFull,
		}); err != nil {
			log.Error().Err(err).Msg("error deleting removed NFO dir state")
		} else {
			for _, full := range removedFull {
				delete(nfoState, full)
			}
		}
	}

	if reapplied > 0 || len(removedFull) > 0 || len(changed) > 0 {
		log.Info().
			Int("reapplied", reapplied).
			Int("changed_nfos", len(changed)).
			Int("removed_nfos", len(removedFull)).
			Msg("NFO reconcile applied local metadata changes")
	}
}

// findMovedFile returns the soft-deleted file this new path is a relocation of,
// or nil. Candidates share the byte size (7-day window, newest deletion first);
// the claim additionally needs a matching basename (move across dirs) or a
// matching mtime (rename in place) — size alone would let an unrelated
// same-sized file steal the deleted file's identity and watch history.
func (s *Scanner) findMovedFile(ctx context.Context, libraryID, size int64, fullPath string, mtime time.Time) *sqlc.LibraryFile {
	candidates, err := s.q.ListDeletedFilesBySize(ctx, sqlc.ListDeletedFilesBySizeParams{
		LibraryID: libraryID,
		Size:      size,
	})
	if err != nil || len(candidates) == 0 {
		return nil
	}
	base := filepath.Base(fullPath)
	for i := range candidates {
		if filepath.Base(candidates[i].Path) == base {
			return &candidates[i]
		}
	}
	want := mtime.Truncate(time.Microsecond)
	for i := range candidates {
		if candidates[i].Mtime.Valid && candidates[i].Mtime.Time.Truncate(time.Microsecond).Equal(want) {
			return &candidates[i]
		}
	}
	return nil
}

func (s *Scanner) detectDeletions(ctx context.Context, libraryID int64, discovered map[string]bool, smbTrustworthy bool) (int, error) {
	rows, err := s.q.ListAllLibraryFilePaths(ctx, libraryID)
	if err != nil {
		return 0, err
	}

	var toSoftDelete []string
	for _, dbPath := range rows {
		if discovered[dbPath] {
			continue
		}
		if vfs.IsSMBPath(dbPath) {
			// Can't stat SMB cheaply here; only trust the discovered-set diff
			// when the whole scan completed without a failed root.
			if smbTrustworthy {
				toSoftDelete = append(toSoftDelete, dbPath)
			}
		} else if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			toSoftDelete = append(toSoftDelete, dbPath)
		}
	}

	if len(toSoftDelete) > 0 {
		log.Info().Int("count", len(toSoftDelete)).Msg("soft-deleting missing files")
		for _, p := range toSoftDelete {
			log.Debug().Str("path", vfs.RedactPath(p)).Msg("soft-deleting")
		}
		err = s.q.SoftDeleteLibraryFilesByPath(ctx, sqlc.SoftDeleteLibraryFilesByPathParams{
			LibraryID: libraryID,
			Column2:   toSoftDelete,
		})
		if err != nil {
			return 0, err
		}
	}

	return len(toSoftDelete), nil
}

func (s *Scanner) syncTrickplayFlag(ctx context.Context, fileID int64, fullPath string, current bool) {
	vttPath := filepath.Join(filepath.Dir(fullPath), "trickplay", "index.vtt")
	_, err := os.Stat(vttPath)
	hasTrickplay := err == nil

	if hasTrickplay != current {
		s.q.UpdateLibraryFileTrickplay(ctx, sqlc.UpdateLibraryFileTrickplayParams{
			ID:           fileID,
			HasTrickplay: hasTrickplay,
		})
	}
}
