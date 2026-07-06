package jellyfin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/service"
)

// Movie recommendations + suggestions, backed by service.App.Recommended — the
// same personalized engine behind Heya's native Recommended landing. Jellyfin's
// recommendation surface is movies-biased and shape-constrained (a fixed
// RecommendationType enum, no custom row titles), so this is a lossy projection
// of Heya's richer rails; the full experience lives on the native API. Official
// Jellyfin clients (jellyfin-web, Swiftfin) render these.

// recommendationDto is one categorized row in a GET /Movies/Recommendations
// response (Jellyfin's RecommendationDto).
type recommendationDto struct {
	CategoryID         string        `json:"CategoryId"`
	RecommendationType string        `json:"RecommendationType"`
	BaselineItemName   string        `json:"BaselineItemName"`
	Items              []baseItemDto `json:"Items"`
}

// recommendationType maps a Heya rail key to the closest Jellyfin
// RecommendationType. The enum is fixed and actor/genre-centric, so rails
// without a natural category fall back to SimilarToLikedItem.
func recommendationType(railKey string) string {
	switch railKey {
	case "by-actor":
		return "HasActorFromRecentlyPlayed"
	case "more-genre":
		return "SimilarToRecentlyPlayed"
	default:
		return "SimilarToLikedItem"
	}
}

// GET /Movies/Recommendations — personalized movie recommendation categories.
// Each Heya movie rail becomes one recommendation row; TV rails have no home in
// this (movie-only) endpoint and surface via the native API instead.
func (s *Server) handleMovieRecommendations(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	categoryLimit := intQuery(r, "categoryLimit", 8, 1, 20)
	itemLimit := intQuery(r, "itemLimit", 12, 1, 50)

	rec, err := s.app.Recommended(r.Context(), u.ID, "movie")
	if err != nil {
		writeJSON(w, http.StatusOK, []recommendationDto{})
		return
	}

	out := make([]recommendationDto, 0, len(rec.Rails))
	for _, rail := range rec.Rails {
		if len(out) >= categoryLimit {
			break
		}
		ids := railItemIDs(rail, itemLimit)
		if len(ids) == 0 {
			continue
		}
		res, err := s.queryByIDs(r.Context(), u.ID, s.serverID(r), itemsRequest{ids: encodeItemIDs(ids)})
		if err != nil || len(res.Items) == 0 {
			continue
		}
		out = append(out, recommendationDto{
			CategoryID:         EncodeID(KindGenre, hashName("rec:"+rail.Key)),
			RecommendationType: recommendationType(rail.Key),
			BaselineItemName:   rail.Baseline,
			Items:              res.Items,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /Items/Suggestions — a flat suggestion feed. The `type` param
// (BaseItemKind[]) picks the section: series suggestions when it asks only for
// Series, movies otherwise. Rails are flattened and deduped, best-first.
func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request, _ Params) {
	u, _ := UserFrom(r.Context())
	limit := intQuery(r, "limit", 12, 1, 100)

	section := "movie"
	kinds := strings.ToLower(queryCI(r, "type"))
	if strings.Contains(kinds, "series") && !strings.Contains(kinds, "movie") {
		section = "tv"
	}

	rec, err := s.app.Recommended(r.Context(), u.ID, section)
	if err != nil {
		writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
		return
	}

	seen := map[int64]bool{}
	ids := make([]int64, 0, limit)
	for _, rail := range rec.Rails {
		for _, it := range rail.Items {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			ids = append(ids, it.ID)
			if len(ids) >= limit {
				break
			}
		}
		if len(ids) >= limit {
			break
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, queryResult[baseItemDto]{Items: []baseItemDto{}})
		return
	}
	res, err := s.queryByIDs(r.Context(), u.ID, s.serverID(r), itemsRequest{ids: encodeItemIDs(ids)})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// railItemIDs pulls up to limit local media-item ids from a rail, in order.
func railItemIDs(rail service.RecRail, limit int) []int64 {
	n := len(rail.Items)
	if limit > 0 && n > limit {
		n = limit
	}
	ids := make([]int64, 0, n)
	for _, it := range rail.Items[:n] {
		ids = append(ids, it.ID)
	}
	return ids
}

// intQuery reads a bounded integer query param, falling back to def when absent
// or out of [lo, hi].
func intQuery(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(queryCI(r, name))
	if err != nil || v < lo || v > hi {
		return def
	}
	return v
}
