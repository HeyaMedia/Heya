package formats

import (
	"os"
	"testing"
)

func importTestdata(t *testing.T, kind, file string) map[string]Format {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	imported, err := ParseArrFormats(kind, raw)
	if err != nil {
		t.Fatalf("importing %s: %v", file, err)
	}
	byName := make(map[string]Format, len(imported))
	for _, format := range imported {
		for _, spec := range format.Specs {
			if err := spec.Validate(); err != nil && spec.Implementation != SpecIndexerFlag {
				t.Errorf("%s / %q: imported spec fails validation: %v", format.Name, spec.Name, err)
			}
		}
		byName[format.Name] = Format{Name: format.Name, Specs: format.Specs}
	}
	return byName
}

func TestImportRadarrCorpus(t *testing.T) {
	byName := importTestdata(t, "radarr", "radarr-cf.json")
	if len(byName) != 83 {
		t.Fatalf("expected 83 radarr formats, got %d", len(byName))
	}

	x265 := byName["x265"]
	if len(x265.Specs) != 2 {
		t.Fatalf("x265 should keep 2 specs, got %d", len(x265.Specs))
	}
	var modifier *CustomFormatSpec
	for i := range x265.Specs {
		if x265.Specs[i].Implementation == SpecQualityModifier {
			modifier = &x265.Specs[i]
		}
	}
	if modifier == nil {
		t.Fatal("x265 lost its QualityModifierSpecification")
	}
	if got := modifier.Fields["value"]; got != "remux" {
		t.Fatalf("radarr modifier enum 5 should normalize to %q, got %v", "remux", got)
	}
	if !modifier.Negate || !modifier.Required {
		t.Fatal("x265 Not-Remux spec should stay negated and required")
	}
}

func TestImportSonarrCorpus(t *testing.T) {
	byName := importTestdata(t, "sonarr", "sonarr-cf.json")
	if len(byName) != 105 {
		t.Fatalf("expected 105 sonarr formats, got %d", len(byName))
	}

	// Sonarr's source enum 7 is a remux — the same number Radarr uses for
	// WEBDL. The kind-aware tables must keep them apart.
	x265 := byName["x265"]
	for _, spec := range x265.Specs {
		if spec.Implementation == SpecSource {
			if got := spec.Fields["value"]; got != "remux" {
				t.Fatalf("sonarr source enum 7 should normalize to %q, got %v", "remux", got)
			}
		}
	}
}

func TestEvaluateAgainstCorpus(t *testing.T) {
	radarr := importTestdata(t, "radarr", "radarr-cf.json")

	cases := []struct {
		title   string
		format  string
		matches bool
	}{
		// Plain 1080p web x264: codec and resolution formats hit.
		{"Movie.Name.2023.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", "x264", true},
		{"Movie.Name.2023.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", "AMZN", true},
		{"Movie.Name.2023.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", "1080p", true},
		{"Movie.Name.2023.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", "2160p", false},
		{"Movie.Name.2023.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", "x265", false},
		// x265 requires Not-Remux: an HEVC remux must NOT count as x265.
		{"Movie.Name.2023.2160p.UHD.BluRay.REMUX.HDR.HEVC.TrueHD.7.1.Atmos-FraMeSToR", "x265", false},
		{"Movie.Name.2023.1080p.WEB-DL.x265-GRP", "x265", true},
		// Multi-group format with a negated lookalike title regex.
		{"Movie.Name.2023.1080p.WEB-DL.x265-GRP", "x265 (no HDR/DV)", true},
		{"Movie.Name.2023.1080p.WEB-DL.HDR.x265-GRP", "x265 (no HDR/DV)", false},
		{"Movie.Name.2023.2160p.WEB-DL.x265-GRP", "x265 (no HDR/DV)", false},
		// Language: release with an explicit non-English token vs none.
		{"Movie.Name.2023.FRENCH.1080p.WEB-DL.x264-GRP", "Language: Not Original", true},
		{"Movie.Name.2023.1080p.WEB-DL.x264-GRP", "Language: Not Original", false},
	}

	for _, testCase := range cases {
		format, ok := radarr[testCase.format]
		if !ok {
			t.Fatalf("corpus is missing format %q", testCase.format)
		}
		attrs := ParseVideoRelease(testCase.title, 0, false)
		if got := Matches(format, attrs); got != testCase.matches {
			t.Errorf("%q vs %q: got %v, want %v (attrs %+v)", testCase.title, testCase.format, got, testCase.matches, attrs)
		}
	}
}

