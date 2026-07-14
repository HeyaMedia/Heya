package communitysegments

import (
	"encoding/json"
)

func normalizeTheIntroDB(body []byte) ([]Candidate, error) {
	type stamp struct {
		Start *int64 `json:"start_ms"`
		End   *int64 `json:"end_ms"`
	}
	var response struct{ Intro, Recap, Credits, Preview []stamp }
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	var result []Candidate
	bounded := func(kind string, stamps []stamp) {
		for _, value := range stamps {
			start := int64(0)
			if value.Start != nil {
				start = *value.Start
			}
			if value.End != nil && *value.End > start {
				result = append(result, Candidate{Type: kind, StartMs: start, EndMs: value.End, Source: "theintrodb"})
			}
		}
	}
	open := func(kind string, stamps []stamp) {
		for _, value := range stamps {
			if value.Start == nil {
				continue
			}
			end := value.End
			if end != nil && *end <= *value.Start {
				end = nil
			}
			result = append(result, Candidate{Type: kind, StartMs: *value.Start, EndMs: end, Source: "theintrodb"})
		}
	}
	bounded("intro", response.Intro)
	bounded("recap", response.Recap)
	open("credits", response.Credits)
	open("preview", response.Preview)
	return result, nil
}

func normalizeSkipMeDB(body []byte) ([]Candidate, error) {
	type stamp struct {
		Start       int64  `json:"start_ms"`
		End         *int64 `json:"end_ms"`
		Duration    int64  `json:"duration_ms"`
		Submissions int    `json:"submissions"`
	}
	type media struct{ Intro, Recap, Credits, Preview []stamp }
	var batch []*media
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, err
	}
	if len(batch) == 0 || batch[0] == nil {
		return nil, nil
	}
	var result []Candidate
	appendValues := func(kind string, values []stamp) {
		for _, value := range values {
			if value.End != nil && *value.End <= value.Start {
				continue
			}
			result = append(result, Candidate{Type: kind, StartMs: value.Start, EndMs: value.End, DurationMs: value.Duration, Submissions: value.Submissions, Source: "skipmedb"})
		}
	}
	appendValues("intro", batch[0].Intro)
	appendValues("recap", batch[0].Recap)
	appendValues("credits", batch[0].Credits)
	appendValues("preview", batch[0].Preview)
	return result, nil
}

func normalizeAniSkip(body []byte) ([]Candidate, error) {
	var response struct {
		Found   bool `json:"found"`
		Results []struct {
			Interval struct {
				Start float64 `json:"startTime"`
				End   float64 `json:"endTime"`
			} `json:"interval"`
			SkipType      string  `json:"skipType"`
			EpisodeLength float64 `json:"episodeLength"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if !response.Found {
		return nil, nil
	}
	types := map[string]string{"op": "intro", "mixed-op": "intro", "ed": "credits", "mixed-ed": "credits", "recap": "recap"}
	var result []Candidate
	for _, value := range response.Results {
		kind := types[value.SkipType]
		if kind == "" {
			continue
		}
		start, end := int64(value.Interval.Start*1000), int64(value.Interval.End*1000)
		if end <= start {
			continue
		}
		result = append(result, Candidate{Type: kind, StartMs: start, EndMs: &end, DurationMs: int64(value.EpisodeLength * 1000), Source: "aniskip"})
	}
	return result, nil
}
