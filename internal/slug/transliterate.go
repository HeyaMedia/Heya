package slug

import (
	"strings"
	"sync"
	"unicode"

	ipadict "github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"github.com/rs/zerolog/log"
)

// Japanese transliteration so URL slugs and matchers can collapse
// kana/kanji titles to romaji instead of stripping them to "untitled".
// Ported from ../HeyaMedia/internal/enrich/transliterate.go — kept
// in sync deliberately so both services agree on the romanization
// for the same input.
//
// Two layers:
//
//  1. Kana (hiragana + katakana) -> romaji via the lookup tables below.
//     Fully rule-based, instant.
//
//  2. Kanji -> romaji via kagome morphological analyzer with the IPA
//     dictionary. The dictionary knows compound readings, so a token
//     like a two-kanji word resolves as a single katakana token
//     rather than as two character-by-character readings glued
//     together. We then run that katakana through the kana tables.
//
// Kagome's IPA dictionary loads on first use (lazy) — ~10MB compressed
// in the binary, ~50MB resident after load. We keep one tokenizer per
// process via sync.Once. If init ever fails the package falls back to
// kana-only conversion and logs once.

var (
	jpTokenizerOnce sync.Once
	jpTokenizer     *tokenizer.Tokenizer
)

func getJPTokenizer() *tokenizer.Tokenizer {
	jpTokenizerOnce.Do(func() {
		t, err := tokenizer.New(ipadict.Dict(), tokenizer.OmitBosEos())
		if err != nil {
			log.Warn().Err(err).Msg("kagome tokenizer init failed; transliteration limited to kana")
			return
		}
		jpTokenizer = t
	})
	return jpTokenizer
}

// Transliterate converts Japanese text in `s` to Hepburn romaji. Mixed
// content is fine — Latin/digit/punctuation pass through unchanged,
// only kana/kanji runs get romanized. Returns the original string when
// no Japanese characters are present (cheap early-out so non-JP titles
// don't pay the kagome init cost).
func Transliterate(s string) string {
	if !hasJapanese(s) {
		return s
	}
	t := getJPTokenizer()
	if t == nil {
		return kanaToRomajiOnly(s)
	}
	var kata strings.Builder
	kata.Grow(len(s))
	for _, tok := range t.Tokenize(s) {
		surface := tok.Surface
		if surface == "" {
			continue
		}
		// IPA dictionary features index 7 = reading (yomi) in
		// katakana. Tokens not in the dictionary (Latin words,
		// numbers, symbols, unknown kanji) contribute their surface
		// verbatim — the romaji pass leaves non-kana alone.
		feats := tok.Features()
		if len(feats) > 7 && feats[7] != "*" && feats[7] != "" {
			kata.WriteString(feats[7])
		} else {
			kata.WriteString(surface)
		}
	}
	return kanaToRomajiOnly(kata.String())
}

func hasJapanese(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}

// kanaToRomajiOnly converts hiragana + katakana in `s` to romaji
// without invoking kagome — kanji passes through unchanged. Used as
// a fallback when kagome init fails, and for handling surfaces of
// kagome tokens that have no dictionary reading.
func kanaToRomajiOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		// Try a 2-rune digraph first (small ya/yu/yo combos).
		if i+1 < len(runes) {
			if rom, ok := kanaDigraph[[2]rune{runes[i], runes[i+1]}]; ok {
				b.WriteString(rom)
				i++
				continue
			}
		}
		// Sokuon (small tsu): doubles the next consonant.
		if runes[i] == 'っ' || runes[i] == 'ッ' {
			if i+1 < len(runes) {
				next := lookupKana(runes[i+1])
				if len(next) > 0 && next[0] != 0 && isAsciiConsonant(next[0]) {
					b.WriteByte(next[0])
				}
			}
			continue
		}
		// Long mark (chouonpu): stretch the previous vowel.
		if runes[i] == 'ー' {
			if b.Len() > 0 {
				out := b.String()
				last := out[len(out)-1]
				if isVowel(last) {
					b.WriteByte(last)
				}
			}
			continue
		}
		if rom := lookupKana(runes[i]); rom != "" {
			b.WriteString(rom)
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func lookupKana(r rune) string {
	if rom, ok := katakana[r]; ok {
		return rom
	}
	if rom, ok := hiragana[r]; ok {
		return rom
	}
	return ""
}

func isAsciiConsonant(b byte) bool {
	if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') {
		return false
	}
	return !isVowel(b)
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}

