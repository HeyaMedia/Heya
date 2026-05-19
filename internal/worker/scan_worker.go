package worker

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/parser"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type ScanLibraryWorker struct {
	river.WorkerDefaults[ScanLibraryArgs]
	DB *pgxpool.Pool
}

func (w *ScanLibraryWorker) Work(ctx context.Context, job *river.Job[ScanLibraryArgs]) error {
	q := sqlc.New(w.DB)

	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return err
	}

	log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Msg("scanning library")

	discovered := make(map[string]bool)
	var newFiles []ProcessFileArgs

	for _, rootPath := range lib.Paths {
		filepath.WalkDir(rootPath, func(filePath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || skipDirs[name] {
					return filepath.SkipDir
				}
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !parser.IsMediaExtension(ext) {
				return nil
			}

			discovered[filePath] = true

			info, err := d.Info()
			if err != nil {
				return nil
			}

			if !job.Args.Force {
				existing, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
					LibraryID: lib.ID,
					Path:      filePath,
				})
				if err == nil && existing.Size == info.Size() && existing.Mtime.Valid && existing.Mtime.Time.Equal(info.ModTime()) && existing.DeletedAt.Time.IsZero() {
					return nil
				}
			}

			relPath, _ := filepath.Rel(rootPath, filePath)
			parsed := parser.ParseStoragePath(relPath)
			parseJSON, _ := json.Marshal(parsed)

			file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
				LibraryID:   lib.ID,
				Path:        filePath,
				Size:        info.Size(),
				Mtime:       pgtype.Timestamptz{Time: info.ModTime(), Valid: true},
				ParseResult: parseJSON,
				Status:      sqlc.FileStatusPending,
			})
			if err != nil {
				log.Error().Err(err).Str("path", filePath).Msg("upsert error")
				return nil
			}

			newFiles = append(newFiles, ProcessFileArgs{
				LibraryFileID: file.ID,
				LibraryID:     lib.ID,
				FilePath:      filePath,
			})

			return nil
		})
	}

	dbPaths, _ := q.ListAllLibraryFilePaths(ctx, lib.ID)
	var missing []string
	for _, p := range dbPaths {
		if !discovered[p] {
			if _, err := os.Stat(p); os.IsNotExist(err) {
				missing = append(missing, p)
			}
		}
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	if len(missing) > 0 {
		batchSize := 100
		for i := 0; i < len(missing); i += batchSize {
			end := i + batchSize
			if end > len(missing) {
				end = len(missing)
			}
			client.Insert(ctx, SoftDeleteArgs{
				LibraryID: lib.ID,
				Paths:     missing[i:end],
			}, nil)
		}
	}

	for _, f := range newFiles {
		client.Insert(ctx, f, nil)
	}

	log.Info().Int64("library_id", lib.ID).Int("discovered", len(discovered)).Int("new", len(newFiles)).Int("missing", len(missing)).Msg("scan complete")
	return nil
}

var skipDirs = map[string]bool{
	"@eaDir": true, "#recycle": true, ".Trash": true, "lost+found": true,
}
