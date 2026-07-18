package server

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/karbowiak/heya/internal/safedial"
)

// publicMediaHTTPClient is shared by the authenticated arbitrary-URL audio
// proxies. Its transport rejects non-public resolved addresses on every dial,
// ignores environment proxies, and revalidates each redirect hop.
var publicMediaHTTPClient = safedial.NewPublicHTTPClient()

func newPublicMediaRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	// ValidateHTTPURL rejects non-HTTP schemes here; the public-only transport
	// independently validates every resolved address and redirect hop.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil) //nolint:gosec
	if err != nil {
		return nil, err
	}
	if err := safedial.ValidateHTTPURL(request.URL); err != nil {
		return nil, err
	}
	return request, nil
}

func mediaHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return publicMediaHTTPClient
}

// safeAudioContentType turns semi-trusted upstream metadata into a media type
// safe to serve from Heya's origin. Explicit HTML, XML, JavaScript, video, and
// other non-audio responses are rejected; missing/generic binary types use a
// conservative type inferred from the enclosure/stream path.
func safeAudioContentType(rawContentType, sourceURL string) (string, bool) {
	rawContentType = strings.TrimSpace(rawContentType)
	if rawContentType == "" {
		return inferredAudioContentType(sourceURL), true
	}
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return "", false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" {
		return inferredAudioContentType(sourceURL), true
	}
	for _, activeType := range []string{"html", "javascript", "ecmascript", "xml", "svg"} {
		if strings.Contains(mediaType, activeType) {
			return "", false
		}
	}
	switch mediaType {
	case "application/ogg":
		return "audio/ogg", true
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return "application/vnd.apple.mpegurl", true
	case "audio/mp3", "audio/x-mp3":
		return "audio/mpeg", true
	case "audio/x-m4a":
		return "audio/mp4", true
	case "audio/x-flac":
		return "audio/flac", true
	case "audio/x-wav", "audio/wave":
		return "audio/wav", true
	}
	if strings.HasPrefix(mediaType, "audio/") {
		return mediaType, true
	}
	return "", false
}

func inferredAudioContentType(sourceURL string) string {
	target, _ := url.Parse(sourceURL)
	switch strings.ToLower(path.Ext(target.Path)) {
	case ".m4a", ".m4b", ".mp4":
		return "audio/mp4"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".aac", ".aacp":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	case ".m3u", ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".pls":
		return "audio/x-scpls"
	default:
		return "audio/mpeg"
	}
}
