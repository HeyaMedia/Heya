package jellyfin

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// The /Items translation. Jellyfin funnels nearly all browsing through one
// endpoint with a wide param grid; Heya's catalog is level-addressed
// (media_items / seasons / episodes / albums / tracks). This file picks the
// level from IncludeItemTypes + ParentId, scopes it, and runs the matching
// purpose-built query. Params we consciously ignore are logged at debug —
// point a client at the server and the log is the phase-coverage worklist.

type itemsRequest struct {
	parentKind Kind
	parentID   int64
	hasParent  bool

	types      []string
	recursive  bool
	ids        []string
	searchTerm string

	// Artist scoping: AlbumArtistIds / ArtistIds / ContributingArtistIds.
	// Music clients (Feishin) fetch an artist's discography as
	// /Items?IncludeItemTypes=MusicAlbum&AlbumArtistIds={id} — ignoring the
	// filter returned the ENTIRE library on every artist page.
	// artistFiltered with artistID 0 means "filter present but matches
	// nothing" (a foreign id) → empty result, never everything.
	artistID       int64
	artistFiltered bool

	// fields is the client's Fields= request (lowercased tokens). Most dto
	// fields are always emitted, but the expensive per-item ones
	// (MediaSources/MediaStreams) attach only on request, like upstream.
	fields map[string]bool

	sortBy   string
	sortDesc bool

	startIndex int
	limit      int

	filterPlayed   bool
	filterUnplayed bool
	filterFavorite bool
}

func parseItemsRequest(r *http.Request) itemsRequest {
	req := itemsRequest{
		searchTerm: queryCI(r, "searchTerm"),
		recursive:  strings.EqualFold(queryCI(r, "recursive"), "true"),
		fields:     parseFields(r),
	}

	if pid := queryCI(r, "parentId"); pid != "" {
		if kind, id, err := DecodeID(pid); err == nil {
			req.parentKind, req.parentID, req.hasParent = kind, id, true
		} else {
			// A foreign parent (client cached from another server) matches
			// nothing — signalled by hasParent with an id no row has.
			req.parentKind, req.parentID, req.hasParent = KindInvalid, -1, true
		}
	}

	if t := queryCI(r, "includeItemTypes"); t != "" {
		for _, tok := range strings.Split(t, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				req.types = append(req.types, tok)
			}
		}
	}
	if ids := queryCI(r, "ids"); ids != "" {
		for _, tok := range strings.Split(ids, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				req.ids = append(req.ids, tok)
			}
		}
	}

	if raw := firstNonEmpty(queryCI(r, "albumArtistIds"), queryCI(r, "artistIds"), queryCI(r, "contributingArtistIds")); raw != "" {
		req.artistFiltered = true
		for _, tok := range strings.Split(raw, ",") {
			// Heya's albums/tracks hang off ONE artist media item; the first
			// resolvable id wins (clients send exactly one from artist pages).
			if id, err := DecodeIDKind(strings.TrimSpace(tok), KindItem); err == nil {
				req.artistID = id
				break
			}
		}
	}

	req.sortBy, req.sortDesc = mapSort(queryCI(r, "sortBy"), queryCI(r, "sortOrder"))
	req.startIndex, _ = strconv.Atoi(queryCI(r, "startIndex"))
	req.limit, _ = strconv.Atoi(queryCI(r, "limit"))
	if req.startIndex < 0 {
		req.startIndex = 0
	}
	if req.limit < 0 {
		req.limit = 0
	}

	for _, f := range strings.Split(queryCI(r, "filters"), ",") {
		switch strings.TrimSpace(strings.ToLower(f)) {
		case "isplayed":
			req.filterPlayed = true
		case "isunplayed":
			req.filterUnplayed = true
		case "isfavorite":
			req.filterFavorite = true
		case "isresumable":
			// Resume rails come through /UserItems/Resume; a filtered /Items
			// variant is rare. Logged via the generic ignore below.
		}
	}
	if strings.EqualFold(queryCI(r, "isPlayed"), "true") {
		req.filterPlayed = true
	}
	if strings.EqualFold(queryCI(r, "isPlayed"), "false") {
		req.filterUnplayed = true
	}
	if strings.EqualFold(queryCI(r, "isFavorite"), "true") {
		req.filterFavorite = true
	}
	return req
}

