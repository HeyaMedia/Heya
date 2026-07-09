package subsonic

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Album/song list + search endpoints.

func intParam(r *http.Request, name string, def int32) int32 {
	v, err := strconv.ParseInt(param(r, name), 10, 32)
	if err != nil {
		return def
	}
	return int32(v)
}

// albumList2 dispatches getAlbumList2's type param. alphabetical / random /
// starred ride the existing listers; the ranking types (newest, frequent,
// recent, byYear, byGenre) come from the id-ranking queries and are
// re-ordered after hydration.
func (s *Server) albumList2(w http.ResponseWriter, r *http.Request) ([]AlbumID3, bool) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	listType := param(r, "type")
	size := intParam(r, "size", 10)
	if size <= 0 || size > 500 {
		size = 10
	}
	offset := intParam(r, "offset", 0)

	switch listType {
	case "alphabeticalByName":
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{Lim: size, Off: offset})
		if err != nil {
			respondError(w, r, errGeneric, "listing albums failed")
			return nil, false
		}
		return s.albumID3sFrom(ctx, u.ID, rows), true
	case "alphabeticalByArtist":
		// The generic lister has no artist-name sort; name sort is close
		// enough that no client has noticed — worth a dedicated query only
		// if one does.
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{Lim: size, Off: offset})
		if err != nil {
			respondError(w, r, errGeneric, "listing albums failed")
			return nil, false
		}
		return s.albumID3sFrom(ctx, u.ID, rows), true
	case "random":
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{
			SortBy: "random", RandSeed: strconv.FormatInt(time.Now().UnixNano(), 10), Lim: size,
		})
		if err != nil {
			respondError(w, r, errGeneric, "listing albums failed")
			return nil, false
		}
		return s.albumID3sFrom(ctx, u.ID, rows), true
	case "starred", "highest":
		// "highest" (community rating) has no Heya equivalent; starred is
		// the least-wrong neighbor and keeps clients' rails populated.
		page, err := s.app.ListUserLovedAlbums(ctx, u.ID, size, offset)
		if err != nil || page == nil {
			respondError(w, r, errGeneric, "listing starred albums failed")
			return nil, false
		}
		ids := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			ids = append(ids, it.ID)
		}
		return s.albumsByIDs(ctx, u.ID, ids), true
	case "newest", "frequent", "recent", "byYear", "byGenre":
		if listType == "byYear" && (param(r, "fromYear") == "" || param(r, "toYear") == "") {
			respondError(w, r, errMissingParameter, "byYear requires fromYear and toYear")
			return nil, false
		}
		if listType == "byGenre" && param(r, "genre") == "" {
			respondError(w, r, errMissingParameter, "byGenre requires genre")
			return nil, false
		}
		ids, err := s.app.SubsonicAlbumIDsByList(ctx, listType, u.ID, size, offset,
			param(r, "genre"), intParam(r, "fromYear", 0), intParam(r, "toYear", 0))
		if err != nil {
			respondError(w, r, errGeneric, "listing albums failed")
			return nil, false
		}
		return s.albumsByIDs(ctx, u.ID, ids), true
	case "":
		respondError(w, r, errMissingParameter, `required parameter "type" is missing`)
		return nil, false
	}
	respondError(w, r, errGeneric, "unknown album list type: "+listType)
	return nil, false
}

// getAlbumList2 — ID3 shape.
func (s *Server) handleGetAlbumList2(w http.ResponseWriter, r *http.Request) {
	albums, ok := s.albumList2(w, r)
	if !ok {
		return
	}
	if albums == nil {
		albums = []AlbumID3{}
	}
	respond(w, r, "albumList2", &AlbumList2{Albums: albums})
}

// getAlbumList — legacy directory shape from the same data.
func (s *Server) handleGetAlbumList(w http.ResponseWriter, r *http.Request) {
	albums, ok := s.albumList2(w, r)
	if !ok {
		return
	}
	out := AlbumList{Albums: []Child{}}
	for _, al := range albums {
		out.Albums = append(out.Albums, albumAsChild(al, al.ArtistID))
	}
	respond(w, r, "albumList", &out)
}

// getRandomSongs.
func (s *Server) handleGetRandomSongs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	ids, err := s.app.SubsonicRandomTrackIDs(r.Context(), intParam(r, "size", 10),
		param(r, "genre"), intParam(r, "fromYear", 0), intParam(r, "toYear", 0))
	if err != nil {
		respondError(w, r, errGeneric, "listing songs failed")
		return
	}
	respond(w, r, "randomSongs", &SongList{
		XMLName: xml.Name{Local: "randomSongs"},
		Songs:   s.tracksByIDs(r.Context(), u.ID, ids),
	})
}

// getSongsByGenre.
func (s *Server) handleGetSongsByGenre(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	genre := param(r, "genre")
	if genre == "" {
		respondError(w, r, errMissingParameter, `required parameter "genre" is missing`)
		return
	}
	ids, err := s.app.SubsonicTrackIDsByGenre(r.Context(), genre,
		intParam(r, "count", 10), intParam(r, "offset", 0))
	if err != nil {
		respondError(w, r, errGeneric, "listing songs failed")
		return
	}
	respond(w, r, "songsByGenre", &SongList{
		XMLName: xml.Name{Local: "songsByGenre"},
		Songs:   s.tracksByIDs(r.Context(), u.ID, ids),
	})
}

