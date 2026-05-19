package worker

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

var (
	imageExts    = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}
	subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".sub": true, ".vtt": true}
	videoExts    = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".mov": true, ".m4v": true, ".wmv": true}
	lyricsExts   = map[string]bool{".lrc": true}

	imageAssetMap = map[string]sqlc.AssetType{
		"poster":     sqlc.AssetTypePoster,
		"fanart":     sqlc.AssetTypeFanart,
		"banner":     sqlc.AssetTypeBanner,
		"clearart":   sqlc.AssetTypeClearart,
		"clearlogo":  sqlc.AssetTypeClearlogo,
		"landscape":  sqlc.AssetTypeLandscape,
		"logo":       sqlc.AssetTypeLogo,
		"folder":     sqlc.AssetTypeFolder,
		"backdrop":   sqlc.AssetTypeBackdrop,
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
	DB *pgxpool.Pool
}

func (w *DetectLocalAssetsWorker) Work(ctx context.Context, job *river.Job[DetectLocalAssetsArgs]) error {
	q := sqlc.New(w.DB)
	filePath := job.Args.FilePath
	dir := filepath.Dir(filePath)
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	showDir := dir
	if strings.HasPrefix(strings.ToLower(filepath.Base(dir)), "season") {
		showDir = filepath.Dir(dir)
	}

	w.detectSiblingAssets(ctx, q, job.Args.MediaItemID, dir, base)
	w.detectShowLevelAssets(ctx, q, job.Args.MediaItemID, showDir)
	w.detectExtras(ctx, q, job.Args.MediaItemID, showDir)

	return nil
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
			log.Debug().Str("path", name).Str("lang", lang).Msg("found local subtitle")
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

func (w *DetectLocalAssetsWorker) detectShowLevelAssets(ctx context.Context, q *sqlc.Queries, mediaItemID int64, showDir string) {
	entries, err := os.ReadDir(showDir)
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
		fullPath := filepath.Join(showDir, name)

		if at, ok := imageAssetMap[nameNoExt]; ok {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   at,
				Source:      "local",
				LocalPath:   fullPath,
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
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeBackdrop,
				Source:      "local",
				LocalPath:   fullPath,
				SortOrder:   int32(order),
			})
			continue
		}

		if seasonPosterRE.MatchString(strings.ToLower(name)) {
			q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeSeasonPoster,
				Source:      "local",
				LocalPath:   fullPath,
				Label:       nameNoExt,
			})
		}
	}
}

func (w *DetectLocalAssetsWorker) detectExtras(ctx context.Context, q *sqlc.Queries, mediaItemID int64, showDir string) {
	entries, err := os.ReadDir(showDir)
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
						FilePath:    filepath.Join(showDir, name),
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

		extraDir := filepath.Join(showDir, e.Name())
		extraEntries, err := os.ReadDir(extraDir)
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
				FilePath:    filepath.Join(extraDir, ee.Name()),
				FileSize:    size,
			})
			log.Debug().Str("title", title).Str("type", string(extraType)).Msg("found extra")
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