// parseFields reads the Fields= comma list into a lowercased set.
func parseFields(r *http.Request) map[string]bool {
	raw := queryCI(r, "fields")
	if raw == "" {
		return nil
	}
	out := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		if tok = strings.TrimSpace(strings.ToLower(tok)); tok != "" {
			out[tok] = true
		}
	}
	return out
}

// wantsSources reports whether the request asked for per-item media info
// (fields=MediaSources or MediaStreams) — the trigger for list-level source
// decoration.
func (r itemsRequest) wantsSources() bool {
	return r.fields["mediasources"] || r.fields["mediastreams"]
}

// mapSort translates Jellyfin SortBy/SortOrder onto the SQL sort switch.
// Jellyfin sends comma lists ("SortName,ProductionYear"); the first token we
// support wins — the rest are tiebreakers our ORDER BY approximates anyway.
func mapSort(sortBy, sortOrder string) (string, bool) {
	desc := strings.EqualFold(strings.TrimSpace(sortOrder), "Descending") ||
		strings.HasPrefix(strings.ToLower(sortOrder), "desc")
	for _, tok := range strings.Split(sortBy, ",") {
		switch strings.TrimSpace(strings.ToLower(tok)) {
		case "", "default":
			continue
		case "sortname", "name", "album", "albumartist", "seriessortname":
			return "sortname", desc
		case "datecreated", "datelastcontentadded", "dateadded":
			return "added", desc
		case "premieredate", "startdate", "airtime":
			return "premiere", desc
		case "productionyear":
			return "year", desc
		case "communityrating", "criticrating":
			return "rating", desc
		case "random":
			return "random", desc
		default:
			log.Debug().Str("component", "jellyfin").Str("sort_by", tok).Msg("unsupported SortBy token, falling back")
		}
	}
	return "sortname", desc
}

// itemLevel is which entity table a query resolves against.
type itemLevel int

const (
	levelNone itemLevel = iota
	levelItems
	levelSeasons
	levelEpisodes
	levelAlbums
	levelTracks
	levelViews
)

// resolveLevel picks the entity level from types and parent. mediaType is
// meaningful only for levelItems.
func (s *Server) resolveLevel(ctx context.Context, req *itemsRequest) (itemLevel, sqlc.MediaType) {
	for _, t := range req.types {
		switch strings.ToLower(t) {
		case "episode":
			return levelEpisodes, ""
		case "season":
			return levelSeasons, ""
		case "musicalbum":
			return levelAlbums, ""
		case "audio":
			return levelTracks, ""
		case "movie":
			return levelItems, sqlc.MediaTypeMovie
		case "series":
			return levelItems, sqlc.MediaTypeTv
		case "musicartist":
			return levelItems, sqlc.MediaTypeMusic
		case "book":
			return levelItems, sqlc.MediaTypeBook
		case "boxset", "playlist", "collectionfolder", "folder":
			// No Heya equivalent at this level yet (BoxSets → phase 3
			// collections). Empty result is correct, not an error.
			return levelNone, ""
		default:
			log.Debug().Str("component", "jellyfin").Str("include_item_type", t).Msg("unsupported IncludeItemTypes token")
		}
	}

	if !req.hasParent {
		return levelNone, ""
	}
	switch req.parentKind {
	case KindLibrary:
		lib, err := s.app.GetLibrary(ctx, req.parentID)
		if err != nil {
			return levelNone, ""
		}
		return levelItems, lib.MediaType
	case KindItem:
		// A media_item parent: series → seasons (episodes when recursive),
		// artist → albums. Movie/book parents have no children.
		rows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{
			MediaType: sqlc.MediaTypeTv, OnlyIds: []int64{req.parentID}, Lim: 1,
		})
		if err == nil && len(rows) == 1 {
			if req.recursive {
				return levelEpisodes, ""
			}
			return levelSeasons, ""
		}
		rows, _, err = s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{
			MediaType: sqlc.MediaTypeMusic, OnlyIds: []int64{req.parentID}, Lim: 1,
		})
		if err == nil && len(rows) == 1 {
			return levelAlbums, ""
		}
		return levelNone, ""
	case KindSeason:
		return levelEpisodes, ""
	case KindAlbum:
		return levelTracks, ""
	}
	return levelNone, ""
}

