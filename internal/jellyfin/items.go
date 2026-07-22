package jellyfin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
)

// routeUserOK validates an explicit userId (path param or query) the way
// upstream's RequestHelpers.GetUserId does: your own id always passes, a
// non-admin naming another user gets 403, and an id no user has gets 404.
// Handlers still act on the AUTHENTICATED user — Heya has no per-user
// impersonation on this surface — but the status semantics match.
func (s *Server) routeUserOK(w http.ResponseWriter, r *http.Request, p Params) bool {
	raw := p["userId"]
	if raw == "" {
		raw = queryCI(r, "userId")
	}
	if raw == "" {
		return true
	}
	cur, _ := UserFrom(r.Context())
	id, err := DecodeIDKind(raw, KindUser)
	if err != nil {
		http.NotFound(w, r)
		return false
	}
	if id == cur.ID {
		return true
	}
	if !cur.IsAdmin {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	if _, err := s.app.SessionLookup().GetUserByID(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return false
	}
	return true
}

// itemExists is the lightweight "does this id resolve to anything" probe —
// upstream 404s Theme/Similar/etc. requests for foreign ids instead of
// returning empty results.
func (s *Server) itemExists(ctx context.Context, encoded string) bool {
	kind, id, err := DecodeID(encoded)
	if err != nil {
		return false
	}
	switch kind {
	case KindLibrary:
		if id == 0 {
			return true // the aggregate root folder
		}
		_, err := s.app.GetLibrary(ctx, id)
		return err == nil
	case KindItem:
		for _, mt := range []sqlc.MediaType{sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeMusic, sqlc.MediaTypeBook} {
			rows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{MediaType: mt, OnlyIds: []int64{id}, Lim: 1})
			if err == nil && len(rows) > 0 {
				return true
			}
		}
	case KindSeason:
		rows, err := s.app.JFListSeasons(ctx, 0, []int64{id})
		return err == nil && len(rows) > 0
	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		return err == nil && len(rows) > 0
	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: []int64{id}})
		return err == nil && len(rows) > 0
	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		return err == nil && len(rows) > 0
	}
	return false
}

// requireItem wraps a handler behind an itemExists check on {itemId}.
func (s *Server) requireItem(next handlerFunc) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p Params) {
		if !s.itemExists(r.Context(), p["itemId"]) {
			http.NotFound(w, r)
			return
		}
		next(w, r, p)
	}
}

