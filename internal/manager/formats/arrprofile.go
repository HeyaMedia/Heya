package formats

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ImportedQualityItem is one rung of an imported ladder: either a single
// quality or a named group whose members rank equally.
type ImportedQualityItem struct {
	Quality   string
	Group     string
	Qualities []string
	Allowed   bool
}

// ImportedProfile is a quality profile normalized out of an arr payload.
// FormatScores is keyed by custom format name — the caller resolves names to
// Heya format ids after the formats themselves are imported. Language is the
// Radarr-only profile gate ('any' when the payload has none — Sonarr models
// language preference through scored custom formats instead).
type ImportedProfile struct {
	Name              string
	UpgradesEnabled   bool
	Cutoff            string
	Language          string
	MinFormatScore    int
	CutoffFormatScore int
	MinUpgradeScore   int
	Items             []ImportedQualityItem
	FormatScores      map[string]int
}

type arrProfile struct {
	Name                  string           `json:"name"`
	UpgradeAllowed        bool             `json:"upgradeAllowed"`
	Cutoff                int              `json:"cutoff"`
	Language              *arrLanguageRef  `json:"language"`
	MinFormatScore        int              `json:"minFormatScore"`
	CutoffFormatScore     int              `json:"cutoffFormatScore"`
	MinUpgradeFormatScore int              `json:"minUpgradeFormatScore"`
	Items                 []arrProfileItem `json:"items"`
	FormatItems           []arrFormatItem  `json:"formatItems"`
}

type arrLanguageRef struct {
	Name string `json:"name"`
}

type arrProfileItem struct {
	ID      int              `json:"id"`
	Name    string           `json:"name"`
	Quality *arrQuality      `json:"quality"`
	Items   []arrProfileItem `json:"items"`
	Allowed bool             `json:"allowed"`
}

type arrQuality struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type arrFormatItem struct {
	Format int    `json:"format"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
}

// The arr quality names slugify cleanly onto Heya's vocabulary except where
// the apps chose different words for the same rung.
var qualityNameOverrides = map[string]string{
	"bluray-2160p remux": "remux-2160p",
	"bluray-1080p remux": "remux-1080p",
	"flac 24bit":         "flac-24",
	"alac 24bit":         "alac-24",
	"mp3-vbr-v0":         "mp3-v0",
	"mp3-vbr-v2":         "mp3-v2",
}

// MapArrQualityName translates an arr quality name into Heya's ladder key.
func MapArrQualityName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if mapped, ok := qualityNameOverrides[lowered]; ok {
		return mapped
	}
	return strings.Join(strings.Fields(lowered), "-")
}

// ParseArrProfiles normalizes a quality-profile payload. Arr ladders are
// worst-first; Heya stores best-first, so the order flips.
func ParseArrProfiles(raw []byte) ([]ImportedProfile, error) {
	var payload []arrProfile
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parsing quality profile JSON: %w", err)
	}

	imported := make([]ImportedProfile, 0, len(payload))
	for _, profile := range payload {
		result := ImportedProfile{
			Name:              profile.Name,
			UpgradesEnabled:   profile.UpgradeAllowed,
			Language:          "any",
			MinFormatScore:    profile.MinFormatScore,
			CutoffFormatScore: profile.CutoffFormatScore,
			MinUpgradeScore:   profile.MinUpgradeFormatScore,
			FormatScores:      map[string]int{},
		}
		if profile.Language != nil && profile.Language.Name != "" {
			result.Language = strings.ToLower(profile.Language.Name)
		}
		if result.MinUpgradeScore <= 0 {
			result.MinUpgradeScore = 1
		}

		for index := len(profile.Items) - 1; index >= 0; index-- {
			item := profile.Items[index]
			converted, cutoffName := convertProfileItem(item, profile.Cutoff)
			if cutoffName != "" {
				result.Cutoff = cutoffName
			}
			if converted != nil {
				result.Items = append(result.Items, *converted)
			}
		}

		for _, formatItem := range profile.FormatItems {
			if formatItem.Score != 0 && formatItem.Name != "" {
				result.FormatScores[formatItem.Name] = formatItem.Score
			}
		}
		imported = append(imported, result)
	}
	return imported, nil
}

func convertProfileItem(item arrProfileItem, cutoff int) (*ImportedQualityItem, string) {
	if len(item.Items) > 0 {
		group := ImportedQualityItem{Group: item.Name, Allowed: item.Allowed}
		for _, member := range item.Items {
			if member.Quality != nil {
				group.Qualities = append(group.Qualities, MapArrQualityName(member.Quality.Name))
			}
		}
		var cutoffName string
		if item.ID == cutoff {
			cutoffName = item.Name
		}
		return &group, cutoffName
	}
	if item.Quality == nil {
		return nil, ""
	}
	mapped := MapArrQualityName(item.Quality.Name)
	var cutoffName string
	if item.Quality.ID == cutoff {
		cutoffName = mapped
	}
	return &ImportedQualityItem{Quality: mapped, Allowed: item.Allowed}, cutoffName
}