// searchTypes returns the item types a request should span. resolveLevel can
// only pick ONE level, so a request naming several IncludeItemTypes (or a
// top-level search naming none) must fan out per type and merge — otherwise
// only the first type's level is ever queried, which is why global search
// (Infuse sends Movie,Series,Episode,MusicAlbum,Audio,... or nothing)
// returned nothing.
func (r itemsRequest) searchTypes() []string {
	known := map[string]bool{
		"movie": true, "series": true, "episode": true, "season": true,
		"musicalbum": true, "audio": true, "musicartist": true, "book": true,
	}
	var out []string
	for _, t := range r.types {
		if known[strings.ToLower(t)] {
			out = append(out, t)
		}
	}
	if len(out) > 0 {
		return out
	}
	// No explicit types + a search term at library scope (no specific parent):
	// span the everyday catalog kinds, like a Jellyfin global search.
	if r.searchTerm != "" && (!r.hasParent || r.parentKind == KindLibrary) {
		return []string{"Movie", "Series", "Episode", "MusicArtist", "MusicAlbum", "Audio"}
	}
	return out
}

// queryMultiType fans a multi-type request out into one single-type
// sub-request per type (each recurses through the single-level switch),
// then merges, sorts by name, and paginates the combined set.
func (s *Server) queryMultiType(ctx context.Context, userID int64, serverID string, req itemsRequest, types []string) (queryResult[baseItemDto], error) {
	merged := []baseItemDto{}
	total := 0
	for _, t := range types {
		sub := req
		sub.types = []string{t}
		sub.startIndex = 0 // paginate after merge
		if req.limit > 0 {
			sub.limit = req.startIndex + req.limit // enough to cover the page
		}
		res, err := s.queryItems(ctx, userID, serverID, sub)
		if err != nil {
			continue // one level failing shouldn't empty the whole search
		}
		merged = append(merged, res.Items...)
		total += res.TotalRecordCount
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].SortName) < strings.ToLower(merged[j].SortName)
	})
	if req.startIndex > 0 && req.startIndex < len(merged) {
		merged = merged[req.startIndex:]
	} else if req.startIndex >= len(merged) {
		merged = []baseItemDto{}
	}
	if req.limit > 0 && len(merged) > req.limit {
		merged = merged[:req.limit]
	}
	return queryResult[baseItemDto]{Items: merged, TotalRecordCount: total, StartIndex: req.startIndex}, nil
}

