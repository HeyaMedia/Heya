package worker

import (
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// musicLocalAssets is the per-slot tally of artist art that landed locally.
// The enrich worker reads counts (not bools) so the backdrop pipeline can
// queue partial gap-fill — e.g. 2 local backdrops + 3 remote = 5 total.
//
// UsedURLs holds remote URLs already present in media_assets for this item
// — the orchestrator skips those when queueing new downloads so a refresh
// that ran once doesn't re-enqueue the same heya.media URL on the next
// pass.
type musicLocalAssets struct {
	Poster   int
	Backdrop int
	Logo     int
	Banner   int
	Clearart int
	Disc     int
	Thumb    int
	UsedURLs map[string]bool
}

// maxArtistBackdrops caps how many backdrop rows we keep per artist —
// local detection truncates here, and the remote gap-fill only queues up
// to (cap - local). Aligned with the plan in
// docs/plans/music-coverage-next.md.
const maxArtistBackdrops = 5

// detectLocalMusicAssets walks the artist's filesystem folder for Kodi-style
// art (folder.jpg, backdrop*.jpg, logo.png, banner.jpg, ...), copies any
// matches into the data dir, writes media_assets rows tagged source='local',
// and updates media_items.poster_path / backdrop_path for the primary
// (sort_order=0) assets.
//
// Returns which asset types were filled locally so the caller can skip
// enqueueing remote downloads for those slots.
//
// Best-effort throughout: each file failure is logged and skipped — the
// chain continues so a single unreadable image doesn't block the others.
func detectLocalMusicAssets(ctx context.Context, q *sqlc.Queries, dataDir string, mediaItemID int64, useLocalArtwork bool) musicLocalAssets {
	result := musicLocalAssets{UsedURLs: map[string]bool{}}

	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		log.Debug().Err(err).Int64("item_id", mediaItemID).Msg("detect local music: media item not found")
		return result
	}

	dirName := item.Slug
	if dirName == "" {
		dirName = strings.ToLower(strings.ReplaceAll(item.Title, " ", "-"))
	}

	// Existing locally-sourced assets so we don't re-copy or duplicate rows
	// on repeated enrich runs. Also remember remote URLs already downloaded
	// so the gap-fill orchestrator can skip them on the next pass.
	existing, _ := q.ListMediaAssets(ctx, mediaItemID)
	existingLocal := map[string]bool{}
	for _, a := range existing {
		if a.Source == "local" {
			key := string(a.AssetType)
			if a.Label != "" {
				key = a.Label
			}
			existingLocal[key] = true
		}
		if a.RemoteUrl != "" {
			result.UsedURLs[a.RemoteUrl] = true
		}
	}

	artistDir := ""
	if useLocalArtwork {
		artistDir = resolveArtistDir(ctx, q, mediaItemID)
		if artistDir == "" {
			log.Debug().Int64("item_id", mediaItemID).Msg("detect local music: no artist dir resolved")
		} else if source, err := vfs.Open(artistDir); err != nil {
			log.Debug().Err(err).Str("dir", artistDir).Msg("detect local music: cannot open artist dir")
		} else {
			defer source.Close() //nolint:errcheck // defer-close on vfs source

			cacheDir := filepath.Join(dataDir, "images", "music", dirName)
			if err := ensureDir(cacheDir); err != nil {
				log.Warn().Err(err).Str("cache_dir", cacheDir).Msg("detect local music: mkdir failed")
			} else {
				// Poster: pick the first Kodi-conventional file that exists.
				posterCandidates := []string{
					"folder.jpg", "folder.png",
					"poster.jpg", "poster.png",
					"artist.jpg", "artist.png",
				}
				if posterPath := copyFirstMatch(source.FS, posterCandidates, cacheDir, "poster"); posterPath != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypePoster, posterPath, 0, "", existingLocal)
					updateArtworkPathColumns(ctx, q, item, posterPath, item.BackdropPath)
					item.PosterPath = posterPath
					result.Poster++
				}

				// Backdrop: primary at sort_order=0, then numbered extras.
				backdropCandidates := []string{
					"backdrop.jpg", "backdrop.png",
					"fanart.jpg", "fanart.png",
				}
				primaryBackdrop := copyFirstMatch(source.FS, backdropCandidates, cacheDir, "backdrop")
				numbered := findNumberedExtras(source.FS, []string{"backdrop", "fanart"}, []string{".jpg", ".png"})
				if primaryBackdrop == "" && len(numbered) > 0 {
					first := numbered[0]
					dst := filepath.Join(cacheDir, "backdrop"+filepath.Ext(first))
					if err := copyFromFS(source.FS, first, dst, true); err == nil {
						primaryBackdrop = dst
						numbered = numbered[1:]
					}
				}
				if primaryBackdrop != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeBackdrop, primaryBackdrop, 0, "", existingLocal)
					updateArtworkPathColumns(ctx, q, item, item.PosterPath, primaryBackdrop)
					item.BackdropPath = primaryBackdrop
					result.Backdrop++
				}
				if room := maxArtistBackdrops - result.Backdrop; room < len(numbered) {
					if room < 0 {
						room = 0
					}
					numbered = numbered[:room]
				}
				for i, extra := range numbered {
					dst := filepath.Join(cacheDir, "backdrop"+strconv.Itoa(i+1)+filepath.Ext(extra))
					if err := copyFromFS(source.FS, extra, dst, true); err == nil {
						writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeBackdrop, dst, int32(i+1), "", existingLocal)
						result.Backdrop++
					}
				}

				if logoPath := copyFirstMatch(source.FS, []string{"logo.png", "clearlogo.png"}, cacheDir, "logo"); logoPath != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeLogo, logoPath, 0, "", existingLocal)
					result.Logo++
				}
				if bannerPath := copyFirstMatch(source.FS, []string{"banner.jpg", "banner.png", "landscape.jpg", "landscape.png"}, cacheDir, "banner"); bannerPath != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeBanner, bannerPath, 0, "", existingLocal)
					result.Banner++
				}
				if clearart := copyFirstMatch(source.FS, []string{"clearart.png"}, cacheDir, "clearart"); clearart != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeClearart, clearart, 0, "", existingLocal)
					result.Clearart++
				}
				if thumb := copyFirstMatch(source.FS, []string{"thumb.jpg", "thumb.png"}, cacheDir, "thumb"); thumb != "" {
					writeAsset(ctx, q, mediaItemID, sqlc.AssetTypeThumb, thumb, 0, "", existingLocal)
					result.Thumb++
				}
			}
		}
	}

	// Per-album passes: walk each album folder for covers (cover.jpg /
	// folder.jpg / front.jpg / disc.png / cdart.png) and bind sibling
	// lyrics files (.lrc / .elrc / .txt) onto their tracks.
	albumsScanned, coversFound, lyricsBound := scanAlbumAssets(ctx, q, dataDir, dirName, mediaItemID, useLocalArtwork)

	log.Info().
		Int64("item_id", mediaItemID).
		Str("artist_dir", artistDir).
		Int("poster", result.Poster).
		Int("backdrop", result.Backdrop).
		Int("logo", result.Logo).
		Int("banner", result.Banner).
		Int("clearart", result.Clearart).
		Int("thumb", result.Thumb).
		Int("albums_scanned", albumsScanned).
		Int("covers_found", coversFound).
		Int("lyrics_bound", lyricsBound).
		Msg("detect local music assets: scan complete")

	return result
}

