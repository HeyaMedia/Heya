package worker

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

var (
	imageExts    = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true}
	videoExts    = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".wmv": true}
	lyricsExts   = map[string]bool{".lrc": true}

	imageAssetMap = map[string]sqlc.AssetType{
		"poster":    sqlc.AssetTypePoster,
		"fanart":    sqlc.AssetTypeFanart,
		"banner":    sqlc.AssetTypeBanner,
		"clearart":  sqlc.AssetTypeClearart,
		"clearlogo": sqlc.AssetTypeClearlogo,
		"landscape": sqlc.AssetTypeLandscape,
		"logo":      sqlc.AssetTypeLogo,
		"folder":    sqlc.AssetTypeFolder,
		"backdrop":  sqlc.AssetTypeBackdrop,
		"disc":      sqlc.AssetTypeDisc,
		"discart":   sqlc.AssetTypeDisc,
		"cdart":     sqlc.AssetTypeDisc,
	}

	backdropRE     = regexp.MustCompile(`^backdrop(\d*)\.`)
	seasonPosterRE = regexp.MustCompile(`^season(\d+|specials|all)-poster\.`)
	thumbRE        = regexp.MustCompile(`-thumb\.`)

	extraFolders = map[string]sqlc.ExtraType{
		"trailers":          sqlc.ExtraTypeTrailer,
		"trailer":           sqlc.ExtraTypeTrailer,
		"behind the scenes": sqlc.ExtraTypeBehindTheScenes,
		"deleted scenes":    sqlc.ExtraTypeDeletedScene,
		"featurettes":       sqlc.ExtraTypeFeaturette,
		"interviews":        sqlc.ExtraTypeInterview,
		"scenes":            sqlc.ExtraTypeScene,
		"shorts":            sqlc.ExtraTypeShort,
		"other":             sqlc.ExtraTypeOther,
	}

	extraSuffixes = map[string]sqlc.ExtraType{
		"(trailer)":           sqlc.ExtraTypeTrailer,
		"(teaser)":            sqlc.ExtraTypeTeaser,
		"(behind the scenes)": sqlc.ExtraTypeBehindTheScenes,
		"(deleted scene)":     sqlc.ExtraTypeDeletedScene,
		"(featurette)":        sqlc.ExtraTypeFeaturette,
		"(interview)":         sqlc.ExtraTypeInterview,
		"(scene)":             sqlc.ExtraTypeScene,
		"(short)":             sqlc.ExtraTypeShort,
		"-trailer":            sqlc.ExtraTypeTrailer,
		"-teaser":             sqlc.ExtraTypeTeaser,
	}
)

type DetectLocalAssetsWorker struct {
	river.WorkerDefaults[DetectLocalAssetsArgs]
	DB      *pgxpool.Pool
	DataDir string
}

