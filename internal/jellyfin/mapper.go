package jellyfin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Mapping from Heya rows to BaseItemDto. Per the repo convention, image tags
// are unconditional: every item gets a Primary tag (the image endpoint's
// media_assets walk decides what actually serves), and backdrop tags exist
// only for the entity kinds that have backdrops (movies, series, artists).

// videoDecor carries the per-user sets one request needs to decorate video
// dtos: loaded once per handler call, O(1) per item.
type videoDecor struct {
	watchedMovies map[int64]bool
	watchedSeries map[int64]bool
	favorites     map[int64]bool
	showCounts    map[int64][2]int32
	// progress is keyed by the entity id matching the dto being decorated
	// (movie media_item ids or episode ids — never mixed in one page).
	progress map[int64]sqlc.JFListWatchProgressByIDsRow
}

func (s *Server) dtoFromMediaItemRow(row sqlc.JFListLibraryItemsRow, serverID string, dec *videoDecor) baseItemDto {
	dto := baseItemDto{
		Name:              row.Title,
		OriginalTitle:     "",
		ServerID:          serverID,
		ID:                EncodeID(KindItem, row.ID),
		Etag:              etagFromTime(row.UpdatedAt),
		DateCreated:       tsTime(row.CreatedAt),
		CanDownload:       true,
		SortName:          firstNonEmpty(row.SortTitle, row.Title),
		Overview:          row.Description,
		Taglines:          taglines(row.Tagline),
		Genres:            []string{},
		ParentID:          EncodeID(KindLibrary, row.LibraryID),
		LocationType:      "FileSystem",
		ProviderIds:       providerIDs(row.ExternalIds, row.MediaType),
		ImageTags:         map[string]string{"Primary": etagFromTime(row.UpdatedAt)},
		BackdropImageTags: []string{},
		ProductionYear:    yearPtr(row.Year),
	}

	switch row.MediaType {
	case sqlc.MediaTypeMovie:
		dto.Type = "Movie"
		dto.MediaType = "Video"
		dto.RunTimeTicks = minutesToTicks(row.MovieRuntimeMinutes.Int32)
		dto.Genres = orEmpty(row.MovieGenres)
		dto.CommunityRating = ratingPtr(row.MovieRating)
		dto.PremiereDate = dateTime(row.MovieReleaseDate)
		dto.BackdropImageTags = []string{etagFromTime(row.UpdatedAt)}
		dto.PrimaryImageAspectRatio = &aspectPoster
		if dec != nil {
			dto.UserData = movieUserData(row.ID, dec)
		}
	case sqlc.MediaTypeTv:
		dto.Type = "Series"
		dto.MediaType = "Unknown"
		dto.IsFolder = true
		dto.Genres = orEmpty(row.SeriesGenres)
		dto.CommunityRating = ratingPtr(row.SeriesRating)
		dto.PremiereDate = dateTime(row.SeriesFirstAirDate)
		dto.Status = seriesStatus(row.SeriesStatus.String)
		dto.BackdropImageTags = []string{etagFromTime(row.UpdatedAt)}
		dto.PrimaryImageAspectRatio = &aspectPoster
		if row.SeriesEpisodeCount.Valid && row.SeriesEpisodeCount.Int32 > 0 {
			n := row.SeriesEpisodeCount.Int32
			dto.ChildCount = &n
		}
		if dec != nil {
			dto.UserData = seriesUserData(row.ID, dec)
		}
	case sqlc.MediaTypeMusic:
		dto.Type = "MusicArtist"
		dto.MediaType = "Unknown"
		dto.IsFolder = true
		if row.ArtistName.Valid && row.ArtistName.String != "" {
			dto.Name = row.ArtistName.String
		}
		dto.BackdropImageTags = []string{etagFromTime(row.UpdatedAt)}
		dto.PrimaryImageAspectRatio = &aspectSquare
		if dec != nil {
			dto.UserData = plainUserData(row.ID, dec)
		}
	case sqlc.MediaTypeBook:
		dto.Type = "Book"
		dto.MediaType = "Book"
		dto.PrimaryImageAspectRatio = &aspectPoster
		if dec != nil {
			dto.UserData = plainUserData(row.ID, dec)
		}
	default:
		dto.Type = "Folder"
		dto.MediaType = "Unknown"
		dto.IsFolder = true
	}
	return dto
}

