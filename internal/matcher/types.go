package matcher

type MatchResult struct {
	Matched   int `json:"matched"`
	Unmatched int `json:"unmatched"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
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