// queryItems executes a parsed /Items request for user and returns the
// Jellyfin list envelope.
func (s *Server) queryItems(ctx context.Context, userID int64, serverID string, req itemsRequest) (queryResult[baseItemDto], error) {
	empty := queryResult[baseItemDto]{Items: []baseItemDto{}, StartIndex: req.startIndex}

	// Ids= hydration: group by kind, fetch, return in request order.
	if len(req.ids) > 0 {
		return s.queryByIDs(ctx, userID, serverID, req)
	}

	// Multi-type / typeless search fans out per type and merges.
	if types := req.searchTypes(); len(types) > 1 {
		return s.queryMultiType(ctx, userID, serverID, req, types)
	}

	level, mediaType := s.resolveLevel(ctx, &req)
	lim, off := int32(req.limit), int32(req.startIndex)

	switch level {
	case levelItems:
		if mediaType == "" {
			return empty, nil
		}
		params := sqlc.JFListLibraryItemsParams{
			MediaType: mediaType,
			Search:    req.searchTerm,
			SortBy:    req.sortBy,
			SortDesc:  req.sortDesc,
			RandSeed:  randSeed(userID),
			Lim:       lim,
			Off:       off,
		}
		if req.hasParent && req.parentKind == KindLibrary {
			params.LibraryID = req.parentID
		} else if req.hasParent && req.parentKind != KindLibrary {
			return empty, nil
		}

		dec, err := s.videoDecorations(ctx, userID)
		if err != nil {
			return empty, err
		}
		if req.filterPlayed || req.filterUnplayed {
			params.FilterPlayed = req.filterPlayed
			params.FilterUnplayed = req.filterUnplayed
			params.PlayedIds = playedIDsFor(mediaType, dec)
		}
		if req.filterFavorite {
			params.FilterFavorite = true
			params.FavoriteIds = keys(dec.favorites)
		}

		rows, total, err := s.app.JFListLibraryItems(ctx, params)
		if err != nil {
			return empty, err
		}
		if mediaType == sqlc.MediaTypeMovie {
			s.loadProgress(ctx, userID, "movie", rowIDs(rows), dec)
		}
		items := make([]baseItemDto, 0, len(rows))
		for _, row := range rows {
			items = append(items, s.dtoFromMediaItemRow(row, serverID, dec))
		}
		if mediaType == sqlc.MediaTypeMovie && req.wantsSources() {
			s.attachMovieSources(ctx, rows, items, TokenFrom(ctx), req)
		}
		return queryResult[baseItemDto]{Items: items, TotalRecordCount: int(total), StartIndex: req.startIndex}, nil

	case levelSeasons:
		seriesID := int64(0)
		if req.hasParent && req.parentKind == KindItem {
			seriesID = req.parentID
		}
		if seriesID == 0 {
			return empty, nil
		}
		rows, err := s.app.JFListSeasons(ctx, seriesID, nil)
		if err != nil {
			return empty, err
		}
		dec := s.favoriteDecor(ctx, userID, "season")
		items := make([]baseItemDto, 0, len(rows))
		for _, row := range rows {
			items = append(items, s.dtoFromSeasonRow(row, serverID, dec))
		}
		return queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items), StartIndex: 0}, nil

	case levelEpisodes:
		params := sqlc.JFListEpisodesParams{
			Search: req.searchTerm,
			SortBy: req.sortBy,
			Lim:    lim,
			Off:    off,
		}
		if req.hasParent {
			switch req.parentKind {
			case KindSeason:
				params.SeasonID = req.parentID
			case KindItem:
				params.SeriesMediaItemID = req.parentID
			case KindLibrary:
				params.LibraryID = req.parentID
			default:
				return empty, nil
			}
		}
		rows, total, err := s.app.JFListEpisodes(ctx, params)
		if err != nil {
			return empty, err
		}
		dec := s.episodeDecorations(ctx, userID)
		s.loadProgress(ctx, userID, "episode", episodeIDs(rows), dec)
		items := make([]baseItemDto, 0, len(rows))
		for _, row := range rows {
			items = append(items, s.dtoFromEpisodeRow(row, serverID, dec))
		}
		if req.wantsSources() {
			s.attachEpisodeSources(ctx, rows, items, TokenFrom(ctx), req)
		}
		return queryResult[baseItemDto]{Items: items, TotalRecordCount: int(total), StartIndex: req.startIndex}, nil

	case levelAlbums:
		params := sqlc.JFListAlbumsParams{
			Search:   req.searchTerm,
			SortBy:   req.sortBy,
			SortDesc: req.sortDesc,
			RandSeed: randSeed(userID),
			Lim:      lim,
			Off:      off,
		}
		if req.artistFiltered {
			if req.artistID == 0 {
				return empty, nil
			}
			params.ArtistMediaItemID = req.artistID
		}
		if req.hasParent {
			switch req.parentKind {
			case KindItem:
				params.ArtistMediaItemID = req.parentID
			case KindLibrary:
				params.LibraryID = req.parentID
			default:
				return empty, nil
			}
		}
		rows, total, err := s.app.JFListAlbums(ctx, params)
		if err != nil {
			return empty, err
		}
		dec := s.favoriteDecor(ctx, userID, "album")
		items := make([]baseItemDto, 0, len(rows))
		for _, row := range rows {
			items = append(items, s.dtoFromAlbumRow(row, serverID, dec))
		}
		return queryResult[baseItemDto]{Items: items, TotalRecordCount: int(total), StartIndex: req.startIndex}, nil

	case levelTracks:
		params := sqlc.JFListTracksParams{
			Search:   req.searchTerm,
			SortBy:   trackSort(req.sortBy),
			SortDesc: req.sortDesc,
			RandSeed: randSeed(userID),
			Lim:      lim,
			Off:      off,
		}
		if req.artistFiltered {
			if req.artistID == 0 {
				return empty, nil
			}
			params.ArtistMediaItemID = req.artistID
		}
		if req.hasParent {
			switch req.parentKind {
			case KindAlbum:
				params.AlbumID = req.parentID
			case KindItem:
				params.ArtistMediaItemID = req.parentID
			case KindLibrary:
				params.LibraryID = req.parentID
			default:
				return empty, nil
			}
		}
		rows, total, err := s.app.JFListTracks(ctx, params)
		if err != nil {
			return empty, err
		}
		dec := s.favoriteDecor(ctx, userID, "track")
		items := make([]baseItemDto, 0, len(rows))
		for _, row := range rows {
			items = append(items, s.dtoFromTrackRow(row, serverID, dec))
		}
		if req.wantsSources() {
			s.attachTrackSources(ctx, rows, items, req)
		}
		return queryResult[baseItemDto]{Items: items, TotalRecordCount: int(total), StartIndex: req.startIndex}, nil
	}

	log.Debug().Str("component", "jellyfin").
		Strs("types", req.types).Bool("has_parent", req.hasParent).
		Msg("unresolvable /Items request shape — returning empty result")
	return empty, nil
}

