package decision

import (
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/manager/formats"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/parser/video"
)

// ResolveUnits resolves every existing file and queued release in the
// target against the profile: ladder position and custom-format score (the
// basename or queued title is scored exactly like a candidate would be).
// Callers run this once after loading the profile — without it, existing
// files carry no position and the upgrade spec would treat everything on
// disk as infinitely upgradeable.
func ResolveUnits(target *Target) {
	profile := target.Profile
	if profile == nil {
		return
	}
	scoreTitle := func(title string) int32 {
		attrs := ParseFor(target.Domain, title)
		attrs.OriginalLanguage = target.OriginalLanguage
		var total int32
		for _, format := range profile.Formats {
			if formats.Matches(format, attrs) {
				total += profile.FormatScores[format.ID]
			}
		}
		return total
	}
	for ui := range target.Units {
		unit := &target.Units[ui]
		for fi := range unit.Existing {
			file := &unit.Existing[fi]
			file.Position, _, file.PositionFound = profile.Position(file.Quality)
			if file.Basename != "" {
				file.FormatScore = scoreTitle(file.Basename)
			}
		}
		for qi := range unit.Queued {
			queued := &unit.Queued[qi]
			queued.Position, _, queued.PositionFound = profile.Position(queued.Quality)
			if queued.Title != "" {
				queued.FormatScore = scoreTitle(queued.Title)
			}
		}
	}
}

// ParseFor parses a release title with the right domain parser.
func ParseFor(domain, title string) formats.Attrs {
	return formats.ParseVideoRelease(title, 0, domain == "tv")
}

// Evaluate runs the full gate pipeline for every candidate against every
// unit of the target and derives per-unit decisions. Every failure is a
// recorded rejection — nothing is silently dropped.
func Evaluate(target Target, candidates []Candidate) Result {
	result := Result{}

	if target.Profile == nil {
		// Still record the candidates for the ledger; every unit is a
		// configuration error.
		for _, unit := range target.Units {
			result.Units = append(result.Units, UnitDecision{
				UnitKey: unit.Key, Verdict: VerdictConfigurationError, ChosenCandidate: -1,
			})
		}
		for _, cand := range candidates {
			result.Candidates = append(result.Candidates, CandidateResult{
				Input: cand,
				RunRejections: []Rejection{{
					Code: CodeConfigNoProfile, Stage: "wanted",
					Message: "monitored item has no quality profile assigned",
				}},
			})
		}
		return result
	}

	for _, cand := range candidates {
		result.Candidates = append(result.Candidates, evaluateCandidate(target, cand))
	}
	decideUnits(target, &result)
	return result
}

