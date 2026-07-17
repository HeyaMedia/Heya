package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/mediafile"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

var (
	imageAssetMap = map[string]sqlc.AssetType{
		"poster":    sqlc.AssetTypePoster,
		"primary":   sqlc.AssetTypePoster,
		"fanart":    sqlc.AssetTypeBackdrop,
		"backdrop":  sqlc.AssetTypeBackdrop,
		"banner":    sqlc.AssetTypeBanner,
		"clearart":  sqlc.AssetTypeArt,
		"art":       sqlc.AssetTypeArt,
		"clearlogo": sqlc.AssetTypeLogo,
		"logo":      sqlc.AssetTypeLogo,
		"landscape": sqlc.AssetTypeThumb,
		"thumb":     sqlc.AssetTypeThumb,
		"disc":      sqlc.AssetTypeDisc,
		"discart":   sqlc.AssetTypeDisc,
		"cdart":     sqlc.AssetTypeDisc,
	}

	backdropRE     = regexp.MustCompile(`^backdrop(\d*)\.`)
	seasonPosterRE = regexp.MustCompile(`^season(\d+|specials|all)-poster\.`)
	thumbRE        = regexp.MustCompile(`-thumb\.`)
)

type DetectLocalAssetsWorker struct {
	river.WorkerDefaults[DetectLocalAssetsArgs]
	DB       *pgxpool.Pool
	DataDir  string
	Hub      EventPublisher
	Progress *TaskProgressBroadcaster
}

