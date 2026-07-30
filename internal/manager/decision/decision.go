// Package decision is the pure acquisition decision engine: given a target
// (what we want, what we have, under which policy) and a set of release
// candidates, it produces per-candidate, per-unit verdicts with stable
// machine-readable rejection codes. No I/O, no DB — callers snapshot policy
// and inventory in, and persist the results as the accountability ledger.
//
// The semantics deliberately track the current Sonarr/Radarr/Lidarr decision
// engines (UpgradableSpecification + DownloadDecisionComparer) so that
// dry-run verdicts are comparable against the live arrs release-for-release.
package decision

import (
	"time"

	"github.com/karbowiak/heya/internal/manager/formats"
)

// EvaluatorVersion stamps every persisted decision; bump on any semantic
// change so historical verdicts stay interpretable.
const EvaluatorVersion = 1

// ParserVersion mirrors the release-parser vocabulary generation.
const ParserVersion = 1

// Code is a stable rejection-code identifier. Display text is derived at
// read time; the code + params are the durable record.
type Code string

const (
	CodeUnparseable           Code = "unparseable"
	CodeIdentityMismatch      Code = "identity_mismatch"
	CodeIdentityAmbiguous     Code = "identity_ambiguous"
	CodeWrongDomain           Code = "wrong_domain"
	CodeMultiSeason           Code = "multi_season"
	CodeDailyUnsupported      Code = "daily_unsupported"
	CodeQualityUnmapped       Code = "quality_unmapped"
	CodeQualityNotAllowed     Code = "quality_not_allowed"
	CodeSizeOutOfBounds       Code = "size_out_of_bounds"
	CodeLanguageGate          Code = "language_gate"
	CodeFormatScoreBelowMin   Code = "format_score_below_min"
	CodeAlreadyDownloading    Code = "already_downloading"
	CodeCutoffMet             Code = "cutoff_met"
	CodeNotAnUpgrade          Code = "not_an_upgrade"
	CodeUpgradeIncrementUnmet Code = "upgrade_increment_unmet"
	CodeRevisionDowngrade     Code = "revision_downgrade"
	CodePackDowngradesUnit    Code = "pack_downgrades_unit"
	CodePackNoWantedCoverage  Code = "pack_no_wanted_coverage"
	CodeUpgradesDisabled      Code = "upgrades_disabled"
	CodeUnmonitored           Code = "unmonitored"
	CodeUnreleased            Code = "unreleased"
	CodeConfigNoProfile       Code = "config_no_profile"
	CodeComparisonUncertain   Code = "comparison_uncertain"
)

// Rejection is one recorded gate failure.
type Rejection struct {
	Code    Code           `json:"code"`
	Stage   string         `json:"stage"`
	Params  map[string]any `json:"params,omitempty"`
	Message string         `json:"message"`
}

// LadderItem is one profile ladder entry, best-first; grouped entries carry
// the member quality keys and compare equal for upgrade purposes.
type LadderItem struct {
	Quality   string
	Group     string
	Qualities []string
	Allowed   bool
}

// SizeDef bounds a quality in MB per minute of runtime, arr-style.
type SizeDef struct {
	MinMBPerMin       float64
	MaxMBPerMin       float64
	PreferredMBPerMin float64
}

// Profile is the full policy snapshot the engine evaluates under.
type Profile struct {
	ID                 int64
	Name               string
	Domain             string // movie | tv | music | book
	Items              []LadderItem
	Cutoff             string
	UpgradesEnabled    bool
	MinFormatScore     int32
	CutoffFormatScore  int32
	MinUpgradeScore    int32
	Language           string
	PreferProperRepack bool
	Formats            []formats.Format
	FormatScores       map[int64]int32
	SizeDefs           map[string]SizeDef
}

// Position resolves a quality key to its ladder position (0 = best).
func (p *Profile) Position(key string) (pos int, allowed bool, found bool) {
	if key == "" {
		return 0, false, false
	}
	for i, item := range p.Items {
		if item.Quality == key {
			return i, item.Allowed, true
		}
		for _, member := range item.Qualities {
			if member == key {
				return i, item.Allowed, true
			}
		}
	}
	return 0, false, false
}

