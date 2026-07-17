// Package musicsemantic composes the deliberately narrow text used for music
// similarity. It describes a recording's musical character; identity/display
// context (title, artist, biography, album prose) and lyrics stay out.
package musicsemantic

import (
	"slices"
	"sort"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type Facets struct {
	Genres               []string
	Tags                 []string
	Moods                []string
	Instrumentation      []string
	VocalCharacteristics []string
	RecordingAttributes  []string
}

var moodTerms = []string{
	"aggressive", "angry", "atmospheric", "bittersweet", "calm", "dark",
	"dreamy", "energetic", "euphoric", "happy", "hopeful", "melancholic",
	"melancholy", "party", "peaceful", "playful", "relaxed", "romantic",
	"sad", "sentimental", "somber", "uplifting",
}

var recordingAttributeTerms = []string{
	"acoustic", "alternate version", "demo", "extended mix", "instrumental",
	"karaoke", "live", "radio edit", "re-recording", "remaster", "remastered",
	"remix", "studio recording", "unplugged",
}

// FromRecording extracts only explicit provider evidence. The small mood and
// recording-attribute vocabularies classify existing tags/disambiguation; they
// never invent a mood from prose or lyrics.
func FromRecording(value metadata.RecordingMetadata) Facets {
	result := Facets{
		Genres: cleanTerms(value.Genres),
		Tags:   cleanTerms(value.Tags),
	}
	classificationText := strings.ToLower(strings.Join(append(append([]string{}, value.Tags...), value.Disambiguation), " "))
	for _, term := range moodTerms {
		if containsTerm(classificationText, term) {
			result.Moods = append(result.Moods, term)
		}
	}
	for _, term := range recordingAttributeTerms {
		if containsTerm(classificationText, term) {
			result.RecordingAttributes = append(result.RecordingAttributes, normalizeTerm(term))
		}
	}
	for _, credit := range value.Credits {
		role := normalizeTerm(credit.Role)
		attributes := cleanTerms(credit.Attributes)
		switch {
		case strings.Contains(role, "vocal"):
			if len(attributes) == 0 {
				result.VocalCharacteristics = append(result.VocalCharacteristics, role)
			} else {
				result.VocalCharacteristics = append(result.VocalCharacteristics, attributes...)
			}
		case role == "instrument" || role == "performer" || strings.Contains(role, "instrument"):
			result.Instrumentation = append(result.Instrumentation, attributes...)
		}
	}

	result.Moods = cleanTerms(result.Moods)
	result.Instrumentation = cleanTerms(result.Instrumentation)
	result.VocalCharacteristics = cleanTerms(result.VocalCharacteristics)
	result.RecordingAttributes = cleanTerms(result.RecordingAttributes)

	// A term gets one semantic vote. If a provider classifies "rock" as both a
	// genre and tag, keep it in the more specific field rather than repeating it
	// in the document and accidentally increasing its weight.
	specific := termSet(result.Genres, result.Moods, result.Instrumentation, result.VocalCharacteristics, result.RecordingAttributes)
	result.Tags = slices.DeleteFunc(result.Tags, func(value string) bool {
		_, exists := specific[strings.ToLower(value)]
		return exists
	})
	return result
}

// Document returns an empty string when no musical-character evidence exists;
// callers must not create an identity-only embedding for that recording.
func Document(f Facets) string {
	var sections []string
	appendSection := func(label string, values []string) {
		if len(values) > 0 {
			sections = append(sections, label+": "+strings.Join(values, ", "))
		}
	}
	appendSection("Genres and styles", f.Genres)
	appendSection("Tags", f.Tags)
	appendSection("Moods", f.Moods)
	appendSection("Instrumentation", f.Instrumentation)
	appendSection("Vocals", f.VocalCharacteristics)
	appendSection("Recording attributes", f.RecordingAttributes)
	return strings.Join(sections, ". ")
}

func SharedTerms(seed, candidate Facets, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seedSet := termSet(seed.Moods, seed.Instrumentation, seed.VocalCharacteristics, seed.RecordingAttributes, seed.Tags, seed.Genres)
	ordered := append([]string{}, candidate.Moods...)
	ordered = append(ordered, candidate.Instrumentation...)
	ordered = append(ordered, candidate.VocalCharacteristics...)
	ordered = append(ordered, candidate.RecordingAttributes...)
	ordered = append(ordered, candidate.Tags...)
	ordered = append(ordered, candidate.Genres...)
	var result []string
	seen := map[string]bool{}
	for _, value := range ordered {
		key := strings.ToLower(value)
		if _, ok := seedSet[key]; !ok || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}

func cleanTerms(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeTerm(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

func normalizeTerm(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(value), "_", " ")), " ")
}

func containsTerm(text, term string) bool {
	text = " " + strings.NewReplacer("_", " ", "-", " ", "/", " ").Replace(text) + " "
	term = " " + strings.ToLower(term) + " "
	return strings.Contains(text, term)
}

func termSet(groups ...[]string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, group := range groups {
		for _, value := range group {
			result[strings.ToLower(value)] = struct{}{}
		}
	}
	return result
}