// starred2Payload assembles the shared getStarred/getStarred2 body.
func (s *Server) starred2Payload(r *http.Request, key string) *Starred2 {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	out := &Starred2{
		XMLName: xml.Name{Local: key},
		Artists: []ArtistID3{},
		Albums:  []AlbumID3{},
		Songs:   []Child{},
	}

	stars := s.starStateFor(ctx, u.ID)

	if page, err := s.app.ListUserLovedArtists(ctx, u.ID, 500, 0); err == nil && page != nil {
		for _, it := range page.Items {
			if ar, err := s.app.SubsonicArtistByID(ctx, it.ID); err == nil {
				out.Artists = append(out.Artists, artistID3From(ar, stars))
			}
		}
	}
	if page, err := s.app.ListUserLovedAlbums(ctx, u.ID, 500, 0); err == nil && page != nil {
		ids := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			ids = append(ids, it.ID)
		}
		out.Albums = s.albumsByIDs(ctx, u.ID, ids)
	}
	if page, err := s.app.ListUserLovedTracks(ctx, u.ID, 500, 0); err == nil && page != nil {
		ids := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			ids = append(ids, it.TrackID)
		}
		out.Songs = s.tracksByIDs(ctx, u.ID, ids)
	}
	return out
}

// getStarred2 / getStarred.
func (s *Server) handleGetStarred2(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "starred2", s.starred2Payload(r, "starred2"))
}

func (s *Server) handleGetStarred(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "starred", s.starred2Payload(r, "starred"))
}

// getNowPlaying — live music sessions from the same store Heya's activity
// panel reads.
func (s *Server) handleGetNowPlaying(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	out := NowPlaying{Entries: []NowPlayingEntry{}}
	store := s.app.Sessions()
	if store == nil {
		respond(w, r, "nowPlaying", &out)
		return
	}
	for i, sess := range store.List() {
		if sess.MediaType != "music" || sess.EntityType != "track" {
			continue
		}
		children := s.tracksByIDs(r.Context(), u.ID, []int64{sess.EntityID})
		if len(children) == 0 {
			continue
		}
		out.Entries = append(out.Entries, NowPlayingEntry{
			Child:      children[0],
			Username:   sess.Username,
			MinutesAgo: int32(time.Since(sess.LastHeartbeatAt).Minutes()),
			PlayerID:   int32(i), //nolint:gosec // session index, tiny
			PlayerName: sess.ClientUserAgent,
		})
	}
	respond(w, r, "nowPlaying", &out)
}

// --- Search ---

// search3 — ID3 search; also backs search/search2 with swapped keys.
func (s *Server) searchResult(r *http.Request) *SearchResult3 {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	query := param(r, "query")
	// Clients send `""` or `*` to mean "everything" (offline-sync full
	// fetches); the ILIKE listers treat empty as no filter, matching that.
	if query == `""` || query == "*" {
		query = ""
	}

	artistCount := intParam(r, "artistCount", 20)
	albumCount := intParam(r, "albumCount", 20)
	songCount := intParam(r, "songCount", 20)

	out := &SearchResult3{Artists: []ArtistID3{}, Albums: []AlbumID3{}, Songs: []Child{}}
	stars := s.starStateFor(ctx, u.ID)

	if artistCount > 0 {
		if rows, err := s.app.SubsonicListArtists(ctx, 0); err == nil {
			matched := make([]ArtistID3, 0, artistCount)
			skipped := int32(0)
			offset := intParam(r, "artistOffset", 0)
			for _, ar := range rows {
				if query != "" && !containsFold(ar.Name, query) {
					continue
				}
				if skipped < offset {
					skipped++
					continue
				}
				matched = append(matched, artistID3From(ar, stars))
				if int32(len(matched)) >= artistCount {
					break
				}
			}
			out.Artists = matched
		}
	}
	if albumCount > 0 {
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{
			Search: query, Lim: albumCount, Off: intParam(r, "albumOffset", 0),
		})
		if err == nil {
			out.Albums = s.albumID3sFrom(ctx, u.ID, rows)
		}
	}
	if songCount > 0 {
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{
			Search: query, Lim: songCount, Off: intParam(r, "songOffset", 0),
		})
		if err == nil {
			out.Songs = s.childrenFromTracks(ctx, u.ID, rows)
		}
	}
	return out
}

func (s *Server) handleSearch3(w http.ResponseWriter, r *http.Request) {
	out := s.searchResult(r)
	out.XMLName = xml.Name{Local: "searchResult3"}
	respond(w, r, "searchResult3", out)
}

// search2 (and the deprecated search) — same data, legacy key.
func (s *Server) handleSearch2(w http.ResponseWriter, r *http.Request) {
	out := s.searchResult(r)
	out.XMLName = xml.Name{Local: "searchResult2"}
	respond(w, r, "searchResult2", out)
}

func containsFold(haystack, needle string) bool {
	return needle == "" || strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
