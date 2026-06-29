package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeSubtitleFilename(t *testing.T) {
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("movie.en.srt"))
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("../movie.en.srt"))
	assert.Equal(t, "movie.en.srt", safeSubtitleFilename("dir\\movie.en.srt"))
	assert.Empty(t, safeSubtitleFilename("movie.exe"))
	assert.Empty(t, safeSubtitleFilename(".."))
}

func TestSafeSubtitleLanguage(t *testing.T) {
	assert.Equal(t, "en", safeSubtitleLanguage("en"))
	assert.Equal(t, "pt-BR", safeSubtitleLanguage("pt-BR"))
	assert.Equal(t, "zh_Hant", safeSubtitleLanguage("zh_Hant"))
	assert.Equal(t, "und", safeSubtitleLanguage("../evil"))
	assert.Equal(t, "und", safeSubtitleLanguage(""))
}

func TestSafeSubtitleDownloadURL(t *testing.T) {
	u, err := safeSubtitleDownloadURL("https://dl.opensubtitles.com/en/subtitle.srt?download=1")
	assert.NoError(t, err)
	assert.Equal(t, "https://dl.opensubtitles.com/en/subtitle.srt?download=1", u)

	for _, raw := range []string{"", "/relative/subtitle.srt", "http://example.com/subtitle.srt", "file:///etc/passwd"} {
		t.Run(raw, func(t *testing.T) {
			_, err := safeSubtitleDownloadURL(raw)
			assert.Error(t, err)
		})
	}
}
