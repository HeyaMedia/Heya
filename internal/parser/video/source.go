package video

import "regexp"

type Source string

const (
	SourceBLURAY    Source = "BLURAY"
	SourceWEBDL     Source = "WEBDL"
	SourceWEBRIP    Source = "WEBRIP"
	SourceDVD       Source = "DVD"
	SourceCAM       Source = "CAM"
	SourceSCREENER  Source = "SCREENER"
	SourcePPV       Source = "PPV"
	SourceTELESYNC  Source = "TELESYNC"
	SourceTELECINE  Source = "TELECINE"
	SourceWORKPRINT Source = "WORKPRINT"
	SourceTV        Source = "TV"
)

var (
	blurayExp    = regexp.MustCompile(`(?i)\b(?P<bluray>M?Blu-?Ray|HDDVD|BD|UHDBD|BDISO|BDMux|BD25|BD50|BR\.?DISK|Bluray(?:1080|720)p?|BD(?:1080|720)p?)\b`)
	WebdlExp     = regexp.MustCompile(`(?i)\b(?P<webdl>WEB[-_. ]DL|HDRIP|WEBDL|WEB-DLMux|NF|APTV|NETFLIX|NetflixU?HD|DSNY|DSNP|HMAX|AMZN|AmazonHD|iTunesHD|MaxdomeHD|WebHD\b|[. ]WEB[. ](?:[xh]26[45]|DD5[. ]1)|\d+0p[. ]WEB[. ]|\b\s/\sWEB\s/\s\b|AMZN[. ]WEB[. ])\b`)
	webripExp    = regexp.MustCompile(`(?i)\b(?P<webrip>WebRip|Web-Rip|WEBCap|WEBMux)\b`)
	hdtvExp      = regexp.MustCompile(`(?i)\b(?P<hdtv>HDTV)\b`)
	bdripExp     = regexp.MustCompile(`(?i)\b(?P<bdrip>BDRip|UHDBDRip|HD[-_. ]?DVDRip)\b`)
	brripExp     = regexp.MustCompile(`(?i)\b(?P<brrip>BRRip)\b`)
	dvdrExp      = regexp.MustCompile(`(?i)\b(?P<dvdr>DVD-R|DVDR)\b`)
	dvdExp       = regexp.MustCompile(`(?i)\b(?P<dvd>DVD9?|DVDRip|NTSC|PAL|xvidvd|DvDivX)\b`)
	dsrExp       = regexp.MustCompile(`(?i)\b(?P<dsr>WS[-_. ]DSR|DSR)\b`)
	regionalExp  = regexp.MustCompile(`(?i)\b(?P<regional>R[0-9]{1}|REGIONAL)\b`)
	ppvExp       = regexp.MustCompile(`(?i)\b(?P<ppv>PPV)\b`)
	scrExp       = regexp.MustCompile(`(?i)\b(?P<scr>SCR|SCREENER|DVDSCR|(?:DVD|WEB)\.?SCREENER)\b`)
	tsExp        = regexp.MustCompile(`(?i)\b(?P<ts>TS|TELESYNC|HD-TS|HDTS|PDVD|TSRip|HDTSRip)\b`)
	tcExp        = regexp.MustCompile(`(?i)\b(?P<tc>TC|TELECINE|HD-TC|HDTC)\b`)
	camExp       = regexp.MustCompile(`(?i)\b(?P<cam>CAMRIP|CAM|HDCAM|HD-CAM)\b`)
	workprintExp = regexp.MustCompile(`(?i)\b(?P<workprint>WORKPRINT|WP)\b`)
	pdtvExp      = regexp.MustCompile(`(?i)\b(?P<pdtv>PDTV)\b`)
	sdtvExp      = regexp.MustCompile(`(?i)\b(?P<sdtv>SDTV)\b`)
	tvripExp     = regexp.MustCompile(`(?i)\b(?P<tvrip>TVRip)\b`)
)

type SourceGroups struct {
	Bluray   bool
	Webdl    bool
	Webrip   bool
	Hdtv     bool
	Bdrip    bool
	Brrip    bool
	Scr      bool
	Dvdr     bool
	Dvd      bool
	Dsr      bool
	Regional bool
	Ppv      bool
	Ts       bool
	Tc       bool
	Cam      bool
	Workprint bool
	Pdtv     bool
	Sdtv     bool
	Tvrip    bool
}

func ParseSourceGroups(title string) SourceGroups {
	n := title
	n = regexp.MustCompile(`_`).ReplaceAllString(n, " ")
	n = regexp.MustCompile(`\[`).ReplaceAllString(n, " ")
	n = regexp.MustCompile(`\]`).ReplaceAllString(n, " ")

	return SourceGroups{
		Bluray:    blurayExp.MatchString(n),
		Webdl:     WebdlExp.MatchString(n),
		Webrip:    webripExp.MatchString(n),
		Hdtv:      hdtvExp.MatchString(n),
		Bdrip:     bdripExp.MatchString(n),
		Brrip:     brripExp.MatchString(n),
		Scr:       scrExp.MatchString(n),
		Dvdr:      dvdrExp.MatchString(n),
		Dvd:       dvdExp.MatchString(n),
		Dsr:       dsrExp.MatchString(n),
		Regional:  regionalExp.MatchString(n),
		Ppv:       ppvExp.MatchString(n),
		Ts:        tsExp.MatchString(n),
		Tc:        tcExp.MatchString(n),
		Cam:       camExp.MatchString(n),
		Workprint: workprintExp.MatchString(n),
		Pdtv:      pdtvExp.MatchString(n),
		Sdtv:      sdtvExp.MatchString(n),
		Tvrip:     tvripExp.MatchString(n),
	}
}

func ParseSource(title string) []Source {
	groups := ParseSourceGroups(title)
	var result []Source

	if groups.Bluray || groups.Bdrip || groups.Brrip {
		result = append(result, SourceBLURAY)
	}
	if groups.Webrip {
		result = append(result, SourceWEBRIP)
	}
	if !groups.Webrip && groups.Webdl {
		result = append(result, SourceWEBDL)
	}
	if groups.Dvdr || (groups.Dvd && !groups.Scr) {
		result = append(result, SourceDVD)
	}
	if groups.Ppv {
		result = append(result, SourcePPV)
	}
	if groups.Workprint {
		result = append(result, SourceWORKPRINT)
	}
	if groups.Pdtv || groups.Sdtv || groups.Dsr || groups.Tvrip || groups.Hdtv {
		result = append(result, SourceTV)
	}
	if groups.Cam {
		result = append(result, SourceCAM)
	}
	if groups.Ts {
		result = append(result, SourceTELESYNC)
	}
	if groups.Tc {
		result = append(result, SourceTELECINE)
	}
	if groups.Scr {
		result = append(result, SourceSCREENER)
	}

	return result
}
