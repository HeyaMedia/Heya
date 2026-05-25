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
	// --- Images (no auth — browsers can't attach Authorization to <img>) ---
	huma.Register(api, binaryOp(http.MethodGet, "/api/media/{id}/image/{type}", "media-image", "Media poster/backdrop bytes", "Images"),
		wrapStream(handleMediaImage(app)))

	huma.Register(api, binaryOp(http.MethodGet, "/api/person/{id}/image", "person-image", "Person photo bytes", "Images"),
		wrapStream(handlePersonImage(app)))

	huma.Register(api, binaryOp(http.MethodGet, "/api/studio/{id}/image", "studio-image", "Studio logo bytes", "Images"),
		wrapStream(handleStudioImage(app)))

	huma.Register(api, binaryOp(http.MethodGet, "/api/extras/{id}/thumbnail", "extra-thumbnail", "Extras thumbnail bytes", "Images"),
		wrapStream(handleExtraThumbnail(app)))

	huma.Register(api, binaryOp(http.MethodGet, "/api/tmdb/image/{path}", "tmdb-image-proxy", "Proxied TMDB image bytes", "Images"),
		wrapStream(handleTMDBImageProxy()))

	huma.Register(api, binaryOp(http.MethodGet, "/api/albums/{id}/cover", "album-cover", "Album cover bytes (local file or 302 to upstream URL)", "Images"),
		wrapStream(handleAlbumCover(app)))

	// --- Video streaming (HLS + direct play) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}", "stream-direct", "Direct video stream (range-served bytes)", "Streaming"),
		wrapStream(handleDirectStream(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/master.m3u8", "stream-hls-master", "HLS master playlist", "Streaming"),
		wrapStream(handleHLSMaster(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/index.m3u8", "stream-hls-index", "HLS variant playlist", "Streaming"),
		wrapStream(handleHLSPlaylist(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/hls/{segment}", "stream-hls-segment", "HLS segment / init.mp4", "Streaming"),
		wrapStream(handleHLSSegment(app)))

	// --- Subtitles (text body) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/subtitles/{index}", "stream-subtitle-body", "Extracted subtitle file (VTT or ASS)", "Streaming"),
		wrapStream(handleGetSubtitle(app)))

	// --- Trickplay scrubbing previews ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/trickplay/index.vtt", "trickplay-vtt", "Trickplay WebVTT index", "Streaming"),
		wrapStream(handleTrickplayVTT(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/stream/{file_id}/trickplay/{filename}", "trickplay-sprite", "Trickplay sprite JPEG", "Streaming"),
		wrapStream(handleTrickplaySprite(app)))

	// --- Music streaming (range-served audio bytes) ---
	huma.Register(api, securedBinary(http.MethodGet, "/api/tracks/{id}/stream", "stream-track", "Best-quality playable audio for a track", "Music"),
		wrapStream(handleStreamTrack(app)))

	huma.Register(api, securedBinary(http.MethodGet, "/api/tracks/{id}/file/{track_file_id}", "stream-track-file", "Specific track file (bit-perfect)", "Music"),
		wrapStream(handleStreamTrackFile(app)))

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
		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				r, w := humago.Unwrap(hctx)
				h(w, r)
			},
		}, nil
	}
}
