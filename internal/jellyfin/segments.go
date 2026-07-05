package jellyfin

import (
	"net/http"
	"strings"
)

// mediaSegmentDto mirrors Jellyfin's MediaSegmentDto (10.10+): typed
// skip markers with .NET-tick (100ns) bounds.
type mediaSegmentDto struct {
	Id         string `json:"Id"`
	ItemId     string `json:"ItemId"`
	Type       string `json:"Type"` // Intro | Outro | Recap | Preview | Commercial
	StartTicks int64  `json:"StartTicks"`
	EndTicks   int64  `json:"EndTicks"`
}

// heyaSegmentTypeToJF maps Heya's segment vocabulary onto Jellyfin's
// MediaSegmentType names. Heya's "credits" is Jellyfin's Outro — the
// upstream enum has no separate credits value.
var heyaSegmentTypeToJF = map[string]string{
	"intro":      "Intro",
	"credits":    "Outro",
	"recap":      "Recap",
	"preview":    "Preview",
	"commercial": "Commercial",
}

// GET /MediaSegments/{itemId}?includeSegmentTypes=Intro&includeSegmentTypes=Outro
//
// Serves the stored media_segments rows for the file Heya would play
// for this item. Clients probe every item before playback and expect an
// empty page (not a 404) for anything without markers — including item
// kinds that can never carry them.
func (s *Server) handleMediaSegments(w http.ResponseWriter, r *http.Request, p Params) {
	empty := queryResult[mediaSegmentDto]{Items: []mediaSegmentDto{}}

	target, ok := s.resolvePlayTarget(r.Context(), p["itemId"])
	if !ok {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	segments, err := s.app.ListFileSegments(r.Context(), target.file.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	include := map[string]bool{}
	for _, t := range r.URL.Query()["includeSegmentTypes"] {
		include[strings.ToLower(t)] = true
	}

	const ticksPerMs = 10_000
	items := make([]mediaSegmentDto, 0, len(segments))
	for _, seg := range segments {
		jfType, known := heyaSegmentTypeToJF[seg.Type]
		if !known {
			continue
		}
		if len(include) > 0 && !include[strings.ToLower(jfType)] {
			continue
		}
		items = append(items, mediaSegmentDto{
			Id:         EncodeID(KindSegment, seg.ID),
			ItemId:     p["itemId"],
			Type:       jfType,
			StartTicks: seg.StartMs * ticksPerMs,
			EndTicks:   seg.EndMs * ticksPerMs,
		})
	}
	writeJSON(w, http.StatusOK, queryResult[mediaSegmentDto]{Items: items, TotalRecordCount: len(items)})
}
