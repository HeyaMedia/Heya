package subsonic

import "sort"

// CoverageStatus is the exported view of a manifest status.
type CoverageStatus string

const (
	CoverageImplemented CoverageStatus = "implemented"
	CoverageStubbed     CoverageStatus = "stubbed"
	CoverageUnsupported CoverageStatus = "unsupported"
)

// CoverageEntry is one endpoint's triage state.
type CoverageEntry struct {
	Endpoint string         `json:"endpoint"`
	Category string         `json:"category"`
	Status   CoverageStatus `json:"status"`
}

// CoverageReport returns the full triage of the Subsonic + OpenSubsonic
// endpoint list, sorted by category then endpoint — the data behind
// `heya subsonic coverage`.
func CoverageReport() []CoverageEntry {
	out := make([]CoverageEntry, 0, len(specEndpoints))
	for _, ep := range specEndpoints {
		out = append(out, CoverageEntry{
			Endpoint: ep.Name,
			Category: ep.Category,
			Status:   manifest[ep.Name].export(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Endpoint < out[j].Endpoint
	})
	return out
}

func (s opStatus) export() CoverageStatus {
	switch s {
	case opImplemented:
		return CoverageImplemented
	case opStubbed:
		return CoverageStubbed
	default:
		return CoverageUnsupported
	}
}