func (w *DetectLocalAssetsWorker) Work(ctx context.Context, job *river.Job[DetectLocalAssetsArgs]) error {
	q := sqlc.New(w.DB)
	filePath := job.Args.FilePath
	mediaType := job.Args.MediaType
	mediaItemID := job.Args.MediaItemID
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return nil
	}
	if filePath == "" && job.Args.LibraryFileID > 0 {
		if file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID); err == nil {
			filePath = file.Path
		}
	}
	if filePath == "" {
		files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
		if err == nil && len(files) > 0 {
			filePath = files[0].Path
		}
	}

	settings := metadata.LibrarySettings{UseLocalData: true}
	if lib, err := q.GetLibraryByID(ctx, item.LibraryID); err == nil {
		settings = metadata.ParseSettings(lib.Settings)
	}
	useLocalData := settings.UseLocalData

	current := item.Title
	if filePath != "" {
		current = vfs.Base(filePath)
	}
	w.Progress.SetCurrent(DetectLocalAssetsArgs{}.Kind(), job.Args.ScheduledTaskID, current)

	dirName := fmt.Sprintf("%d", mediaItemID)
	if item.Slug != "" {
		dirName = item.Slug
	}
	cacheDir := filepath.Join(w.DataDir, "images", mediaType, dirName)
	os.MkdirAll(cacheDir, 0o755)

	var source *vfs.Source
	dir := ""
	base := ""
	showDir := ""
	if filePath != "" {
		dir = vfs.Dir(filePath)
		base = strings.TrimSuffix(vfs.Base(filePath), filepath.Ext(vfs.Base(filePath)))
		showDir = dir
		if strings.HasPrefix(strings.ToLower(vfs.Base(dir)), "season") {
			showDir = vfs.Dir(dir)
		}
		var openErr error
		source, openErr = vfs.Open(showDir)
		if openErr != nil {
			log.Warn().Err(openErr).Str("dir", showDir).Msg("cannot open show directory for assets")
		}
	} else {
		log.Debug().Int64("media_item_id", mediaItemID).Msg("skipping local asset detection: no library file path")
	}

	assetsCreated := 0

	if source != nil && useLocalData {
		defer func() { _ = source.Close() }()
		assetsCreated += w.detectShowLevelImages(ctx, q, mediaItemID, source.FS, cacheDir)
	} else if source != nil {
		defer func() { _ = source.Close() }()
	}

	if filePath != "" && !vfs.IsSMBPath(dir) {
		// Local path: os.DirFS + vfs.Join ≡ os.ReadDir + filepath.Join here,
		// so the FS-based walker covers both the local and SMB shapes.
		assetsCreated += w.detectSiblingAssetsFS(ctx, q, mediaItemID, os.DirFS(dir), dir, base, useLocalData)
	} else if source != nil {
		relDir := vfs.Base(dir)
		if relDir != vfs.Base(showDir) {
			subFS, err := fs.Sub(source.FS, relDir)
			if err == nil {
				assetsCreated += w.detectSiblingAssetsFS(ctx, q, mediaItemID, subFS, dir, base, useLocalData)
			}
		}
	}

	posterPath := filepath.Join(cacheDir, "poster.jpg")
	backdropPath := filepath.Join(cacheDir, "backdrop.jpg")

	hasPoster := false
	hasBackdrop := false

	if source != nil && useLocalData {
		for _, name := range []string{"poster.jpg", "poster.png", "folder.jpg", "folder.png"} {
			if findAndCopyFS(source.FS, name, posterPath) != "" {
				hasPoster = true
				break
			}
		}
	}
	if source != nil && useLocalData {
		for _, name := range []string{"backdrop.jpg", "backdrop.png", "fanart.jpg", "fanart.png"} {
			if findAndCopyFS(source.FS, name, backdropPath) != "" {
				hasBackdrop = true
				break
			}
		}
	}

	newPoster := item.PosterPath
	newBackdrop := item.BackdropPath
	if hasPoster {
		newPoster = posterPath
	}
	if hasBackdrop {
		newBackdrop = backdropPath
	}

	pathsChanged := newPoster != item.PosterPath || newBackdrop != item.BackdropPath
	if pathsChanged {
		updateArtworkPathColumns(ctx, q, item, newPoster, newBackdrop)
		log.Info().Str("poster", newPoster).Str("backdrop", newBackdrop).Int64("media_id", mediaItemID).Msg("local images copied to cache")
	}

	if pathsChanged || assetsCreated > 0 {
		emit(w.Hub, eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: mediaItemID,
			LibraryID:   item.LibraryID,
			Title:       item.Title,
			MediaType:   mediaType,
		})
	}

	existingAssets, _ := q.ListMediaAssets(ctx, mediaItemID)
	hasAsset := map[string]bool{}
	for _, a := range existingAssets {
		key := string(a.AssetType)
		if a.Label != "" {
			key = a.Label
		}
		hasAsset[key] = true
	}

	// Record every remote image as a pending media_assets row (the serve path
	// can always fall back to remote_url), then warm the bytes eagerly through
	// download_image so first view is a local stat, not a synchronous fetch.
	// The row is created first and the job targets it by AssetID — see
	// DownloadImageArgs.AssetID for why creating rows at store time instead
	// would corrupt label groups.
	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, img := range job.Args.PendingImages {
		key := img.AssetType
		if img.Label != "" {
			key = img.Label
		}
		if img.AssetType == "poster" && img.SortOrder == 0 && hasPoster {
			continue
		}
		if img.AssetType == "backdrop" && img.SortOrder == 0 && hasBackdrop {
			continue
		}
		if hasAsset[key] {
			continue
		}

		var asset sqlc.MediaAsset
		var err error
		if SingleAssetTypes[img.AssetType] && img.Label == "" {
			asset, err = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetType(img.AssetType),
				Source:      "remote",
				RemoteUrl:   img.URL,
			})
		} else {
			asset, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetType(img.AssetType),
				Source:      "remote",
				RemoteUrl:   img.URL,
				Label:       img.Label,
				SortOrder:   int32(img.SortOrder),
			})
		}
		if err != nil {
			// ErrNoRows = the local-wins upsert guard (or a conflict) kept the
			// existing row — nothing to warm.
			if !errors.Is(err, pgx.ErrNoRows) {
				log.Debug().Err(err).Int64("media_item_id", mediaItemID).Str("asset_type", img.AssetType).Msg("pending image row insert skipped")
			}
			continue
		}
		if asset.LocalPath != "" {
			continue // upsert returned an already-local row
		}
		if _, err := client.Insert(ctx, DownloadImageArgs{
			MediaItemID: mediaItemID,
			EntityType:  "media",
			AssetID:     asset.ID,
			URL:         img.URL,
			AssetType:   img.AssetType,
			MediaType:   mediaType,
			Label:       img.Label,
			SortOrder:   img.SortOrder,
		}, &river.InsertOpts{Priority: img.Priority}); err != nil {
			return fmt.Errorf("enqueue download image: %w", err)
		}
	}

	// Secondary artwork (extra backdrops, logos, ...) is written directly at
	// enrich time from the detail response we already have (writeSecondaryArtwork)
	// — no separate FetchArtwork pass, which used to re-fetch the same doc.

	return nil
}