// queryByIDs hydrates an explicit id list, preserving request order.
func (s *Server) queryByIDs(ctx context.Context, userID int64, serverID string, req itemsRequest) (queryResult[baseItemDto], error) {
	byKind := map[Kind][]int64{}
	order := make([]string, 0, len(req.ids))
	for _, raw := range req.ids {
		kind, id, err := DecodeID(raw)
		if err != nil {
			continue
		}
		byKind[kind] = append(byKind[kind], id)
		order = append(order, raw)
	}

	dec, err := s.videoDecorations(ctx, userID)
	if err != nil {
		return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
	}
	found := map[string]baseItemDto{}

	for kind, ids := range byKind {
		switch kind {
		case KindItem:
			for _, mt := range []sqlc.MediaType{sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeMusic, sqlc.MediaTypeBook} {
				rows, _, err := s.app.JFListLibraryItems(ctx, sqlc.JFListLibraryItemsParams{MediaType: mt, OnlyIds: ids})
				if err != nil {
					return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
				}
				if mt == sqlc.MediaTypeMovie {
					s.loadProgress(ctx, userID, "movie", rowIDs(rows), dec)
				}
				dtos := make([]baseItemDto, len(rows))
				for i, row := range rows {
					dtos[i] = s.dtoFromMediaItemRow(row, serverID, dec)
				}
				if mt == sqlc.MediaTypeMovie && req.wantsSources() {
					s.attachMovieSources(ctx, rows, dtos, TokenFrom(ctx), req)
				}
				for i, row := range rows {
					found[EncodeID(KindItem, row.ID)] = dtos[i]
				}
			}
		case KindSeason:
			rows, err := s.app.JFListSeasons(ctx, 0, ids)
			if err != nil {
				return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
			}
			seasonDec := s.favoriteDecor(ctx, userID, "season")
			for _, row := range rows {
				found[EncodeID(KindSeason, row.ID)] = s.dtoFromSeasonRow(row, serverID, seasonDec)
			}
		case KindEpisode:
			rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: ids})
			if err != nil {
				return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
			}
			epDec := s.episodeDecorations(ctx, userID)
			s.loadProgress(ctx, userID, "episode", episodeIDs(rows), epDec)
			dtos := make([]baseItemDto, len(rows))
			for i, row := range rows {
				dtos[i] = s.dtoFromEpisodeRow(row, serverID, epDec)
			}
			if req.wantsSources() {
				s.attachEpisodeSources(ctx, rows, dtos, TokenFrom(ctx), req)
			}
			for i, row := range rows {
				found[EncodeID(KindEpisode, row.ID)] = dtos[i]
			}
		case KindAlbum:
			rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: ids})
			if err != nil {
				return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
			}
			albumDec := s.favoriteDecor(ctx, userID, "album")
			for _, row := range rows {
				found[EncodeID(KindAlbum, row.ID)] = s.dtoFromAlbumRow(row, serverID, albumDec)
			}
		case KindTrack:
			rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: ids})
			if err != nil {
				return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
			}
			trackDec := s.favoriteDecor(ctx, userID, "track")
			dtos := make([]baseItemDto, len(rows))
			for i, row := range rows {
				dtos[i] = s.dtoFromTrackRow(row, serverID, trackDec)
			}
			if req.wantsSources() {
				s.attachTrackSources(ctx, rows, dtos, req)
			}
			for i, row := range rows {
				found[EncodeID(KindTrack, row.ID)] = dtos[i]
			}
		case KindLibrary:
			libs, err := s.app.ListLibraries(ctx)
			if err != nil {
				return queryResult[baseItemDto]{Items: []baseItemDto{}}, err
			}
			for _, id := range ids {
				if id == 0 { // the aggregate root folder
					found[EncodeID(KindLibrary, 0)] = s.rootFolderDto(serverID)
					continue
				}
				for _, lib := range libs {
					if lib.ID == id {
						found[EncodeID(KindLibrary, lib.ID)] = s.dtoFromLibrary(lib, serverID)
					}
				}
			}
		}
	}

	items := make([]baseItemDto, 0, len(found))
	for _, raw := range order {
		kind, id, err := DecodeID(raw)
		if err != nil {
			continue
		}
		if dto, ok := found[EncodeID(kind, id)]; ok {
			items = append(items, dto)
		}
	}
	return queryResult[baseItemDto]{Items: items, TotalRecordCount: len(items)}, nil
}

