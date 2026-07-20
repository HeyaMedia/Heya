package jellyfin

import "net/http"

// Localization: static reference data. Upstream derives these from .NET
// culture tables; clients use them for audio/subtitle language preference
// pickers (Cultures) and the OfficialRating filter (ParentalRatings). The
// kotlin SDK requires Name/DisplayName/TwoLetter/ThreeLetterNames non-null
// on every culture entry.

type cultureDto struct {
	Name                        string   `json:"Name"`
	DisplayName                 string   `json:"DisplayName"`
	TwoLetterISOLanguageName    string   `json:"TwoLetterISOLanguageName"`
	ThreeLetterISOLanguageName  string   `json:"ThreeLetterISOLanguageName"`
	ThreeLetterISOLanguageNames []string `json:"ThreeLetterISOLanguageNames"`
}

// culture builds one entry; extra three-letter aliases (ISO 639-2/B vs /T,
// e.g. ger/deu) follow upstream, which lists both.
func culture(display, two, three string, aliases ...string) cultureDto {
	return cultureDto{
		Name:                        display,
		DisplayName:                 display,
		TwoLetterISOLanguageName:    two,
		ThreeLetterISOLanguageName:  three,
		ThreeLetterISOLanguageNames: append([]string{three}, aliases...),
	}
}

// cultures covers every language with meaningful media presence — the same
// practical subset language pickers in other self-hosted servers ship.
var cultures = []cultureDto{
	culture("Arabic", "ar", "ara"),
	culture("Bulgarian", "bg", "bul"),
	culture("Catalan", "ca", "cat"),
	culture("Chinese", "zh", "zho", "chi"),
	culture("Croatian", "hr", "hrv"),
	culture("Czech", "cs", "ces", "cze"),
	culture("Danish", "da", "dan"),
	culture("Dutch", "nl", "nld", "dut"),
	culture("English", "en", "eng"),
	culture("Estonian", "et", "est"),
	culture("Filipino", "fil", "fil"),
	culture("Finnish", "fi", "fin"),
	culture("French", "fr", "fra", "fre"),
	culture("German", "de", "deu", "ger"),
	culture("Greek", "el", "ell", "gre"),
	culture("Hebrew", "he", "heb"),
	culture("Hindi", "hi", "hin"),
	culture("Hungarian", "hu", "hun"),
	culture("Icelandic", "is", "isl", "ice"),
	culture("Indonesian", "id", "ind"),
	culture("Italian", "it", "ita"),
	culture("Japanese", "ja", "jpn"),
	culture("Korean", "ko", "kor"),
	culture("Latvian", "lv", "lav"),
	culture("Lithuanian", "lt", "lit"),
	culture("Malay", "ms", "msa", "may"),
	culture("Norwegian", "no", "nor"),
	culture("Norwegian Bokmål", "nb", "nob"),
	culture("Persian", "fa", "fas", "per"),
	culture("Polish", "pl", "pol"),
	culture("Portuguese", "pt", "por"),
	culture("Romanian", "ro", "ron", "rum"),
	culture("Russian", "ru", "rus"),
	culture("Serbian", "sr", "srp"),
	culture("Slovak", "sk", "slk", "slo"),
	culture("Slovenian", "sl", "slv"),
	culture("Spanish", "es", "spa"),
	culture("Swedish", "sv", "swe"),
	culture("Tamil", "ta", "tam"),
	culture("Thai", "th", "tha"),
	culture("Turkish", "tr", "tur"),
	culture("Ukrainian", "uk", "ukr"),
	culture("Vietnamese", "vi", "vie"),
}

// GET /Localization/Cultures
func (s *Server) handleCultures(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, cultures)
}

type parentalRating struct {
	Name  string `json:"Name"`
	Value *int   `json:"Value,omitempty"`
}

func rating(name string, value int) parentalRating { return parentalRating{name, &value} }

// parentalRatings mirrors upstream's en-US table — the values drive
// "max parental rating" comparisons and the OfficialRating filter.
var parentalRatings = []parentalRating{
	rating("Approved", 1),
	rating("G", 1),
	rating("TV-G", 1),
	rating("TV-Y", 1),
	rating("TV-Y7", 3),
	rating("TV-Y7-FV", 4),
	rating("PG", 5),
	rating("TV-PG", 5),
	rating("PG-13", 7),
	rating("TV-14", 8),
	rating("R", 9),
	rating("TV-MA", 9),
	rating("NC-17", 10),
	rating("XXX", 100),
	rating("Unrated", 100000),
}

// GET /Localization/ParentalRatings
func (s *Server) handleParentalRatings(w http.ResponseWriter, _ *http.Request, _ Params) {
	writeJSON(w, http.StatusOK, parentalRatings)
}