// resolveArtistDir picks any library_file for the artist's media_item and
// walks up to its grandparent — that's the artist folder under the
// `<library>/<Artist>/<Album>/<file>` convention this codebase uses
// throughout the parser tests.
func resolveArtistDir(ctx context.Context, q *sqlc.Queries, mediaItemID int64) string {
	files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil || len(files) == 0 {
		return ""
	}
	// Pick the file with the *shallowest* path — that minimises the chance
	// of hitting nested disc folders ("Album/Disc 2/track.flac") which
	// would have us walk one extra level up by mistake.
	sort.Slice(files, func(i, j int) bool {
		return strings.Count(files[i].Path, "/") < strings.Count(files[j].Path, "/")
	})
	trackPath := files[0].Path
	albumDir := vfs.Dir(trackPath)
	artistDir := vfs.Dir(albumDir)
	if artistDir == "" || artistDir == "." || artistDir == "/" {
		return ""
	}
	return artistDir
}

// copyFirstMatch tries each candidate filename in the artist directory,
// copies the first one that exists into cacheDir as <baseName>.<ext>, and
// returns the destination path. Returns "" when nothing matched.
//
// Force-overwrites the destination — a stale heya.media download under the
// same name must be replaced by the user-provided art (local always wins).
func copyFirstMatch(fsys fs.FS, candidates []string, cacheDir, baseName string) string {
	for _, name := range candidates {
		if _, err := fs.Stat(fsys, name); err != nil {
			continue
		}
		dst := filepath.Join(cacheDir, baseName+filepath.Ext(name))
		if err := copyFromFS(fsys, name, dst, true); err != nil {
			log.Debug().Err(err).Str("src", name).Str("dst", dst).Msg("local asset copy failed")
			continue
		}
		return dst
	}
	return ""
}