func evaluateCandidate(target Target, cand Candidate) CandidateResult {
	res := CandidateResult{Input: cand, Position: -1, PerUnit: map[string]*UnitEval{}}
	profile := target.Profile

	// Stage: parse.
	isTV := target.Domain == "tv"
	attrs := formats.ParseVideoRelease(cand.Title, cand.SizeBytes, isTV)
	attrs.OriginalLanguage = target.OriginalLanguage
	res.Attrs = attrs
	if strings.TrimSpace(cand.Title) == "" {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeUnparseable, Stage: "parse", Message: "empty release title",
		})
		return res
	}

	// Stage: identity — provider ids win outright; otherwise normalized
	// title containment plus a year gate for movies.
	if rej := checkIdentity(target, cand, attrs); rej != nil {
		res.RunRejections = append(res.RunRejections, *rej)
		return res
	}

	// Stage: domain sanity.
	if isTV && attrs.IsMultiSeason {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeMultiSeason, Stage: "sanity",
			Message: "multi-season releases are rejected (Sonarr parity)",
		})
		return res
	}

	// Stage: quality mapping.
	res.QualityKey = qualityKeyFor(target.Domain, cand.Title, attrs)
	pos, allowed, found := profile.Position(res.QualityKey)
	res.Position, res.PositionFound = pos, found
	if !found {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeQualityUnmapped, Stage: "quality",
			Params:  map[string]any{"quality": res.QualityKey},
			Message: fmt.Sprintf("release quality %q has no slot in the profile ladder", orUnknown(res.QualityKey)),
		})
		return res
	}
	if !allowed {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeQualityNotAllowed, Stage: "quality",
			Params:  map[string]any{"quality": res.QualityKey},
			Message: fmt.Sprintf("quality %s is not allowed by profile %q", res.QualityKey, profile.Name),
		})
		return res
	}

	// Stage: size bounds — runtime-scaled, and a multi-episode release
	// scales by how many units it covers (a season pack is N episodes of
	// runtime, arr-style).
	coverage := coveredUnits(target, cand.Title, attrs)
	runtimeUnits := 1
	if len(coverage) > 1 {
		runtimeUnits = len(coverage)
	}
	if rej := checkSize(profile, res.QualityKey, cand.SizeBytes, target.RuntimeMinutes*runtimeUnits); rej != nil {
		res.RunRejections = append(res.RunRejections, *rej)
		return res
	}

	// Stage: language gate.
	if !formats.LanguageAcceptable(profile.Language, attrs) {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeLanguageGate, Stage: "language",
			Params:  map[string]any{"profile_language": profile.Language, "languages": attrs.Languages},
			Message: fmt.Sprintf("release languages %v fail the profile %q language gate", attrs.Languages, profile.Language),
		})
		return res
	}

	// Stage: custom-format score.
	for _, format := range profile.Formats {
		if formats.Matches(format, attrs) {
			score := profile.FormatScores[format.ID]
			res.FormatScore += score
			res.FormatBreakdown = append(res.FormatBreakdown, FormatHit{FormatID: format.ID, Name: format.Name, Score: score})
		}
	}
	if res.FormatScore < profile.MinFormatScore {
		res.RunRejections = append(res.RunRejections, Rejection{
			Code: CodeFormatScoreBelowMin, Stage: "format",
			Params:  map[string]any{"score": res.FormatScore, "min": profile.MinFormatScore},
			Message: fmt.Sprintf("format score %d below profile minimum %d", res.FormatScore, profile.MinFormatScore),
		})
		return res
	}

	// Per-unit stages: queue + upgrade. TV candidates are evaluated only
	// against the units they COVER (a single-episode release neither
	// accepts nor rejects the season's other episodes); everything else
	// covers every unit of its single-target run.
	for i := range target.Units {
		unit := &target.Units[i]
		if coverage != nil && !coverage[unitNumberKey(unit)] {
			continue
		}
		res.PerUnit[unit.Key] = evaluateUnit(profile, unit, &res)
	}
	return res
}

// coveredUnits resolves which units a TV release spans, keyed by
// season×episode (absolute numbers resolve through the unit's mapping).
// nil means "covers everything" (non-TV domains).
func coveredUnits(target Target, title string, attrs formats.Attrs) map[string]bool {
	if target.Domain != "tv" {
		return nil
	}
	show := video.FilenameParseShow(title)
	covered := map[string]bool{}
	if show.FullSeason && len(show.Seasons) > 0 {
		for _, unit := range target.Units {
			for _, season := range show.Seasons {
				if unit.SeasonNumber == season {
					covered[unitNumberKey(&unit)] = true
				}
			}
		}
		return covered
	}
	if len(show.Seasons) > 0 && len(show.EpisodeNumbers) > 0 {
		season := show.Seasons[0]
		for _, unit := range target.Units {
			if unit.SeasonNumber != season {
				continue
			}
			for _, episode := range show.EpisodeNumbers {
				if unit.EpisodeNumber == episode {
					covered[unitNumberKey(&unit)] = true
				}
			}
		}
		return covered
	}
	// Anime absolute numbering: no explicit season — resolve through the
	// units' absolute numbers.
	if target.Anime && len(show.EpisodeNumbers) > 0 {
		for _, unit := range target.Units {
			if unit.AbsoluteNumber == 0 {
				continue
			}
			for _, episode := range show.EpisodeNumbers {
				if unit.AbsoluteNumber == episode {
					covered[unitNumberKey(&unit)] = true
				}
			}
		}
	}
	return covered
}