// CutoffPosition resolves the profile cutoff (a quality key or group name)
// to a ladder position; found=false when the cutoff names nothing.
func (p *Profile) CutoffPosition() (int, bool) {
	for i, item := range p.Items {
		if item.Quality == p.Cutoff || (item.Group != "" && item.Group == p.Cutoff) {
			return i, true
		}
		for _, member := range item.Qualities {
			if member == p.Cutoff {
				return i, true
			}
		}
	}
	return 0, false
}

// ExistingFile is one on-disk file snapshot for a unit, pre-resolved against
// the evaluating profile.
type ExistingFile struct {
	FileID          int64
	Basename        string
	Quality         string
	Position        int
	PositionFound   bool
	RevisionVersion int
	FormatScore     int32
	// Provenance: parsed_name | media_info | inferred. Uncertain existing
	// quality forbids confident upgrade verdicts.
	Provenance string
	Uncertain  bool
}

// QueuedRelease is one in-flight download already covering a unit — a
// virtual existing file for the already-downloading gate.
type QueuedRelease struct {
	Title           string
	Quality         string
	Position        int
	PositionFound   bool
	RevisionVersion int
	FormatScore     int32
}

// Unit is one atomic wanted unit: a movie, one episode, one music release
// group, or one book.
type Unit struct {
	Key            string // immutable target_key
	EpisodeID      int64
	SeasonNumber   int
	EpisodeNumber  int
	AbsoluteNumber int
	// Monitored and Released gate wantedness; both true for plain movies
	// that are out.
	Monitored bool
	Released  bool
	Existing  []ExistingFile
	Queued    []QueuedRelease
}

// Target is everything the engine knows about what it's deciding for.
type Target struct {
	Domain           string // movie | tv | music | book
	MediaItemID      int64
	NormalizedTitles []string // matcher-normalized title + aliases
	Year             int
	IDs              map[string]string // imdb / tvdb / tmdb (bare values)
	OriginalLanguage string
	RuntimeMinutes   int
	Anime            bool
	Units            []Unit
	Profile          *Profile
}

// Candidate is one release under consideration, as fetched.
type Candidate struct {
	Index           int
	Title           string
	SizeBytes       int64
	PublishDate     time.Time
	IndexerID       int64
	IndexerName     string
	IndexerPriority int32
	Categories      []int
	// IDHints are provider ids the indexer attached (imdbid/tvdbid/...).
	IDHints map[string]string
}

// FormatHit is one matched custom format and the score it contributed.
type FormatHit struct {
	FormatID int64  `json:"id"`
	Name     string `json:"name"`
	Score    int32  `json:"score"`
}

// UnitEval is one candidate's verdict against one unit.
type UnitEval struct {
	Acceptable    bool
	Rejections    []Rejection
	SelectionRank int // 1-based among accepted for this unit; 0 = none
}

// CandidateResult is the full evaluation record for one candidate.
type CandidateResult struct {
	Input           Candidate
	Attrs           formats.Attrs
	QualityKey      string
	Position        int
	PositionFound   bool
	FormatScore     int32
	FormatBreakdown []FormatHit
	// RunRejections are target-independent failures (parse, identity,
	// domain sanity); a candidate with any is evaluated against no unit.
	RunRejections []Rejection
	PerUnit       map[string]*UnitEval
}

// Verdict values for a unit decision.
const (
	VerdictWouldGrab             = "would_grab"
	VerdictAlreadySatisfied      = "already_satisfied"
	VerdictNoAcceptableCandidate = "no_acceptable_candidate"
	VerdictComparisonUncertain   = "comparison_uncertain"
	VerdictConfigurationError    = "configuration_error"
)

// UnitDecision is the atomic per-unit outcome.
type UnitDecision struct {
	UnitKey         string
	Verdict         string
	ChosenCandidate int // Candidate.Index of the winner; -1 = none
}

// Result is one full evaluation run.
type Result struct {
	Candidates []CandidateResult
	Units      []UnitDecision
}