// findNumberedExtras enumerates suffixed art files in the directory:
// backdrop1.jpg, backdrop2.jpg, fanart1.png, ... in numeric order. Used to
// pull *additional* art beyond the primary slot.
func findNumberedExtras(fsys fs.FS, prefixes, exts []string) []string {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	type ext struct {
		name string
		n    int
	}
	var found []ext
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		for _, p := range prefixes {
			for _, x := range exts {
				if !strings.HasPrefix(lower, p) || !strings.HasSuffix(lower, x) {
					continue
				}
				stem := strings.TrimSuffix(strings.TrimPrefix(lower, p), x)
				// Only the *numbered* variants (backdrop1, backdrop2). The
				// unsuffixed primary (backdrop.jpg) is handled by the
				// caller as sort_order=0.
				if stem == "" {
					continue
				}
				n := 0
				for _, r := range stem {
					if r < '0' || r > '9' {
						n = -1
						break
					}
					n = n*10 + int(r-'0')
				}
				if n <= 0 {
					continue
				}
				found = append(found, ext{name: name, n: n})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n < found[j].n })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.name
	}
	return out
}

func writeAsset(ctx context.Context, q *sqlc.Queries, mediaItemID int64, assetType sqlc.AssetType, localPath string, sortOrder int32, label string, existingLocal map[string]bool) {
	key := string(assetType)
	if label != "" {
		key = label
	}
	// For single-asset primary slots (poster, backdrop, logo, banner,
	// thumb, clearart at sort_order=0 with no label), local always wins
	// and supersedes any prior heya.media-sourced row. Clear remote rows
	// unconditionally — even when a local row already exists from a
	// previous run, lingering remote rows from an earlier remote-only
	// enrich should be evicted now that we have user art.
	if label == "" && SingleAssetTypes[string(assetType)] && sortOrder == 0 {
		existing, _ := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
			MediaItemID: mediaItemID,
			AssetType:   assetType,
		})
		for _, old := range existing {
			if old.Label != "" || old.SortOrder != 0 {
				continue
			}
			// Drop remotes outright; drop conflicting locals only when
			// the file path differs (the unique index would block the
			// insert otherwise).
			if old.Source != "local" || (existingLocal[key] && old.LocalPath != localPath) {
				_ = q.DeleteMediaAsset(ctx, old.ID)
			}
		}
	}
	if existingLocal[key] {
		return
	}
	if assetType == sqlc.AssetTypeBackdrop && label == "" && sortOrder == 0 {
		if err := ShiftMediaAssetSortOrders(ctx, q, mediaItemID, sqlc.AssetTypeBackdrop); err != nil {
			log.Warn().Err(err).Int64("item_id", mediaItemID).Msg("make room for local music backdrop failed")
			return
		}
	}
	if _, err := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: mediaItemID,
		AssetType:   assetType,
		Source:      "local",
		LocalPath:   localPath,
		Label:       label,
		SortOrder:   sortOrder,
	}); err != nil {
		log.Debug().Err(err).Int64("item_id", mediaItemID).Str("asset_type", string(assetType)).Msg("create local media_asset failed")
	}
	existingLocal[key] = true
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o750)
}

