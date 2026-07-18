package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
)

// GetPlaybackPreference fetches a per-media playback preference for a user.
// Returns the preference and true if found, or a zero value and false if not.
func (a *App) GetPlaybackPreference(ctx context.Context, userID, mediaItemID int64) (sqlc.UserPlaybackPreference, bool, error) {
	q := sqlc.New(a.db)
	pref, err := q.GetPlaybackPreference(ctx, sqlc.GetPlaybackPreferenceParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
	if err != nil {
		return sqlc.UserPlaybackPreference{}, false, nil
	}
	return pref, true, nil
}

// SetPlaybackPreference upserts a per-media playback preference.
func (a *App) SetPlaybackPreference(ctx context.Context, userID, mediaItemID int64, audioLang, subLang, subMode string) (sqlc.UserPlaybackPreference, error) {
	q := sqlc.New(a.db)
	return q.UpsertPlaybackPreference(ctx, sqlc.UpsertPlaybackPreferenceParams{
		UserID:           userID,
		MediaItemID:      mediaItemID,
		AudioLanguage:    audioLang,
		SubtitleLanguage: subLang,
		SubtitleMode:     subMode,
	})
}

// DeletePlaybackPreference removes a per-media playback preference.
func (a *App) DeletePlaybackPreference(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.DeletePlaybackPreference(ctx, sqlc.DeletePlaybackPreferenceParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
}

// LanguageInfo describes a language code and its count across files.
type LanguageInfo struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// MediaLanguages holds the audio and subtitle languages available for a media item.
type MediaLanguages struct {
	AudioLanguages    []LanguageInfo `json:"audio_languages"`
	SubtitleLanguages []LanguageInfo `json:"subtitle_languages"`
}

// GetMediaLanguages scans library files for a media item and aggregates available languages.
func (a *App) GetMediaLanguages(ctx context.Context, mediaItemID int64) (MediaLanguages, error) {
	q := sqlc.New(a.db)
	files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil || len(files) == 0 {
		return MediaLanguages{
			AudioLanguages:    []LanguageInfo{},
			SubtitleLanguages: []LanguageInfo{},
		}, nil
	}

	audioCounts := map[string]int{}
	subCounts := map[string]int{}

	for _, f := range files {
		if len(f.MediaInfo) == 0 {
			continue
		}
		var info mediaprobe.MediaInfo
		if err := json.Unmarshal(f.MediaInfo, &info); err != nil {
			continue
		}
		audioSeen := map[string]bool{}
		subSeen := map[string]bool{}
		for _, s := range info.Streams {
			lang := s.Tags["language"]
			if lang == "" || lang == "und" {
				continue
			}
			switch s.CodecType {
			case "audio":
				if !audioSeen[lang] {
					audioCounts[lang]++
					audioSeen[lang] = true
				}
			case "subtitle":
				if !subSeen[lang] {
					subCounts[lang]++
					subSeen[lang] = true
				}
			}
		}
	}

	return MediaLanguages{
		AudioLanguages:    sortedLanguageInfos(audioCounts),
		SubtitleLanguages: sortedLanguageInfos(subCounts),
	}, nil
}

// sortedLanguageInfos converts counts to a sorted slice, highest count first.
func sortedLanguageInfos(counts map[string]int) []LanguageInfo {
	result := make([]LanguageInfo, 0, len(counts))
	for code, count := range counts {
		result = append(result, LanguageInfo{Code: code, Count: count})
	}
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Count > result[i].Count {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}
