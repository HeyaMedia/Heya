package sonicanalysis

import "fmt"

// PitchClass is one of the 12 equal-tempered pitch classes, indexed
// so the smallint values stored in track_facets map directly:
// 0=C, 1=C#, ..., 11=B.
type PitchClass int8

const (
	PitchC PitchClass = iota
	PitchCsharp
	PitchD
	PitchDsharp
	PitchE
	PitchF
	PitchFsharp
	PitchG
	PitchGsharp
	PitchA
	PitchAsharp
	PitchB
)

// Sharp returns the canonical "sharp" spelling (C, C#, D, …).
func (p PitchClass) Sharp() string {
	if p < 0 || p > 11 {
		return "?"
	}
	return [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}[p]
}

// Flat returns the canonical "flat" spelling (C, Db, D, Eb, …).
func (p PitchClass) Flat() string {
	if p < 0 || p > 11 {
		return "?"
	}
	return [...]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}[p]
}

func (p PitchClass) String() string { return p.Sharp() }

// KeyMode is the modal quality of a key: major or minor. Mapped to
// the smallint stored in track_facets.key_mode (0=major, 1=minor).
type KeyMode int8

const (
	KeyModeMajor KeyMode = iota
	KeyModeMinor
)

func (m KeyMode) String() string {
	if m == KeyModeMajor {
		return "major"
	}
	return "minor"
}

// Key bundles a tonic pitch class with a mode. The string form
// ("C major", "A minor", …) is suitable for display.
type Key struct {
	Root PitchClass
	Mode KeyMode
}

func (k Key) String() string { return fmt.Sprintf("%s %s", k.Root, k.Mode) }

// camelotMajor / camelotMinor give the Mixed-In-Key "Camelot Wheel"
// code for each (root, mode). The wheel is arranged so that adjacent
// numbers + the relative (A↔B) at the same number are harmonically
// compatible. Order: indexed by PitchClass (0=C..11=B).
var (
	camelotMajor = [12]string{
		PitchC:      "8B",
		PitchCsharp: "3B",
		PitchD:      "10B",
		PitchDsharp: "5B",
		PitchE:      "12B",
		PitchF:      "7B",
		PitchFsharp: "2B",
		PitchG:      "9B",
		PitchGsharp: "4B",
		PitchA:      "11B",
		PitchAsharp: "6B",
		PitchB:      "1B",
	}
	camelotMinor = [12]string{
		PitchC:      "5A",
		PitchCsharp: "12A",
		PitchD:      "7A",
		PitchDsharp: "2A",
		PitchE:      "9A",
		PitchF:      "4A",
		PitchFsharp: "11A",
		PitchG:      "6A",
		PitchGsharp: "1A",
		PitchA:      "8A",
		PitchAsharp: "3A",
		PitchB:      "10A",
	}
)

// CamelotCode returns the Mixed-In-Key wheel position for k.
// Returns "" for an out-of-range key.
func (k Key) CamelotCode() string {
	if k.Root < 0 || k.Root > 11 {
		return ""
	}
	if k.Mode == KeyModeMajor {
		return camelotMajor[k.Root]
	}
	return camelotMinor[k.Root]
}

// MoodTagName is the canonical name for a classifier-head output.
// Strings match the keys in track_facets.mood_tags JSON.
type MoodTagName string

const (
	MoodDanceability MoodTagName = "danceability"
	MoodVoice        MoodTagName = "voice"
	MoodHappy        MoodTagName = "mood_happy"
	MoodSad          MoodTagName = "mood_sad"
	MoodAggressive   MoodTagName = "mood_aggressive"
	MoodRelaxed      MoodTagName = "mood_relaxed"
	MoodParty        MoodTagName = "mood_party"
	MoodElectronic   MoodTagName = "mood_electronic"
	MoodAcoustic     MoodTagName = "mood_acoustic"
)

// MoodScores maps each classifier head to its P(positive class) in
// [0..1]. Marshaled as a JSON object into track_facets.mood_tags.
type MoodScores map[MoodTagName]float32
