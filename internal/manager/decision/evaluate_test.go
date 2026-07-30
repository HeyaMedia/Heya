package decision

import (
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/manager/formats"
)

// movieProfile builds a standard 1080p movie profile: Bluray-1080p cutoff,
// upgrades on, one +100 format for DTS and one -50 format for CAM-tagged
// names, min score 0, format cutoff 200, increment 50.
func movieProfile() *Profile {
	return &Profile{
		ID: 1, Name: "HD-1080p", Domain: "movie",
		Items: []LadderItem{
			{Quality: "remux-2160p", Allowed: false},
			{Quality: "bluray-2160p", Allowed: false},
			{Quality: "webdl-2160p", Allowed: false},
			{Quality: "remux-1080p", Allowed: false},
			{Quality: "bluray-1080p", Allowed: true},
			{Group: "WEB 1080p", Qualities: []string{"webdl-1080p", "webrip-1080p"}, Allowed: true},
			{Quality: "hdtv-1080p", Allowed: true},
			{Quality: "bluray-720p", Allowed: true},
			{Group: "WEB 720p", Qualities: []string{"webdl-720p", "webrip-720p"}, Allowed: true},
			{Quality: "hdtv-720p", Allowed: false},
		},
		Cutoff: "bluray-1080p", UpgradesEnabled: true,
		MinFormatScore: 0, CutoffFormatScore: 200, MinUpgradeScore: 50,
		Language: "any", PreferProperRepack: true,
		Formats: []formats.Format{
			{ID: 10, Name: "DTS", Specs: []formats.CustomFormatSpec{{
				Name: "dts", Implementation: formats.SpecReleaseTitle, Required: true,
				Fields: map[string]any{"value": `\bDTS\b`},
			}}},
		},
		FormatScores: map[int64]int32{10: 100},
		SizeDefs:     VideoSizeDefs,
	}
}

func movieTarget(profile *Profile, existing []ExistingFile, queued []QueuedRelease) Target {
	return Target{
		Domain: "movie", MediaItemID: 42,
		NormalizedTitles: []string{"dark harvest"},
		Year:             2023,
		IDs:              map[string]string{"imdbid": "tt1234567"},
		RuntimeMinutes:   93,
		Units: []Unit{{
			Key: "movie:42", Monitored: true, Released: true,
			Existing: existing, Queued: queued,
		}},
		Profile: profile,
	}
}

func cand(index int, title string, sizeGB float64) Candidate {
	return Candidate{
		Index: index, Title: title,
		SizeBytes:   int64(sizeGB * 1024 * 1024 * 1024),
		PublishDate: time.Now().Add(-2 * time.Hour),
		IndexerID:   1, IndexerName: "idx", IndexerPriority: 25,
	}
}

func unitEval(t *testing.T, res Result, candIndex int, unitKey string) *UnitEval {
	t.Helper()
	for _, cr := range res.Candidates {
		if cr.Input.Index == candIndex {
			if len(cr.RunRejections) > 0 {
				t.Fatalf("candidate %d has run rejections %v, expected unit eval", candIndex, cr.RunRejections)
			}
			eval := cr.PerUnit[unitKey]
			if eval == nil {
				t.Fatalf("candidate %d has no eval for unit %s", candIndex, unitKey)
			}
			return eval
		}
	}
	t.Fatalf("candidate %d not found", candIndex)
	return nil
}

func runRejectionCode(t *testing.T, res Result, candIndex int) Code {
	t.Helper()
	for _, cr := range res.Candidates {
		if cr.Input.Index == candIndex {
			if len(cr.RunRejections) == 0 {
				t.Fatalf("candidate %d has no run rejections", candIndex)
			}
			return cr.RunRejections[0].Code
		}
	}
	t.Fatalf("candidate %d not found", candIndex)
	return ""
}

func firstUnitRejection(t *testing.T, eval *UnitEval) Code {
	t.Helper()
	if eval.Acceptable {
		t.Fatalf("expected rejection, candidate acceptable")
	}
	if len(eval.Rejections) == 0 {
		t.Fatalf("not acceptable but no rejections recorded")
	}
	return eval.Rejections[0].Code
}