// GET /Items — the universal browse endpoint.
func (s *Server) handleItems(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	u, _ := UserFrom(r.Context())
	res, err := s.queryItems(r.Context(), u.ID, s.serverID(r), parseItemsRequest(r))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /Items/Root and /Users/{userId}/Items/Root — the aggregate root
// folder, the one item every Jellyfin server is guaranteed to have. Clients
// use it as the browse anchor and as the auth smoke test.
func (s *Server) handleItemsRoot(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	writeJSON(w, http.StatusOK, s.rootFolderDto(s.serverID(r)))
}

// GET /Items/{itemId} — single-item hydration.
func (s *Server) handleItemByID(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	u, _ := UserFrom(r.Context())
	req := itemsRequest{ids: []string{p["itemId"]}, fields: parseFields(r)}
	res, err := s.queryByIDs(r.Context(), u.ID, s.serverID(r), req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(res.Items) == 0 {
		http.NotFound(w, r)
		return
	}
	dto := res.Items[0]
	// Full detail carries MediaSources for playable items, like upstream —
	// Infuse decides video playability from the detail response, and Feishin
	// won't queue a song without sources.
	switch dto.Type {
	case "Movie", "Episode":
		s.attachVideoSource(r.Context(), &dto, p["itemId"])
	case "Audio":
		if target, ok := s.resolvePlayTarget(r.Context(), p["itemId"]); ok {
			src := s.trackMediaSource(target)
			dto.MediaSources = []mediaSourceInfo{src}
			dto.Container = src.Container
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

// DELETE /Items/{itemId} and DELETE /Items?ids= — Heya media is immutable
// through the Jellyfin surface: unknown ids 404 (like upstream), known ids
// 403 (a deliberate "you may not", never a silent no-op 204).
func (s *Server) handleDeleteItems(w http.ResponseWriter, r *http.Request, p Params) {
	var ids []string
	if v := p["itemId"]; v != "" {
		ids = append(ids, v)
	}
	for _, tok := range strings.Split(queryCI(r, "ids"), ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			ids = append(ids, tok)
		}
	}
	if len(ids) == 0 {
		http.NotFound(w, r)
		return
	}
	for _, id := range ids {
		if !s.itemExists(r.Context(), id) {
			http.NotFound(w, r)
			return
		}
	}
	w.WriteHeader(http.StatusForbidden)
}

// GET /Items/{itemId}/Intros (+ the /Users/{userId}/... legacy form) — Heya
// has no cinema-intros feature; an empty result after real user/item
// validation is exactly what an upstream server without intros returns.
func (s *Server) handleItemIntros(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	if !s.itemExists(r.Context(), p["itemId"]) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
}

// GET /Items/{itemId}/LocalTrailers and /SpecialFeatures — bare arrays, not
// the queryResult envelope (upstream returns BaseItemDto[]).
func (s *Server) handleItemExtrasArray(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	if !s.itemExists(r.Context(), p["itemId"]) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, []baseItemDto{})
}

// GET /Users/{userId}/Items/{itemId}/Lyrics — legacy alias; validates like
// upstream, then answers via the same lyrics path as /Audio/{itemId}/Lyrics.
func (s *Server) handleUserItemLyrics(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	if !s.itemExists(r.Context(), p["itemId"]) {
		http.NotFound(w, r)
		return
	}
	s.handleLyrics(w, r, p)
}

// GET /UserViews — one view per Heya library.
func (s *Server) handleUserViews(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	serverID := s.serverID(r)
	items := make([]baseItemDto, 0, len(libs))
	for _, lib := range libs {
		dto := s.dtoFromLibrary(lib, serverID)
		// ChildCount like upstream: item total for the view. A count query
		// per view is fine — views are a handful of rows.
		if _, total, err := s.app.JFListLibraryItems(r.Context(), sqlc.JFListLibraryItemsParams{MediaType: mediatype.Runtime(lib.MediaType), LibraryID: lib.ID, Lim: 1}); err == nil {
			n := int32(total)
			dto.ChildCount = &n
		}
		items = append(items, dto)
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items)})
}

// GET /UserViews/GroupingOptions — the standalone movie/TV folders a user
// could group; upstream returns {Name, Id} per eligible library.
func (s *Server) handleGroupingOptions(w http.ResponseWriter, r *http.Request, _ Params) {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	out := []nameGuidPair{}
	for _, lib := range libs {
		if mediatype.IsVideo(lib.MediaType) {
			out = append(out, nameGuidPair{Name: lib.Name, ID: EncodeID(KindLibrary, lib.ID)})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /Items/Latest — home-screen "Latest in <library>" rails. Returns a bare
// array (not the queryResult envelope), matching upstream.
func (s *Server) handleItemsLatest(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
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
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
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

	rows, err := s.app.ListContinueWatching(ctx, u.ID, int32(min(limit, 100)), 0)
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

	req := parseItemsRequest(r)
	byID := map[string]baseItemDto{}
	if len(movieIDs) > 0 {
		mrows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{MediaType: sqlc.MediaTypeMovie, OnlyIds: movieIDs})
		if err == nil {
			dtos := make([]baseItemDto, len(mrows))
			for i, mr := range mrows {
				dtos[i] = s.dtoFromMediaItemRow(mr, serverID, dec)
			}
			if req.wantsSources() {
				s.attachMovieSources(ctx, mrows, dtos, TokenFrom(ctx), req)
			}
			for i, mr := range mrows {
				byID["m"+strconv.FormatInt(mr.ID, 10)] = dtos[i]
			}
		}
	}
	if len(episodeIDs) > 0 {
		erows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: episodeIDs})
		if err == nil {
			dtos := make([]baseItemDto, len(erows))
			for i, er := range erows {
				dtos[i] = s.dtoFromEpisodeRow(er, serverID, dec)
			}
			if req.wantsSources() {
				s.attachEpisodeSources(ctx, erows, dtos, TokenFrom(ctx), req)
			}
			for i, er := range erows {
				byID["e"+strconv.FormatInt(er.ID, 10)] = dtos[i]
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
func (s *Server) handleNextUp(w http.ResponseWriter, r *http.Request, p Params) {
	if !s.routeUserOK(w, r, p) {
		return
	}
	u, _ := UserFrom(r.Context())
	ctx := r.Context()
	serverID := s.serverID(r)

	limit64, _ := strconv.ParseInt(queryCI(r, "limit"), 10, 32)
	limit := int32(limit64)
	if limit <= 0 {
		limit = 16
	}
	if limit > 100 {
		limit = 100
	}

	var epIDs []int64
	if sid := queryCI(r, "seriesId"); sid != "" {
		// Explicit series probe (clients ask after finishing an episode) —
		// direct lookup, no recency window involved.
		id, err := DecodeIDKind(sid, KindItem)
		if err != nil {
			writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
			return
		}
		next, ok, err := s.app.JFNextUnwatchedEpisode(ctx, u.ID, id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if ok {
			epIDs = append(epIDs, next.EpisodeID)
		}
	} else {
		// Same server-owned rail as the native Home: one query, per
		// recently-watched series the next unwatched episode that has a
		// playable file — specials excluded, fully-watched series
		// prefiltered. The old derivation here (first 20 recently-watched
		// rows → per-series next-episode fan-out) went blind after a bulk
		// mark-watched pass and nominated S0 specials for finished shows.
		rail, err := s.app.ListUpNextRail(ctx, u.ID, min(limit, 50), 0)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		for _, item := range rail {
			epIDs = append(epIDs, item.EpisodeID)
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
		dtos := make([]baseItemDto, len(rows))
		for i, row := range rows {
			dtos[i] = s.dtoFromEpisodeRow(row, serverID, dec)
		}
		if req := parseItemsRequest(r); req.wantsSources() {
			s.attachEpisodeSources(ctx, rows, dtos, TokenFrom(ctx), req)
		}
		byID := map[int64]baseItemDto{}
		for i, row := range rows {
			byID[row.ID] = dtos[i]
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