// Hepburn romanization. Coverage is the gojuon (basic kana grid) plus
// dakuten/handakuten variants — what shows up in 99%+ of modern
// Japanese music titles. Archaic kana and standalone small vowels
// extend the preceding vowel so output matches what other providers
// surface (e.g. Apple Music writes the long-e form explicitly).

var hiragana = map[rune]string{
	'ぁ': "a", 'ぃ': "i", 'ぅ': "u", 'ぇ': "e", 'ぉ': "o",
	'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
	'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
	'さ': "sa", 'し': "shi", 'す': "su", 'せ': "se", 'そ': "so",
	'た': "ta", 'ち': "chi", 'つ': "tsu", 'て': "te", 'と': "to",
	'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
	'は': "ha", 'ひ': "hi", 'ふ': "fu", 'へ': "he", 'ほ': "ho",
	'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
	'や': "ya", 'ゆ': "yu", 'よ': "yo",
	'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
	'わ': "wa", 'を': "wo", 'ん': "n",
	'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
	'ざ': "za", 'じ': "ji", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
	'だ': "da", 'ぢ': "ji", 'づ': "zu", 'で': "de", 'ど': "do",
	'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
	'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
}

var katakana = map[rune]string{
	'ァ': "a", 'ィ': "i", 'ゥ': "u", 'ェ': "e", 'ォ': "o",
	'ア': "a", 'イ': "i", 'ウ': "u", 'エ': "e", 'オ': "o",
	'カ': "ka", 'キ': "ki", 'ク': "ku", 'ケ': "ke", 'コ': "ko",
	'サ': "sa", 'シ': "shi", 'ス': "su", 'セ': "se", 'ソ': "so",
	'タ': "ta", 'チ': "chi", 'ツ': "tsu", 'テ': "te", 'ト': "to",
	'ナ': "na", 'ニ': "ni", 'ヌ': "nu", 'ネ': "ne", 'ノ': "no",
	'ハ': "ha", 'ヒ': "hi", 'フ': "fu", 'ヘ': "he", 'ホ': "ho",
	'マ': "ma", 'ミ': "mi", 'ム': "mu", 'メ': "me", 'モ': "mo",
	'ヤ': "ya", 'ユ': "yu", 'ヨ': "yo",
	'ラ': "ra", 'リ': "ri", 'ル': "ru", 'レ': "re", 'ロ': "ro",
	'ワ': "wa", 'ヲ': "wo", 'ン': "n",
	'ガ': "ga", 'ギ': "gi", 'グ': "gu", 'ゲ': "ge", 'ゴ': "go",
	'ザ': "za", 'ジ': "ji", 'ズ': "zu", 'ゼ': "ze", 'ゾ': "zo",
	'ダ': "da", 'ヂ': "ji", 'ヅ': "zu", 'デ': "de", 'ド': "do",
	'バ': "ba", 'ビ': "bi", 'ブ': "bu", 'ベ': "be", 'ボ': "bo",
	'パ': "pa", 'ピ': "pi", 'プ': "pu", 'ペ': "pe", 'ポ': "po",
	'ヴ': "vu",
}

