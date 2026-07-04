package jellyfin

import (
	"context"
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

	// In every branch below: a query error is a 500 (retryable — Feishin's
	// cover-art request storms surfaced transient errors being served as
	// 404s, which clients cache as "no image"), only a missing row is a 404.
	case KindSeason:
		rows, err := s.app.JFListSeasons(ctx, 0, []int64{id})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		season := rows[0]
		target = fmt.Sprintf("/api/media/%d/image/poster?label=season-%d", season.SeriesMediaItemID, season.SeasonNumber)

	case KindEpisode:
		rows, _, err := s.app.JFListEpisodes(ctx, sqlc.JFListEpisodesParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			http.NotFound(w, r)
			return
		}
		ep := rows[0]
		// Fallback chain: a downloaded per-episode still (local asset) →
		// the episode's remote still URL (not downloaded) → series backdrop
		// → series poster. The GetMediaImagePath ok-checks read the DB only
		// (asset rows), so they work in passive mode; serving is delegated to
		// the native endpoint (or the remote URL) that we redirect to.
		label := fmt.Sprintf("s%de%d", ep.SeasonNumber, ep.EpisodeNumber)
		switch {
		case s.hasImage(ctx, ep.SeriesMediaItemID, "still", label):
			target = fmt.Sprintf("/api/media/%d/image/still?label=%s", ep.SeriesMediaItemID, label)
		case strings.HasPrefix(ep.StillPath, "http"):
			http.Redirect(w, r, ep.StillPath, http.StatusFound)
			return
		case s.hasImage(ctx, ep.SeriesMediaItemID, "backdrop", ""):
			target = fmt.Sprintf("/api/media/%d/image/backdrop", ep.SeriesMediaItemID)
		default:
			target = fmt.Sprintf("/api/media/%d/image/poster", ep.SeriesMediaItemID)
		}

	case KindAlbum:
		rows, _, err := s.app.JFListAlbums(ctx, sqlc.JFListAlbumsParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].Slug == "" {
			http.NotFound(w, r)
			return
		}
		target = fmt.Sprintf("/api/music/artists/%s/albums/%s/cover", rows[0].ArtistSlug, rows[0].Slug)

	case KindTrack:
		rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: []int64{id}})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 || rows[0].ArtistSlug == "" || rows[0].AlbumSlug == "" {
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

// hasImage reports whether a media item has a resolvable image of the given
// type/label (DB check only — no file access), so we can pick which native
// endpoint to redirect to without serving locally.
func (s *Server) hasImage(ctx context.Context, mediaItemID int64, assetType, label string) bool {
	_, ok := s.app.GetMediaImagePath(ctx, mediaItemID, assetType, -1, label)
	return ok
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
