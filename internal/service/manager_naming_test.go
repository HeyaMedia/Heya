package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderManagerFilenameConditionalTokens(t *testing.T) {
	template := `{Series TitleYear} - S{season:00}E{episode:00} [{Custom Formats }{Quality Full}]{-Release Group}`
	got := renderManagerFilename(template, map[string]string{
		"Series TitleYear": "Lioness (2023)", "season:00": "02", "episode:00": "08",
		"Quality Full": "WEBDL-1080p", "Release Group": "NTb",
	})
	require.Equal(t, "Lioness (2023) - S02E08 [WEBDL-1080p]-NTb", got)
}

func TestRenderManagerFilenameDropsEmptyWrappers(t *testing.T) {
	got := renderManagerFilename(`Movie {[Edition Tags]} {[Unknown]} [{Release Group}]`, map[string]string{
		"Edition Tags": "", "Release Group": "",
	})
	require.Equal(t, "Movie", got)
}

func TestRenderManagerFilenameNestedConditionalPrefix(t *testing.T) {
	got := renderManagerFilename(`{Movie CleanTitle} {imdb-{ImdbId}} {edition-{Edition Tags}}`, map[string]string{
		"Movie CleanTitle": "Dune Part Two", "ImdbId": "tt15239678", "Edition Tags": "IMAX",
	})
	require.Equal(t, "Dune Part Two imdb-tt15239678 edition-IMAX", got)
}

func TestRenderManagerFilenameArrFormatting(t *testing.T) {
	got := renderManagerFilename(`S{Season:00} {-Release Group:4}`, map[string]string{
		"Season": "2", "Release Group": "LONGGROUP",
	})
	require.Equal(t, "S02 -LONG", got)
}

func TestSanitizeImportRelativePathKeepsHierarchy(t *testing.T) {
	require.Equal(t, "Sabrina Carpenter/Album - 2022/0102 - Vicious", sanitizeImportRelativePath("Sabrina Carpenter/Album: 2022/0102 - Vicious"))
}
