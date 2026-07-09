package subsonic

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// --- System ---

// ping — the connection test every client starts with.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "", nil)
}

// getLicense — self-hosted server, perpetually licensed.
func (s *Server) handleGetLicense(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "license", &License{Valid: true})
}

// getOpenSubsonicExtensions — the one endpoint the spec requires to work
// WITHOUT auth. Advertises what this server actually implements.
func (s *Server) handleGetOpenSubsonicExtensions(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "openSubsonicExtensions", []OpenSubsonicExtension{
		{Name: "formPost", Versions: []int{1}},
		{Name: "apiKeyAuthentication", Versions: []int{1}},
		{Name: "songLyrics", Versions: []int{1}},
	})
}

// tokenInfo — apiKeyAuthentication companion: who am I.
func (s *Server) handleTokenInfo(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	respond(w, r, "tokenInfo", &TokenInfo{Username: u.Username})
}

// --- Browsing ---

// getMusicFolders — one folder per music library.
func (s *Server) handleGetMusicFolders(w http.ResponseWriter, r *http.Request) {
	libs, err := s.app.ListLibraries(r.Context())
	if err != nil {
		respondError(w, r, errGeneric, "listing libraries failed")
		return
	}
	out := MusicFolders{Folders: []MusicFolder{}}
	for _, l := range libs {
		if l.MediaType == sqlc.MediaTypeMusic {
			out.Folders = append(out.Folders, MusicFolder{ID: l.ID, Name: l.Name})
		}
	}
	respond(w, r, "musicFolders", &out)
}

// musicFolderID parses the optional musicFolderId param (0 = all).
func musicFolderID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(param(r, "musicFolderId"), 10, 64)
	if id < 0 {
		return 0
	}
	return id
}

// artistIndex builds the shared getArtists/getIndexes payload.
func (s *Server) artistIndex(w http.ResponseWriter, r *http.Request, key string) {
	u, _ := userFrom(r.Context())
	rows, err := s.app.SubsonicListArtists(r.Context(), musicFolderID(r))
	if err != nil {
		respondError(w, r, errGeneric, "listing artists failed")
		return
	}
	stars := s.starStateFor(r.Context(), u.ID)
	artists := make([]ArtistID3, 0, len(rows))
	sortKeys := make([]string, 0, len(rows))
	for _, ar := range rows {
		artists = append(artists, artistID3From(ar, stars))
		key := ar.SortName
		if key == "" {
			key = ar.Name
		}
		sortKeys = append(sortKeys, key)
	}
	payload := &ArtistsID3{
		XMLName:         xml.Name{Local: key},
		IgnoredArticles: ignoredArticles,
		Index:           indexArtists(artists, sortKeys),
	}
	if payload.Index == nil {
		payload.Index = []IndexID3{}
	}
	if key == "indexes" {
		// The folder-style indexes shape carries lastModified (epoch ms);
		// clients use it for cache invalidation. We have no cheap global
		// change cursor, so "now" keeps caches conservatively fresh.
		ms := time.Now().UnixMilli()
		payload.LastModified = &ms
	}
	respond(w, r, key, payload)
}

// getArtists — the ID3 artist index.
func (s *Server) handleGetArtists(w http.ResponseWriter, r *http.Request) {
	s.artistIndex(w, r, "artists")
}

// getIndexes — folder-style index; Heya has no folder tree, so it serves
// the ID3 index (Navidrome answers the same way).
func (s *Server) handleGetIndexes(w http.ResponseWriter, r *http.Request) {
	s.artistIndex(w, r, "indexes")
}

// getArtist — artist detail + albums.
func (s *Server) handleGetArtist(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	artistID, err := DecodeIDKind(param(r, "id"), KindArtist)
	if err != nil {
		respondError(w, r, errNotFound, "artist not found")
		return
	}
	ar, err := s.app.SubsonicArtistByID(r.Context(), artistID)
	if err != nil {
		respondError(w, r, errNotFound, "artist not found")
		return
	}
	albums, _, err := s.app.JFListAlbums(r.Context(), sqlc.JFListAlbumsParams{
		ArtistMediaItemID: ar.MediaItemID, SortBy: "year",
	})
	if err != nil {
		respondError(w, r, errGeneric, "listing albums failed")
		return
	}
	stars := s.starStateFor(r.Context(), u.ID)
	out := &ArtistWithAlbumsID3{
		ArtistID3: artistID3From(ar, stars),
		Albums:    s.albumID3sFrom(r.Context(), u.ID, albums),
	}
	out.AlbumCount = int64(len(out.Albums))
	respond(w, r, "artist", out)
}