// detectShowLevelImages returns the number of media_assets rows it created,
// so the caller can decide whether to emit media.updated.
func (w *DetectLocalAssetsWorker) detectShowLevelImages(ctx context.Context, q *sqlc.Queries, mediaItemID int64, fsys fs.FS, cacheDir string) int {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return 0
	}

	existing, _ := q.ListMediaAssets(ctx, mediaItemID)
	seen := map[string]bool{}
	for _, a := range existing {
		// A remote row does not fill a local-data slot. If the sidecar exists,
		// the upsert below must be allowed to replace it.
		if a.Label == "" && a.Source == "local" && SingleAssetTypes[string(a.AssetType)] {
			seen[string(a.AssetType)] = true
		}
	}

	created := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !mediafile.IsImageExt(ext) {
			continue
		}

		nameNoExt := strings.TrimSuffix(strings.ToLower(name), ext)

		if at, ok := imageAssetMap[nameNoExt]; ok {
			key := string(at)
			if seen[key] {
				continue
			}
			seen[key] = true
			cacheName := nameNoExt + ext
			destPath := filepath.Join(cacheDir, cacheName)
			copyFromFS(fsys, name, destPath, false)

			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			var err error
			if SingleAssetTypes[string(at)] {
				_, err = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   at,
					Source:      "local",
					LocalPath:   destPath,
					FileSize:    size,
				})
			} else {
				if at == sqlc.AssetTypeBackdrop {
					err = ShiftMediaAssetSortOrders(ctx, q, mediaItemID, sqlc.AssetTypeBackdrop)
					if err != nil {
						log.Warn().Err(err).Int64("media_item_id", mediaItemID).Msg("make room for local backdrop failed")
						continue
					}
				}
				_, err = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
					MediaItemID: mediaItemID,
					AssetType:   at,
					Source:      "local",
					LocalPath:   destPath,
					FileSize:    size,
				})
			}
			if err == nil {
				created++
			}
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
			copyFromFS(fsys, name, destPath, false)
			if order == 0 {
				if err := ShiftMediaAssetSortOrders(ctx, q, mediaItemID, sqlc.AssetTypeBackdrop); err != nil {
					log.Warn().Err(err).Int64("media_item_id", mediaItemID).Msg("make room for numbered local backdrop failed")
					continue
				}
			}

			if _, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeBackdrop,
				Source:      "local",
				LocalPath:   destPath,
				SortOrder:   int32(order),
			}); err == nil {
				created++
			}
			continue
		}

		if m := seasonPosterRE.FindStringSubmatch(strings.ToLower(name)); m != nil {
			seasonLabel := "season-0"
			if m[1] != "specials" && m[1] != "all" {
				num := 0
				for _, c := range m[1] {
					num = num*10 + int(c-'0')
				}
				seasonLabel = fmt.Sprintf("season-%d", num)
			}

			key := "poster:" + seasonLabel
			if seen[key] {
				continue
			}
			seen[key] = true

			destPath := filepath.Join(cacheDir, name)
			copyFromFS(fsys, name, destPath, false)

			if _, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypePoster,
				Source:      "local",
				LocalPath:   destPath,
				Label:       seasonLabel,
			}); err == nil {
				created++
			}
		}
	}

	return created
}

// detectSiblingAssetsFS returns the number of media_assets rows it created,
// so the caller can decide whether to emit media.updated.
func (w *DetectLocalAssetsWorker) detectSiblingAssetsFS(ctx context.Context, q *sqlc.Queries, mediaItemID int64, fsys fs.FS, dir, baseName string, useLocalImages bool) int {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return 0
	}

	existing, _ := q.ListMediaAssets(ctx, mediaItemID)
	hasThumb := false
	for _, a := range existing {
		if a.AssetType == sqlc.AssetTypeThumb && a.Label == "" && a.Source == "local" {
			hasThumb = true
		}
	}

	created := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		nameNoExt := strings.TrimSuffix(name, filepath.Ext(name))
		fullPath := vfs.Join(dir, name)

		if mediafile.IsSubtitleExt(ext) && strings.HasPrefix(nameNoExt, baseName) {
			lang := extractLanguageCode(nameNoExt, baseName)
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			if _, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeSubtitle,
				Source:      "local",
				LocalPath:   fullPath,
				Language:    lang,
				FileSize:    size,
			}); err == nil {
				created++
			}
		}

		if mediafile.IsLyricsExt(ext) && strings.HasPrefix(nameNoExt, baseName) {
			if _, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeLyrics,
				Source:      "local",
				LocalPath:   fullPath,
			}); err == nil {
				created++
			}
		}

		if useLocalImages && mediafile.IsImageExt(ext) && thumbRE.MatchString(name) && strings.HasPrefix(name, baseName) {
			if hasThumb {
				continue
			}
			hasThumb = true
			if _, err := q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
				MediaItemID: mediaItemID,
				AssetType:   sqlc.AssetTypeThumb,
				Source:      "local",
				LocalPath:   fullPath,
			}); err == nil {
				created++
			}
		}
	}

	return created
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

// copyFromFS copies name from fsys to dst. With overwrite=false the copy
// bails when dst already exists — right for remote downloads. Local
// re-detection passes overwrite=true so a refresh picks up replacement files.
func copyFromFS(fsys fs.FS, name, dst string, overwrite bool) error {
	if !overwrite && fileExists(dst) {
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

func findAndCopyFS(fsys fs.FS, name, dst string) string {
	if err := copyFromFS(fsys, name, dst, false); err != nil {
		return ""
	}
	return dst
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
