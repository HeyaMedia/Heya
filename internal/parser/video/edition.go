package video

import (
	"regexp"
	"strings"
)

type Edition struct {
	Internal      bool
	Limited       bool
	Remastered    bool
	Extended      bool
	Theatrical    bool
	Directors     bool
	Unrated       bool
	Imax          bool
	FanEdit       bool
	Hdr           bool
	ThreeD        bool
	Hsbs          bool
	Sbs           bool
	Hou           bool
	Uhd           bool
	Oar           bool
	DolbyVision   bool
	HardcodedSubs bool
	DeletedScenes bool
	BonusContent  bool
	Bw            bool
}

var (
	internalEdExp     = regexp.MustCompile(`(?i)\b(INTERNAL)\b`)
	remasteredEdExp   = regexp.MustCompile(`(?i)\b(Remastered|Anniversary|Restored)\b`)
	imaxEdExp         = regexp.MustCompile(`(?i)\b(IMAX)\b`)
	unratedEdExp      = regexp.MustCompile(`(?i)\b(Uncensored|Unrated)\b`)
	extendedEdExp     = regexp.MustCompile(`(?i)\b(Extended|Uncut|Ultimate|Rogue|Collector)\b`)
	theatricalEdExp   = regexp.MustCompile(`(?i)\b(Theatrical)\b`)
	directorsEdExp    = regexp.MustCompile(`(?i)\b(Directors?)\b`)
	fanEdExp          = regexp.MustCompile(`(?i)\b(Despecialized|Fan\.?Edit)\b`)
	limitedEdExp      = regexp.MustCompile(`(?i)\b(LIMITED)\b`)
	hdrEdExp          = regexp.MustCompile(`(?i)\b(HDR)\b`)
	threeDEdExp       = regexp.MustCompile(`(?i)\b(3D)\b`)
	hsbsEdExp         = regexp.MustCompile(`(?i)\b(Half-?SBS|HSBS)\b`)
	sbsEdExp          = regexp.MustCompile(`(?i)\bSBS\b`)
	houEdExp          = regexp.MustCompile(`(?i)\b(HOU)\b`)
	uhdEdExp          = regexp.MustCompile(`(?i)\b(UHD)\b`)
	oarEdExp          = regexp.MustCompile(`(?i)\b(OAR)\b`)
	dolbyVisionEdExp  = regexp.MustCompile(`(?i)\b(DV(?:\b(?:HDR10|HLG|SDR))?)\b`)
	hardcodedSubsExp  = regexp.MustCompile(`(?i)\b(?:(?:\w+(?:(?:SOFT|HORRIBLE).*)?SUBS?)|(?:HC|SUBBED))\b`)
	deletedScenesExp  = regexp.MustCompile(`(?i)\b(?:(?:Bonus\.)?Deleted\.Scenes)\b`)
	bonusContentExp   = regexp.MustCompile(`(?i)\b(?:Bonus|Extras|Behind\.the\.Scenes|Making\.of|Interviews|Featurettes|Outtakes|Bloopers|Gag\.Reel)\.`)
	bwEdExp           = regexp.MustCompile(`(?i)\b(BW)\b`)
)

func ParseEdition(title string) Edition {
	parsedTitle := ParseTitleAndYear(title).Title
	withoutTitle := strings.ToLower(strings.ReplaceAll(strings.Replace(title, ".", " ", 1), parsedTitle, ""))

	return Edition{
		Internal:      internalEdExp.MatchString(withoutTitle),
		Limited:       limitedEdExp.MatchString(withoutTitle),
		Remastered:    remasteredEdExp.MatchString(withoutTitle),
		Extended:      extendedEdExp.MatchString(withoutTitle),
		Theatrical:    theatricalEdExp.MatchString(withoutTitle),
		Directors:     directorsEdExp.MatchString(withoutTitle),
		Unrated:       unratedEdExp.MatchString(withoutTitle),
		Imax:          imaxEdExp.MatchString(withoutTitle),
		FanEdit:       fanEdExp.MatchString(withoutTitle),
		Hdr:           hdrEdExp.MatchString(withoutTitle),
		ThreeD:        threeDEdExp.MatchString(withoutTitle),
		Hsbs:          hsbsEdExp.MatchString(withoutTitle),
		Sbs:           sbsEdExp.MatchString(withoutTitle),
		Hou:           houEdExp.MatchString(withoutTitle),
		Uhd:           uhdEdExp.MatchString(withoutTitle),
		Oar:           oarEdExp.MatchString(withoutTitle),
		DolbyVision:   dolbyVisionEdExp.MatchString(withoutTitle),
		HardcodedSubs: hardcodedSubsExp.MatchString(withoutTitle),
		DeletedScenes: deletedScenesExp.MatchString(withoutTitle),
		BonusContent:  bonusContentExp.MatchString(withoutTitle),
		Bw:            bwEdExp.MatchString(withoutTitle),
	}
}

func (e Edition) Flags() []string {
	var flags []string
	if e.Internal {
		flags = append(flags, "internal")
	}
	if e.Limited {
		flags = append(flags, "limited")
	}
	if e.Remastered {
		flags = append(flags, "remastered")
	}
	if e.Extended {
		flags = append(flags, "extended")
	}
	if e.Theatrical {
		flags = append(flags, "theatrical")
	}
	if e.Directors {
		flags = append(flags, "directors")
	}
	if e.Unrated {
		flags = append(flags, "unrated")
	}
	if e.Imax {
		flags = append(flags, "imax")
	}
	if e.FanEdit {
		flags = append(flags, "fanedit")
	}
	if e.Hdr {
		flags = append(flags, "hdr")
	}
	if e.ThreeD {
		flags = append(flags, "threed")
	}
	if e.Hsbs {
		flags = append(flags, "hsbs")
	}
	if e.Sbs {
		flags = append(flags, "sbs")
	}
	if e.Hou {
		flags = append(flags, "hou")
	}
	if e.Uhd {
		flags = append(flags, "uhd")
	}
	if e.Oar {
		flags = append(flags, "oar")
	}
	if e.DolbyVision {
		flags = append(flags, "dolbyvision")
	}
	if e.HardcodedSubs {
		flags = append(flags, "hardcodedsubs")
	}
	if e.DeletedScenes {
		flags = append(flags, "deletedscenes")
	}
	if e.BonusContent {
		flags = append(flags, "bonuscontent")
	}
	if e.Bw {
		flags = append(flags, "bw")
	}
	return flags
}
