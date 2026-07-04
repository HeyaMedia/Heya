package sonicanalysis

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Discogs-400 genre class names embedded at compile time. Static
// (one set of class names per model; the model itself is downloaded
// at runtime). 15 KB so the binary cost is trivial.

//go:embed embedded/discogs-effnet-genres.json
var discogsGenreJSON []byte

var discogsGenres []string

func init() {
	var meta struct {
		Classes []string `json:"classes"`
	}
	if err := json.Unmarshal(discogsGenreJSON, &meta); err != nil {
		panic(fmt.Errorf("parse discogs genre metadata: %w", err))
	}
	if len(meta.Classes) != effnetGenreDim {
		panic(fmt.Errorf("expected %d genre classes, got %d", effnetGenreDim, len(meta.Classes)))
	}
	discogsGenres = meta.Classes
}

// GenreName returns the human-readable name for a Discogs-400 class
// index, or "" if the index is out of range.
func GenreName(idx int) string {
	if idx < 0 || idx >= len(discogsGenres) {
		return ""
	}
	return discogsGenres[idx]
}