// favoriteDecor loads just the favorites set for one entity type — the
// lightweight decoration for kinds that don't need the movie/series sets
// videoDecorations pulls. Each favoritable kind has its own id-space
// ("episode", "season", "album", "track"), matching what handleSetFavorite
// writes; mixing sets would cross-match unrelated int64 ids.
func (s *Server) favoriteDecor(ctx context.Context, userID int64, entityType string) *videoDecor {
	favs, err := s.app.JFFavoriteIDs(ctx, userID, entityType)
	if err != nil {
		favs = map[int64]bool{}
	}
	return &videoDecor{favorites: favs}
}

func (s *Server) episodeDecorations(ctx context.Context, userID int64) *videoDecor {
	return s.favoriteDecor(ctx, userID, "episode")
}

// videoDecorations loads the per-user id-sets once per request.
func (s *Server) videoDecorations(ctx context.Context, userID int64) (*videoDecor, error) {
	watchedMovies, watchedSeries, favorites, showCounts, err := s.app.JFUserVideoSets(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &videoDecor{
		watchedMovies: watchedMovies,
		watchedSeries: watchedSeries,
		favorites:     favorites,
		showCounts:    showCounts,
		progress:      map[int64]sqlc.JFListWatchProgressByIDsRow{},
	}, nil
}

// loadProgress fills dec.progress for a page of entity ids. Errors degrade to
// missing resume positions rather than failing the browse — but loudly.
func (s *Server) loadProgress(ctx context.Context, userID int64, entityType string, ids []int64, dec *videoDecor) {
	if dec.progress == nil {
		dec.progress = map[int64]sqlc.JFListWatchProgressByIDsRow{}
	}
	rows, err := s.app.JFWatchProgressByIDs(ctx, userID, entityType, ids)
	if err != nil {
		log.Warn().Err(err).Str("component", "jellyfin").Str("entity_type", entityType).Msg("progress decoration failed; dtos will lack resume state")
		return
	}
	for id, row := range rows {
		dec.progress[id] = row
	}
}

func playedIDsFor(mt sqlc.MediaType, dec *videoDecor) []int64 {
	switch mt {
	case sqlc.MediaTypeMovie:
		return keys(dec.watchedMovies)
	case sqlc.MediaTypeTv:
		return keys(dec.watchedSeries)
	default:
		return []int64{}
	}
}

func trackSort(s string) string {
	// Track default ordering is album/disc/track; only explicit name and
	// random sorts override.
	switch s {
	case "sortname":
		return "name"
	case "random":
		return "random"
	default:
		return ""
	}
}

// randSeed keeps Random sort stable across pagination: same user, same day,
// same shuffle.
func randSeed(userID int64) string {
	return strconv.FormatInt(userID, 10) + time.Now().UTC().Format("2006-01-02")
}

func keys(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func rowIDs(rows []sqlc.JFListLibraryItemsRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func episodeIDs(rows []sqlc.JFListEpisodesRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}
