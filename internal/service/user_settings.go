package service

import (
	"context"
	"encoding/json"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// UserSettings holds all per-user settings.
type UserSettings struct {
	Playback PlaybackSettings `json:"playback"`
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
