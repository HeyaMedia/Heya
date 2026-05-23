package matcher

type MatchResult struct {
	Matched   int `json:"matched"`
	Unmatched int `json:"unmatched"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	// MusicArtistIDs is populated by matchMusicLibrary: the set of artist
	// rows touched during the scan. The scan task uses this to enqueue
	// RefreshMusicArtist jobs per artist after the match phase completes.
	MusicArtistIDs []int64 `json:"music_artist_ids,omitempty"`
}

type MatchOptions struct {
	AutoMatchThreshold float64
	MaxCandidates      int
}

func DefaultOptions() MatchOptions {
	return MatchOptions{
		AutoMatchThreshold: 0.85,
		MaxCandidates:      10,
	}
}