func unitNumberKey(unit *Unit) string {
	return fmt.Sprintf("%dx%d", unit.SeasonNumber, unit.EpisodeNumber)
}

// evaluateUnit applies the wanted/queue/upgrade gates of one candidate
// against one unit.
func evaluateUnit(profile *Profile, unit *Unit, cand *CandidateResult) *UnitEval {
	eval := &UnitEval{}
	reject := func(code Code, stage, msg string, params map[string]any) {
		eval.Rejections = append(eval.Rejections, Rejection{Code: code, Stage: stage, Message: msg, Params: params})
	}

	if !unit.Monitored {
		reject(CodeUnmonitored, "wanted", "unit is not monitored", nil)
		return eval
	}
	if !unit.Released {
		reject(CodeUnreleased, "wanted", "unit has not aired/released yet", nil)
		return eval
	}

	// Queue gate: an in-flight release that this candidate wouldn't upgrade
	// means the unit is already being handled.
	for _, queued := range unit.Queued {
		virtual := ExistingFile{
			Quality: queued.Quality, Position: queued.Position,
			PositionFound: queued.PositionFound, RevisionVersion: queued.RevisionVersion,
			FormatScore: queued.FormatScore,
		}
		if rej := upgradeCheck(profile, cand, virtual); rej != nil {
			reject(CodeAlreadyDownloading, "queue",
				fmt.Sprintf("an equal-or-better release is already downloading (%s)", queued.Title),
				map[string]any{"queued": queued.Title, "reason": rej.Code})
			return eval
		}
	}

	// Upgrade gate vs existing files. Missing unit → fill.
	uncertain := false
	for _, existing := range unit.Existing {
		if existing.Uncertain {
			uncertain = true
			continue
		}
		if rej := upgradeCheck(profile, cand, existing); rej != nil {
			eval.Rejections = append(eval.Rejections, *rej)
			return eval
		}
	}
	if uncertain && len(unit.Existing) > 0 {
		// Existing quality can't be reconstructed reliably: refuse a
		// confident would_grab rather than assert an upgrade.
		reject(CodeComparisonUncertain, "upgrade",
			"existing file quality could not be reliably determined; refusing a confident upgrade verdict", nil)
		return eval
	}

	eval.Acceptable = true
	return eval
}