func TestEvaluateSonarrSeasonPack(t *testing.T) {
	sonarr := importTestdata(t, "sonarr", "sonarr-cf.json")
	format, ok := sonarr["x265"]
	if !ok {
		t.Fatal("sonarr corpus is missing x265")
	}

	pack := ParseVideoRelease("Show.Name.S01.2160p.NF.WEB-DL.DDP5.1.HDR.HEVC-XEBEC", 0, true)
	if pack.ReleaseType != "season-pack" {
		t.Fatalf("expected a season pack, got %q", pack.ReleaseType)
	}
	if !Matches(format, pack) {
		t.Error("HEVC web season pack should match sonarr x265")
	}

	remux := ParseVideoRelease("Show.Name.S01E03.Episode.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON", 0, true)
	if Matches(format, remux) {
		t.Error("remux should not match sonarr x265 (Not-Remux source condition)")
	}
}

func TestTrashObjectFieldsAndLookahead(t *testing.T) {
	// TRaSH guide JSON: object-form fields and a .NET lookahead RE2 rejects.
	trashJSON := `{
		"trash_id": "abc123",
		"trash_scores": {"default": 100},
		"name": "SDR (no WEBDL)",
		"includeCustomFormatWhenRenaming": false,
		"specifications": [
			{
				"name": "Not HDR",
				"implementation": "ReleaseTitleSpecification",
				"negate": false,
				"required": true,
				"fields": {"value": "^(?!.*\\b(HDR|DV|HLG)\\b).*$"}
			},
			{
				"name": "Not WEBDL",
				"implementation": "SourceSpecification",
				"negate": true,
				"required": true,
				"fields": {"value": 7}
			}
		]
	}`
	imported, err := ParseArrFormats("radarr", []byte(trashJSON))
	if err != nil {
		t.Fatalf("importing TRaSH JSON: %v", err)
	}
	if len(imported) != 1 || imported[0].TrashID != "abc123" || imported[0].TrashScores["default"] != 100 {
		t.Fatalf("TRaSH identity fields lost: %+v", imported)
	}

	format := Format{Name: imported[0].Name, Specs: imported[0].Specs}
	bluray := ParseVideoRelease("Movie.Name.2023.1080p.BluRay.x264-GRP", 0, false)
	if !Matches(format, bluray) {
		t.Error("SDR bluray should match (lookahead passes, source is not webdl)")
	}
	hdrWeb := ParseVideoRelease("Movie.Name.2023.2160p.WEB-DL.HDR.x265-GRP", 0, false)
	if Matches(format, hdrWeb) {
		t.Error("HDR web release should fail both conditions")
	}
}

func TestSizeSpecification(t *testing.T) {
	format := Format{Name: "size", Specs: []CustomFormatSpec{{
		Name: "1-10GB", Implementation: SpecSize, Required: true,
		Fields: map[string]any{"min": float64(1), "max": float64(10)},
	}}}
	within := Attrs{Title: "x", SizeBytes: 5 << 30}
	if !Matches(format, within) {
		t.Error("5 GiB should sit inside a 1-10 GB window")
	}
	over := Attrs{Title: "x", SizeBytes: 40 << 30}
	if Matches(format, over) {
		t.Error("40 GiB should exceed a 1-10 GB window")
	}
	unknown := Attrs{Title: "x"}
	if Matches(format, unknown) {
		t.Error("unknown size should never match a size window")
	}
}

