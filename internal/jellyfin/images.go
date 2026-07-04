package jellyfin

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Image delivery. Anonymous, like Jellyfin's own image endpoints and Heya's
// /api/media/{id}/image/{type} — <img> tags carry no auth headers. Every
// request 302-redirects to the matching native Heya image endpoint, which
// owns the full pipeline (media_assets walk, resizer, passive-mode proxy).

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
//
// Rather than resolve + serve here, redirect to Heya's own image endpoints.
// Those are the source of truth: they run the media_assets walk, the on-disk
// resizer, AND the passive-mode image proxy (proxiedImage) that fetches bytes
// from an upstream Heya when the local data dir has none. Serving locally here
// bypassed that proxy and 404'd in passive/dev deployments. A 302 costs one
// hop; image requests are anonymous so no auth is lost.
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

	target := ""
	switch kind {
	case KindItem:
		assetType, known := imageTypeMap[imgType]
		if !known {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/media/%d/image/%s", id, assetType)
		if imgType == "backdrop" && index > 0 {
			target += fmt.Sprintf("?sort=%d", index)
		}

	case KindSeason:
		rows, err := s.app.JFListSeasons(ctx, 0, []int64{id})
		if err != nil || len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		season := rows[0]
		target = fmt.Sprintf("/api/media/%d/image/poster?label=season-%d", season.SeriesMediaItemID, season.SeasonNumber)

	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		ep := rows[0]
		target = fmt.Sprintf("/api/media/%d/image/still?label=s%de%d", ep.SeriesMediaItemID, ep.SeasonNumber, ep.EpisodeNumber)

	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].Slug == "" {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", rows[0].ArtistSlug, rows[0].Slug)

	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		if err != nil || len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].AlbumSlug == "" {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", rows[0].ArtistSlug, rows[0].AlbumSlug)

	default: // KindLibrary, KindUser, ...
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, appendResizeQuery(target, r), http.StatusFound)
}

// appendResizeQuery carries Jellyfin's image sizing params onto the native
// image URL as the resizer's own param names (w/h/q).
func appendResizeQuery(target string, r *http.Request) string {
	v := url.Values{}
	if wv := firstNonEmpty(queryCI(r, "maxWidth"), queryCI(r, "fillWidth"), queryCI(r, "width")); wv != "" {
		v.Set("w", wv)
	}
	if hv := firstNonEmpty(queryCI(r, "maxHeight"), queryCI(r, "fillHeight"), queryCI(r, "height")); hv != "" {
		v.Set("h", hv)
	}
	if qv := queryCI(r, "quality"); qv != "" {
		v.Set("q", qv)
	}
	if len(v) == 0 {
		return target
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + v.Encode()
}