// scanAlbumAssets walks every album owned by the artist's media_item and
// pulls in per-album local art (cover.jpg / folder.jpg / front.jpg /
// disc.png / cdart.png) plus per-track lyrics sidecars (.lrc / .elrc /
// .txt). Returns (albumsScanned, coversFound, lyricsBound) for the parent
// log line — best-effort throughout, individual failures are logged at
// debug and ignored.
//
// Lyrics are NOT copied — `tracks.lyrics_path` points at the original
// sidecar next to the audio file (tiny text, no upside to duplicating into
// the data dir).
func scanAlbumAssets(ctx context.Context, q *sqlc.Queries, dataDir, artistSlug string, mediaItemID int64, useLocalArtwork bool) (int, int, int) {
	artist, err := q.GetArtistByMediaItemID(ctx, mediaItemID)
	if err != nil {
		return 0, 0, 0
	}
	albums, err := q.ListAlbumsByArtist(ctx, artist.ID)
	if err != nil || len(albums) == 0 {
		return 0, 0, 0
	}

	albumsScanned, coversFound, lyricsBound := 0, 0, 0
	for _, album := range albums {
		albumsScanned++

		tracks, err := q.ListTracksByAlbum(ctx, album.ID)
		if err != nil || len(tracks) == 0 {
			continue
		}

		// Album dirs: every distinct parent of a track. Albums whose
		// tracks were ingested from multiple folders (re-rips, mixed-
		// quality dupes) only get a cover if we scan *all* of them —
		// looking at just the first track's parent misses cover.jpg
		// when a sibling folder is the one carrying it.
		albumDirs := resolveAlbumDirs(tracks)
		if len(albumDirs) == 0 {
			continue
		}

		if useLocalArtwork {
			albumSlug := album.Slug
			if albumSlug == "" {
				albumSlug = strconv.FormatInt(album.ID, 10)
			}
			coverCacheDir := filepath.Join(dataDir, "images", "music", artistSlug, "albums", albumSlug)

			coverPath := ""
			for _, d := range albumDirs {
				coverPath = copyAlbumCover(d, coverCacheDir)
				if coverPath != "" {
					break
				}
			}
			// Embedded-art fallback: most rippers (Apple Music, Deezer, some
			// Bandcamp/Discogs flows) put art only inside the audio container
			// itself, not as a sidecar. Pull the first attached picture out
			// via ffmpeg when no folder image was found.
			if coverPath == "" {
				coverPath = extractEmbeddedCover(ctx, tracks, coverCacheDir)
			}

			if coverPath != "" {
				if album.CoverPath != coverPath {
					_ = q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{
						ID:        album.ID,
						CoverPath: coverPath,
					})
				}
				coversFound++
			}

			// Disc art (transparent CD/vinyl render): conventionally
			// disc.png / cdart.png / disc.jpg next to cover.jpg. We don't
			// have an "album-level media_assets" table — media_assets keys
			// on media_item_id, not album_id — so disc art landing in the
			// album cache dir means it's served as a sibling of cover.jpg
			// for the UI to pick up by predictable URL convention.
			for _, d := range albumDirs {
				copyAlbumDiscArt(d, coverCacheDir)
			}
		}

		// Per-track lyrics binding. For each track with a file_path,
		// look for a sibling lyrics sidecar matching the audio basename.
		for _, t := range tracks {
			if t.FilePath == "" {
				continue
			}
			lrc := findLyricsSidecar(t.FilePath)
			if lrc == "" || lrc == t.LyricsPath {
				continue
			}
			if err := q.UpdateTrackLyricsPath(ctx, sqlc.UpdateTrackLyricsPathParams{
				ID:         t.ID,
				LyricsPath: lrc,
			}); err == nil {
				lyricsBound++
			}
		}
	}
	return albumsScanned, coversFound, lyricsBound
}

