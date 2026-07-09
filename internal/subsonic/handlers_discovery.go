package subsonic

import (
	"encoding/xml"
	"net/http"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Discovery: artist/album info, similar songs, top songs — backed by the
// same heya.media-fed metadata and sonic-embedding services the web UI uses.

// artistInfoPayload builds getArtistInfo/getArtistInfo2 from the artist row
// plus metadata-based similar artists (only ones that exist locally get
// ids — clients navigate to them).
func (s *Server) artistInfoPayload(r *http.Request, key string) (*ArtistInfo2, bool) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	artistID, err := DecodeIDKind(param(r, "id"), KindArtist)
	if err != nil {
		return nil, false
	}
	ar, err := s.app.SubsonicArtistByID(ctx, artistID)
	if err != nil {
		return nil, false
	}

	imageURL := requestBaseURL(r) + "/subsonic/rest/getCoverArt?id=" + EncodeID(KindArtist, artistID)
	out := &ArtistInfo2{
		XMLName:        xml.Name{Local: key},
		Biography:      ar.Biography,
		MusicBrainzID:  ar.MusicbrainzID,
		SmallImageURL:  imageURL,
		MediumImageURL: imageURL,
		LargeImageURL:  imageURL,
		SimilarArtists: []ArtistID3{},
	}

	count := intParam(r, "count", 20)
	if hits, err := s.app.GetSimilarArtists(ctx, artistID); err == nil {
		stars := s.starStateFor(ctx, u.ID)
		for _, h := range hits {
			if h.LocalArtistID == 0 {
				continue // includeNotPresent=false is the only honest mode: foreign artists have no navigable id
			}
			local, err := s.app.SubsonicArtistByID(ctx, h.LocalArtistID)
			if err != nil {
				continue
			}
			out.SimilarArtists = append(out.SimilarArtists, artistID3From(local, stars))
			if int32(len(out.SimilarArtists)) >= count {
				break
			}
		}
	}
	return out, true
}

func (s *Server) handleGetArtistInfo2(w http.ResponseWriter, r *http.Request) {
	out, ok := s.artistInfoPayload(r, "artistInfo2")
	if !ok {
		respondError(w, r, errNotFound, "artist not found")
		return
	}
	respond(w, r, "artistInfo2", out)
}

func (s *Server) handleGetArtistInfo(w http.ResponseWriter, r *http.Request) {
	out, ok := s.artistInfoPayload(r, "artistInfo")
	if !ok {
		respondError(w, r, errNotFound, "artist not found")
		return
	}
	respond(w, r, "artistInfo", out)
}

// getAlbumInfo / getAlbumInfo2 — album notes don't exist in Heya's schema;
// the mbid + cover URLs are real, notes stay empty.
func (s *Server) handleGetAlbumInfo(w http.ResponseWriter, r *http.Request) {
	albumID, err := DecodeIDKind(param(r, "id"), KindAlbum)
	if err != nil {
		// getAlbumInfo (non-ID3) may be called with a song/directory id.
		if trackID, terr := DecodeIDKind(param(r, "id"), KindTrack); terr == nil {
			if rows, _, lerr := s.app.JFListTracks(r.Context(), jfTracksByIDs(trackID)); lerr == nil && len(rows) > 0 {
				albumID = rows[0].AlbumID
				err = nil
			}
		}
		if err != nil {
			respondError(w, r, errNotFound, "album not found")
			return
		}
	}
	rows, _, lerr := s.app.JFListAlbums(r.Context(), jfAlbumsByIDs(albumID))
	if lerr != nil || len(rows) == 0 {
		respondError(w, r, errNotFound, "album not found")
		return
	}
	coverURL := requestBaseURL(r) + "/subsonic/rest/getCoverArt?id=" + EncodeID(KindAlbum, albumID)
	key := "albumInfo"
	respond(w, r, key, &AlbumInfo{
		XMLName:        xml.Name{Local: key},
		SmallImageURL:  coverURL,
		MediumImageURL: coverURL,
		LargeImageURL:  coverURL,
	})
}

// similarSongsPayload — sonic-embedding KNN around a seed track. Artist and
// album ids resolve to a representative seed track first.
func (s *Server) similarSongsPayload(r *http.Request, key string) (*SongList, bool) {
	u, _ := userFrom(r.Context())
	ctx := r.Context()
	kind, id, err := DecodeID(param(r, "id"))
	if err != nil {
		return nil, false
	}

	var seedTrack int64
	switch kind {
	case KindTrack:
		seedTrack = id
	case KindAlbum:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{AlbumID: id, Lim: 1})
		if err != nil || len(rows) == 0 {
			return nil, false
		}
		seedTrack = rows[0].ID
	case KindArtist:
		ar, err := s.app.SubsonicArtistByID(ctx, id)
		if err != nil {
			return nil, false
		}
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{ArtistMediaItemID: ar.MediaItemID, Lim: 1})
		if err != nil || len(rows) == 0 {
			return nil, false
		}
		seedTrack = rows[0].ID
	default:
		return nil, false
	}

	count := intParam(r, "count", 50)
	out := &SongList{XMLName: xml.Name{Local: key}, Songs: []Child{}}
	similar, err := s.app.SimilarMusicTracks(ctx, seedTrack, count)
	if err != nil {
		// No embeddings (sonic analysis disabled) — an empty list keeps
		// clients' radio features degrading gracefully.
		return out, true
	}
	ids := make([]int64, 0, len(similar))
	for _, row := range similar {
		ids = append(ids, row.TrackID)
	}
	out.Songs = s.tracksByIDs(ctx, u.ID, ids)
	return out, true
}

func (s *Server) handleGetSimilarSongs2(w http.ResponseWriter, r *http.Request) {
	out, ok := s.similarSongsPayload(r, "similarSongs2")
	if !ok {
		respondError(w, r, errNotFound, "id not found")
		return
	}
	respond(w, r, "similarSongs2", out)
}

func (s *Server) handleGetSimilarSongs(w http.ResponseWriter, r *http.Request) {
	out, ok := s.similarSongsPayload(r, "similarSongs")
	if !ok {
		respondError(w, r, errNotFound, "id not found")
		return
	}
	respond(w, r, "similarSongs", out)
}

// getTopSongs — Last.fm-derived top tracks joined to local recordings
// (artist addressed BY NAME, per spec).
func (s *Server) handleGetTopSongs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	name := param(r, "artist")
	if name == "" {
		respondError(w, r, errMissingParameter, `required parameter "artist" is missing`)
		return
	}
	out := SongList{XMLName: xml.Name{Local: "topSongs"}, Songs: []Child{}}
	ar, err := s.app.SubsonicArtistByName(r.Context(), name)
	if err != nil {
		// Unknown artist → empty list (a real server answers the same).
		respond(w, r, "topSongs", &out)
		return
	}
	count := intParam(r, "count", 50)
	tops, err := s.app.ListArtistTopTracksBySlug(r.Context(), ar.Slug, count)
	if err == nil {
		ids := make([]int64, 0, len(tops))
		for _, t := range tops {
			if t.LocalTrackID > 0 {
				ids = append(ids, t.LocalTrackID)
			}
		}
		out.Songs = s.tracksByIDs(r.Context(), u.ID, ids)
	}
	respond(w, r, "topSongs", &out)
}

// requestBaseURL reconstructs the client-facing origin for absolute URLs
// (artistImageUrl etc.), honoring reverse-proxy forwarding headers.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		scheme = v
	}
	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		host = v
	}
	return scheme + "://" + host
}