func (s *Server) dtoFromSeasonRow(row sqlc.JFListSeasonsRow, serverID string, dec *videoDecor) baseItemDto {
	n := row.SeasonNumber
	count := row.EpisodeCount
	dto := baseItemDto{
		Name:                    seasonName(row.Title, n),
		ServerID:                serverID,
		ID:                      EncodeID(KindSeason, row.ID),
		Etag:                    "s" + strconv.FormatInt(row.ID, 16),
		CanDownload:             false,
		Overview:                row.Overview,
		Taglines:                []string{},
		Genres:                  []string{},
		IndexNumber:             &n,
		IsFolder:                true,
		Type:                    "Season",
		MediaType:               "Unknown",
		ParentID:                EncodeID(KindItem, row.SeriesMediaItemID),
		SeriesName:              row.SeriesTitle,
		SeriesID:                EncodeID(KindItem, row.SeriesMediaItemID),
		SeriesPrimaryImageTag:   "p" + strconv.FormatInt(row.SeriesMediaItemID, 16),
		LocationType:            "FileSystem",
		PremiereDate:            dateTime(row.AirDate),
		ImageTags:               map[string]string{"Primary": "s" + strconv.FormatInt(row.ID, 16)},
		BackdropImageTags:       []string{},
		ChildCount:              &count,
		PrimaryImageAspectRatio: &aspectPoster,
	}
	if dec != nil {
		dto.UserData = plainUserData(row.ID, dec)
	}
	return dto
}

func (s *Server) dtoFromEpisodeRow(row sqlc.JFListEpisodesRow, serverID string, dec *videoDecor) baseItemDto {
	epNum := row.EpisodeNumber
	seasonNum := row.SeasonNumber
	dto := baseItemDto{
		Name:                    firstNonEmpty(row.Title, fmt.Sprintf("Episode %d", epNum)),
		ServerID:                serverID,
		ID:                      EncodeID(KindEpisode, row.ID),
		Etag:                    "e" + strconv.FormatInt(row.ID, 16),
		CanDownload:             true,
		Overview:                row.Overview,
		Taglines:                []string{},
		Genres:                  []string{},
		CommunityRating:         ratingPtr(row.Rating),
		RunTimeTicks:            minutesToTicks(row.RuntimeMinutes),
		IndexNumber:             &epNum,
		ParentIndexNumber:       &seasonNum,
		Type:                    "Episode",
		MediaType:               "Video",
		ParentID:                EncodeID(KindSeason, row.SeasonID),
		SeriesName:              row.SeriesTitle,
		SeriesID:                EncodeID(KindItem, row.SeriesMediaItemID),
		SeasonID:                EncodeID(KindSeason, row.SeasonID),
		SeasonName:              seasonName(row.SeasonTitle, seasonNum),
		SeriesPrimaryImageTag:   "p" + strconv.FormatInt(row.SeriesMediaItemID, 16),
		LocationType:            "FileSystem",
		PremiereDate:            dateTime(row.AirDate),
		ImageTags:               map[string]string{"Primary": "e" + strconv.FormatInt(row.ID, 16)},
		BackdropImageTags:       []string{},
		PrimaryImageAspectRatio: &aspectStill,
	}
	if dec != nil {
		dto.UserData = episodeUserData(row.ID, dec)
	}
	return dto
}

func (s *Server) dtoFromAlbumRow(row sqlc.JFListAlbumsRow, serverID string, dec *videoDecor) baseItemDto {
	artistPair := nameGuidPair{Name: row.ArtistName, ID: EncodeID(KindItem, row.ArtistMediaItemID)}
	var runtime *int64
	if row.DurationSeconds > 0 {
		t := secondsToTicks(row.DurationSeconds)
		runtime = &t
	}
	count := row.TotalTracks
	dto := baseItemDto{
		Name:                    row.Title,
		ServerID:                serverID,
		ID:                      EncodeID(KindAlbum, row.ID),
		Etag:                    "a" + strconv.FormatInt(row.ID, 16),
		CanDownload:             false,
		Taglines:                []string{},
		Genres:                  orEmpty(row.Genres),
		RunTimeTicks:            runtime,
		ProductionYear:          yearPtr(row.Year),
		PremiereDate:            dateTime(row.ReleaseDate),
		IsFolder:                true,
		Type:                    "MusicAlbum",
		MediaType:               "Unknown",
		ParentID:                EncodeID(KindItem, row.ArtistMediaItemID),
		AlbumArtist:             row.ArtistName,
		AlbumArtists:            []nameGuidPair{artistPair},
		Artists:                 []string{row.ArtistName},
		ArtistItems:             []nameGuidPair{artistPair},
		LocationType:            "FileSystem",
		ImageTags:               map[string]string{"Primary": "a" + strconv.FormatInt(row.ID, 16)},
		BackdropImageTags:       []string{},
		PrimaryImageAspectRatio: &aspectSquare,
	}
	if count > 0 {
		dto.ChildCount = &count
	}
	if dec != nil {
		dto.UserData = plainUserData(row.ID, dec)
	}
	return dto
}