// resolveAlbumDirs returns every distinct parent directory across the
// album's tracks, with "Disc N" / "CD N" intermediates collapsed to the
// album root. Used by the cover scanner so we can probe every candidate
// folder — albums whose tracks live in multiple sibling directories
// (e.g. user re-ripped at a different quality and kept both) still get
// a cover when only one of the folders carries the image.
func resolveAlbumDirs(tracks []sqlc.Track) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tracks {
		if t.FilePath == "" {
			continue
		}
		dir := vfs.Dir(t.FilePath)
		base := vfs.Base(dir)
		lower := strings.ToLower(base)
		if strings.HasPrefix(lower, "disc ") || strings.HasPrefix(lower, "disc-") || strings.HasPrefix(lower, "cd ") || strings.HasPrefix(lower, "cd-") {
			dir = vfs.Dir(dir)
		}
		if dir == "" || dir == "." || dir == "/" {
			continue
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out
}

// extractEmbeddedCover pulls the first attached picture (mjpeg / png
// stream tagged as attached_pic) out of any track via ffmpeg. Walks
// tracks in order until one yields an image — most albums where all
// tracks share an embedded cover succeed on the first try.
//
// Output lands at <cacheDir>/cover.jpg (jpeg is what every ripper uses
// for attached art in practice; PNG is rare enough that re-encoding via
// `-c:v copy` then trusting the .jpg extension is a fine trade for not
// branching on the source codec).
//
// Copy mode also makes this immune to art blocks whose declared MIME lies
// about the bytes (PNG tag, JPEG data) — the packet is written out without
// ever touching the (wrong) decoder, so the true bytes land in cover.jpg.
// The loudness/analysis pipelines need explicit -vn guards for those files;
// this path deliberately does not.
func extractEmbeddedCover(ctx context.Context, tracks []sqlc.Track, cacheDir string) string {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return ""
	}
	dst := filepath.Join(cacheDir, "cover.jpg")
	for _, t := range tracks {
		if t.FilePath == "" {
			continue
		}
		inputPath, cleanup, err := localFileForFFmpeg(t.FilePath)
		if err != nil {
			continue
		}
		// -an drops audio, -vcodec copy keeps the attached-pic frame
		// without re-encoding, -y overwrites the dst we just nuked.
		// ffmpeg yells when a file has no video stream, but that's a
		// normal "no embedded art" result here.
		cmd := exec.CommandContext(ctx, "ffmpeg", //nolint:gosec // inputPath comes from library_files; ffmpeg binary is fixed
			"-hide_banner", "-loglevel", "error",
			"-i", inputPath,
			"-an", "-vcodec", "copy",
			"-frames:v", "1",
			"-y", dst)
		runErr := cmd.Run()
		cleanup()
		if runErr == nil {
			if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
				return dst
			}
		}
	}
	// No track produced an embedded picture — clean up the empty file
	// ffmpeg may have left behind so the directory listing stays tidy.
	_ = os.Remove(dst)
	return ""
}

// localFileForFFmpeg returns a path ffmpeg can read directly. For local
// filesystem paths it's a no-op (returns the original path + a noop
// cleanup). For SMB-backed paths it spools the file to a temp location
// first since ffmpeg can't read smb:// URLs natively, and music files
// need random access (FLAC vorbis-comment pictures live near the header,
// M4A moov atoms can sit at either end).
//
// Returns (path, cleanup, err) where cleanup MUST be called by the caller
// to drop the temp copy when extraction is done. The cleanup is a no-op
// for local-fs inputs so callers don't need to branch.
func localFileForFFmpeg(srcPath string) (string, func(), error) {
	if !vfs.IsSMBPath(srcPath) {
		if _, err := os.Stat(srcPath); err != nil {
			return "", func() {}, err
		}
		return srcPath, func() {}, nil
	}
	// SMB path — spool to a temp file. Use vfs.Open + fs.Open against the
	// parent dir so the SMB connection setup happens through the shared
	// helper.
	dir := vfs.Dir(srcPath)
	name := vfs.Base(srcPath)
	source, err := vfs.Open(dir)
	if err != nil {
		return "", func() {}, err
	}
	in, err := source.FS.Open(name)
	if err != nil {
		_ = source.Close()
		return "", func() {}, err
	}
	tmp, err := os.CreateTemp("", "heya-art-*"+filepath.Ext(name))
	if err != nil {
		_ = in.Close()
		_ = source.Close()
		return "", func() {}, err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = in.Close()
		_ = source.Close()
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", func() {}, err
	}
	_ = in.Close()
	_ = tmp.Close()
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
		_ = source.Close()
	}
	return tmpPath, cleanup, nil
}