func TestImportRadarrProfiles(t *testing.T) {
	raw, err := os.ReadFile("testdata/radarr-qp.json")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ParseArrProfiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 radarr profiles, got %d", len(profiles))
	}

	var all *ImportedProfile
	for i := range profiles {
		if profiles[i].Name == "All" {
			all = &profiles[i]
		}
	}
	if all == nil {
		t.Fatal("missing the All profile")
	}
	if all.MinFormatScore != 10 || all.CutoffFormatScore != 4500 {
		t.Fatalf("score thresholds mangled: %+v", all)
	}
	if all.Language != "original" {
		t.Fatalf("radarr profile language should import as %q, got %q", "original", all.Language)
	}
	if all.FormatScores["x265"] != 2000 {
		t.Fatalf("format score for x265 should survive import, got %d", all.FormatScores["x265"])
	}

	// Ladder must arrive best-first with groups intact.
	any := profiles[0]
	if any.Name != "Any" {
		any = profiles[1]
	}
	if any.Items[0].Quality != "raw-hd" {
		t.Fatalf("best rung should be raw-hd after the flip, got %+v", any.Items[0])
	}
	foundGroup := false
	for _, item := range any.Items {
		if item.Group == "WEB 1080p" {
			foundGroup = true
			if len(item.Qualities) != 2 || item.Qualities[0] != "webdl-1080p" {
				t.Fatalf("WEB 1080p group members mangled: %+v", item.Qualities)
			}
		}
	}
	if !foundGroup {
		t.Fatal("WEB 1080p group lost in import")
	}
}

func TestImportSonarrProfileCutoff(t *testing.T) {
	raw, err := os.ReadFile("testdata/sonarr-qp.json")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ParseArrProfiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range profiles {
		if profile.Name == "4K" {
			if profile.Cutoff == "" {
				t.Fatal("4K profile cutoff did not resolve")
			}
			if !profile.UpgradesEnabled {
				t.Fatal("4K profile should allow upgrades")
			}
			if profile.Language != "any" {
				t.Fatalf("sonarr profiles carry no language and should default to %q, got %q", "any", profile.Language)
			}
		}
	}
}

func TestLanguageAcceptable(t *testing.T) {
	danish := ParseVideoRelease("Forbrydelsen.S01E01.DANISH.1080p.WEB-DL.H.264-GRP", 0, true)
	plain := ParseVideoRelease("Show.Name.S01E01.1080p.WEB-DL.H.264-GRP", 0, true)

	if !LanguageAcceptable("any", plain) || !LanguageAcceptable("", plain) {
		t.Error("'any' must accept everything")
	}
	if !LanguageAcceptable("danish", danish) {
		t.Error("a DANISH-tagged release should pass a danish gate")
	}
	if LanguageAcceptable("danish", plain) {
		t.Error("an untagged release defaults to original/english and should fail a danish gate")
	}
	if !LanguageAcceptable("original", plain) {
		t.Error("an untagged release is the original audio and should pass 'original'")
	}
	danishOriginal := danish
	danishOriginal.OriginalLanguage = "danish"
	if !LanguageAcceptable("original", danishOriginal) {
		t.Error("a DANISH release of a Danish show should pass 'original'")
	}
	if LanguageAcceptable("original", danish) {
		t.Error("a DANISH release with unknown original (assumed english) should fail 'original'")
	}
}

func TestMapArrQualityName(t *testing.T) {
	cases := map[string]string{
		"Bluray-2160p Remux": "remux-2160p",
		"Remux-2160p":        "remux-2160p",
		"WEBDL-1080p":        "webdl-1080p",
		"WEBRip-720p":        "webrip-720p",
		"Raw-HD":             "raw-hd",
		"FLAC 24bit":         "flac-24",
		"MP3-VBR-V0":         "mp3-v0",
		"OGG Vorbis Q10":     "ogg-vorbis-q10",
	}
	for input, want := range cases {
		if got := MapArrQualityName(input); got != want {
			t.Errorf("MapArrQualityName(%q) = %q, want %q", input, got, want)
		}
	}
}
