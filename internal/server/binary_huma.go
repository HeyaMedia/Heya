package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/karbowiak/heya/internal/service"
)

// registerBinaryRoutes mounts the file-serving + streaming endpoints under
// Huma so they show up in the OpenAPI spec, even though their bodies are raw
// bytes (images, HLS playlists/segments, subtitle files, trickplay sprites,
// music streams). Each operation declares the response as binary so clients
// know not to try and JSON-parse it.
//
// The actual byte serving still goes through the existing stdlib handlers —
// we wrap them in a huma.StreamResponse and unwrap the underlying http.Request
// + http.ResponseWriter via humago.Unwrap. This preserves Range support,
// content-type sniffing, and any custom header logic.
func registerBinaryRoutes(api huma.API, app *service.App) {
	// --- Images ---
	// Browser image elements authenticate with Heya's same-origin HttpOnly
	// cookie. Keeping the catalog artwork behind the same user boundary avoids
	// leaking library contents from a publicly reachable instance.
	// In passive mode with HEYA_IMAGE_PROXY_URL set, imgProxy is non-nil and
	// these endpoints reverse-proxy the identical path to the source instance
	// (whose disk actually holds the files); otherwise it's nil and the local
	// byte handlers serve as normal. The TMDB proxy is excluded — it already
	// fetches from image.tmdb.org directly and needs no local files.
	imgProxy := newPassiveImageProxy(app.ConfigSnapshot())

	huma.Register(api, securedBinary(http.MethodGet, "/api/media/{id}/image/{type}", "media-image", "Media poster/backdrop bytes", "Images"),
		wrapStreamAs[mediaImageInput](proxiedImage(imgProxy, handleMediaImage(app))))

	huma.Register(api, securedBinary(http.MethodGet, "/api/person/{id}/image", "person-image", "Person photo bytes", "Images"),
		wrapStreamAs[idBinaryInput](proxiedImage(imgProxy, handlePersonImage(app))))

	huma.Register(api, securedBinary(http.MethodGet, "/api/metadata/images/{id}", "metadata-image", "Wait for canonical HeyaMetadata image bytes", "Images"),
		wrapStreamAs[metadataImageInput](handleMetadataImage(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/studio/{id}/image", "studio-image", "Studio logo bytes", "Images"),
		wrapStreamAs[idBinaryInput](proxiedImage(imgProxy, handleStudioImage(app))))

	huma.Register(api, securedBinary(http.MethodGet, "/api/extras/{id}/thumbnail", "extra-thumbnail", "Extras thumbnail bytes", "Images"),
		wrapStreamAs[extraBinaryInput](proxiedImage(imgProxy, handleExtraThumbnail(app))))

	huma.Register(api, securedBinary(http.MethodGet, "/api/tmdb/image/{path}", "tmdb-image-proxy", "Proxied TMDB image bytes", "Images"),
		wrapStreamAs[tmdbImageInput](handleTMDBImageProxy(nil)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/music/artists/{artist_slug}/albums/{album_slug}/cover", "album-cover", "Album cover bytes (local file or 302 to upstream URL)", "Images"),
		wrapStreamAs[albumCoverInput](proxiedImage(imgProxy, handleAlbumCover(app))))

	// Playlist covers are custom uploads (not passively-mirrored provider
	// artwork), so they're excluded from the passive-mode image proxy — the
	// file only exists on whichever instance the user uploaded it to.
	huma.Register(api, securedBinary(http.MethodGet, "/api/me/playlists/{id}/cover", "playlist-cover", "Custom playlist cover bytes", "Images"),
		wrapStreamAs[idBinaryInput](handlePlaylistCover(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/ai/images/{filename}", "ai-generated-image", "Generated AI image bytes", "AI"),
		wrapStreamAs[generatedImageInput](func(w http.ResponseWriter, r *http.Request) {
			path, ok := app.ImageOutputPath(r.PathValue("filename"))
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "private, max-age=86400")
			http.ServeFile(w, r, path)
		}))

	// --- Video streaming (HLS + direct play) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}", "stream-direct", "Direct video stream (range-served bytes)", "Streaming"),
		wrapStreamAs[streamFileInput](handleDirectStream(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/extras/{id}/stream", "extra-stream", "Media extra video stream (trailer/featurette, range-served bytes)", "Streaming"),
		wrapStreamAs[extraBinaryInput](handleExtraStream(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/master.m3u8", "stream-hls-master", "HLS master playlist", "Streaming"),
		wrapStreamAs[streamFileInput](handleHLSMaster(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/index.m3u8", "stream-hls-index", "HLS variant playlist", "Streaming"),
		wrapStreamAs[streamPlaylistInput](handleHLSPlaylist(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/{segment}", "stream-hls-segment", "HLS segment / init.mp4", "Streaming"),
		wrapStreamAs[streamSegmentInput](handleHLSSegment(app)))

	// --- Subtitles (text body) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/subtitles/{index}", "stream-subtitle-body", "Extracted subtitle file (VTT or ASS)", "Streaming"),
		wrapStreamAs[streamSubtitleInput](handleGetSubtitle(app)))

	// --- Trickplay scrubbing previews ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/trickplay/index.vtt", "trickplay-vtt", "Trickplay WebVTT index", "Streaming"),
		wrapStreamAs[streamFileInput](handleTrickplayVTT(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/trickplay/{filename}", "trickplay-sprite", "Trickplay sprite JPEG", "Streaming"),
		wrapStreamAs[trickplaySpriteInput](handleTrickplaySprite(app)))

	// --- Music streaming (range-served audio bytes) ---
	// Receiver pull endpoint: intentionally no normal bearer auth. The signed
	// cast_token is bound to this exact path + user and the handler rechecks the
	// live casting allowance before delegating to the normal range server.
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/music/{id}", "cast-stream-track", "Scoped audio stream for a cast receiver", "Cast"),
		wrapStreamAs[castMusicStreamInput](handleCastMusicStream(app)))

	// Chromecast video pulls either one direct MP4 or an HLS dependency tree.
	// These routes deliberately use cast_token instead of bearer auth: the
	// receiver has no Heya account, and its signed token is scoped to one file.
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/video/{file_id}", "cast-stream-video", "Scoped direct video stream for a cast receiver", "Cast"),
		wrapStreamAs[castVideoStreamInput](handleCastVideoDirect(app)))
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/video/{file_id}/hls/master.m3u8", "cast-stream-video-hls-master", "Scoped video HLS master playlist for a cast receiver", "Cast"),
		wrapStreamAs[castVideoStreamInput](handleCastVideoHLS(app, handleHLSMaster(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/video/{file_id}/hls/index.m3u8", "cast-stream-video-hls-index", "Scoped video HLS variant playlist for a cast receiver", "Cast"),
		wrapStreamAs[castVideoStreamInput](handleCastVideoHLS(app, handleHLSPlaylist(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/video/{file_id}/hls/{segment}", "cast-stream-video-hls-segment", "Scoped video HLS segment for a cast receiver", "Cast"),
		wrapStreamAs[castVideoSegmentInput](handleCastVideoHLS(app, handleHLSSegment(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/cast/media/video/{file_id}/subtitles/{index}", "cast-stream-video-subtitle", "Scoped WebVTT subtitle for a cast receiver", "Cast"),
		wrapStreamAs[castVideoSubtitleInput](handleCastVideoSubtitle(app)))

	// Native desktop playback uses one fixed header containing an opaque grant
	// tied to the current Heya auth session and this one file subtree. These
	// routes never accept the user's normal bearer token.
	huma.Register(api, binaryOp(http.MethodGet, "/api/playback/native/media/{file_id}", "native-playback-stream-video", "Scoped direct video stream for a native renderer", "Streaming"),
		wrapStreamAs[nativePlaybackStreamInput](handleNativePlaybackStream(app, handleDirectStream(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/playback/native/media/{file_id}/hls/master.m3u8", "native-playback-stream-hls-master", "Scoped HLS master playlist for a native renderer", "Streaming"),
		wrapStreamAs[nativePlaybackStreamInput](handleNativePlaybackStream(app, handleHLSMaster(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/playback/native/media/{file_id}/hls/index.m3u8", "native-playback-stream-hls-index", "Scoped HLS variant playlist for a native renderer", "Streaming"),
		wrapStreamAs[nativePlaybackStreamInput](handleNativePlaybackStream(app, handleHLSPlaylist(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/playback/native/media/{file_id}/hls/{segment}", "native-playback-stream-hls-segment", "Scoped HLS segment for a native renderer", "Streaming"),
		wrapStreamAs[nativePlaybackSegmentInput](handleNativePlaybackStream(app, handleHLSSegment(app))))
	huma.Register(api, binaryOp(http.MethodGet, "/api/playback/native/media/{file_id}/subtitles/{index}", "native-playback-stream-subtitle", "Scoped subtitle stream for a native renderer", "Streaming"),
		wrapStreamAs[nativePlaybackSubtitleInput](handleNativePlaybackStream(app, handleGetSubtitleAs(app, true))))

	huma.Register(api, securedBinary(http.MethodGet, "/api/music/tracks/{id}/stream", "stream-track", "Best-quality playable audio for a track", "Music"),
		wrapStreamAs[musicTrackStreamInput](handleStreamTrack(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/music/tracks/{id}/file/{track_file_id}", "stream-track-file", "Specific original track file", "Music"),
		wrapStreamAs[musicTrackFileInput](handleStreamTrackFile(app)))

	// --- Internet-radio stream proxy (long-lived, ICY metadata stripped) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/radio/stream", "stream-radio", "Proxy an internet-radio stream URL", "Radio"),
		func(ctx context.Context, _ *proxyStreamInput) (*huma.StreamResponse, error) {
			// Capture the authenticated owner before Huma hands the response to
			// the stdlib streaming handler. ICY metadata is per-playback state and
			// must only reach this user's WebSocket connections.
			return streamResponse(handleRadioStream(app.EventHub(), userFrom(ctx).ID, publicMediaHTTPClient)), nil
		})

	// --- Podcast episode stream proxy (range-served audio from RSS enclosure) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/podcasts/episode/stream", "stream-podcast-episode", "Proxy a podcast episode audio URL", "Podcasts"),
		wrapStreamAs[proxyStreamInput](handlePodcastStream(publicMediaHTTPClient)))

	// Multipart upload lives in metadata_editor_huma.go because it uses
	// huma.MultipartFormFiles instead of wrapStream — proper typed binding
	// for the form file plus auto-generated OpenAPI schema.
}

// binaryOp builds an Operation that documents a binary response body. The
// "200" response is replaced with a single application/octet-stream entry so
// the OpenAPI spec doesn't claim we return JSON.
func binaryOp(method, path, opID, summary, tag string) huma.Operation {
	o := op(method, path, opID, summary, tag)
	o.Responses = map[string]*huma.Response{
		"200": {
			Description: "Binary response — content type set per endpoint",
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {},
			},
		},
	}
	return o
}

// securedBinary is binaryOp + bearer auth, used for everything that's not a
// public image endpoint.
func securedBinary(method, path, opID, summary, tag string) huma.Operation {
	return secured(binaryOp(method, path, opID, summary, tag))
}

// wrapStream returns a Huma handler that delegates the entire response to an
// existing stdlib HTTP handler. The Huma input struct is ignored — path
// validation happens at the mux level, and the legacy handler re-parses what
// it needs via r.PathValue.
func wrapStream(h http.HandlerFunc) func(context.Context, *struct{}) (*huma.StreamResponse, error) {
	return func(_ context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		return streamResponse(h), nil
	}
}

func wrapStreamAs[I any](h http.HandlerFunc) func(context.Context, *I) (*huma.StreamResponse, error) {
	return func(_ context.Context, _ *I) (*huma.StreamResponse, error) {
		return streamResponse(h), nil
	}
}

func streamResponse(h http.HandlerFunc) *huma.StreamResponse {
	return &huma.StreamResponse{
		Body: func(hctx huma.Context) {
			r, w := humago.Unwrap(hctx)
			h(w, r)
		},
	}
}

type idBinaryInput struct {
	ID int64 `path:"id" minimum:"1"`
}

type generatedImageInput struct {
	Filename string `path:"filename" pattern:"^[A-Za-z0-9._-]+\\.png$" maxLength:"128"`
}

type metadataImageInput struct {
	ID string `path:"id" format:"uuid"`
}

type extraBinaryInput struct {
	ID int64 `path:"id" minimum:"-9223372036854775808"`
}

type mediaImageInput struct {
	ID   string `path:"id" maxLength:"64"`
	Type string `path:"type" maxLength:"32"`
}

type tmdbImageInput struct {
	Path string `path:"path" maxLength:"512"`
}

type albumCoverInput struct {
	ArtistSlug string `path:"artist_slug" pattern:"^[a-z0-9-]+$" maxLength:"200"`
	AlbumSlug  string `path:"album_slug" pattern:"^[a-z0-9-]+$" maxLength:"200"`
}

type streamFileInput struct {
	FileID string `path:"file_id" maxLength:"64"`
}

type streamPlaylistInput struct {
	FileID string `path:"file_id" maxLength:"64"`
	Audio  string `query:"audio" required:"false" maxLength:"16"`
}

type streamSegmentInput struct {
	FileID  string `path:"file_id" maxLength:"64"`
	Segment string `path:"segment" maxLength:"128"`
}

type streamSubtitleInput struct {
	FileID string `path:"file_id" maxLength:"64"`
	Index  int    `path:"index" minimum:"0"`
}

type trickplaySpriteInput struct {
	FileID string `path:"file_id" maxLength:"64"`
	// pattern is OpenAPI documentation only — wrapStreamAs ignores this input
	// struct, so the actual traversal guard is the filepath.Base check in
	// handleTrickplaySprite. Kept in sync for spec accuracy.
	Filename string `path:"filename" maxLength:"128" pattern:"^[A-Za-z0-9._-]+$"`
}

type musicTrackStreamInput struct {
	ID                 int64 `path:"id" minimum:"1"`
	SupportsFLACNative bool  `query:"supports_flac_native" required:"false"`
	SupportsFLAC       bool  `query:"supports_flac" required:"false"`
	SupportsALAC       bool  `query:"supports_alac" required:"false"`
	SupportsMP3        bool  `query:"supports_mp3" required:"false"`
	SupportsAACAudio   bool  `query:"supports_aac_audio" required:"false"`
	SupportsOggVorbis  bool  `query:"supports_ogg_vorbis" required:"false"`
	SupportsOpusAudio  bool  `query:"supports_opus_audio" required:"false"`
	SupportsOpus       bool  `query:"supports_opus" required:"false"`
	SupportsWavPCM     bool  `query:"supports_wav_pcm" required:"false"`
	// Quality requests an explicit AAC transcode tier instead of the default
	// caps-based direct-or-256k-fallback decision. Deliberately NOT an
	// `enum:`-constrained field: Huma hard-rejects unrecognized enum values
	// with 422 before the handler ever runs (see validation_huma_test.go),
	// but the API contract requires unrecognized/absent values to silently
	// fall through to today's behavior instead of erroring. The allowed set
	// is documented in prose and enforced in Go by audioQualityTiers in
	// music_stream_handlers.go.
	Quality string `query:"quality" required:"false" maxLength:"16" doc:"AAC transcode tier — one of aac-320, aac-256, aac-192, aac-128. Omit for the default caps-based direct-or-256k-fallback behavior. Unrecognized values are ignored, not rejected."`
}

type castMusicStreamInput struct {
	ID        int64  `path:"id" minimum:"1"`
	CastToken string `query:"cast_token" required:"false" maxLength:"2048"`
}

type castVideoStreamInput struct {
	FileID    string `path:"file_id" maxLength:"64"`
	CastToken string `query:"cast_token" required:"false" maxLength:"2048"`
}

type castVideoSegmentInput struct {
	FileID    string `path:"file_id" maxLength:"64"`
	Segment   string `path:"segment" maxLength:"128"`
	CastToken string `query:"cast_token" required:"false" maxLength:"2048"`
}

type castVideoSubtitleInput struct {
	FileID    string `path:"file_id" maxLength:"64"`
	Index     int    `path:"index" minimum:"0"`
	CastToken string `query:"cast_token" required:"false" maxLength:"2048"`
}

type nativePlaybackStreamInput struct {
	FileID        string `path:"file_id" maxLength:"64"`
	PlaybackGrant string `header:"X-Heya-Playback-Grant" required:"false" maxLength:"64"`
}

type nativePlaybackSegmentInput struct {
	FileID        string `path:"file_id" maxLength:"64"`
	Segment       string `path:"segment" maxLength:"128"`
	PlaybackGrant string `header:"X-Heya-Playback-Grant" required:"false" maxLength:"64"`
}

type nativePlaybackSubtitleInput struct {
	FileID        string `path:"file_id" maxLength:"64"`
	Index         int    `path:"index" minimum:"0"`
	PlaybackGrant string `header:"X-Heya-Playback-Grant" required:"false" maxLength:"64"`
}

type musicTrackFileInput struct {
	ID          int64 `path:"id" minimum:"1"`
	TrackFileID int64 `path:"track_file_id" minimum:"1"`
}

type proxyStreamInput struct {
	URL string `query:"url" minLength:"1" maxLength:"2000"`
}