// copyAlbumDiscArt looks for the optional disc/CD/vinyl render
// (disc.png / cdart.png / discart.jpg) that some users keep next to
// cover.jpg. Lands at <cacheDir>/disc.<ext>. Best-effort and silent on
// miss — most libraries don't have this and that's fine.
func copyAlbumDiscArt(albumDir, cacheDir string) {
	candidates := []string{
		"disc.png", "disc.jpg", "disc.jpeg",
		"cdart.png", "cdart.jpg",
		"discart.png", "discart.jpg",
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return
	}
	source, err := vfs.Open(albumDir)
	if err != nil {
		return
	}
	defer source.Close() //nolint:errcheck // defer-close on vfs source
	_ = copyFirstMatch(source.FS, candidates, cacheDir, "disc")
}

// copyAlbumCover looks for the conventional album cover filenames in the
// album folder and copies the first match into the cache dir as cover.<ext>.
// Returns the destination path on success, "" on miss/failure.
//
// Multi-disc fallback: if the album root has no cover, scan each "Disc N"
// (or "CD N") subdir and use the first cover found. The convention used by
// most rippers (and by the user's library) is one cover.jpg per disc; the
// album-level art is whichever disc surfaces first.
func copyAlbumCover(albumDir, cacheDir string) string {
	// Conventional Kodi music album art filenames. cover.jpg is the
	// dominant one; folder.jpg is what most rippers default to (matches
	// Kodi's "use folder image" mode); front.jpg shows up on releases
	// dumped from Discogs/MusicBrainz tooling.
	candidates := []string{
		"cover.jpg", "cover.png", "cover.jpeg",
		"folder.jpg", "folder.png", "folder.jpeg",
		"front.jpg", "front.png",
		"albumart.jpg", "albumart.png",
	}

	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return ""
	}

	if source, err := vfs.Open(albumDir); err == nil {
		defer source.Close() //nolint:errcheck // defer-close on vfs source
		if dst := copyFirstMatch(source.FS, candidates, cacheDir, "cover"); dst != "" {
			return dst
		}
		// No root-level cover — peek into Disc/CD subdirs.
		if entries, err := fs.ReadDir(source.FS, "."); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				lower := strings.ToLower(e.Name())
				if !strings.HasPrefix(lower, "disc") && !strings.HasPrefix(lower, "cd") {
					continue
				}
				sub, err := fs.Sub(source.FS, e.Name())
				if err != nil {
					continue
				}
				if dst := copyFirstMatch(sub, candidates, cacheDir, "cover"); dst != "" {
					return dst
				}
			}
		}
	}
	return ""
}

// findLyricsSidecar returns the full path of a lyrics sidecar that sits
// next to the audio file (same basename, lyrics extension). Returns "" when
// no sidecar exists. Checked extensions in preference order:
//
//	.lrc   — synced timed lyrics (most common; what the player surfaces)
//	.elrc  — enhanced LRC with per-word timing
//	.txt   — unsynced plain lyrics
func findLyricsSidecar(audioPath string) string {
	if audioPath == "" {
		return ""
	}
	dir := vfs.Dir(audioPath)
	base := vfs.Base(audioPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// SMB paths can't be Stat'd directly with os.Stat — defer to vfs.Open
	// + fs.Stat against the parent dir. The local-fs case also works
	// through this since vfs.Open returns os.DirFS for non-smb paths.
	source, err := vfs.Open(dir)
	if err != nil {
		return ""
	}
	defer source.Close() //nolint:errcheck // defer-close on vfs source

	for _, lyricsExt := range []string{".lrc", ".elrc", ".txt"} {
		candidate := stem + lyricsExt
		if _, err := fs.Stat(source.FS, candidate); err == nil {
			return vfs.Join(dir, candidate)
		}
		// Some rippers uppercase the extension (.LRC). Cover that too.
		upper := stem + strings.ToUpper(lyricsExt)
		if _, err := fs.Stat(source.FS, upper); err == nil {
			return vfs.Join(dir, upper)
		}
	}
	return ""
}
