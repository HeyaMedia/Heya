package service

import (
	"context"
	"encoding/json"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// UserSettings holds all per-user settings.
type UserSettings struct {
	Playback   PlaybackSettings   `json:"playback"`
	UI         UISettings         `json:"ui"`
	Appearance AppearanceSettings `json:"appearance"`
	Home       HomeSettings       `json:"home"`
}

// UISettings holds small frontend preferences that should follow the user
// across devices (unlike localStorage). Kept flat and optional — absent
// fields mean "app default".
type UISettings struct {
	// PinnedHeroMode is the home-hero mode to show on page load
	// (featured / tonight / new / music / roulette). Empty = featured.
	PinnedHeroMode string `json:"pinned_hero_mode,omitempty"`
}

// AppearanceSettings holds theme/visual preferences that follow the user
// across devices. Empty string / zero values mean "app default" so newly
// added fields never need migrations.
type AppearanceSettings struct {
	// Theme is one of "system", "dark", "light", "oled". Empty = dark.
	Theme string `json:"theme,omitempty"`
	// Accent is a named accent preset (gold, ember, crimson, rose, iris,
	// ocean, teal, moss, silver). Empty = gold.
	Accent string `json:"accent,omitempty"`
	// Density is "comfortable" or "compact". Empty = comfortable.
	Density string `json:"density,omitempty"`
	// AmbientMode toggles the rotating library-backdrop background:
	// "on", "off", or empty for the app default (on).
	AmbientMode string `json:"ambient_mode,omitempty"`
	// AmbientIntensity is the backdrop visibility percentage (5-60).
	// 0 = app default.
	AmbientIntensity int `json:"ambient_intensity,omitempty"`
	// ShowUnavailableRecs includes non-library titles in detail-page
	// "More Like This" rails (they link out to heya.media). Default off.
	// Deliberately NOT omitempty: with it, an explicit "off" marshals to an
	// absent key, and clients that keep local state when a key is missing
	// (the FE appearance hydrate) could never learn about the off cross-
	// device. Always speaking true/false makes the server authoritative.
	ShowUnavailableRecs bool `json:"show_unavailable_recs"`
	// AccentCustom is a user-picked hex accent overriding the preset.
	// Empty = the named preset in Accent applies.
	AccentCustom string `json:"accent_custom,omitempty"`
	// AccentCustomDerived caches the family derived from AccentCustom so
	// clients replay it verbatim pre-paint instead of re-deriving.
	AccentCustomDerived *AccentDerived `json:"accent_custom_derived,omitempty"`
	// TypeSet is a curated font pairing ("heya", "editorial", "grotesk",
	// "rounded", "system"). Empty = heya.
	TypeSet string `json:"typeset,omitempty"`
	// FontScale is "sm", "md", or "lg". Empty = md.
	FontScale string `json:"font_scale,omitempty"`
	// ToneFollow lets pages tint toward their artwork. A pointer so legacy
	// records (key absent) keep the default ON — a bare bool would marshal
	// absent as false and silently disable it cross-device.
	ToneFollow *bool `json:"tone_follow,omitempty"`
	// Lighting is "dramatic" or "flat". Empty = dramatic.
	Lighting string `json:"lighting,omitempty"`
	// Glass is "rich" or "minimal". Empty = rich.
	Glass string `json:"glass,omitempty"`
	// Radius is "soft" or "sharp". Empty = soft.
	Radius string `json:"radius,omitempty"`
	// Hero is "standard" or "short". Empty = standard.
	Hero string `json:"hero,omitempty"`
	// Motion is "system", "reduced", or "full". Empty = system.
	Motion string `json:"motion,omitempty"`
}

// AccentDerived is the client-computed accent family for a custom accent —
// stored verbatim so the boot script never re-derives colors pre-paint.
type AccentDerived struct {
	Accent string `json:"accent,omitempty"`
	RGB    string `json:"rgb,omitempty"`
	Bright string `json:"bright,omitempty"`
	Deep   string `json:"deep,omitempty"`
	Ink    string `json:"ink,omitempty"`
}

// HomeSettings controls the composition of the home page.
type HomeSettings struct {
	// Sections is the ordered list of home sections. Absent = default
	// order with everything visible. Unknown IDs are ignored by the FE;
	// sections missing from the list render after the listed ones.
	Sections []HomeSectionPref `json:"sections,omitempty"`
}

// HomeSectionPref is one home section's visibility + position (by index).
type HomeSectionPref struct {
	ID     string `json:"id"`
	Hidden bool   `json:"hidden,omitempty"`
}

// PlaybackSettings holds playback-related user preferences.
type PlaybackSettings struct {
	DefaultAudioLanguage    string                       `json:"default_audio_language"`
	DefaultSubtitleLanguage string                       `json:"default_subtitle_language"`
	SubtitleMode            string                       `json:"subtitle_mode"`
	SubtitlePriority        []string                     `json:"subtitle_priority"`
	DefaultQuality          string                       `json:"default_quality"`
	LibraryOverrides        map[string]LibraryPlaybackOv `json:"library_overrides"`
}

// LibraryPlaybackOv holds per-library overrides for playback settings.
type LibraryPlaybackOv struct {
	DefaultAudioLanguage    string   `json:"default_audio_language,omitempty"`
	DefaultSubtitleLanguage string   `json:"default_subtitle_language,omitempty"`
	SubtitleMode            string   `json:"subtitle_mode,omitempty"`
	SubtitlePriority        []string `json:"subtitle_priority,omitempty"`
}

// DefaultUserSettings returns the default settings for a new user.
func DefaultUserSettings() UserSettings {
	return UserSettings{
		Playback: PlaybackSettings{
			DefaultAudioLanguage:    "",
			DefaultSubtitleLanguage: "",
			SubtitleMode:            "auto",
			SubtitlePriority:        []string{"ass", "srt", "subrip", "webvtt", "pgs"},
			DefaultQuality:          "auto",
			LibraryOverrides:        map[string]LibraryPlaybackOv{},
		},
	}
}

// GetUserSettings loads and returns the settings for a user, falling back to defaults.
func (a *App) GetUserSettings(ctx context.Context, userID int64) (UserSettings, error) {
	q := sqlc.New(a.db)
	raw, err := q.GetUserSettings(ctx, userID)
	if err != nil {
		return UserSettings{}, err
	}

	settings := DefaultUserSettings()
	if len(raw) > 2 {
		json.Unmarshal(raw, &settings)
	}
	return settings, nil
}

// UpdateUserSettings persists the given settings for a user.
func (a *App) UpdateUserSettings(ctx context.Context, userID int64, settings UserSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	q := sqlc.New(a.db)
	return q.UpdateUserSettings(ctx, sqlc.UpdateUserSettingsParams{
		Settings: raw,
		ID:       userID,
	})
}
