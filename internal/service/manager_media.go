package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Manager media detail: the arr-style item page under /manager — hero facts
// plus a domain-shaped body (seasons/episodes for TV, files for movies and
// books, albums for artists). Read-only except for what the bulk endpoint
// already covers (monitored / profile); search-and-grab arrives with the
// acquisition phase.

type ManagerEpisodeView struct {
	ID        int64  `json:"id"`
	Number    int32  `json:"number"`
	Title     string `json:"title"`
	AirDate   string `json:"air_date,omitempty"`
	Special   bool   `json:"special"`
	HasFile   bool   `json:"has_file"`
	Quality   string `json:"quality,omitempty" doc:"Resolution label of the best on-disk file, e.g. 1080p"`
	SizeBytes int64  `json:"size_bytes"`
}

type ManagerSeasonView struct {
	Number     int32                `json:"number"`
	Title      string               `json:"title"`
	Aired      int32                `json:"aired"`
	Have       int32                `json:"have"`
	SizeOnDisk int64                `json:"size_on_disk"`
	Episodes   []ManagerEpisodeView `json:"episodes"`
}

type ManagerFileView struct {
	ID         int64  `json:"id"`
	Path       string `json:"path" doc:"Relative to the item's folder"`
	Kind       string `json:"kind" doc:"primary | part | extra type (trailer, clip, ...)"`
	SizeBytes  int64  `json:"size_bytes"`
	Quality    string `json:"quality,omitempty"`
	VideoCodec string `json:"video_codec,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
	AddedAt    string `json:"added_at"`
}

type ManagerAlbumView struct {
	ID            int64   `json:"id" doc:"Local album id; 0 for catalog-only releases the library doesn't have"`
	DiscographyID int64   `json:"discography_id,omitempty" doc:"artist_discography row id when the release is in the provider catalog"`
	Title         string  `json:"title"`
	Slug          string  `json:"slug"`
	Type          string  `json:"type" doc:"album | ep | single | compilation | ..."`
	ReleaseDate   string  `json:"release_date,omitempty"`
	Year          string  `json:"year,omitempty"`
	Rating        float64 `json:"rating,omitempty"`
	TracksHave    int32   `json:"tracks_have"`
	TracksTotal   int32   `json:"tracks_total"`
	SizeBytes     int64   `json:"size_bytes"`
	InLibrary     bool    `json:"in_library"`
	CoverURL      string  `json:"cover_url,omitempty" doc:"Provider cover for catalog-only releases (local albums use the cover endpoint)"`
}

type ManagerMediaDetailView struct {
	Item        ManagerLibraryItemView `json:"item"`
	Library     ManagerLibraryRef      `json:"library"`
	Overview    string                 `json:"overview,omitempty"`
	Genres      []string               `json:"genres,omitempty"`
	Runtime     int32                  `json:"runtime_minutes,omitempty"`
	Rating      float64                `json:"rating,omitempty"`
	Language    string                 `json:"language,omitempty"`
	ReleaseFrom string                 `json:"release_from,omitempty" doc:"Release date / first air date / active-from year"`
	ReleaseTo   string                 `json:"release_to,omitempty" doc:"Last air date / disbanded year, when the run has ended"`
	Path        string                 `json:"path,omitempty" doc:"Common on-disk folder of the item's live files"`
	ProfileName string                 `json:"profile_name,omitempty"`
	ExternalIDs map[string]string      `json:"external_ids,omitempty"`
	Seasons     []ManagerSeasonView    `json:"seasons,omitempty"`
	Files       []ManagerFileView      `json:"files,omitempty"`
	Albums      []ManagerAlbumView     `json:"albums,omitempty"`
}

func (a *App) GetManagerMediaDetail(ctx context.Context, mediaItemID int64) (*ManagerMediaDetailView, error) {
	view := &ManagerMediaDetailView{}

	var libraryID int64
	var mediaType, overview string
	var extIDs map[string]string
	err := a.db.QueryRow(ctx, `
		SELECT library_id, media_type::text, description,
		       (SELECT COALESCE(jsonb_object_agg(ei.provider, ei.external_id), '{}'::jsonb)
		          FROM media_item_external_ids ei WHERE ei.media_item_id = c.id)
		FROM media_item_cards c WHERE c.id = $1`, mediaItemID).
		Scan(&libraryID, &mediaType, &overview, &extIDs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManagerNotFound
		}
		return nil, err
	}
	view.Overview = overview
	view.ExternalIDs = extIDs

	lib, err := a.GetLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	view.Library = ManagerLibraryRef{
		ID:        lib.ID,
		Name:      lib.Name,
		MediaType: mediaType,
		Domain:    managerProfileDomain(mediaType),
	}

	// The listing row (completeness, size, monitored, profile) through the
	// same per-domain CTE, scoped to this one item via $2.
	cte, err := managerLibraryRowsCTE(mediaType)
	if err != nil {
		return nil, err
	}
	var profileID *int64
	var addedAt time.Time
	item := &view.Item
	err = a.db.QueryRow(ctx, "WITH "+cte+`
		SELECT id, slug, title, year, media_type, status, monitored, quality_profile_id,
		       added_at, size_on_disk, file_count, have_count, total_count,
		       GREATEST(total_count - have_count, 0), group_count
		FROM rows`, libraryID, mediaItemID).
		Scan(&item.ID, &item.Slug, &item.Title, &item.Year, &item.MediaType, &item.Status,
			&item.Monitored, &profileID, &addedAt, &item.SizeOnDisk, &item.FileCount,
			&item.HaveCount, &item.TotalCount, &item.MissingCount, &item.GroupCount)
	if err != nil {
		return nil, fmt.Errorf("manager media detail row: %w", err)
	}
	item.QualityProfileID = profileID
	item.AddedAt = addedAt.UTC().Format(time.RFC3339)

	if profileID != nil {
		var name string
		if err := a.db.QueryRow(ctx, "SELECT name FROM manager_quality_profiles WHERE id = $1", *profileID).Scan(&name); err == nil {
			view.ProfileName = name
		}
	}

	// Common folder: with live paths sorted, the shared prefix of min and max
	// is the shared prefix of the whole set.
	var minPath, maxPath *string
	_ = a.db.QueryRow(ctx, `
		SELECT min(path), max(path) FROM library_files
		WHERE media_item_id = $1 AND deleted_at IS NULL`, mediaItemID).
		Scan(&minPath, &maxPath)
	if minPath != nil && maxPath != nil {
		view.Path = commonDir(*minPath, *maxPath)
	}

	switch mediaType {
	case "movie":
		if err := a.loadManagerMovieFacts(ctx, mediaItemID, view); err != nil {
			return nil, err
		}
		view.Files, err = a.managerMediaFiles(ctx, mediaItemID, view.Path)
	case "tv", "anime":
		if err := a.loadManagerSeriesFacts(ctx, mediaItemID, view); err != nil {
			return nil, err
		}
		view.Seasons, err = a.managerSeriesSeasons(ctx, mediaItemID)
	case "music":
		if err := a.loadManagerArtistFacts(ctx, mediaItemID, view); err != nil {
			return nil, err
		}
		view.Albums, err = a.managerArtistAlbums(ctx, mediaItemID)
	case "book":
		if err := a.loadManagerBookFacts(ctx, mediaItemID, view); err != nil {
			return nil, err
		}
		view.Files, err = a.managerMediaFiles(ctx, mediaItemID, view.Path)
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

// commonDir returns the deepest directory shared by two absolute paths.
func commonDir(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	prefix := a[:i]
	if idx := strings.LastIndexByte(prefix, '/'); idx > 0 {
		return prefix[:idx]
	}
	return prefix
}

func numericToFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

func dateToString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func (a *App) loadManagerMovieFacts(ctx context.Context, id int64, view *ManagerMediaDetailView) error {
	var genres []string
	var rating pgtype.Numeric
	var release pgtype.Date
	var runtime int32
	var language string
	err := a.db.QueryRow(ctx, `
		SELECT genres, rating, release_date, runtime_minutes, original_language
		FROM movies WHERE media_item_id = $1`, id).
		Scan(&genres, &rating, &release, &runtime, &language)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	view.Genres = genres
	view.Rating = numericToFloat(rating)
	view.ReleaseFrom = dateToString(release)
	view.Runtime = runtime
	view.Language = language
	return nil
}

func (a *App) loadManagerSeriesFacts(ctx context.Context, id int64, view *ManagerMediaDetailView) error {
	var genres []string
	var rating pgtype.Numeric
	var first, last pgtype.Date
	var language, status string
	err := a.db.QueryRow(ctx, `
		SELECT genres, rating, first_air_date, last_air_date, original_language, status
		FROM tv_series WHERE media_item_id = $1`, id).
		Scan(&genres, &rating, &first, &last, &language, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	view.Genres = genres
	view.Rating = numericToFloat(rating)
	view.ReleaseFrom = dateToString(first)
	if status == "ended" || status == "canceled" {
		view.ReleaseTo = dateToString(last)
	}
	view.Language = language
	return nil
}

func (a *App) loadManagerArtistFacts(ctx context.Context, id int64, view *ManagerMediaDetailView) error {
	var genres []string
	var beginYear int32
	var endDate string
	var ended bool
	err := a.db.QueryRow(ctx, `
		SELECT genres, begin_year, end_date, ended
		FROM artists WHERE media_item_id = $1`, id).
		Scan(&genres, &beginYear, &endDate, &ended)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	view.Genres = genres
	if beginYear > 0 {
		view.ReleaseFrom = fmt.Sprintf("%d", beginYear)
	}
	if ended && endDate != "" {
		view.ReleaseTo = endDate
	}
	return nil
}

func (a *App) loadManagerBookFacts(ctx context.Context, id int64, view *ManagerMediaDetailView) error {
	var subjects []string
	var language string
	var publish pgtype.Date
	err := a.db.QueryRow(ctx, `
		SELECT subjects, language, publish_date
		FROM books WHERE media_item_id = $1`, id).
		Scan(&subjects, &language, &publish)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	view.Genres = subjects
	view.Language = language
	view.ReleaseFrom = dateToString(publish)
	return nil
}

// managerMediaFiles lists an item's live files with codec info pulled out of
// the stored ffprobe blob. Radarr-style: primary/part files first, extras
// after, paths relative to the item folder.
func (a *App) managerMediaFiles(ctx context.Context, id int64, basePath string) ([]ManagerFileView, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT ON (lf.id)
		       lf.id, lf.path, lf.size, lf.video_height, lf.created_at,
		       COALESCE(NULLIF(lfl.extra_type, ''), lfl.relation_type, 'file') AS kind,
		       CASE WHEN jsonb_typeof(lf.media_info->'streams') = 'array' THEN
		         (SELECT s->>'codec_name' FROM jsonb_array_elements(lf.media_info->'streams') s
		          WHERE s->>'codec_type' = 'video' LIMIT 1) END,
		       COALESCE(CASE WHEN jsonb_typeof(lf.media_info->'streams') = 'array' THEN
		         (SELECT (s->>'width')::int FROM jsonb_array_elements(lf.media_info->'streams') s
		          WHERE s->>'codec_type' = 'video' LIMIT 1) END, 0),
		       CASE WHEN jsonb_typeof(lf.media_info->'streams') = 'array' THEN
		         (SELECT s->>'codec_name' FROM jsonb_array_elements(lf.media_info->'streams') s
		          WHERE s->>'codec_type' = 'audio' LIMIT 1) END,
		       CASE WHEN jsonb_typeof(lf.media_info->'streams') = 'array' THEN
		         (SELECT (s->>'channels')::int FROM jsonb_array_elements(lf.media_info->'streams') s
		          WHERE s->>'codec_type' = 'audio' LIMIT 1) END
		FROM library_files lf
		LEFT JOIN library_file_links lfl ON lfl.library_file_id = lf.id AND lfl.media_item_id = lf.media_item_id
		WHERE lf.media_item_id = $1 AND lf.deleted_at IS NULL
		ORDER BY lf.id, CASE lfl.relation_type WHEN 'primary' THEN 0 WHEN 'part' THEN 1 ELSE 2 END`, id)
	if err != nil {
		return nil, fmt.Errorf("manager media files: %w", err)
	}
	defer rows.Close()

	files := []ManagerFileView{}
	for rows.Next() {
		var f ManagerFileView
		var path string
		var height, width int32
		var created time.Time
		var videoCodec, audioCodec *string
		var audioChannels *int32
		if err := rows.Scan(&f.ID, &path, &f.SizeBytes, &height, &created, &f.Kind,
			&videoCodec, &width, &audioCodec, &audioChannels); err != nil {
			return nil, err
		}
		f.Path = strings.TrimPrefix(strings.TrimPrefix(path, basePath), "/")
		f.Quality = resolutionLabel(width, height)
		if videoCodec != nil {
			f.VideoCodec = *videoCodec
		}
		if audioCodec != nil {
			f.AudioCodec = *audioCodec
			if audioChannels != nil {
				f.AudioCodec += " " + channelLayoutLabel(*audioChannels)
			}
		}
		f.AddedAt = created.UTC().Format(time.RFC3339)
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Primary and part files lead, extras trail, stable by path within a band.
	kindRank := func(kind string) int {
		switch kind {
		case "primary":
			return 0
		case "part":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if kindRank(files[i].Kind) != kindRank(files[j].Kind) {
			return kindRank(files[i].Kind) < kindRank(files[j].Kind)
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

// resolutionLabel maps stored dimensions to the release-name tier. Width wins
// when known — a 1920x802 cinemascope encode is a 1080p release, and height
// alone would call it 802p. The height bands are cut low for the same reason.
func resolutionLabel(width, height int32) string {
	switch {
	case width >= 3000:
		return "2160p"
	case width >= 1800:
		return "1080p"
	case width >= 1200:
		return "720p"
	}
	switch {
	case height >= 1700:
		return "2160p"
	case height >= 800:
		return "1080p"
	case height >= 560:
		return "720p"
	case height >= 490:
		return "576p"
	case height >= 380:
		return "480p"
	case height > 0:
		return fmt.Sprintf("%dp", height)
	default:
		return ""
	}
}

// channelLayoutLabel renders an ffprobe channel count the way release names
// do: 6 channels is 5.1, 8 is 7.1.
func channelLayoutLabel(channels int32) string {
	switch channels {
	case 1:
		return "1.0"
	case 2:
		return "2.0"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%dch", channels)
	}
}

func (a *App) managerSeriesSeasons(ctx context.Context, id int64) ([]ManagerSeasonView, error) {
	rows, err := a.db.Query(ctx, `
		SELECT s.season_number, s.title,
		       e.id, e.episode_number, e.title, e.air_date, e.is_special,
		       bool_or(lf.id IS NOT NULL),
		       COALESCE(max(lf.video_height), 0),
		       COALESCE(sum(lf.size) FILTER (WHERE lf.id IS NOT NULL), 0)
		FROM tv_series sr
		JOIN tv_seasons s ON s.series_id = sr.id
		JOIN tv_episodes e ON e.season_id = s.id
		LEFT JOIN library_file_links lfl ON lfl.tv_episode_id = e.id
		LEFT JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
		WHERE sr.media_item_id = $1
		GROUP BY s.season_number, s.title, e.id, e.episode_number, e.title, e.air_date, e.is_special
		ORDER BY s.season_number DESC, e.episode_number DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("manager series seasons: %w", err)
	}
	defer rows.Close()

	seasons := []ManagerSeasonView{}
	byNumber := map[int32]int{}
	today := time.Now().Format("2006-01-02")
	for rows.Next() {
		var seasonNumber int32
		var seasonTitle string
		var e ManagerEpisodeView
		var airDate pgtype.Date
		var height int32
		if err := rows.Scan(&seasonNumber, &seasonTitle, &e.ID, &e.Number, &e.Title,
			&airDate, &e.Special, &e.HasFile, &height, &e.SizeBytes); err != nil {
			return nil, err
		}
		e.AirDate = dateToString(airDate)
		if e.HasFile {
			e.Quality = resolutionLabel(0, height)
		}
		idx, ok := byNumber[seasonNumber]
		if !ok {
			idx = len(seasons)
			byNumber[seasonNumber] = idx
			seasons = append(seasons, ManagerSeasonView{Number: seasonNumber, Title: seasonTitle, Episodes: []ManagerEpisodeView{}})
		}
		s := &seasons[idx]
		if !e.Special && e.AirDate != "" && e.AirDate <= today {
			s.Aired++
		}
		if e.HasFile && !e.Special {
			s.Have++
		}
		s.Episodes = append(s.Episodes, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Season size counts each on-disk file once — a multi-episode file is
	// linked from every episode it covers, so the per-episode sum above
	// would double it.
	sizeRows, err := a.db.Query(ctx, `
		SELECT season_number, sum(size)::bigint FROM (
		  SELECT DISTINCT s.season_number, lf.id, lf.size
		  FROM tv_series sr
		  JOIN tv_seasons s ON s.series_id = sr.id
		  JOIN tv_episodes e ON e.season_id = s.id
		  JOIN library_file_links lfl ON lfl.tv_episode_id = e.id
		  JOIN library_files lf ON lf.id = lfl.library_file_id AND lf.deleted_at IS NULL
		  WHERE sr.media_item_id = $1
		) d GROUP BY season_number`, id)
	if err != nil {
		return nil, err
	}
	defer sizeRows.Close()
	for sizeRows.Next() {
		var number int32
		var size int64
		if err := sizeRows.Scan(&number, &size); err != nil {
			return nil, err
		}
		if idx, ok := byNumber[number]; ok {
			seasons[idx].SizeOnDisk = size
		}
	}
	return seasons, sizeRows.Err()
}

// managerArtistAlbums merges the provider's full discography with the
// library's local albums, Lidarr-style: every known release appears, linked
// ones carry local completeness and size, catalog-only ones read 0/N. Local
// albums the catalog doesn't know (bootlegs, unmatched) still show.
func (a *App) managerArtistAlbums(ctx context.Context, id int64) ([]ManagerAlbumView, error) {
	rows, err := a.db.Query(ctx, `
		SELECT al.id, al.title, al.slug, al.album_type, al.release_date, al.year, al.rating,
		       count(DISTINCT t.id)::int,
		       count(DISTINCT t.id) FILTER (WHERE lf.id IS NOT NULL)::int,
		       COALESCE(sum(tf.size_bytes) FILTER (WHERE lf.id IS NOT NULL), 0)::bigint
		FROM albums al
		JOIN artists ar ON ar.id = al.artist_id AND ar.media_item_id = $1
		JOIN tracks t ON t.album_id = al.id
		LEFT JOIN track_files tf ON tf.track_id = t.id
		LEFT JOIN library_files lf ON lf.id = tf.library_file_id AND lf.deleted_at IS NULL
		GROUP BY al.id
		ORDER BY al.release_date DESC NULLS LAST, al.year DESC, al.title ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("manager artist albums: %w", err)
	}
	defer rows.Close()

	locals := []ManagerAlbumView{}
	byAlbumID := map[int64]int{}
	for rows.Next() {
		var al ManagerAlbumView
		var release pgtype.Date
		var rating pgtype.Numeric
		if err := rows.Scan(&al.ID, &al.Title, &al.Slug, &al.Type, &release, &al.Year,
			&rating, &al.TracksTotal, &al.TracksHave, &al.SizeBytes); err != nil {
			return nil, err
		}
		al.ReleaseDate = dateToString(release)
		al.Rating = numericToFloat(rating)
		al.InLibrary = true
		byAlbumID[al.ID] = len(locals)
		locals = append(locals, al)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	discoRows, err := a.db.Query(ctx, `
		SELECT d.id, d.title, d.album_type, d.release_date, d.year, d.track_count, d.cover_url, d.album_id
		FROM artist_discography d
		JOIN artists ar ON ar.id = d.artist_id AND ar.media_item_id = $1
		ORDER BY d.release_date DESC NULLS LAST, d.year DESC, d.title ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("manager artist discography: %w", err)
	}
	defer discoRows.Close()

	merged := []ManagerAlbumView{}
	linked := map[int64]bool{}
	haveDiscography := false
	for discoRows.Next() {
		haveDiscography = true
		var discoID, trackCount int64
		var title, albumType, year, coverURL string
		var release pgtype.Date
		var albumID pgtype.Int8
		if err := discoRows.Scan(&discoID, &title, &albumType, &release, &year,
			&trackCount, &coverURL, &albumID); err != nil {
			return nil, err
		}
		if albumID.Valid {
			if idx, ok := byAlbumID[albumID.Int64]; ok {
				local := locals[idx]
				local.DiscographyID = discoID
				// The catalog's release-group type wins — local rows often
				// default to 'album'; catalog track counts fill gaps where
				// the local rip is partial.
				local.Type = albumType
				if int32(trackCount) > local.TracksTotal {
					local.TracksTotal = int32(trackCount)
				}
				linked[albumID.Int64] = true
				merged = append(merged, local)
				continue
			}
		}
		merged = append(merged, ManagerAlbumView{
			DiscographyID: discoID,
			Title:         title,
			Type:          albumType,
			ReleaseDate:   dateToString(release),
			Year:          year,
			TracksTotal:   int32(trackCount),
			CoverURL:      coverURL,
		})
	}
	if err := discoRows.Err(); err != nil {
		return nil, err
	}
	if !haveDiscography {
		return locals, nil
	}
	for _, local := range locals {
		if !linked[local.ID] {
			merged = append(merged, local)
		}
	}
	return merged, nil
}
