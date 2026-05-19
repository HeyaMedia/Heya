package video

import (
	"regexp"
	"strings"
)

type Language string

const (
	LangEnglish    Language = "English"
	LangFrench     Language = "French"
	LangSpanish    Language = "Spanish"
	LangGerman     Language = "German"
	LangItalian    Language = "Italian"
	LangDanish     Language = "Danish"
	LangDutch      Language = "Dutch"
	LangJapanese   Language = "Japanese"
	LangCantonese  Language = "Cantonese"
	LangMandarin   Language = "Mandarin"
	LangRussian    Language = "Russian"
	LangPolish     Language = "Polish"
	LangVietnamese Language = "Vietnamese"
	LangNordic     Language = "Nordic"
	LangSwedish    Language = "Swedish"
	LangNorwegian  Language = "Norwegian"
	LangFinnish    Language = "Finnish"
	LangTurkish    Language = "Turkish"
	LangPortuguese Language = "Portuguese"
	LangFlemish    Language = "Flemish"
	LangGreek      Language = "Greek"
	LangKorean     Language = "Korean"
	LangHungarian  Language = "Hungarian"
	LangPersian    Language = "Persian"
	LangBengali    Language = "Bengali"
	LangBulgarian  Language = "Bulgarian"
	LangBrazilian  Language = "Brazilian"
	LangHebrew     Language = "Hebrew"
	LangCzech      Language = "Czech"
	LangUkrainian  Language = "Ukrainian"
	LangCatalan    Language = "Catalan"
	LangChinese    Language = "Chinese"
	LangThai       Language = "Thai"
	LangHindi      Language = "Hindi"
	LangTamil      Language = "Tamil"
	LangArabic     Language = "Arabic"
	LangEstonian   Language = "Estonian"
	LangIcelandic  Language = "Icelandic"
	LangLatvian    Language = "Latvian"
	LangLithuanian Language = "Lithuanian"
	LangRomanian   Language = "Romanian"
	LangSlovak     Language = "Slovak"
	LangSerbian    Language = "Serbian"
)

type langCheck struct {
	lang    Language
	pattern *regexp.Regexp
	simple  string
}

var langChecks = []langCheck{
	{LangEnglish, regexp.MustCompile(`(?i)\b(english|eng|EN|FI)\b`), ""},
	{LangSpanish, nil, "spanish"},
	{LangDanish, regexp.MustCompile(`(?i)\b(DK|DAN|danish)\b`), ""},
	{LangJapanese, nil, "japanese"},
	{LangCantonese, nil, "cantonese"},
	{LangMandarin, nil, "mandarin"},
	{LangKorean, nil, "korean"},
	{LangVietnamese, nil, "vietnamese"},
	{LangSwedish, regexp.MustCompile(`(?i)\b(SE|SWE|swedish)\b`), ""},
	{LangFinnish, nil, "finnish"},
	{LangTurkish, nil, "turkish"},
	{LangPortuguese, nil, "portuguese"},
	{LangHebrew, nil, "hebrew"},
	{LangCzech, nil, "czech"},
	{LangUkrainian, nil, "ukrainian"},
	{LangCatalan, nil, "catalan"},
	{LangEstonian, nil, "estonian"},
	{LangIcelandic, regexp.MustCompile(`(?i)\b(ice|Icelandic)\b`), ""},
	{LangChinese, regexp.MustCompile(`(?i)\b(chi|chinese)\b`), ""},
	{LangThai, nil, "thai"},
	{LangItalian, regexp.MustCompile(`(?i)\b(ita|italian)\b`), ""},
	{LangGerman, regexp.MustCompile(`(?i)\b(german|videomann)\b`), ""},
	{LangFlemish, regexp.MustCompile(`(?i)\b(flemish)\b`), ""},
	{LangGreek, regexp.MustCompile(`(?i)\b(greek)\b`), ""},
	{LangFrench, regexp.MustCompile(`(?i)\b(FR|FRENCH|VOSTFR|VO|VFF|VFQ|VF2|TRUEFRENCH|SUBFRENCH)\b`), ""},
	{LangRussian, regexp.MustCompile(`(?i)\b(russian|rus)\b`), ""},
	{LangNorwegian, regexp.MustCompile(`(?i)\b(norwegian|NO)\b`), ""},
	{LangHungarian, regexp.MustCompile(`(?i)\b(HUNDUB|HUN|hungarian)\b`), ""},
	{LangHebrew, regexp.MustCompile(`(?i)\b(HebDub)\b`), ""},
	{LangCzech, regexp.MustCompile(`(?i)\b(CZ|SK)\b`), ""},
	{LangUkrainian, regexp.MustCompile(`(?i)\bukr\b`), ""},
	{LangPolish, regexp.MustCompile(`(?i)\b(PL|PLDUB|POLISH)\b`), ""},
	{LangDutch, regexp.MustCompile(`(?i)\b(nl|dutch)\b`), ""},
	{LangHindi, regexp.MustCompile(`(?i)\b(HIN|Hindi)\b`), ""},
	{LangTamil, regexp.MustCompile(`(?i)\b(TAM|Tamil)\b`), ""},
	{LangArabic, regexp.MustCompile(`(?i)\b(Arabic)\b`), ""},
	{LangLatvian, regexp.MustCompile(`(?i)\b(Latvian)\b`), ""},
	{LangLithuanian, regexp.MustCompile(`(?i)\b(Lithuanian)\b`), ""},
	{LangRomanian, regexp.MustCompile(`(?i)\b(RO|Romanian|rodubbed)\b`), ""},
	{LangSlovak, regexp.MustCompile(`(?i)\b(SK|Slovak)\b`), ""},
	{LangBrazilian, regexp.MustCompile(`(?i)\b(Brazilian)\b`), ""},
	{LangPersian, regexp.MustCompile(`(?i)\b(Persian)\b`), ""},
	{LangBengali, regexp.MustCompile(`(?i)\b(Bengali)\b`), ""},
	{LangBulgarian, regexp.MustCompile(`(?i)\b(Bulgarian)\b`), ""},
	{LangSerbian, regexp.MustCompile(`(?i)\b(Serbian)\b`), ""},
	{LangNordic, regexp.MustCompile(`(?i)\b(nordic|NORDICSUBS)\b`), ""},
}

func ParseLanguage(title string) []Language {
	parsedTitle := ParseTitleAndYear(title).Title
	languageTitle := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(title, ".", " "), parsedTitle, ""))

	seen := make(map[Language]bool)
	var languages []Language

	add := func(l Language) {
		if !seen[l] {
			seen[l] = true
			languages = append(languages, l)
		}
	}

	for _, check := range langChecks {
		if check.pattern != nil {
			if check.pattern.MatchString(languageTitle) {
				add(check.lang)
			}
		} else if check.simple != "" {
			if strings.Contains(languageTitle, check.simple) {
				add(check.lang)
			}
		}
	}

	if IsMulti(languageTitle) {
		add(LangEnglish)
	}

	if len(languages) == 0 {
		languages = append(languages, LangEnglish)
	}

	return languages
}

var multiExp = regexp.MustCompile(`(?i)\b(MULTi|DUAL|DL)\b`)

func IsMulti(title string) bool {
	noWebTitle := regexp.MustCompile(`(?i)\bWEB-?DL\b`).ReplaceAllString(title, "")
	return multiExp.MatchString(noWebTitle)
}
