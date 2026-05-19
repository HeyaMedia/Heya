package scanner

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/parser"
	"github.com/rs/zerolog/log"
)

var skipDirs = map[string]bool{
	".":          true,
	"..":         true,
	"@eaDir":     true,
	"#recycle":   true,
	".Trash":     true,
	"lost+found": true,
}

var skipFiles = map[string]bool{
	".DS_Store":  true,
	"Thumbs.db":  true,
	"desktop.ini": true,
	".nfo":       true,
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

	for _, rootPath := range lib.Paths {
		if err := s.walkPath(ctx, lib.ID, rootPath, opts, &result, discovered); err != nil {
			log.Error().Err(err).Str("path", rootPath).Msg("error walking path")
		}
	}

	deleted, err := s.detectDeletions(ctx, lib.ID, discovered)
	if err != nil {
		log.Error().Err(err).Msg("error detecting deletions")
	}
	result.Deleted = deleted

	return result, nil
}

func (s *Scanner) walkPath(ctx context.Context, libraryID int64, rootPath string, opts ScanOptions, result *ScanResult, discovered map[string]bool) error {
	return filepath.WalkDir(rootPath, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors++
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()
		if skipFiles[name] {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if !parser.IsMediaExtension(ext) {
			return nil
		}

		result.Discovered++
		discovered[filePath] = true

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
				Path:      filePath,
			})
			if err == nil && existing.Size == size && existing.Mtime.Valid && existing.Mtime.Time.Equal(mtime) {
				result.Unchanged++
				return nil
			}
		}

		relPath, err := filepath.Rel(rootPath, filePath)
		if err != nil {
			relPath = filePath
		}
		parsed := parser.ParseStoragePath(relPath)

		parseJSON, err := json.Marshal(parsed)
		if err != nil {
			parseJSON = []byte("{}")
		}

		_, upsertErr := s.q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID:   libraryID,
			Path:        filePath,
			Size:        size,
			Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
			ParseResult: parseJSON,
			Status:      sqlc.FileStatusPending,
		})
		if upsertErr != nil {
			log.Error().Err(upsertErr).Str("path", filePath).Msg("error upserting file")
			result.Errors++
			return nil
		}

		if existing, err := s.q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
			LibraryID: libraryID,
			Path:      filePath,
		}); err == nil && existing.CreatedAt.Time.Before(existing.UpdatedAt.Time) {
			result.Updated++
		} else {
			result.New++
		}

		return nil
	})
}

func (s *Scanner) detectDeletions(ctx context.Context, libraryID int64, discovered map[string]bool) (int, error) {
	rows, err := s.q.ListAllLibraryFilePaths(ctx, libraryID)
	if err != nil {
		return 0, err
	}

	var toDelete []string
	for _, dbPath := range rows {
		if !discovered[dbPath] {
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				toDelete = append(toDelete, dbPath)
			}
		}
	}

	if len(toDelete) > 0 {
		err = s.q.DeleteLibraryFilesByPath(ctx, sqlc.DeleteLibraryFilesByPathParams{
			LibraryID: libraryID,
			Column2:   toDelete,
		})
		if err != nil {
			return 0, err
		}
	}

	return len(toDelete), nil
}
