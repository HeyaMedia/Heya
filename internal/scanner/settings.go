package scanner

import (
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

func libraryUsesLocalData(lib sqlc.Library) bool {
	return metadata.ParseSettings(lib.Settings).UseLocalData
}

// MatchThresholdForLibrary resolves the auto-accept confidence floor for a
// library's scanner searches. Settings value 0 (or absent) means the built-in
// default; anything else is clamped to a sane band so a fat-fingered slider
// can't auto-accept everything or nothing.
func MatchThresholdForLibrary(lib sqlc.Library) float64 {
	threshold := metadata.ParseSettings(lib.Settings).MatchThreshold
	if threshold <= 0 {
		if lib.MediaType == sqlc.MediaTypeBook {
			return bookAutoMatchThreshold
		}
		return movieAutoMatchThreshold
	}
	return min(max(threshold, 0.3), 0.99)
}