// kanaDigraph covers the combining small-ya/yu/yo cases. A base kana
// followed by small ya/yu/yo (either script) collapses to one syllable.
var kanaDigraph = map[[2]rune]string{
	// Hiragana digraphs
	{'き', 'ゃ'}: "kya", {'き', 'ゅ'}: "kyu", {'き', 'ょ'}: "kyo",
	{'し', 'ゃ'}: "sha", {'し', 'ゅ'}: "shu", {'し', 'ょ'}: "sho",
	{'ち', 'ゃ'}: "cha", {'ち', 'ゅ'}: "chu", {'ち', 'ょ'}: "cho",
	{'に', 'ゃ'}: "nya", {'に', 'ゅ'}: "nyu", {'に', 'ょ'}: "nyo",
	{'ひ', 'ゃ'}: "hya", {'ひ', 'ゅ'}: "hyu", {'ひ', 'ょ'}: "hyo",
	{'み', 'ゃ'}: "mya", {'み', 'ゅ'}: "myu", {'み', 'ょ'}: "myo",
	{'り', 'ゃ'}: "rya", {'り', 'ゅ'}: "ryu", {'り', 'ょ'}: "ryo",
	{'ぎ', 'ゃ'}: "gya", {'ぎ', 'ゅ'}: "gyu", {'ぎ', 'ょ'}: "gyo",
	{'じ', 'ゃ'}: "ja", {'じ', 'ゅ'}: "ju", {'じ', 'ょ'}: "jo",
	{'び', 'ゃ'}: "bya", {'び', 'ゅ'}: "byu", {'び', 'ょ'}: "byo",
	{'ぴ', 'ゃ'}: "pya", {'ぴ', 'ゅ'}: "pyu", {'ぴ', 'ょ'}: "pyo",
	// Katakana digraphs
	{'キ', 'ャ'}: "kya", {'キ', 'ュ'}: "kyu", {'キ', 'ョ'}: "kyo",
	{'シ', 'ャ'}: "sha", {'シ', 'ュ'}: "shu", {'シ', 'ョ'}: "sho",
	{'チ', 'ャ'}: "cha", {'チ', 'ュ'}: "chu", {'チ', 'ョ'}: "cho",
	{'ニ', 'ャ'}: "nya", {'ニ', 'ュ'}: "nyu", {'ニ', 'ョ'}: "nyo",
	{'ヒ', 'ャ'}: "hya", {'ヒ', 'ュ'}: "hyu", {'ヒ', 'ョ'}: "hyo",
	{'ミ', 'ャ'}: "mya", {'ミ', 'ュ'}: "myu", {'ミ', 'ョ'}: "myo",
	{'リ', 'ャ'}: "rya", {'リ', 'ュ'}: "ryu", {'リ', 'ョ'}: "ryo",
	{'ギ', 'ャ'}: "gya", {'ギ', 'ュ'}: "gyu", {'ギ', 'ョ'}: "gyo",
	{'ジ', 'ャ'}: "ja", {'ジ', 'ュ'}: "ju", {'ジ', 'ョ'}: "jo",
	{'ビ', 'ャ'}: "bya", {'ビ', 'ュ'}: "byu", {'ビ', 'ョ'}: "byo",
	{'ピ', 'ャ'}: "pya", {'ピ', 'ュ'}: "pyu", {'ピ', 'ョ'}: "pyo",
	// Foreign-sound loanword digraphs (katakana only)
	{'フ', 'ァ'}: "fa", {'フ', 'ィ'}: "fi", {'フ', 'ェ'}: "fe", {'フ', 'ォ'}: "fo",
	{'ウ', 'ィ'}: "wi", {'ウ', 'ェ'}: "we", {'ウ', 'ォ'}: "wo",
	{'ヴ', 'ァ'}: "va", {'ヴ', 'ィ'}: "vi", {'ヴ', 'ェ'}: "ve", {'ヴ', 'ォ'}: "vo",
	{'テ', 'ィ'}: "ti", {'デ', 'ィ'}: "di", {'ト', 'ゥ'}: "tu", {'ド', 'ゥ'}: "du",
}
