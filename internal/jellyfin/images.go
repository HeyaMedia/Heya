package jellyfin

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/imageserve"
)

// Image delivery. Anonymous, like Jellyfin's own image endpoints and Heya's
// /api/media/{id}/image/{type} — <img> tags carry no auth headers. All
// resolution funnels into Heya's media_assets walk (App.GetMediaImagePath)
// and the shared on-disk-cached resizer.

// jellyfin ImageType → Heya asset type. Unknown types 404 (upstream does the
// same for types an item lacks).
var imageTypeMap = map[string]string{
	"primary":  "poster",
	"backdrop": "backdrop",
	"logo":     "logo",
	"banner":   "banner",
	"thumb":    "thumb",
	"art":      "art",
	"disc":     "disc",
}

// GET /Items/{itemId}/Images/{imageType} and .../{imageIndex}
func (s *Server) handleItemImage(w http.ResponseWriter, r *http.Request, p Params) {
	ctx := r.Context()
	kind, id, err := DecodeID(p["itemId"])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	imgType := strings.ToLower(p["imageType"])
	index := 0
	if idx := p["imageIndex"]; idx != "" {
		index, _ = strconv.Atoi(idx)
	}

	var path string
	var ok bool
	switch kind {
	case KindItem:
		assetType, known := imageTypeMap[imgType]
		if !known {
			http.NotFound(w, r)
			return
		}
		sortOrder := -1
		if imgType == "backdrop" && index > 0 {
			sortOrder = index
		}
		path, ok = s.app.GetMediaImagePath(ctx, id, assetType, sortOrder, "")

	case KindSeason:
		rows, err := s.app.JFListSeasons(ctx, 0, []int64{id})
		if err != nil || len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		season := rows[0]
		path, ok = s.app.GetMediaImagePath(ctx, season.SeriesMediaItemID, "poster", -1, fmt.Sprintf("season-%d", season.SeasonNumber))
		if !ok {
			path, ok = s.app.GetMediaImagePath(ctx, season.SeriesMediaItemID, "poster", -1, "")
		}

	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		ep := rows[0]
		label := fmt.Sprintf("s%de%d", ep.SeasonNumber, ep.EpisodeNumber)
		path, ok = s.app.GetMediaImagePath(ctx, ep.SeriesMediaItemID, "still", -1, label)
		if !ok {
			// Upstream falls back to the series art for episodes without
			// stills; a backdrop reads better than a portrait poster in the
			// 16:9 slots clients render episodes into.
			path, ok = s.app.GetMediaImagePath(ctx, ep.SeriesMediaItemID, "backdrop", -1, "")
		}
		if !ok {
			path, ok = s.app.GetMediaImagePath(ctx, ep.SeriesMediaItemID, "poster", -1, "")
		}

	case KindAlbum:
		path, ok = s.albumCover(r, w, id)
		if path == "" && ok {
			return // redirected
		}

	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		path, ok = coverFromPath(rows[0].AlbumCoverPath)
		if !ok && rows[0].AlbumCoverPath != "" {
			http.Redirect(w, r, rows[0].AlbumCoverPath, http.StatusFound)
			return
		}

	case KindLibrary, KindUser:
		http.NotFound(w, r)
		return

	default:
		http.NotFound(w, r)
		return
	}

	if !ok || path == "" {
		http.NotFound(w, r)
		return
	}
	s.app.ImageResizer().Serve(w, r, path, resizeParams(r))
}

// albumCover resolves an album's cover. Local paths serve through the
// resizer; upstream URLs 302 like Heya's own cover endpoint. Returns
// ("", true) after writing a redirect.
func (s *Server) albumCover(r *http.Request, w http.ResponseWriter, albumID int64) (string, bool) {
	rows, _, err := s.app.JFListAlbums(r.Context(), sqlc.JFListAlbumsParams{OnlyIds: []int64{albumID}})
	if err != nil || len(rows) == 0 {
		return "", false
	}
	cover := rows[0].CoverPath
	if path, ok := coverFromPath(cover); ok {
		return path, true
	}
	if cover != "" {
		http.Redirect(w, r, cover, http.StatusFound)
		return "", true
	}
	// Fall back to the artist's primary image so albums without ripped
	// covers still render something.
	path, ok := s.app.GetMediaImagePath(r.Context(), rows[0].ArtistMediaItemID, "poster", -1, "")
	return path, ok
}

func coverFromPath(p string) (string, bool) {
	if p == "" || strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return "", false
	}
	return p, true
}

// resizeParams maps Jellyfin's image query params onto the shared resizer.
func resizeParams(r *http.Request) imageserve.Params {
	v := url.Values{}
	if w := firstNonEmpty(queryCI(r, "maxWidth"), queryCI(r, "fillWidth"), queryCI(r, "width")); w != "" {
		v.Set("w", w)
	}
	if h := firstNonEmpty(queryCI(r, "maxHeight"), queryCI(r, "fillHeight"), queryCI(r, "height")); h != "" {
		v.Set("h", h)
	}
	if q := queryCI(r, "quality"); q != "" {
		v.Set("q", q)
	}
	return imageserve.ParseQuery(v)
}
