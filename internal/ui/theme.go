package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var (
	ColorPrimary   = lipgloss.Color("#7C6DD8")
	ColorSecondary = lipgloss.Color("#6E95C4")
	ColorSuccess   = lipgloss.Color("#73D08A")
	ColorError     = lipgloss.Color("#E0636F")
	ColorWarn      = lipgloss.Color("#E0C064")
	ColorDim       = lipgloss.Color("#666666")
	ColorWhite     = lipgloss.Color("#FFFFFF")

	ColorMovie = lipgloss.Color("#5B9FE4")
	ColorTV    = lipgloss.Color("#9B7DD4")
	ColorMusic = lipgloss.Color("#5BC48C")
	ColorBook  = lipgloss.Color("#D4A95B")
	ColorComic = lipgloss.Color("#E06B8C")

	StyleBold      = lipgloss.NewStyle().Bold(true)
	StyleDim       = lipgloss.NewStyle().Foreground(ColorDim)
	StylePrimary   = lipgloss.NewStyle().Foreground(ColorPrimary)
	StyleSuccess   = lipgloss.NewStyle().Foreground(ColorSuccess)
	StyleError     = lipgloss.NewStyle().Foreground(ColorError)
	StyleWarn      = lipgloss.NewStyle().Foreground(ColorWarn)
	StyleHeader    = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Underline(true)
	StyleLabel     = lipgloss.NewStyle().Foreground(ColorDim).Width(16)
	StyleValue     = lipgloss.NewStyle()

	badgeStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
)

var mediaTypeColors = map[string]color.Color{
	"movie":   ColorMovie,
	"tv":      ColorTV,
	"music":   ColorMusic,
	"book":    ColorBook,
	"comic":   ColorComic,
	"podcast": ColorSecondary,
	"radio":   ColorSecondary,
}

var mediaTypeLabels = map[string]string{
	"movie":   "MOVIE",
	"tv":      "TV",
	"music":   "MUSIC",
	"book":    "BOOK",
	"comic":   "COMIC",
	"podcast": "PODCAST",
	"radio":   "RADIO",
}

func MediaBadge(mediaType string) string {
	if !ColorEnabled {
		return "[" + mediaTypeLabel(mediaType) + "]"
	}
	c, ok := mediaTypeColors[mediaType]
	if !ok {
		c = ColorDim
	}
	return badgeStyle.Foreground(ColorWhite).Background(c).Render(mediaTypeLabel(mediaType))
}

func StatusBadge(status string) string {
	var c color.Color
	switch status {
	case "matched":
		c = ColorSuccess
	case "pending":
		c = ColorWarn
	case "unmatched":
		c = ColorSecondary
	case "error":
		c = ColorError
	case "ignored":
		c = ColorDim
	default:
		c = ColorDim
	}
	if !ColorEnabled {
		return "[" + status + "]"
	}
	return badgeStyle.Foreground(ColorWhite).Background(c).Render(status)
}

func mediaTypeLabel(mt string) string {
	if label, ok := mediaTypeLabels[mt]; ok {
		return label
	}
	return mt
}