func (w *DetectLocalAssetsWorker) Work(ctx context.Context, job *river.Job[DetectLocalAssetsArgs]) error {
	q := sqlc.New(w.DB)
	filePath := job.Args.FilePath
	mediaType := job.Args.MediaType
	mediaItemID := job.Args.MediaItemID
	dir := vfs.Dir(filePath)
	base := strings.TrimSuffix(vfs.Base(filePath), filepath.Ext(vfs.Base(filePath)))

	showDir := dir
	if strings.HasPrefix(strings.ToLower(vfs.Base(dir)), "season") {
		showDir = vfs.Dir(dir)
	}

	cacheDir := filepath.Join(w.DataDir, "images", mediaType, fmt.Sprintf("%d", mediaItemID))
	os.MkdirAll(cacheDir, 0o755)

	source, err := vfs.Open(showDir)
	if err != nil {
		log.Warn().Err(err).Str("dir", showDir).Msg("cannot open show directory for assets")
	}

	if source != nil {
		defer source.Close()
		w.detectShowLevelImages(ctx, q, mediaItemID, source.FS, cacheDir)
		w.detectExtras(ctx, q, mediaItemID, source.FS, showDir)
	}

	if !vfs.IsSMBPath(dir) {
		w.detectSiblingAssets(ctx, q, mediaItemID, dir, base)
	} else if source != nil {
		relDir := vfs.Base(dir)
		if relDir != vfs.Base(showDir) {
			subFS, err := fs.Sub(source.FS, relDir)
			if err == nil {
				w.detectSiblingAssetsFS(ctx, q, mediaItemID, subFS, dir, base)
			}
		}
	}

	posterPath := filepath.Join(cacheDir, "poster.jpg")
	backdropPath := filepath.Join(cacheDir, "backdrop.jpg")

	hasPoster := fileExists(posterPath)
	hasBackdrop := fileExists(backdropPath)

	if !hasPoster && source != nil {
		for _, name := range []string{"poster.jpg", "poster.png", "folder.jpg", "folder.png"} {
			if findAndCopyFS(source.FS, name, posterPath) != "" {
				hasPoster = true
				break
			}
		}
	}
	if !hasBackdrop && source != nil {
		for _, name := range []string{"backdrop.jpg", "backdrop.png", "fanart.jpg", "fanart.png"} {
			if findAndCopyFS(source.FS, name, backdropPath) != "" {
				hasBackdrop = true
				break
			}
		}
	}

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return nil
	}

	newPoster := item.PosterPath
	newBackdrop := item.BackdropPath
	if hasPoster {
		newPoster = posterPath
	}
	if hasBackdrop {
		newBackdrop = backdropPath
	}

	if newPoster != item.PosterPath || newBackdrop != item.BackdropPath {
		q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
			ID:           item.ID,
			Title:        item.Title,
			SortTitle:    item.SortTitle,
			Year:         item.Year,
			Description:  item.Description,
			PosterPath:   newPoster,
			BackdropPath: newBackdrop,
			ExternalIds:  item.ExternalIds,
		})
		log.Info().Str("poster", newPoster).Str("backdrop", newBackdrop).Int64("media_id", mediaItemID).Msg("local images copied to cache")
	}

	return nil
}

func (w *DetectLocalAssetsWorker) detectShowLevelImages(ctx context.Context, q *sqlc.Queries, mediaItemID int64, fsys fs.FS, cacheDir string) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !imageExts[ext] {
			continue
		}

		nameNoExt := strings.TrimSuffix(strings.ToLower(name), ext)

		if at, ok := imageAssetMap[nameNoExt]; ok {
			cacheName := nameNoExt + ext
			destPath := filepath.Join(cacheDir, cacheName)
			copyFileFromFS(fsys, name, destPath)

			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   at,
				Source:      "local",
				LocalPath:   destPath,
				FileSize:    size,
			})
			continue
		}

		if m := backdropRE.FindStringSubmatch(strings.ToLower(name)); m != nil {
			order := 0
			if m[1] != "" {
				for _, c := range m[1] {
					order = order*10 + int(c-'0')
				}
			}
			destPath := filepath.Join(cacheDir, name)
			copyFileFromFS(fsys, name, destPath)

			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeBackdrop,
				Source:      "local",
				LocalPath:   destPath,
				SortOrder:   int32(order),
			})
			continue
		}

		if seasonPosterRE.MatchString(strings.ToLower(name)) {
			destPath := filepath.Join(cacheDir, name)
			copyFileFromFS(fsys, name, destPath)

			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeSeasonPoster,
				Source:      "local",
				LocalPath:   destPath,
				Label:       nameNoExt,
			})
		}
	}
}

func (w *DetectLocalAssetsWorker) detectSiblingAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, dir, baseName string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		nameNoExt := strings.TrimSuffix(name, filepath.Ext(name))
		fullPath := filepath.Join(dir, name)

		if subtitleExts[ext] && strings.HasPrefix(nameNoExt, baseName) {
			lang := extractLanguageCode(nameNoExt, baseName)
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeSubtitle,
				Source:      "local",
				LocalPath:   fullPath,
				Language:    lang,
				FileSize:    size,
			})
		}

		if lyricsExts[ext] && strings.HasPrefix(nameNoExt, baseName) {
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeLyrics,
				Source:      "local",
				LocalPath:   fullPath,
			})
		}

		if imageExts[ext] && thumbRE.MatchString(name) && strings.HasPrefix(name, baseName) {
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeThumb,
				Source:      "local",
				LocalPath:   fullPath,
			})
		}
	}
}

