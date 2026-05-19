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
	"github.com/karbowiak/kura/internal/nfo"
	"github.com/karbowiak/kura/internal/parser"
	"github.com/karbowiak/kura/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

var skipDirs = map[string]bool{
	"@eaDir": true, "#recycle": true, ".Trash": true, "lost+found": true,
}

var extrasDirs = map[string]bool{
	"trailers": true, "trailer": true, "behind the scenes": true,
	"deleted scenes": true, "featurettes": true, "interviews": true,
	"scenes": true, "shorts": true, "other": true,
}

var junkFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true, "desktop.ini": true,
}

var nfoFileNames = map[string]bool{
	"tvshow.nfo": true, "movie.nfo": true, "artist.nfo": true,
}

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

	log.Info().Int64("library_id", lib.ID).Str("name", lib.Name).Str("type", string(lib.MediaType)).Msg("async scan starting")

	discovered := make(map[string]bool)
	var newFiles []ProcessFileArgs

	for _, rootPath := range lib.Paths {
		log.Info().Str("root", rootPath).Msg("scanning root path")

		source, err := vfs.Open(rootPath)
		if err != nil {
			log.Error().Err(err).Str("path", rootPath).Msg("error opening path")
			continue
		}

		isSMB := vfs.IsSMBPath(rootPath)
		nfoCache := make(map[string]*nfo.ParsedNFO)

		fs.WalkDir(source.FS, ".", func(relPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				log.Warn().Err(walkErr).Str("path", relPath).Msg("walk error")
				return nil
			}

			if d.IsDir() {
				name := d.Name()
				nameLower := strings.ToLower(name)
				if relPath != "." {
					if strings.HasPrefix(name, ".") || skipDirs[name] || strings.HasSuffix(nameLower, ".trickplay") {
						return fs.SkipDir
					}
					if extrasDirs[nameLower] {
						log.Debug().Str("dir", relPath).Msg("skipping extras directory")
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
						Msg("NFO metadata found")
				}
				return nil
			}

			name := d.Name()
			if junkFiles[name] || nfoFileNames[strings.ToLower(name)] {
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

			discovered[fullPath] = true

			info, err := d.Info()
			if err != nil {
				return nil
			}

			if !job.Args.Force {
				existing, err := q.GetLibraryFileByPath(ctx, sqlc.GetLibraryFileByPathParams{
					LibraryID: lib.ID,
					Path:      fullPath,
				})
				if err == nil && existing.Size == info.Size() && existing.Mtime.Valid && existing.Mtime.Time.Equal(info.ModTime()) && !existing.DeletedAt.Valid {
					log.Debug().Str("file", relPath).Msg("unchanged, skipping")
					return nil
				}
			}

			parsed := parser.ParseStoragePath(relPath)
			nfoData := findNFOForPath(nfoCache, relPath)

			parseData := map[string]any{"parsed": parsed}
			if nfoData != nil {
				parseData["nfo"] = nfoData
			}
			parseJSON, _ := json.Marshal(parseData)

			file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
				LibraryID:   lib.ID,
				Path:        fullPath,
				Size:        info.Size(),
				Mtime:       pgtype.Timestamptz{Time: info.ModTime(), Valid: true},
				ParseResult: parseJSON,
				Status:      sqlc.FileStatusPending,
			})
			if err != nil {
				log.Error().Err(err).Str("path", fullPath).Msg("upsert error")
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
				Int64("size", info.Size()).
				Str("media", string(parsed.Media)).
				Str("parsed_title", title).
				Str("nfo_title", nfoTitle).
				Msg("discovered media file")

			newFiles = append(newFiles, ProcessFileArgs{
				LibraryFileID: file.ID,
				LibraryID:     lib.ID,
				FilePath:      fullPath,
			})

			return nil
		})

		source.Close()
	}

	dbPaths, _ := q.ListAllLibraryFilePaths(ctx, lib.ID)
	var missing []string
	for _, p := range dbPaths {
		if discovered[p] {
			continue
		}
		if vfs.IsSMBPath(p) {
			missing = append(missing, p)
		} else if _, err := os.Stat(p); os.IsNotExist(err) {
			missing = append(missing, p)
		}
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	if len(missing) > 0 {
		log.Info().Int("count", len(missing)).Msg("soft-deleting missing files")
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

	log.Info().Int64("library_id", lib.ID).Int("discovered", len(discovered)).Int("new", len(newFiles)).Int("missing", len(missing)).Msg("async scan complete")
	return nil
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
