package jellyfin

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

// List-level MediaSources decoration. Real Jellyfin embeds full MediaSources
// (plus Container and HasSubtitles) on every item of a LIST response when the
// request carries fields=MediaSources — Infuse asks for exactly that on
// /Shows/{id}/Episodes and errors its show page when the key is missing.
// Decoration is batched (one parse-result file map per series + one file
// hydration query per page) and reads stored probe data only — a list request
// must never fan out into per-item ffprobe runs.

// attachEpisodeSources decorates a page of episode dtos in place. rows and
// items are parallel slices (items[i] was mapped from rows[i]).
func (s *Server) attachEpisodeSources(ctx context.Context, rows []sqlc.JFListEpisodesRow, items []baseItemDto, token string, req itemsRequest) {
	if len(rows) == 0 || len(rows) != len(items) {
		return
	}

	// One s{n}e{n} → file-entry map per distinct series on the page.
	fileMaps := map[int64]map[string]service.EpisodeFileEntry{}
	for _, row := range rows {
		if _, ok := fileMaps[row.SeriesMediaItemID]; ok {
			continue
		}
		m, err := s.app.JFEpisodeFileEntries(ctx, row.SeriesMediaItemID)
		if err != nil {
			log.Warn().Err(err).Str("component", "jellyfin").Int64("series", row.SeriesMediaItemID).
				Msg("episode file map failed; list dtos will lack MediaSources")
			return
		}
		fileMaps[row.SeriesMediaItemID] = m
	}

	epKey := func(row sqlc.JFListEpisodesRow) string {
		return fmt.Sprintf("s%de%d", row.SeasonNumber, row.EpisodeNumber)
	}

	fileIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		if entry, ok := fileMaps[row.SeriesMediaItemID][epKey(row)]; ok {
			fileIDs = append(fileIDs, entry.FileID)
		}
	}
	files, err := s.app.JFLibraryFilesByIDs(ctx, fileIDs)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Msg("episode file hydration failed; list dtos will lack MediaSources")
		return
	}

	for i, row := range rows {
		entry, ok := fileMaps[row.SeriesMediaItemID][epKey(row)]
		if !ok {
			continue
		}
		if file, ok := files[entry.FileID]; ok {
			s.decorateWithSource(&items[i], file, token, req)
		}
	}
}

// attachMovieSources decorates a page of movie dtos in place; rows and items
// are parallel slices. Non-movie rows on a mixed page are skipped by the
// batch lookup simply not having a file for them.
func (s *Server) attachMovieSources(ctx context.Context, rows []sqlc.JFListLibraryItemsRow, items []baseItemDto, token string, req itemsRequest) {
	if len(rows) == 0 || len(rows) != len(items) {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.MediaType == sqlc.MediaTypeMovie {
			ids = append(ids, row.ID)
		}
	}
	files, err := s.app.JFBestVideoFiles(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Msg("movie file hydration failed; list dtos will lack MediaSources")
		return
	}
	for i, row := range rows {
		if file, ok := files[row.ID]; ok {
			s.decorateWithSource(&items[i], file, token, req)
		}
	}
}

// attachTrackSources decorates a page of Audio dtos in place; rows and items
// are parallel slices. Audio sources are cheap (no probe data — see
// trackSourceInfo), but Feishin refuses to queue a song whose dto lacks
// MediaSources, so lists must carry them when fields ask.
func (s *Server) attachTrackSources(ctx context.Context, rows []sqlc.JFListTracksRow, items []baseItemDto, req itemsRequest) {
	if len(rows) == 0 || len(rows) != len(items) {
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.BestFileID > 0 {
			ids = append(ids, row.BestFileID)
		}
	}
	files, err := s.app.JFTrackLibraryFiles(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Msg("track file hydration failed; list dtos will lack MediaSources")
		return
	}
	for i, row := range rows {
		file, ok := files[row.BestFileID]
		if !ok {
			continue
		}
		src := trackSourceInfo(row.BestFileID, file.Path, file.Size, row.Title, row.Duration)
		if req.fields["mediasources"] {
			items[i].MediaSources = []mediaSourceInfo{src}
		}
		items[i].Container = src.Container
		if items[i].DateCreated == nil {
			items[i].DateCreated = tsTime(file.CreatedAt)
		}
	}
}

// decorateWithSource attaches the file-derived media info to one dto:
// MediaSources and/or MediaStreams per the requested fields, plus Container,
// HasSubtitles, and DateCreated (episodes have no created_at of their own —
// the file's add time is what upstream reports too).
func (s *Server) decorateWithSource(dto *baseItemDto, file sqlc.LibraryFile, token string, req itemsRequest) {
	src, _, _ := s.mediaSourceForFile(file, dto.Name, token, nil)
	if req.fields["mediasources"] {
		dto.MediaSources = []mediaSourceInfo{src}
	}
	if req.fields["mediastreams"] {
		dto.MediaStreams = src.MediaStreams
	}
	dto.Container = src.Container
	dto.VideoType = "VideoFile"
	hasSubs := false
	for _, ms := range src.MediaStreams {
		if ms.Type == "Subtitle" {
			hasSubs = true
			break
		}
	}
	dto.HasSubtitles = &hasSubs
	if dto.DateCreated == nil {
		dto.DateCreated = tsTime(file.CreatedAt)
	}
	for _, ms := range src.MediaStreams {
		if ms.Type == "Video" {
			hd := ms.Height >= 720
			dto.IsHD = &hd
			dto.Width = ms.Width
			dto.Height = ms.Height
			break
		}
	}
}