// upgradeCheck ports the arr UpgradableSpecification branch order: revision
// preference within the SAME EXACT quality runs before the upgrades-enabled
// gate; quality-cutoff-met blocks quality upgrades; the custom-format path
// applies on equal ladder positions only while the existing score sits below
// the format cutoff, and requires strictly-greater plus the increment.
func upgradeCheck(profile *Profile, cand *CandidateResult, existing ExistingFile) *Rejection {
	if !existing.PositionFound {
		// No slot in this profile's ladder. A KNOWN quality that canonically
		// meets or exceeds the cutoff is above the want — a 2160p disc under
		// a 1080p profile is satisfied, not upgradeable. Only a genuinely
		// unknown quality is treated as lowest (arr semantics).
		if meets, ok := QualityMeetsCutoffCanonically(profile.Domain, profile, existing.Quality); ok && meets {
			return &Rejection{Code: CodeCutoffMet, Stage: "upgrade",
				Params:  map[string]any{"existing": existing.Quality, "cutoff": profile.Cutoff},
				Message: fmt.Sprintf("existing %s already meets or exceeds the cutoff %s", existing.Quality, profile.Cutoff)}
		}
		return nil
	}
	cutoffPos, cutoffFound := profile.CutoffPosition()

	sameExactQuality := existing.Quality != "" && existing.Quality == cand.QualityKey
	samePosition := cand.Position == existing.Position

	// Revision preference: exact-quality only, before everything else.
	if sameExactQuality && profile.PreferProperRepack {
		if cand.Attrs.RevisionVersion > existing.RevisionVersion {
			return nil
		}
		if cand.Attrs.RevisionVersion < existing.RevisionVersion {
			return &Rejection{Code: CodeRevisionDowngrade, Stage: "upgrade",
				Params:  map[string]any{"existing_revision": existing.RevisionVersion, "candidate_revision": cand.Attrs.RevisionVersion},
				Message: "candidate is a revision downgrade (existing is a proper/repack)"}
		}
	}

	if !profile.UpgradesEnabled {
		return &Rejection{Code: CodeUpgradesDisabled, Stage: "upgrade",
			Message: "profile has upgrades disabled and the unit already has a file"}
	}

	switch {
	case cand.Position < existing.Position:
		// Better quality: allowed only while the existing quality cutoff is
		// unmet; no score-improvement requirement.
		if cutoffFound && existing.Position <= cutoffPos {
			return &Rejection{Code: CodeCutoffMet, Stage: "upgrade",
				Params:  map[string]any{"existing": existing.Quality, "cutoff": profile.Cutoff},
				Message: fmt.Sprintf("existing %s already meets the quality cutoff %s", existing.Quality, profile.Cutoff)}
		}
		return nil
	case samePosition:
		// Same ladder position (grouped qualities compare equal): the
		// custom-format path, only while the existing score is below the
		// format cutoff.
		if existing.FormatScore >= profile.CutoffFormatScore {
			return &Rejection{Code: CodeCutoffMet, Stage: "upgrade",
				Params:  map[string]any{"existing_score": existing.FormatScore, "cutoff_score": profile.CutoffFormatScore},
				Message: fmt.Sprintf("existing format score %d already meets the format cutoff %d", existing.FormatScore, profile.CutoffFormatScore)}
		}
		if cand.FormatScore <= existing.FormatScore {
			return &Rejection{Code: CodeNotAnUpgrade, Stage: "upgrade",
				Params:  map[string]any{"existing_score": existing.FormatScore, "candidate_score": cand.FormatScore},
				Message: fmt.Sprintf("format score %d does not improve on existing %d", cand.FormatScore, existing.FormatScore)}
		}
		if int64(cand.FormatScore) < int64(existing.FormatScore)+int64(profile.MinUpgradeScore) {
			return &Rejection{Code: CodeUpgradeIncrementUnmet, Stage: "upgrade",
				Params:  map[string]any{"existing_score": existing.FormatScore, "candidate_score": cand.FormatScore, "min_increment": profile.MinUpgradeScore},
				Message: fmt.Sprintf("format score gain %d below the minimum upgrade increment %d", cand.FormatScore-existing.FormatScore, profile.MinUpgradeScore)}
		}
		return nil
	default:
		return &Rejection{Code: CodeNotAnUpgrade, Stage: "upgrade",
			Params:  map[string]any{"existing": existing.Quality, "candidate": cand.QualityKey},
			Message: fmt.Sprintf("candidate %s is a quality downgrade from existing %s", cand.QualityKey, existing.Quality)}
	}
}

// decideUnits ranks accepted candidates per unit and derives verdicts.
func decideUnits(target Target, result *Result) {
	for i := range target.Units {
		unit := &target.Units[i]
		decision := UnitDecision{UnitKey: unit.Key, ChosenCandidate: -1}

		// A unit whose existing files already satisfy the profile — and
		// where every candidate bounced off cutoff_met — reads
		// already_satisfied rather than no_acceptable_candidate.
		accepted := make([]*CandidateResult, 0)
		sawUncertain := false
		sawCutoffMet := false
		for ci := range result.Candidates {
			cand := &result.Candidates[ci]
			eval, ok := cand.PerUnit[unit.Key]
			if !ok {
				continue
			}
			if eval.Acceptable {
				accepted = append(accepted, cand)
				continue
			}
			for _, rej := range eval.Rejections {
				switch rej.Code {
				case CodeComparisonUncertain:
					sawUncertain = true
				case CodeCutoffMet:
					sawCutoffMet = true
				}
			}
		}

		switch {
		case len(accepted) > 0:
			rankCandidates(target, unit, accepted)
			for rank, cand := range accepted {
				cand.PerUnit[unit.Key].SelectionRank = rank + 1
			}
			decision.Verdict = VerdictWouldGrab
			decision.ChosenCandidate = accepted[0].Input.Index
		case sawUncertain:
			decision.Verdict = VerdictComparisonUncertain
		case sawCutoffMet:
			decision.Verdict = VerdictAlreadySatisfied
		default:
			decision.Verdict = VerdictNoAcceptableCandidate
		}
		result.Units = append(result.Units, decision)
	}
}

