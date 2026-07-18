package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/playbackgrant"
	"github.com/karbowiak/heya/internal/transcoder"
)

var (
	ErrNativePlaybackFileNotFound = errors.New("native playback file not found")
	ErrNativePlaybackMode         = errors.New("invalid native playback mode")
)

type NativePlaybackGrantResult struct {
	MediaPath string
	Grant     string
	ExpiresAt time.Time
}

// IssueNativePlaybackGrant creates a grant-only URL for one file. The grant is
// tied to the current auth session; the user's bearer token never leaves Heya.
func (a *App) IssueNativePlaybackGrant(ctx context.Context, userID int64, sessionToken, fileRef, mode string, audioTrack int, quality string) (NativePlaybackGrantResult, error) {
	if a.playbackGrants == nil || userID <= 0 || sessionToken == "" {
		return NativePlaybackGrantResult{}, playbackgrant.ErrInvalidGrant
	}
	file, err := a.GetLibraryFileByRef(ctx, fileRef)
	if err != nil {
		return NativePlaybackGrantResult{}, ErrNativePlaybackFileNotFound
	}
	if mode == "" {
		mode = "direct"
	}
	if mode != "direct" && mode != "hls" {
		return NativePlaybackGrantResult{}, ErrNativePlaybackMode
	}
	if audioTrack < 0 {
		return NativePlaybackGrantResult{}, fmt.Errorf("%w: audio track must be non-negative", ErrNativePlaybackMode)
	}
	if quality != "" && quality != "auto" {
		if _, ok := transcoder.GetProfile(quality); !ok {
			return NativePlaybackGrantResult{}, fmt.Errorf("%w: unknown quality", ErrNativePlaybackMode)
		}
	}

	publicID := file.PublicID.String()
	scopePath := "/api/playback/native/media/" + publicID
	mediaPath := scopePath
	if mode == "hls" {
		mediaPath += "/hls/master.m3u8"
		query := url.Values{}
		if audioTrack > 0 {
			query.Set("audio", fmt.Sprint(audioTrack))
		}
		if quality != "" && quality != "auto" {
			query.Set("quality", quality)
		}
		if encoded := query.Encode(); encoded != "" {
			mediaPath += "?" + encoded
		}
	}

	duration := time.Hour
	var info mediaprobe.MediaInfo
	if len(file.MediaInfo) > 0 && json.Unmarshal(file.MediaInfo, &info) == nil && info.Duration > 0 {
		duration = time.Duration(info.Duration * float64(time.Second))
	}
	ttl := duration + time.Hour
	grant, expiresAt, err := a.playbackGrants.Issue(userID, sessionToken, scopePath, true, ttl)
	if err != nil {
		return NativePlaybackGrantResult{}, err
	}
	return NativePlaybackGrantResult{MediaPath: mediaPath, Grant: grant, ExpiresAt: expiresAt}, nil
}

func (a *App) ValidateNativePlaybackGrant(ctx context.Context, token, expectedPath string) (int64, error) {
	if a.playbackGrants == nil || !strings.HasPrefix(expectedPath, "/api/playback/native/media/") {
		return 0, playbackgrant.ErrInvalidGrant
	}
	return a.playbackGrants.Validate(ctx, a.SessionLookup(), token, expectedPath)
}
