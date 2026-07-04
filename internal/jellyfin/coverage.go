package jellyfin

import "sort"

// CoverageStatus is the exported view of a manifest status.
type CoverageStatus string

const (
	CoverageImplemented CoverageStatus = "implemented"
	CoverageStubbed     CoverageStatus = "stubbed"
	CoveragePlanned     CoverageStatus = "planned"
	CoverageOutOfScope  CoverageStatus = "out_of_scope"
)

// CoverageEntry is one spec operation's triage state.
type CoverageEntry struct {
	Operation string         `json:"operation"`
	Tag       string         `json:"tag"`
	Status    CoverageStatus `json:"status"`
}

// CoverageReport returns the full triage of the vendored Jellyfin spec,
// sorted by tag then operation — the data behind `heya jellyfin coverage`.
func CoverageReport() []CoverageEntry {
	out := make([]CoverageEntry, 0, len(manifest))
	for op, e := range manifest {
		out = append(out, CoverageEntry{
			Operation: op,
			Tag:       e.Tag,
			Status:    e.Status.export(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Operation < out[j].Operation
	})
	return out
}

func (s opStatus) export() CoverageStatus {
	switch s {
	case opImplemented:
		return CoverageImplemented
	case opStubbed:
		return CoverageStubbed
	case opOutOfScope:
		return CoverageOutOfScope
	default:
		return CoveragePlanned
	}
}