func TestMissingMovieWouldGrab(t *testing.T) {
	target := movieTarget(movieProfile(), nil, nil)
	res := Evaluate(target, []Candidate{
		cand(0, "Dark.Harvest.2023.1080p.BluRay.DTS.x264-GROUP", 12),
	})
	if res.Units[0].Verdict != VerdictWouldGrab {
		t.Fatalf("verdict = %s, want would_grab", res.Units[0].Verdict)
	}
	if res.Units[0].ChosenCandidate != 0 {
		t.Fatalf("chosen = %d, want 0", res.Units[0].ChosenCandidate)
	}
	if res.Candidates[0].FormatScore != 100 {
		t.Fatalf("format score = %d, want 100 (DTS)", res.Candidates[0].FormatScore)
	}
}

func TestIdentityGates(t *testing.T) {
	target := movieTarget(movieProfile(), nil, nil)
	res := Evaluate(target, []Candidate{
		cand(0, "Some.Other.Movie.2023.1080p.BluRay.x264-GROUP", 12),
		cand(1, "Dark.Harvest.1998.1080p.BluRay.x264-GROUP", 12),
	})
	if code := runRejectionCode(t, res, 0); code != CodeIdentityMismatch {
		t.Errorf("wrong title: code %s, want identity_mismatch", code)
	}
	if code := runRejectionCode(t, res, 1); code != CodeIdentityMismatch {
		t.Errorf("wrong year: code %s, want identity_mismatch", code)
	}

	// Indexer-attached imdb id overrides a title that wouldn't match.
	withID := cand(2, "Totally.Renamed.Release.2023.1080p.BluRay.x264-GROUP", 12)
	withID.IDHints = map[string]string{"imdbid": "1234567"}
	res = Evaluate(target, []Candidate{withID})
	if res.Units[0].Verdict != VerdictWouldGrab {
		t.Errorf("imdb id should confirm identity, verdict = %s", res.Units[0].Verdict)
	}

	wrongID := cand(3, "Dark.Harvest.2023.1080p.BluRay.x264-GROUP", 12)
	wrongID.IDHints = map[string]string{"imdbid": "tt7654321"}
	res = Evaluate(target, []Candidate{wrongID})
	if code := runRejectionCode(t, res, 3); code != CodeIdentityMismatch {
		t.Errorf("mismatched id: code %s, want identity_mismatch", code)
	}
}

func TestQualityGates(t *testing.T) {
	target := movieTarget(movieProfile(), nil, nil)
	res := Evaluate(target, []Candidate{
		cand(0, "Dark.Harvest.2023.2160p.WEB-DL.x265-GROUP", 20), // in ladder, not allowed
		cand(1, "Dark.Harvest.2023.TELESYNC.x264-GROUP", 2),      // unmapped
	})
	if code := runRejectionCode(t, res, 0); code != CodeQualityNotAllowed {
		t.Errorf("2160p: code %s, want quality_not_allowed", code)
	}
	if code := runRejectionCode(t, res, 1); code != CodeQualityUnmapped {
		t.Errorf("telesync: code %s, want quality_unmapped", code)
	}
}