// getAlbum — album detail + songs.
func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	albumID, err := DecodeIDKind(param(r, "id"), KindAlbum)
	if err != nil {
		respondError(w, r, errNotFound, "album not found")
		return
	}
	albums, _, err := s.app.JFListAlbums(r.Context(), sqlc.JFListAlbumsParams{OnlyIds: []int64{albumID}})
	if err != nil || len(albums) == 0 {
		respondError(w, r, errNotFound, "album not found")
		return
	}
	tracks, _, err := s.app.JFListTracks(r.Context(), sqlc.JFListTracksParams{AlbumID: albumID})
	if err != nil {
		respondError(w, r, errGeneric, "listing songs failed")
		return
	}
	hydrated := s.albumID3sFrom(r.Context(), u.ID, albums)
	out := &AlbumWithSongsID3{
		AlbumID3: hydrated[0],
		Songs:    s.childrenFromTracks(r.Context(), u.ID, tracks),
	}
	// Metadata track counts can disagree with what we hold; report reality.
	out.SongCount = int32(len(out.Songs))
	var dur int32
	for _, c := range out.Songs {
		dur += c.Duration
	}
	if dur > 0 {
		out.Duration = dur
	}
	respond(w, r, "album", out)
}

// getSong — one song.
func (s *Server) handleGetSong(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	trackID, err := DecodeIDKind(param(r, "id"), KindTrack)
	if err != nil {
		respondError(w, r, errNotFound, "song not found")
		return
	}
	children := s.tracksByIDs(r.Context(), u.ID, []int64{trackID})
	if len(children) == 0 {
		respondError(w, r, errNotFound, "song not found")
		return
	}
	respond(w, r, "song", &SongPayload{Child: children[0]})
}

// getMusicDirectory — folder browse. Heya's "directories" are the ID3
// hierarchy: music folder → artists, artist → albums, album → songs.
func (s *Server) handleGetMusicDirectory(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	kind, id, err := DecodeID(param(r, "id"))
	if err != nil {
		respondError(w, r, errNotFound, "directory not found")
		return
	}
	ctx := r.Context()

	switch kind {
	case KindFolder:
		rows, err := s.app.SubsonicListArtists(ctx, id)
		if err != nil {
			respondError(w, r, errGeneric, "listing artists failed")
			return
		}
		dir := Directory{ID: EncodeID(KindFolder, id), Name: "Music", Children: []Child{}}
		stars := s.starStateFor(ctx, u.ID)
		for _, ar := range rows {
			dir.Children = append(dir.Children, Child{
				ID:       EncodeID(KindArtist, ar.ArtistID),
				Parent:   EncodeID(KindFolder, id),
				IsDir:    true,
				Title:    ar.Name,
				Artist:   ar.Name,
				CoverArt: EncodeID(KindArtist, ar.ArtistID),
				Starred:  starredAt(stars.artists, ar.ArtistID),
			})
		}
		respond(w, r, "directory", &dir)

	case KindArtist:
		ar, err := s.app.SubsonicArtistByID(ctx, id)
		if err != nil {
			respondError(w, r, errNotFound, "artist not found")
			return
		}
		albums, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{
			ArtistMediaItemID: ar.MediaItemID, SortBy: "year",
		})
		if err != nil {
			respondError(w, r, errGeneric, "listing albums failed")
			return
		}
		dir := Directory{ID: EncodeID(KindArtist, id), Name: ar.Name, Children: []Child{}}
		for _, al := range s.albumID3sFrom(ctx, u.ID, albums) {
			dir.Children = append(dir.Children, albumAsChild(al, EncodeID(KindArtist, id)))
		}
		respond(w, r, "directory", &dir)

	case KindAlbum:
		albums, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: []int64{id}})
		if err != nil || len(albums) == 0 {
			respondError(w, r, errNotFound, "album not found")
			return
		}
		tracks, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{AlbumID: id})
		if err != nil {
			respondError(w, r, errGeneric, "listing songs failed")
			return
		}
		dir := Directory{
			ID:       EncodeID(KindAlbum, id),
			Parent:   EncodeID(KindArtist, albums[0].ArtistID),
			Name:     albums[0].Title,
			Children: s.childrenFromTracks(ctx, u.ID, tracks),
		}
		respond(w, r, "directory", &dir)

	default:
		respondError(w, r, errNotFound, "directory not found")
	}
}

// albumAsChild renders an album in Child (directory row) shape — the
// legacy folder endpoints and getAlbumList speak this.
func albumAsChild(al AlbumID3, parent string) Child {
	return Child{
		ID:        al.ID,
		Parent:    parent,
		IsDir:     true,
		Title:     al.Name,
		Album:     al.Name,
		Artist:    al.Artist,
		Year:      al.Year,
		Genre:     al.Genre,
		CoverArt:  al.CoverArt,
		Duration:  al.Duration,
		Created:   al.Created,
		Starred:   al.Starred,
		AlbumID:   al.ID,
		ArtistID:  al.ArtistID,
		PlayCount: al.PlayCount,
	}
}

// getGenres — album-tag aggregation.
func (s *Server) handleGetGenres(w http.ResponseWriter, r *http.Request) {
	rows, err := s.app.SubsonicListGenres(r.Context())
	if err != nil {
		respondError(w, r, errGeneric, "listing genres failed")
		return
	}
	out := Genres{Genre: []Genre{}}
	for _, g := range rows {
		out.Genre = append(out.Genre, Genre{Value: g.Name, SongCount: g.SongCount, AlbumCount: g.AlbumCount})
	}
	respond(w, r, "genres", &out)
}

// getVideos — music-only server: an honest empty list.
func (s *Server) handleGetVideos(w http.ResponseWriter, r *http.Request) {
	respond(w, r, "videos", &Videos{Videos: []Child{}})
}