// checkIdentity verifies the release belongs to the target.
func checkIdentity(target Target, cand Candidate, attrs formats.Attrs) *Rejection {
	// Provider ids attached by the indexer win outright.
	for key, want := range target.IDs {
		if want == "" {
			continue
		}
		if got, ok := cand.IDHints[key]; ok && got != "" {
			if strings.TrimPrefix(got, "tt") == strings.TrimPrefix(want, "tt") {
				return nil
			}
			return &Rejection{Code: CodeIdentityMismatch, Stage: "identity",
				Params:  map[string]any{"id": key, "want": want, "got": got},
				Message: fmt.Sprintf("indexer-reported %s %s does not match the target's %s", key, got, want)}
		}
	}

	parsedTitle := releaseTitle(target.Domain, cand.Title)
	if parsedTitle == "" {
		return &Rejection{Code: CodeUnparseable, Stage: "identity",
			Message: "no title could be parsed from the release name"}
	}
	normalized := matcher.NormalizeTitle(parsedTitle)
	matched := false
	for _, title := range target.NormalizedTitles {
		if title != "" && title == normalized {
			matched = true
			break
		}
	}
	if !matched {
		return &Rejection{Code: CodeIdentityMismatch, Stage: "identity",
			Params:  map[string]any{"parsed_title": parsedTitle},
			Message: fmt.Sprintf("parsed title %q does not match the target or its aliases", parsedTitle)}
	}

	// Movies: a parsed year more than ±1 off the target's is a different
	// film with the same name.
	if target.Domain == "movie" && target.Year > 0 && attrs.Year > 0 {
		diff := attrs.Year - target.Year
		if diff < -1 || diff > 1 {
			return &Rejection{Code: CodeIdentityMismatch, Stage: "identity",
				Params:  map[string]any{"target_year": target.Year, "release_year": attrs.Year},
				Message: fmt.Sprintf("release year %d does not corroborate the target year %d", attrs.Year, target.Year)}
		}
	}
	return nil
}

func qualityKeyFor(domain, title string, attrs formats.Attrs) string {
	switch domain {
	case "music":
		return formats.MusicQualityKey(title)
	case "book":
		return formats.BookQualityKey(title)
	default:
		return formats.QualityKey(attrs)
	}
}

func checkSize(profile *Profile, qualityKey string, sizeBytes int64, runtimeMinutes int) *Rejection {
	if sizeBytes <= 0 || runtimeMinutes <= 0 {
		return nil
	}
	def, ok := profile.SizeDefs[qualityKey]
	if !ok {
		return nil
	}
	sizeMB := float64(sizeBytes) / (1024 * 1024)
	minMB := def.MinMBPerMin * float64(runtimeMinutes)
	maxMB := def.MaxMBPerMin * float64(runtimeMinutes)
	if minMB > 0 && sizeMB < minMB {
		return &Rejection{Code: CodeSizeOutOfBounds, Stage: "size",
			Params:  map[string]any{"size_mb": int64(sizeMB), "min_mb": int64(minMB)},
			Message: fmt.Sprintf("%d MB is below the %d MB minimum for %s at %d min runtime", int64(sizeMB), int64(minMB), qualityKey, runtimeMinutes)}
	}
	if maxMB > 0 && sizeMB > maxMB {
		return &Rejection{Code: CodeSizeOutOfBounds, Stage: "size",
			Params:  map[string]any{"size_mb": int64(sizeMB), "max_mb": int64(maxMB)},
			Message: fmt.Sprintf("%d MB exceeds the %d MB maximum for %s at %d min runtime", int64(sizeMB), int64(maxMB), qualityKey, runtimeMinutes)}
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
