package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/vfs"
)

var ErrTrackLyricsUnavailable = errors.New("no lyrics for track")

// TrackLyrics returns raw LRC or plain-text lyrics for a local track. Local
// sidecars win; HeyaMetadata is an on-demand fallback addressed only through
// the track's canonical recording binding. This method is shared by Heya's
// native, Jellyfin, and Subsonic surfaces so all three behave consistently.
func (a *App) TrackLyrics(ctx context.Context, trackID int64) ([]byte, error) {
	localBody, localErr := a.localTrackLyrics(ctx, trackID)
	if localErr == nil {
		return localBody, nil
	}

	remoteBody, remoteErr := a.metadataTrackLyrics(ctx, trackID)
	if remoteErr == nil {
		return remoteBody, nil
	}
	if errors.Is(remoteErr, ErrTrackLyricsUnavailable) {
		return nil, ErrTrackLyricsUnavailable
	}
	return nil, remoteErr
}

func (a *App) localTrackLyrics(ctx context.Context, trackID int64) ([]byte, error) {
	files, err := a.ListTrackFiles(ctx, trackID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("list track files: %w", err)
	}
	for _, file := range files {
		if strings.TrimSpace(file.LyricsPath) == "" {
			continue
		}
		body, readErr := readTrackLyricsFile(file.LyricsPath)
		if readErr == nil {
			return body, nil
		}
	}

	return nil, ErrTrackLyricsUnavailable
}

func (a *App) metadataTrackLyrics(ctx context.Context, trackID int64) ([]byte, error) {
	if a.heya == nil {
		return nil, ErrTrackLyricsUnavailable
	}
	binding, err := sqlc.New(a.db).GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{
		LocalKind: "track",
		LocalID:   trackID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTrackLyricsUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("read track metadata binding: %w", err)
	}
	if binding.EntityKind != "recording" || binding.EntityID == uuid.Nil {
		return nil, ErrTrackLyricsUnavailable
	}

	entityID := binding.EntityID.String()
	items, err := a.heya.RecordingLyrics(ctx, entityID)
	if err != nil {
		var apiErr *heyametadata.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return nil, ErrTrackLyricsUnavailable
		}
		return nil, fmt.Errorf("fetch HeyaMetadata lyrics for recording %s: %w", entityID, err)
	}
	if body, ok := preferredRecordingLyrics(items); ok {
		return body, nil
	}
	return nil, ErrTrackLyricsUnavailable
}

// preferredRecordingLyrics favors the newest synchronized observation over
// plain text because Heya's players can still render it as text while also
// following timestamps. HeyaMetadata returns observations newest-first.
func preferredRecordingLyrics(items []heyametadata.RecordingLyrics) ([]byte, bool) {
	plain := ""
	for _, item := range items {
		if strings.TrimSpace(item.SyncedLyrics) != "" {
			return []byte(item.SyncedLyrics), true
		}
		if plain == "" && strings.TrimSpace(item.PlainLyrics) != "" {
			plain = item.PlainLyrics
		}
	}
	if plain != "" {
		return []byte(plain), true
	}
	return nil, false
}

func readTrackLyricsFile(path string) ([]byte, error) {
	if vfs.IsSMBPath(path) {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash < 0 {
			return nil, errors.New("invalid smb lyrics path")
		}
		dir, err := vfs.Open(path[:lastSlash])
		if err != nil {
			return nil, err
		}
		defer func() { _ = dir.Close() }()
		file, err := dir.FS.Open(path[lastSlash+1:])
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()

		var body strings.Builder
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			body.WriteString(scanner.Text())
			body.WriteByte('\n')
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return []byte(body.String()), nil
	}
	return os.ReadFile(filepath.Clean(path)) //nolint:gosec // path comes from Heya's track rows
}