func (s *Server) dtoFromTrackRow(row sqlc.JFListTracksRow, serverID string, dec *videoDecor) baseItemDto {
	artistPair := nameGuidPair{Name: row.ArtistName, ID: EncodeID(KindItem, row.ArtistMediaItemID)}
	trackNum := row.TrackNumber
	discNum := row.DiscNumber
	var runtime *int64
	if row.Duration > 0 {
		t := secondsToTicks(row.Duration)
		runtime = &t
	}
	dto := baseItemDto{
		Name:                    row.Title,
		ServerID:                serverID,
		ID:                      EncodeID(KindTrack, row.ID),
		Etag:                    "t" + strconv.FormatInt(row.ID, 16),
		CanDownload:             true,
		Taglines:                []string{},
		Genres:                  orEmpty(row.AlbumGenres),
		RunTimeTicks:            runtime,
		IndexNumber:             &trackNum,
		ParentIndexNumber:       &discNum,
		Type:                    "Audio",
		MediaType:               "Audio",
		ParentID:                EncodeID(KindAlbum, row.AlbumID),
		Album:                   row.AlbumTitle,
		AlbumID:                 EncodeID(KindAlbum, row.AlbumID),
		AlbumPrimaryImageTag:    "a" + strconv.FormatInt(row.AlbumID, 16),
		AlbumArtist:             row.ArtistName,
		AlbumArtists:            []nameGuidPair{artistPair},
		Artists:                 []string{row.ArtistName},
		ArtistItems:             []nameGuidPair{artistPair},
		LocationType:            "FileSystem",
		ImageTags:               map[string]string{"Primary": "a" + strconv.FormatInt(row.AlbumID, 16)},
		BackdropImageTags:       []string{},
		PrimaryImageAspectRatio: &aspectSquare,
	}
	if dec != nil {
		dto.UserData = plainUserData(row.ID, dec)
	}
	return dto
}

// dtoFromLibrary renders a library as a Jellyfin "view" (CollectionFolder).
func (s *Server) dtoFromLibrary(lib sqlc.Library, serverID string) baseItemDto {
	return baseItemDto{
		Name:              lib.Name,
		ServerID:          serverID,
		ID:                EncodeID(KindLibrary, lib.ID),
		Etag:              "l" + strconv.FormatInt(lib.ID, 16),
		DateCreated:       tsTime(lib.CreatedAt),
		Taglines:          []string{},
		Genres:            []string{},
		IsFolder:          true,
		Type:              "CollectionFolder",
		MediaType:         "Unknown",
		CollectionType:    collectionType(lib.MediaType),
		LocationType:      "FileSystem",
		ImageTags:         map[string]string{},
		BackdropImageTags: []string{},
		UserData:          &userDataDto{Key: EncodeID(KindLibrary, lib.ID)},
	}
}

func collectionType(mt sqlc.MediaType) string {
	switch mt {
	case sqlc.MediaTypeMovie:
		return "movies"
	case sqlc.MediaTypeTv:
		return "tvshows"
	case sqlc.MediaTypeMusic:
		return "music"
	case sqlc.MediaTypeBook, sqlc.MediaTypeComic:
		return "books"
	default:
		// podcast / radio have no Jellyfin view type; a plain folder view
		// keeps clients from special-casing them.
		return "folders"
	}
}

// --- user data builders ---

