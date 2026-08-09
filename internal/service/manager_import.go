package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	managerformats "github.com/karbowiak/heya/internal/manager/formats"
	"github.com/karbowiak/heya/internal/manager/sabnzbd"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser/video"
	"github.com/karbowiak/heya/internal/worker"
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

type ManagerImportPlanFile struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	SizeBytes   int64  `json:"size_bytes"`
	Collision   bool   `json:"collision,omitempty"`
}

type ManagerImportPlanView struct {
	PlanID      string                  `json:"plan_id"`
	MatchedItem int64                   `json:"matched_item_id"`
	Title       string                  `json:"matched_title"`
	Destination string                  `json:"destination"`
	Files       []ManagerImportPlanFile `json:"files"`
	Skipped     []string                `json:"skipped,omitempty"`
}

type preparedManagerImport struct {
	client    sqlc.ManagerDownloadClient
	slot      sabnzbd.HistorySlot
	item      ManagerQueueItemView
	mediaType string
	domain    string
	source    string
	dest      string
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
func (a *App) ManagerImport(ctx context.Context, clientID int64, nzoID, expectedPlanID string) (*ManagerImportView, error) {
	prep, err := a.prepareManagerImport(ctx, clientID, nzoID)
	if err != nil {
		return nil, err
	}
	q := sqlc.New(a.db)
	plan, err := a.managerImportPlan(ctx, prep)
	if err != nil {
		return nil, err
	}
	if expectedPlanID == "" || expectedPlanID != plan.PlanID {
		return nil, errors.New("import plan changed or was not confirmed — review the current plan before importing")
	}

	view := &ManagerImportView{
		MatchedItem: *prep.item.MatchedItemID,
		Title:       prep.item.MatchedTitle,
		Destination: plan.Destination,
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
		moved, skipped, merr := importMovePlan(plan.Files, plan.Skipped)
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
		"client": prep.client.Name, "nzo_id": nzoID, "release": prep.slot.Name,
		"media_item_id": *prep.item.MatchedItemID, "title": prep.item.MatchedTitle,
		"destination": plan.Destination, "source": prep.source,
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
	// Hand the imported files to the normal pipeline: scan the library so
	// match/enrich runs exactly as if the files had appeared on disk.
	if len(view.Moved) > 0 {
		if serr := worker.EnqueueProcessLibraryScan(ctx, a.river, a.db, worker.ProcessLibraryScanArgs{
			LibraryID: prep.item.MatchedLibrary, ScopePaths: []string{plan.Destination},
		}, worker.PriorityScan, "manager_import"); serr != nil {
			log.Warn().Err(serr).Int64("library", prep.item.MatchedLibrary).Msg("import: scan enqueue failed")
		} else {
			view.ScanQueued = true
		}
	}
	a.notifyManagerChanged(ctx, "queue")
	return view, nil
}

func (a *App) ManagerImportPlan(ctx context.Context, clientID int64, nzoID string) (*ManagerImportPlanView, error) {
	prep, err := a.prepareManagerImport(ctx, clientID, nzoID)
	if err != nil {
		return nil, err
	}
	return a.managerImportPlan(ctx, prep)
}

func (a *App) prepareManagerImport(ctx context.Context, clientID int64, nzoID string) (*preparedManagerImport, error) {
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
		return nil, errors.New("history entry has no storage path — did the download fail?")
	}
	index, _, err := a.buildIdentityIndex(ctx, false)
	if err != nil {
		return nil, err
	}
	musicIdx, err := a.buildMusicQueueIndex(ctx)
	if err != nil {
		return nil, err
	}
	item := ManagerQueueItemView{Client: client.Name, ClientID: client.ID, NzoID: nzoID, Name: slot.Name,
		Category: slot.Category, Status: slot.Status, SizeMB: float64(slot.Bytes) / (1024 * 1024), History: true}
	a.annotateQueueItem(ctx, q, client, index, musicIdx, &item)
	if item.MatchedItemID == nil {
		if bookIdx, berr := a.buildBookIdentityIndex(ctx, false); berr == nil {
			if ref := bookIdx.match(slot.Name); ref != nil {
				item.MatchedItemID, item.MatchedTitle, item.MatchedLibrary = &ref.itemID, ref.title, ref.libraryID
			}
		}
	}
	if item.MatchedItemID == nil {
		return nil, fmt.Errorf("release %q was not recognized — the tagger found no library match to import into", slot.Name)
	}
	var mediaType string
	if err := a.db.QueryRow(ctx, `SELECT media_type FROM media_items WHERE id = $1`, *item.MatchedItemID).Scan(&mediaType); err != nil {
		return nil, fmt.Errorf("matched item vanished: %w", err)
	}
	dest, err := a.importDestination(ctx, *item.MatchedItemID, mediaType, item)
	if err != nil {
		return nil, err
	}
	var mappings []ManagerPathMapping
	_ = json.Unmarshal(client.PathMappings, &mappings)
	return &preparedManagerImport{client: client, slot: *slot, item: item, mediaType: mediaType,
		domain: managerProfileDomain(mediaType), source: mapClientPath(slot.Storage, mappings), dest: dest}, nil
}

func (a *App) managerImportPlan(ctx context.Context, prep *preparedManagerImport) (*ManagerImportPlanView, error) {
	listed, err := listDownloadFiles(prep.source)
	if err != nil {
		return nil, fmt.Errorf("download folder not reachable: %w", err)
	}
	naming := a.GetManagerFileNaming(ctx).Settings
	plan := &ManagerImportPlanView{MatchedItem: *prep.item.MatchedItemID, Title: prep.item.MatchedTitle,
		Destination: prep.dest, Files: []ManagerImportPlanFile{}}
	for _, file := range listed {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !importExtensions[prep.domain][ext] || strings.Contains(strings.ToLower(filepath.Base(file.Name)), "sample") {
			plan.Skipped = append(plan.Skipped, file.Name)
			continue
		}
		sourcePath := filepath.Join(prep.source, file.Name)
		if info, statErr := os.Stat(prep.source); statErr == nil && !info.IsDir() {
			sourcePath = prep.source
		}
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		mediaInfo, _ := mediaprobe.Probe(probeCtx, sourcePath)
		cancel()
		target, terr := a.managerImportTarget(ctx, prep, naming, file.Name, mediaInfo)
		if terr != nil {
			return nil, terr
		}
		_, statErr := os.Stat(target)
		plan.Files = append(plan.Files, ManagerImportPlanFile{Source: sourcePath, Destination: target,
			SizeBytes: file.SizeBytes, Collision: statErr == nil})
	}
	if len(plan.Files) == 0 {
		return nil, fmt.Errorf("no importable media files found in %s", prep.source)
	}
	if len(plan.Files) == 1 {
		plan.Destination = filepath.Dir(plan.Files[0].Destination)
	} else {
		targets := make([]string, 0, len(plan.Files))
		for _, file := range plan.Files {
			targets = append(targets, file.Destination)
		}
		sort.Strings(targets)
		plan.Destination = commonDir(targets[0], targets[len(targets)-1])
	}
	fingerprint, _ := json.Marshal(struct {
		Destination string                  `json:"destination"`
		Files       []ManagerImportPlanFile `json:"files"`
		Skipped     []string                `json:"skipped"`
	}{plan.Destination, plan.Files, plan.Skipped})
	plan.PlanID = fmt.Sprintf("%x", sha256.Sum256(fingerprint))
	return plan, nil
}

var musicTrackPrefix = regexp.MustCompile(`(?i)^\s*(?:(\d{1,2})[ ._-]+)?(\d{1,3})[ ._-]+(.+?)\s*$`)
var releaseDate = regexp.MustCompile(`(?i)(20\d{2})[ ._-](0?[1-9]|1[0-2])[ ._-](0?[1-9]|[12]\d|3[01])`)

func (a *App) managerImportTarget(ctx context.Context, prep *preparedManagerImport, naming ManagerFileNamingSettings, relative string, mediaInfo *mediaprobe.MediaInfo) (string, error) {
	ext := filepath.Ext(relative)
	base := strings.TrimSuffix(filepath.Base(relative), ext)
	var videoCodec, audioCodec, audioChannels string
	if prep.mediaType == "tv" || prep.mediaType == "anime" {
		parsed := video.FilenameParseShow(prep.slot.Name)
		videoCodec, audioCodec, audioChannels = string(parsed.VideoCodec), string(parsed.AudioCodec), string(parsed.AudioChannels)
	} else {
		parsed := video.FilenameParseMovie(prep.slot.Name)
		videoCodec, audioCodec, audioChannels = string(parsed.VideoCodec), string(parsed.AudioCodec), string(parsed.AudioChannels)
	}
	facts := map[string]string{
		"Movie Title": prep.item.MatchedTitle, "Movie CleanTitle": prep.item.MatchedTitle,
		"Series Title": prep.item.MatchedTitle, "Series CleanTitle": prep.item.MatchedTitle, "Series TitleYear": prep.item.MatchedTitle,
		"Quality Full": prep.item.Quality, "Quality Title": prep.item.Quality, "Release Group": video.ParseGroup(prep.slot.Name),
		"Original Title": prep.slot.Name, "Original Filename": strings.TrimSuffix(filepath.Base(relative), ext),
		"Mediainfo VideoCodec": videoCodec, "MediaInfo VideoCodec": videoCodec,
		"Mediainfo AudioCodec": audioCodec, "MediaInfo AudioCodec": audioCodec,
		"Mediainfo AudioChannels": audioChannels, "MediaInfo AudioChannels": audioChannels,
	}
	attrs := managerformats.ParseVideoRelease(prep.slot.Name, prep.slot.Bytes, prep.mediaType == "tv" || prep.mediaType == "anime")
	facts["Edition Tags"] = attrs.Edition
	if len(attrs.Languages) > 0 {
		facts["MediaInfo AudioLanguagesAll"] = "[" + strings.ToUpper(strings.Join(attrs.Languages, "+")) + "]"
	}
	custom := make([]string, 0, len(prep.item.FormatBreakdown))
	for _, hit := range prep.item.FormatBreakdown {
		if hit.Score > 0 {
			custom = append(custom, hit.Name)
		}
	}
	facts["Custom Formats"] = strings.Join(custom, " ")
	applyMediaInfoNamingFacts(facts, mediaInfo)
	var year string
	_ = a.db.QueryRow(ctx, `SELECT year FROM media_item_cards WHERE id = $1`, *prep.item.MatchedItemID).Scan(&year)
	if len(year) >= 4 {
		year = year[:4]
	}
	facts["Release Year"], facts["Series TitleYear"] = year, prep.item.MatchedTitle
	facts["Series Year"] = year
	if year != "" {
		facts["Series TitleYear"] += " (" + year + ")"
	}
	facts["Series CleanTitleYear"] = facts["Series TitleYear"]
	facts["Series TitleWithoutYear"], facts["Series CleanTitleWithoutYear"] = prep.item.MatchedTitle, prep.item.MatchedTitle
	facts["Movie TitleFirstCharacter"], facts["Series TitleFirstCharacter"] = firstNamingCharacter(prep.item.MatchedTitle), firstNamingCharacter(prep.item.MatchedTitle)
	show := video.FilenameParseShow(prep.slot.Name)
	if len(show.Seasons) > 0 {
		facts["season:00"] = fmt.Sprintf("%02d", show.Seasons[0])
		facts["Season"] = fmt.Sprintf("%d", show.Seasons[0])
	}
	if len(show.EpisodeNumbers) > 0 {
		facts["episode:00"] = fmt.Sprintf("%02d", show.EpisodeNumbers[0])
		facts["Episode"] = fmt.Sprintf("%d", show.EpisodeNumbers[0])
	}

	template, root := naming.Movie, prep.dest
	switch prep.mediaType {
	case "tv":
		template = naming.TV
		if date := releaseDate.FindStringSubmatch(prep.slot.Name); date != nil {
			facts["Air-Date"] = strings.Join(date[1:], "-")
			template = naming.DailyTV
		}
	case "anime":
		template = naming.Anime
	case "music":
		template = naming.Music
		facts["Artist Name"], facts["Artist CleanName"] = prep.item.MatchedTitle, prep.item.MatchedTitle
		album := prep.item.Parsed
		if idx := strings.Index(album, "—"); idx >= 0 {
			album = strings.TrimSpace(album[idx+len("—"):])
		}
		facts["Album Title"], facts["Album CleanTitle"], facts["Album Type"] = album, album, "Album"
		facts["Release Year"] = video.FilenameParseMovie(prep.slot.Name).Year
		facts["medium:00"], facts["track:00"], facts["Track Title"], facts["Track CleanTitle"] = "01", "00", base, base
		if mediaInfo != nil {
			if value := mediaTag(mediaInfo.Format.Tags, "title"); value != "" {
				facts["Track Title"], facts["Track CleanTitle"] = value, value
			}
			if value := mediaTag(mediaInfo.Format.Tags, "album"); value != "" {
				facts["Album Title"], facts["Album CleanTitle"] = value, value
			}
			if value := mediaTag(mediaInfo.Format.Tags, "artist", "album_artist"); value != "" {
				facts["Artist Name"], facts["Artist CleanName"] = value, value
			}
		}
		if match := musicTrackPrefix.FindStringSubmatch(base); match != nil {
			if match[1] != "" {
				facts["medium:00"] = fmt.Sprintf("%02s", match[1])
			}
			facts["track:00"], facts["Track CleanTitle"] = fmt.Sprintf("%02s", match[2]), strings.Trim(match[3], " ._-")
			facts["Track Title"] = facts["Track CleanTitle"]
		}
		if facts["medium:00"] != "01" {
			template = naming.MusicMulti
		}
		var paths []string
		if err := a.db.QueryRow(ctx, `SELECT paths FROM libraries WHERE id = $1`, prep.item.MatchedLibrary).Scan(&paths); err != nil {
			return "", err
		}
		root = filepath.Clean(paths[0])
		settings, _ := a.GetLibrarySettings(ctx, prep.item.MatchedLibrary)
		if settings.DefaultImportPath != "" && libraryContainsPath(paths, settings.DefaultImportPath) {
			root = filepath.Clean(settings.DefaultImportPath)
		}
	}
	rendered := sanitizeImportRelativePath(renderManagerFilename(template, facts))
	if rendered == "" {
		return "", errors.New("file naming template rendered an empty path")
	}
	return checkedImportDestination(root, filepath.Join(root, rendered+ext))
}

func firstNamingCharacter(value string) string {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "_"
}

func applyMediaInfoNamingFacts(facts map[string]string, info *mediaprobe.MediaInfo) {
	if info == nil {
		return
	}
	var audioLanguages, subtitleLanguages []string
	for i := range info.Streams {
		stream := &info.Streams[i]
		lang := strings.ToUpper(mediaTag(stream.Tags, "language"))
		switch stream.CodecType {
		case "video":
			if facts["Mediainfo VideoCodec"] == "" {
				facts["Mediainfo VideoCodec"], facts["MediaInfo VideoCodec"] = stream.CodecName, stream.CodecName
			}
			if depth := videoBitDepth(stream); depth > 0 {
				facts["MediaInfo VideoBitDepth"] = fmt.Sprintf("%d", depth)
			}
			facts["MediaInfo VideoDynamicRangeType"] = hdrLabel(stream)
		case "audio":
			if facts["Mediainfo AudioCodec"] == "" {
				facts["Mediainfo AudioCodec"], facts["MediaInfo AudioCodec"] = stream.CodecName, stream.CodecName
			}
			if facts["Mediainfo AudioChannels"] == "" && stream.Channels > 0 {
				facts["Mediainfo AudioChannels"], facts["MediaInfo AudioChannels"] = channelLayoutLabel(int32(stream.Channels)), channelLayoutLabel(int32(stream.Channels))
			}
			if lang != "" && lang != "UND" {
				audioLanguages = appendUniqueString(audioLanguages, lang)
			}
		case "subtitle":
			if lang != "" && lang != "UND" {
				subtitleLanguages = appendUniqueString(subtitleLanguages, lang)
			}
		}
	}
	if len(audioLanguages) > 0 {
		facts["MediaInfo AudioLanguagesAll"] = "[" + strings.Join(audioLanguages, "+") + "]"
	}
	if len(subtitleLanguages) > 0 {
		facts["MediaInfo SubtitleLanguagesAll"] = "[" + strings.Join(subtitleLanguages, "+") + "]"
	}
	facts["MediaInfo Video"] = facts["MediaInfo VideoCodec"]
	facts["MediaInfo Audio"] = facts["MediaInfo AudioCodec"]
	facts["MediaInfo AudioLanguages"] = facts["MediaInfo AudioLanguagesAll"]
	facts["MediaInfo SubtitleLanguages"] = facts["MediaInfo SubtitleLanguagesAll"]
	facts["MediaInfo Simple"] = strings.TrimSpace(facts["MediaInfo VideoCodec"] + " " + facts["MediaInfo AudioCodec"])
	facts["MediaInfo Full"] = strings.TrimSpace(facts["MediaInfo Simple"] + " " + facts["MediaInfo AudioLanguagesAll"] + " " + facts["MediaInfo SubtitleLanguagesAll"])
}

func mediaTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		for candidate, value := range tags {
			if strings.EqualFold(candidate, key) {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func sanitizeImportRelativePath(path string) string {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := range parts {
		parts[i] = sanitizeFolderName(parts[i])
	}
	return filepath.Join(parts...)
}

func importMovePlan(files []ManagerImportPlanFile, skipped []string) (moved, allSkipped []string, err error) {
	allSkipped = append(allSkipped, skipped...)
	for _, file := range files {
		if file.Collision {
			allSkipped = append(allSkipped, filepath.Base(file.Source)+" (already exists)")
			continue
		}
		if _, statErr := os.Stat(file.Destination); statErr == nil {
			allSkipped = append(allSkipped, filepath.Base(file.Source)+" (already exists)")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.Destination), 0o755); err != nil {
			return moved, allSkipped, err
		}
		if err := moveFile(file.Source, file.Destination); err != nil {
			return moved, allSkipped, fmt.Errorf("moving %s: %w", filepath.Base(file.Source), err)
		}
		moved = append(moved, filepath.Base(file.Destination))
	}
	if len(moved) == 0 {
		return moved, allSkipped, errors.New("no importable media files remained after collision checks")
	}
	return moved, allSkipped, nil
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
	var settingsJSON []byte
	if err := a.db.QueryRow(ctx, `
		SELECT l.paths, l.settings FROM libraries l
		JOIN media_items mi ON mi.library_id = l.id
		WHERE mi.id = $1`, itemID).Scan(&paths, &settingsJSON); err != nil {
		return "", fmt.Errorf("library root: %w", err)
	}
	if len(paths) == 0 || paths[0] == "" {
		return "", fmt.Errorf("the library has no configured path")
	}
	root := filepath.Clean(paths[0])
	settings := metadata.ParseSettings(settingsJSON)
	if settings.DefaultImportPath != "" && libraryContainsPath(paths, settings.DefaultImportPath) {
		root = filepath.Clean(settings.DefaultImportPath)
	}

	var title, year string
	_ = a.db.QueryRow(ctx, `SELECT title, year FROM media_item_cards WHERE id = $1`, itemID).Scan(&title, &year)

	// Existing folder: deepest directory shared by the item's live files.
	var itemDir string
	rows, rowsErr := a.db.Query(ctx, `
		SELECT lf.path FROM library_files lf
		WHERE lf.media_item_id = $1 AND lf.deleted_at IS NULL
		  AND (lf.path = $2 OR left(lf.path, length($2) + 1) = $2 || '/')
		ORDER BY lf.path`, itemID, root)
	var livePaths []string
	if rowsErr == nil {
		for rows.Next() {
			var path string
			if rows.Scan(&path) == nil {
				livePaths = append(livePaths, path)
			}
		}
		rows.Close()
	}
	if len(livePaths) > 0 {
		itemDir = commonDir(livePaths[0], livePaths[len(livePaths)-1])
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
			return checkedImportDestination(root, itemDir)
		}
		return checkedImportDestination(root, filepath.Join(root, folderTitle))
	case "tv", "anime":
		seriesDir := itemDir
		if seriesDir == "" {
			seriesDir = filepath.Join(root, folderTitle)
		} else if strings.HasPrefix(filepath.Base(seriesDir), "Season") {
			seriesDir = filepath.Dir(seriesDir)
		}
		show := video.FilenameParseShow(item.Name)
		if len(show.Seasons) > 0 {
			return checkedImportDestination(root, filepath.Join(seriesDir, fmt.Sprintf("Season %02d", show.Seasons[0])))
		}
		return checkedImportDestination(root, seriesDir)
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
		year := video.FilenameParseMovie(item.Name).Year
		if existing := existingMusicAlbumDir(livePaths, artistDir, album, year); existing != "" {
			return checkedImportDestination(root, existing)
		}
		folder := sanitizeFolderName(album)
		if year != "" {
			folder = fmt.Sprintf("%s (%s)", folder, year)
		}
		return checkedImportDestination(root, filepath.Join(artistDir, folder))
	default:
		return "", fmt.Errorf("import is not supported for %s items", mediaType)
	}
}

func checkedImportDestination(root, destination string) (string, error) {
	root = filepath.Clean(root)
	destination = filepath.Clean(destination)
	rel, err := filepath.Rel(root, destination)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("import destination %q escapes selected library root %q", destination, root)
	}
	return destination, nil
}

func existingMusicAlbumDir(paths []string, artistDir, album, year string) string {
	want := compactQueueTitle(album)
	if want == "" {
		return ""
	}
	candidates := map[string]bool{}
	for _, path := range paths {
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(artistDir, dir)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		// An album folder is the immediate child of the artist folder.
		candidate := filepath.Join(artistDir, strings.Split(rel, string(filepath.Separator))[0])
		name := compactQueueTitle(filepath.Base(candidate))
		if strings.Contains(name, want) && (year == "" || strings.Contains(name, year)) {
			candidates[candidate] = true
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for candidate := range candidates {
		return candidate
	}
	return ""
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