func TestUpgradeBranches(t *testing.T) {
	profile := movieProfile()
	webdl1080 := func(score int32, revision int) ExistingFile {
		return ExistingFile{
			FileID: 7, Basename: "existing.mkv", Quality: "webdl-1080p",
			Position: 5, PositionFound: true, RevisionVersion: revision,
			FormatScore: score, Provenance: "parsed_name",
		}
	}

	t.Run("better quality accepted below cutoff even with lower score", func(t *testing.T) {
		// Existing WEB-DL with a big score; Bluray candidate with score 0
		// must still be accepted — quality upgrades don't require score
		// improvement (arr parity).
		target := movieTarget(profile, []ExistingFile{webdl1080(150, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.BluRay.x264-GROUP", 12)})
		if res.Units[0].Verdict != VerdictWouldGrab {
			t.Fatalf("verdict = %s, want would_grab", res.Units[0].Verdict)
		}
	})

	t.Run("quality cutoff met blocks quality upgrades", func(t *testing.T) {
		existing := ExistingFile{Quality: "bluray-1080p", Position: 4, PositionFound: true, RevisionVersion: 1, FormatScore: 0, Provenance: "parsed_name"}
		target := movieTarget(profile, []ExistingFile{existing}, nil)
		// remux-1080p is not allowed in this profile, so use a 2160p...
		// also not allowed. The blocked-upgrade case needs an allowed
		// higher quality: make bluray-2160p allowed for this test.
		p2 := movieProfile()
		p2.Items[1].Allowed = true // bluray-2160p
		target.Profile = p2
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.2160p.BluRay.x265-GROUP", 30)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeCutoffMet {
			t.Fatalf("code = %s, want cutoff_met", code)
		}
		if res.Units[0].Verdict != VerdictAlreadySatisfied {
			t.Fatalf("verdict = %s, want already_satisfied", res.Units[0].Verdict)
		}
	})

	t.Run("same position needs strict gain plus increment", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(60, 1)}, nil)
		// DTS candidate scores 100: gain 40 < increment 50.
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 10)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeUpgradeIncrementUnmet {
			t.Fatalf("code = %s, want upgrade_increment_unmet", code)
		}
	})

	t.Run("same position equal score is not an upgrade", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(100, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 10)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeNotAnUpgrade {
			t.Fatalf("code = %s, want not_an_upgrade", code)
		}
	})

	t.Run("same position sufficient gain accepted", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(0, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 10)})
		if res.Units[0].Verdict != VerdictWouldGrab {
			t.Fatalf("verdict = %s, want would_grab", res.Units[0].Verdict)
		}
	})

	t.Run("format cutoff met blocks score upgrades", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(200, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 10)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeCutoffMet {
			t.Fatalf("code = %s, want cutoff_met", code)
		}
	})

	t.Run("proper accepted before upgrades-disabled gate", func(t *testing.T) {
		p := movieProfile()
		p.UpgradesEnabled = false
		target := movieTarget(p, []ExistingFile{webdl1080(100, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.PROPER.1080p.WEB-DL.x264-GROUP", 10)})
		if res.Units[0].Verdict != VerdictWouldGrab {
			t.Fatalf("proper with upgrades disabled: verdict = %s, want would_grab", res.Units[0].Verdict)
		}
	})

	t.Run("upgrades disabled rejects plain upgrades", func(t *testing.T) {
		p := movieProfile()
		p.UpgradesEnabled = false
		target := movieTarget(p, []ExistingFile{webdl1080(0, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.BluRay.x264-GROUP", 12)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeUpgradesDisabled {
			t.Fatalf("code = %s, want upgrades_disabled", code)
		}
	})

	t.Run("revision downgrade rejected", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(0, 2)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 10)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeRevisionDowngrade {
			t.Fatalf("code = %s, want revision_downgrade", code)
		}
	})

	t.Run("lower quality rejected", func(t *testing.T) {
		target := movieTarget(profile, []ExistingFile{webdl1080(0, 1)}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.HDTV.x264-GROUP", 6)})
		eval := unitEval(t, res, 0, "movie:42")
		if code := firstUnitRejection(t, eval); code != CodeNotAnUpgrade {
			t.Fatalf("code = %s, want not_an_upgrade", code)
		}
	})

	t.Run("uncertain existing yields comparison_uncertain", func(t *testing.T) {
		existing := ExistingFile{Quality: "webdl-1080p", Position: 5, PositionFound: true, Provenance: "inferred", Uncertain: true}
		target := movieTarget(profile, []ExistingFile{existing}, nil)
		res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.BluRay.x264-GROUP", 12)})
		if res.Units[0].Verdict != VerdictComparisonUncertain {
			t.Fatalf("verdict = %s, want comparison_uncertain", res.Units[0].Verdict)
		}
	})
}

func TestQueueGate(t *testing.T) {
	profile := movieProfile()
	queued := []QueuedRelease{{
		Title: "Dark.Harvest.2023.1080p.BluRay.x264-OTHER", Quality: "bluray-1080p",
		Position: 4, PositionFound: true, RevisionVersion: 1, FormatScore: 0,
	}}
	target := movieTarget(profile, nil, queued)

	// Equal-or-worse candidate: already downloading.
	res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.x264-GROUP", 8)})
	eval := unitEval(t, res, 0, "movie:42")
	if code := firstUnitRejection(t, eval); code != CodeAlreadyDownloading {
		t.Fatalf("code = %s, want already_downloading", code)
	}
	if res.Units[0].Verdict != VerdictNoAcceptableCandidate {
		t.Fatalf("verdict = %s, want no_acceptable_candidate", res.Units[0].Verdict)
	}
}

func TestRankingOrder(t *testing.T) {
	profile := movieProfile()
	target := movieTarget(profile, nil, nil)

	blurayPlain := cand(0, "Dark.Harvest.2023.1080p.BluRay.x264-GROUP", 12)
	webdlDTS := cand(1, "Dark.Harvest.2023.1080p.WEB-DL.DTS.x264-GROUP", 9) // +100 score, worse quality
	webdlProper := cand(2, "Dark.Harvest.2023.PROPER.1080p.WEB-DL.x264-GROUP", 9)
	webdlPlain := cand(3, "Dark.Harvest.2023.1080p.WEB-DL.x264-GROUP", 9)
	webdlPlainLowPrio := cand(4, "Dark.Harvest.2023.1080p.WEB-DL.x264-OTHER", 9)
	webdlPlainLowPrio.IndexerID = 2
	webdlPlainLowPrio.IndexerPriority = 10 // lower = preferred

	res := Evaluate(target, []Candidate{blurayPlain, webdlDTS, webdlProper, webdlPlain, webdlPlainLowPrio})
	if res.Units[0].Verdict != VerdictWouldGrab {
		t.Fatalf("verdict = %s", res.Units[0].Verdict)
	}
	// Quality beats score: bluray wins over DTS webdl.
	if res.Units[0].ChosenCandidate != 0 {
		t.Fatalf("chosen = %d, want 0 (bluray beats higher-scored webdl)", res.Units[0].ChosenCandidate)
	}
	// Among the webdls: proper outranks DTS? No — revision compares before
	// score in the comparer, so PROPER (rev 2) beats DTS (+100, rev 1).
	rank := func(idx int) int { return res.Candidates[idx].PerUnit["movie:42"].SelectionRank }
	if rank(2) >= rank(1) {
		t.Errorf("proper rank %d should beat DTS rank %d at same quality", rank(2), rank(1))
	}
	// Indexer priority breaks the plain-webdl tie.
	if rank(4) >= rank(3) {
		t.Errorf("priority 10 rank %d should beat priority 25 rank %d", rank(4), rank(3))
	}
}

func TestSizeBounds(t *testing.T) {
	target := movieTarget(movieProfile(), nil, nil)
	res := Evaluate(target, []Candidate{
		cand(0, "Dark.Harvest.2023.1080p.WEB-DL.x264-TINY", 0.05), // ~51 MB for 93 min
	})
	if code := runRejectionCode(t, res, 0); code != CodeSizeOutOfBounds {
		t.Fatalf("code = %s, want size_out_of_bounds", code)
	}
}

func TestNoProfileConfigurationError(t *testing.T) {
	target := movieTarget(nil, nil, nil)
	target.Profile = nil
	res := Evaluate(target, []Candidate{cand(0, "Dark.Harvest.2023.1080p.WEB-DL.x264-GROUP", 9)})
	if res.Units[0].Verdict != VerdictConfigurationError {
		t.Fatalf("verdict = %s, want configuration_error", res.Units[0].Verdict)
	}
	if code := runRejectionCode(t, res, 0); code != CodeConfigNoProfile {
		t.Fatalf("code = %s, want config_no_profile", code)
	}
}