func movieUserData(id int64, dec *videoDecor) *userDataDto {
	ud := &userDataDto{Key: strconv.FormatInt(id, 10), IsFavorite: dec.favorites[id]}
	if p, ok := dec.progress[id]; ok {
		ud.PlaybackPositionTicks = secondsToTicks(p.ProgressSeconds)
		ud.Played = p.Completed
		if p.TotalSeconds > 0 && !p.Completed {
			pct := float64(p.ProgressSeconds) / float64(p.TotalSeconds) * 100
			ud.PlayedPercentage = &pct
		}
	}
	if dec.watchedMovies[id] {
		ud.Played = true
		ud.PlaybackPositionTicks = 0
		ud.PlayedPercentage = nil
	}
	if ud.Played {
		ud.PlayCount = 1
	}
	return ud
}

func seriesUserData(id int64, dec *videoDecor) *userDataDto {
	ud := &userDataDto{Key: strconv.FormatInt(id, 10), IsFavorite: dec.favorites[id]}
	if c, ok := dec.showCounts[id]; ok {
		watched, total := c[0], c[1]
		if total > 0 {
			unplayed := max32(total-watched, 0)
			ud.UnplayedItemCount = &unplayed
			ud.Played = watched >= total
		}
	}
	if dec.watchedSeries[id] {
		ud.Played = true
	}
	if ud.Played {
		ud.PlayCount = 1
	}
	return ud
}

func episodeUserData(id int64, dec *videoDecor) *userDataDto {
	ud := &userDataDto{Key: strconv.FormatInt(id, 10)}
	if p, ok := dec.progress[id]; ok {
		ud.PlaybackPositionTicks = secondsToTicks(p.ProgressSeconds)
		ud.Played = p.Completed
		if p.TotalSeconds > 0 && !p.Completed {
			pct := float64(p.ProgressSeconds) / float64(p.TotalSeconds) * 100
			ud.PlayedPercentage = &pct
		}
	}
	if ud.Played {
		ud.PlayCount = 1
		ud.PlaybackPositionTicks = 0
		ud.PlayedPercentage = nil
	}
	return ud
}

// plainUserData covers kinds with favorite state only (artists, albums,
// tracks, seasons — music play history decorates in a later phase).
func plainUserData(id int64, dec *videoDecor) *userDataDto {
	return &userDataDto{Key: strconv.FormatInt(id, 10), IsFavorite: dec.favorites[id]}
}

// --- small converters ---

func tsTime(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time.UTC()
}

func dateTime(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time.UTC()
}

func etagFromTime(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return "0"
	}
	return strconv.FormatInt(ts.Time.Unix(), 16)
}

func ratingPtr(n pgtype.Numeric) *float32 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid || f.Float64 <= 0 {
		return nil
	}
	v := float32(f.Float64)
	return &v
}

func yearPtr(y string) *int32 {
	y = strings.TrimSpace(y)
	if len(y) < 4 {
		return nil
	}
	n, err := strconv.ParseInt(y[:4], 10, 32)
	if err != nil || n <= 0 {
		return nil
	}
	v := int32(n)
	return &v
}

func taglines(t string) []string {
	if t == "" {
		return []string{}
	}
	return []string{t}
}

func orEmpty(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

func seasonName(title string, n int32) string {
	if title != "" {
		return title
	}
	if n == 0 {
		return "Specials"
	}
	return fmt.Sprintf("Season %d", n)
}

func seriesStatus(s string) string {
	switch strings.ToLower(s) {
	case "ended", "canceled", "cancelled":
		return "Ended"
	case "":
		return ""
	default:
		return "Continuing"
	}
}

// providerIDs maps Heya's external_ids JSONB onto Jellyfin's ProviderIds.
func providerIDs(raw []byte, mt sqlc.MediaType) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var ext map[string]any
	if err := json.Unmarshal(raw, &ext); err != nil {
		return nil
	}
	out := make(map[string]string, len(ext))
	for k, v := range ext {
		val := stringifyID(v)
		if val == "" {
			continue
		}
		switch k {
		case "tmdb":
			out["Tmdb"] = val
		case "imdb":
			out["Imdb"] = val
		case "tvdb":
			out["Tvdb"] = val
		case "mbid":
			if mt == sqlc.MediaTypeMusic {
				out["MusicBrainzArtist"] = val
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringifyID(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
