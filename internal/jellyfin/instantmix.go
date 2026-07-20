package jellyfin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

// InstantMix — Jellyfin's "radio from this" — backed by Heya's sonic radio
// engine (BuildRadio: embedding centroid → KNN → diversity pass), the same
// path behind /api/music/radio. Seeds map 1:1: track, album, artist; a
// playlist seeds a multi-track centroid blend.

const instantMixDefaultLimit = 200

// GET /Items/{itemId}/InstantMix (and the Songs/Albums/Artists/Playlists
// aliases — upstream routes them all to the same handler too).
func (s *Server) handleInstantMix(w http.ResponseWriter, r *http.Request, p Params) {
	ctx := r.Context()
	u, _ := UserFrom(ctx)
	kind, id, err := DecodeID(p["itemId"])
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var req service.RadioRequest
	switch kind {
	case KindTrack:
		req.Seed = service.RadioSeed{Kind: "track", TrackID: id}
	case KindAlbum:
		req.Seed = service.RadioSeed{Kind: "album", AlbumID: id}
	case KindItem: // music artist media item
		req.Seed = service.RadioSeed{Kind: "artist", ArtistID: id}
	case KindPlaylist:
		detail, err := s.app.GetUserPlaylistDetail(ctx, u.ID, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		// A handful of seeds is enough for a stable centroid; the full
		// playlist would just be slower to resolve.
		for i, t := range detail.Tracks {
			if i >= 8 {
				break
			}
			req.Seeds = append(req.Seeds, service.RadioSeed{Kind: "track", TrackID: t.TrackID})
		}
	default:
		http.NotFound(w, r)
		return
	}

	limit := instantMixDefaultLimit
	if v, err := strconv.Atoi(queryCI(r, "limit")); err == nil && v > 0 && v < 500 {
		limit = v
	}
	req.Limit = int32(limit)

	empty := queryResult[baseItemDto]{Items: []baseItemDto{}}
	resp, err := s.app.BuildRadio(ctx, u.ID, req)
	if err != nil {
		// No sonic data / unresolvable seed — an empty mix, not an error;
		// clients show "no items" instead of a toast.
		if errors.Is(err, service.ErrNoRadioSeed) {
			writeJSON(w, http.StatusOK, empty)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Upstream leads the mix with the seed itself, then the similar queue.
	ordered := make([]int64, 0, len(resp.Tracks)+1)
	seen := map[int64]bool{}
	push := func(id int64) {
		if id > 0 && !seen[id] {
			seen[id] = true
			ordered = append(ordered, id)
		}
	}
	push(resp.SeedTrackID)
	for _, t := range resp.Tracks {
		push(t.TrackID)
	}
	if len(ordered) == 0 {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	rows, _, err := s.app.JFListTracks(ctx, sqlc.JFListTracksParams{OnlyIds: ordered})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	dec := s.favoriteDecor(ctx, u.ID, "track")
	byID := make(map[int64]baseItemDto, len(rows))
	for _, row := range rows {
		byID[row.ID] = s.dtoFromTrackRow(row, s.serverID(r), dec)
	}
	items := make([]baseItemDto, 0, len(ordered))
	for _, id := range ordered {
		if dto, ok := byID[id]; ok {
			items = append(items, dto)
		}
	}
	writeJSON(w, http.StatusOK, queryResult[baseItemDto]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}
