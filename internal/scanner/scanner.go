package scanner

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/nfo"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

var junkFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true, "desktop.ini": true,
	"theme.mp3": true, "theme.flac": true, "theme.ogg": true,
}

var skipDirNames = map[string]bool{
	"@eaDir": true, "#recycle": true, ".Trash": true, "lost+found": true,
}

var extrasDirNames = map[string]bool{
	"trailers": true, "trailer": true, "behind the scenes": true,
	"deleted scenes": true, "featurettes": true, "interviews": true,
	"scenes": true, "shorts": true, "other": true,
}

var nfoFiles = map[string]bool{
	"tvshow.nfo": true, "movie.nfo": true, "artist.nfo": true,
}

type Scanner struct {
	db *pgxpool.Pool
	q  *sqlc.Queries
}

func New(db *pgxpool.Pool) *Scanner {
	return &Scanner{db: db, q: sqlc.New(db)}
}

func (s *Scanner) ScanLibrary(ctx context.Context, lib sqlc.Library, opts ScanOptions) (ScanResult, error) {
	var result ScanResult
	discovered := make(map[string]bool)

	log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Str("type", string(lib.MediaType)).Int("paths", len(lib.Paths)).Msg("starting library scan")

	for _, rootPath := range lib.Paths {
		log.Info().Str("root", rootPath).Msg("scanning root path")
		if err := s.scanPath(ctx, lib.ID, rootPath, opts, &result, discovered); err != nil {
			log.Error().Err(err).Str("path", rootPath).Msg("error scanning path")
		}
	}

	deleted, err := s.detectDeletions(ctx, lib.ID, discovered)
	if err != nil {
		log.Error().Err(err).Msg("error detecting deletions")
	}
	result.Deleted = deleted

	log.Info().
		Int("discovered", result.Discovered).
		Int("new", result.New).
		Int("updated", result.Updated).
		Int("unchanged", result.Unchanged).
		Int("deleted", result.Deleted).
		Int("errors", result.Errors).
		Msg("scan complete")

	return result, nil
}

func (s *Scanner) scanPath(ctx context.Context, libraryID int64, rootPath string, opts ScanOptions, result *ScanResult, discovered map[string]bool) error {
	source, err := vfs.Open(rootPath)
	if err != nil {
		return err
	}
	defer source.Close()

	isSMB := vfs.IsSMBPath(rootPath)
	nfoCache := make(map[string]*nfo.ParsedNFO)

	return fs.WalkDir(source.FS, ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", relPath).Msg("walk error")
			result.Errors++
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			nameLower := strings.ToLower(name)
			if relPath != "." {
				if strings.HasPrefix(name, ".") || skipDirNames[name] || strings.HasSuffix(nameLower, ".trickplay") {
					log.Debug().Str("dir", relPath).Msg("skipping directory")
					return fs.SkipDir
				}
				if extrasDirNames[nameLower] {
					log.Debug().Str("dir", relPath).Msg("skipping extras directory (handled by asset detection)")
					return fs.SkipDir
				}
			}
			log.Debug().Str("dir", relPath).Msg("entering directory")

			parsed := nfo.FindAndParse(source.FS, relPath)
			if parsed != nil {
				nfoCache[relPath] = parsed
				log.Info().
					Str("dir", relPath).
					Str("kind", parsed.Kind).
					Str("title", parsed.Title).
					Str("tmdb", parsed.TMDBID).
					Str("imdb", parsed.IMDBID).
					Str("tvdb", parsed.TVDBID).
					Msg("NFO metadata found")
			}
			return nil
		}

		name := d.Name()
		if junkFiles[name] || nfoFiles[strings.ToLower(name)] {
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
			existing, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
				LibraryID: libraryID,
				Path:      fullPath,
			})
			if err == nil {
				if existing.DeletedAt.Valid {
					s.q.RestoreLibraryFile(ctx, existing.ID)
					log.Info().Str("file", relPath).Msg("restored previously soft-deleted file")
					result.New++
					return nil
				}
				if existing.Size == size && existing.Mtime.Valid && existing.Mtime.Time.Truncate(time.Microsecond).Equal(mtime.Truncate(time.Microsecond)) {
					s.syncTrickplayFlag(ctx, existing.ID, fullPath, existing.HasTrickplay)
					log.Debug().Str("file", relPath).Msg("unchanged, skipping")
					result.Unchanged++
					return nil
				}
			}
		}

		parsed := parser.ParseStoragePath(relPath)

		nfoData := findNFOForPath(nfoCache, relPath)

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

		moved, moveErr := s.q.GetDeletedFileBySize(ctx, sqlc.GetDeletedFileBySizeParams{
			LibraryID: libraryID,
			Size:      size,
		})
		if moveErr == nil && moved.ID > 0 {
			s.q.RelocateLibraryFile(ctx, sqlc.RelocateLibraryFileParams{
				ID:          moved.ID,
				Path:        fullPath,
				Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
				ParseResult: parseJSON,
			})
			log.Info().Str("from", moved.Path).Str("to", relPath).Int64("file_id", moved.ID).Msg("detected file move")
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
			log.Error().Err(upsertErr).Str("path", fullPath).Msg("error upserting file")
			result.Errors++
			return nil
		}

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
}

func (s *Scanner) detectDeletions(ctx context.Context, libraryID int64, discovered map[string]bool) (int, error) {
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
			toSoftDelete = append(toSoftDelete, dbPath)
		} else if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			toSoftDelete = append(toSoftDelete, dbPath)
		}
	}

	if len(toSoftDelete) > 0 {
		log.Info().Int("count", len(toSoftDelete)).Msg("soft-deleting missing files")
		for _, p := range toSoftDelete {
			log.Debug().Str("path", p).Msg("soft-deleting")
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

func findNFOForPath(cache map[string]*nfo.ParsedNFO, relPath string) *nfo.ParsedNFO {
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "" {
		if n, ok := cache[dir]; ok {
			return n
		}
		dir = filepath.Dir(dir)
	}
	if n, ok := cache["."]; ok {
		return n
	}
	return nil
}
