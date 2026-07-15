package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/playbackgrant"
	"github.com/karbowiak/heya/internal/service"
)

type nativePlaybackGrantBody struct {
	MediaPath           string `json:"media_path"`
	PlaybackGrant       string `json:"playback_grant"`
	ExpiresAtUnixMillis int64  `json:"expires_at_unix_millis"`
	HeaderName          string `json:"header_name"`
}

func registerNativePlaybackRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodPost, "/api/playback/native/grants", "create-native-playback-grant", "Create a session-bound native playback grant", "Streaming")),
		func(ctx context.Context, in *struct {
			Body struct {
				FileID     string `json:"file_id" minLength:"1" maxLength:"64"`
				Mode       string `json:"mode,omitempty" maxLength:"16" doc:"direct or hls; defaults to direct"`
				AudioTrack int    `json:"audio_track,omitempty" minimum:"0"`
				Quality    string `json:"quality,omitempty" maxLength:"32"`
			}
		}) (*JSONOutput[nativePlaybackGrantBody], error) {
			result, err := app.IssueNativePlaybackGrant(
				ctx,
				userFrom(ctx).ID,
				auth.TokenFromContext(ctx),
				in.Body.FileID,
				in.Body.Mode,
				in.Body.AudioTrack,
				in.Body.Quality,
			)
			if err != nil {
				switch {
				case errors.Is(err, service.ErrNativePlaybackFileNotFound):
					return nil, huma.Error404NotFound("file not found")
				case errors.Is(err, service.ErrNativePlaybackMode):
					return nil, huma.Error422UnprocessableEntity("invalid native playback request")
				default:
					return nil, huma.Error500InternalServerError("failed to create native playback grant")
				}
			}
			return noStoreJSON(nativePlaybackGrantBody{
				MediaPath:           result.MediaPath,
				PlaybackGrant:       result.Grant,
				ExpiresAtUnixMillis: result.ExpiresAt.UnixMilli(),
				HeaderName:          playbackgrant.HeaderName,
			}), nil
		})
}
