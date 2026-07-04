package jellyfin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// GET /Items — the universal browse endpoint.
func (s *Server) handleItems(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), parseItemsRequest(r))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /Items/{itemId} — single-item hydration.
func (s *Server) handleItemByID(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	req := itemsRequest{ids: []string{p["itemId"]}}
	res, err := s.queryByIDs(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(res.Items) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, res.Items[0])
}

// GET /UserViews — one view per Heya library.
func (s *Server) handleUserViews(w http.ResponseWriter, r *http.Request, _ Params) {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	serverID := s.serverID(r)
	items := make([]baseItemDto, 0, len(libs))
	for _, lib := range libs {
		items = append(items, s.dtoFromLibrary(lib, serverID))
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items)})
}

// GET /Items/Latest — home-screen "Latest in <library>" rails. Returns a bare
// array (not the queryResult envelope), matching upstream.
func (s *Server) handleItemsLatest(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	req := parseItemsRequest(r)
	req.sortBy, req.sortDesc = "added", true
	if req.limit == 0 {
		req.limit = 16
	}
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res.Items)
}

// GET /UserItems/Resume — continue watching.
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	ctx := r.Context()
	serverID := s.serverID(r)

	// Audio resume isn't a Heya concept (music scrobbles, not bookmarks).
	if mt := queryCI(r, "mediaTypes"); mt != "" && !strings.Contains(strings.ToLower(mt), "video") {
		writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
		return
	}

	limit, _ := strconv.Atoi(queryCI(r, "limit"))
	if limit <= 0 {
		limit = 12
	}

	rows, err := s.app.ListContinueWatching(ctx, u.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}

	var movieIDs, episodeIDs []int64
	dec := s.favoriteDecor(ctx, u.ID, "media_item")
	dec.progress = map[int64]sqlc.JFListWatchProgressByIDsRow{}
	for _, row := range rows {
		pr := sqlc.JFListWatchProgressByIDsRow{
			EntityID:        row.EntityID,
			ProgressSeconds: row.ProgressSeconds,
			TotalSeconds:    row.TotalSeconds,
		}
		switch row.EntityType {
		case "movie":
			movieIDs = append(movieIDs, row.MediaItemID)
			pr.EntityID = row.MediaItemID
			dec.progress[row.MediaItemID] = pr
		case "episode":
			episodeIDs = append(episodeIDs, row.EntityID)
			dec.progress[row.EntityID] = pr
		}
	}

	byID := map[string]baseItemDto{}
	if len(movieIDs) > 0 {
		mrows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{MediaType: sqlc.MediaTypeMovie, OnlyIds: movieIDs})
		if err == nil {
			for _, mr := range mrows {
				byID["m"+strconv.FormatInt(mr.ID, 10)] = s.dtoFromMediaItemRow(mr, serverID, dec)
			}
		}
	}
	if len(episodeIDs) > 0 {
		erows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: episodeIDs})
		if err == nil {
			for _, er := range erows {
				byID["e"+strconv.FormatInt(er.ID, 10)] = s.dtoFromEpisodeRow(er, serverID, dec)
			}
		}
	}

	items := make([]baseItemDto, 0, len(rows))
	for _, row := range rows {
		var key string
		switch row.EntityType {
		case "movie":
			key = "m" + strconv.FormatInt(row.MediaItemID, 10)
		case "episode":
			key = "e" + strconv.FormatInt(row.EntityID, 10)
		}
		if dto, ok := byID[key]; ok {
			items = append(items, dto)
		}
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items)})
}

// GET /Shows/NextUp — next unwatched episode per series.
func (s *Server) handleNextUp(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	ctx := r.Context()
	serverID := s.serverID(r)

	limit, _ := strconv.Atoi(queryCI(r, "limit"))
	if limit <= 0 {
		limit = 16
	}

	var seriesIDs []int64
	if sid := queryCI(r, "seriesId"); sid != "" {
		id, err := DecodeIDKind(sid, KindItem)
		if err != nil {
			writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
			return
		}
		seriesIDs = []int64{id}
	} else {
		recents, err := s.app.ListRecentlyWatched(ctx, u.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		seen := map[int64]bool{}
		for _, row := range recents {
			if row.MediaType != string(sqlc.MediaTypeTv) || seen[row.MediaItemID] {
				continue
			}
			seen[row.MediaItemID] = true
			seriesIDs = append(seriesIDs, row.MediaItemID)
			if len(seriesIDs) >= limit {
				break
			}
		}
	}

	var epIDs []int64
	for _, seriesID := range seriesIDs {
		next, ok, err := s.app.JFNextUnwatchedEpisode(ctx, u.ID, seriesID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if ok {
			epIDs = append(epIDs, next.EpisodeID)
		}
	}

	items := []baseItemDto{}
	if len(epIDs) > 0 {
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: epIDs})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		dec := s.episodeDecorations(ctx, u.ID)
		s.loadProgress(ctx, u.ID, "episode", epIDs, dec)
		byID := map[int64]baseItemDto{}
		for _, row := range rows {
			byID[row.ID] = s.dtoFromEpisodeRow(row, serverID, dec)
		}
		for _, id := range epIDs { // preserve series-recency order
			if dto, ok := byID[id]; ok {
				items = append(items, dto)
			}
		}
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items), StartIndex: 0})
}

// GET /Shows/{seriesId}/Seasons
func (s *Server) handleShowSeasons(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	seriesID, err := DecodeIDKind(p["seriesId"], KindItem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req := itemsRequest{hasParent: true, parentKind: KindItem, parentID: seriesID}
	req.types = []string{"Season"}
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /Shows/{seriesId}/Episodes — optionally scoped to ?seasonId=.
func (s *Server) handleShowEpisodes(w http.ResponseWriter, r *http.Request, p Params) {
	u, _ := UserFrom(r.Context())
	seriesID, err := DecodeIDKind(p["seriesId"], KindItem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req := parseItemsRequest(r)
	req.types = []string{"Episode"}
	req.hasParent, req.parentKind, req.parentID = true, KindItem, seriesID
	if sid := queryCI(r, "seasonId"); sid != "" {
		if seasonID, err := DecodeIDKind(sid, KindSeason); err == nil {
			req.parentKind, req.parentID = KindSeason, seasonID
		}
	}
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /Artists and /Artists/AlbumArtists — Heya artists are album artists.
func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	req := parseItemsRequest(r)
	req.types = []string{"MusicArtist"}
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