func (w *DetectLocalAssetsWorker) detectExtras(ctx context.Context, q *sqlc.Queries, mediaItemID int64, fsys fs.FS, showDir string) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !videoExts[ext] {
				continue
			}
			nameLower := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
			for suffix, extraType := range extraSuffixes {
				if strings.HasSuffix(nameLower, suffix) {
					title := strings.TrimSuffix(name, filepath.Ext(name))
					info, _ := e.Info()
					size := int64(0)
					if info != nil {
						size = info.Size()
					}
					q.CreateMediaExtra(ctx, sqlc.CreateMediaExtraParams{
						MediaItemID: mediaItemID,
						ExtraType:   extraType,
						Title:       title,
						FilePath:    vfs.Join(showDir, name),
						FileSize:    size,
					})
					break
				}
			}
			continue
		}

		folderName := strings.ToLower(e.Name())
		extraType, ok := extraFolders[folderName]
		if !ok {
			continue
		}

		extraEntries, err := fs.ReadDir(fsys, e.Name())
		if err != nil {
			continue
		}

		for _, ee := range extraEntries {
			if ee.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(ee.Name()))
			if !videoExts[ext] {
				continue
			}
			title := strings.TrimSuffix(ee.Name(), filepath.Ext(ee.Name()))
			info, _ := ee.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			q.CreateMediaExtra(ctx, sqlc.CreateMediaExtraParams{
				MediaItemID: mediaItemID,
				ExtraType:   extraType,
				Title:       title,
				FilePath:    vfs.Join(showDir, e.Name(), ee.Name()),
				FileSize:    size,
			})
			log.Debug().Str("title", title).Str("type", string(extraType)).Msg("found extra")
		}
	}
}

func (w *DetectLocalAssetsWorker) detectSiblingAssetsFS(ctx context.Context, q *sqlc.Queries, mediaItemID int64, fsys fs.FS, dir, baseName string) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		nameNoExt := strings.TrimSuffix(name, filepath.Ext(name))
		fullPath := vfs.Join(dir, name)

		if subtitleExts[ext] && strings.HasPrefix(nameNoExt, baseName) {
			lang := extractLanguageCode(nameNoExt, baseName)
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeSubtitle,
				Source:      "local",
				LocalPath:   fullPath,
				Language:    lang,
				FileSize:    size,
			})
		}

		if lyricsExts[ext] && strings.HasPrefix(nameNoExt, baseName) {
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeLyrics,
				Source:      "local",
				LocalPath:   fullPath,
			})
		}

		if imageExts[ext] && thumbRE.MatchString(name) && strings.HasPrefix(name, baseName) {
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeThumb,
				Source:      "local",
				LocalPath:   fullPath,
			})
		}
	}
}

func extractLanguageCode(nameNoExt, baseName string) string {
	suffix := strings.TrimPrefix(nameNoExt, baseName)
	suffix = strings.TrimPrefix(suffix, ".")
	parts := strings.Split(suffix, ".")
	if len(parts) >= 1 && len(parts[0]) >= 2 && len(parts[0]) <= 3 {
		return parts[0]
	}
	return ""
}

func copyFile(src, dst string) error {
	if fileExists(dst) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyFileFromFS(fsys fs.FS, name, dst string) error {
	if fileExists(dst) {
		return nil
	}
	in, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func findAndCopy(src, dst string) string {
	if !fileExists(src) {
		return ""
	}
	if err := copyFile(src, dst); err != nil {
		return ""
	}
	return dst
}

func findAndCopyFS(fsys fs.FS, name, dst string) string {
	if err := copyFileFromFS(fsys, name, dst); err != nil {
		return ""
	}
	return dst
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
