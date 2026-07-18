package server

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/radiobrowser"
)

// handleRadioStream proxies an internet-radio stream to the browser while
// transparently stripping ICY metadata blocks from the audio (so the
// browser sees a contiguous audio stream it can decode natively) and
// emitting each fresh "Now Playing" title via the event hub.
//
// Two-step request:
//  1. Outbound to the station with `Icy-MetaData: 1` so the server
//     interleaves text metadata every `icy-metaint` bytes.
//  2. Inbound: if the response carries `icy-metaint`, wrap the body in
//     our IcyReader; otherwise pass-through unchanged.
//
// Browsers can't attach Authorization headers to <audio src>, so this
// endpoint accepts ?token= as an alternative (same as the music streaming
// endpoints).
func handleRadioStream(hub *eventhub.Hub, userID int64, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		streamURL := r.URL.Query().Get("url")
		if streamURL == "" {
			writeError(w, http.StatusBadRequest, "missing url parameter")
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		req, err := newPublicMediaRequest(ctx, streamURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stream URL")
			return
		}
		// `1` asks for inline metadata; absent means plain audio.
		req.Header.Set("Icy-MetaData", "1")
		req.Header.Set("User-Agent", "Heya/0.1 (+https://heya.media)")
		// The public-only client has no whole-request timeout: streams stay open
		// until either side disconnects, while its transport still bounds dials.
		// newPublicMediaRequest validates the URL and production uses the
		// public-only transport; a custom client is accepted for isolated tests.
		resp, err := mediaHTTPClient(client).Do(req) //nolint:gosec
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to connect to radio stream")
			return
		}
		defer resp.Body.Close() //nolint:errcheck // defer close

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			writeError(w, http.StatusBadGateway, "radio upstream returned "+resp.Status)
			return
		}
		contentURL := streamURL
		if resp.Request != nil && resp.Request.URL != nil {
			contentURL = resp.Request.URL.String()
		}
		contentType, ok := safeAudioContentType(resp.Header.Get("Content-Type"), contentURL)
		if !ok {
			writeError(w, http.StatusBadGateway, "radio upstream returned non-audio content")
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		// Live streams have no Content-Length — anything we write is best-
		// effort flushed to the client.
		flusher, _ := w.(http.Flusher)

		var src io.Reader = resp.Body
		if metaintStr := resp.Header.Get("icy-metaint"); metaintStr != "" {
			if metaint, err := strconv.Atoi(metaintStr); err == nil && metaint > 0 {
				src = radiobrowser.NewIcyReader(resp.Body, metaint, func(artist, title string) {
					if hub != nil {
						hub.EmitToUser(userID, eventhub.EventRadioICY, eventhub.RadioICYPayload{
							Artist:    artist,
							Title:     title,
							StreamURL: streamURL,
						})
					}
				})
			}
		}

		// Copy audio bytes to the client. Flush after each chunk so the
		// browser can start decoding immediately rather than waiting for the
		// default 4KB buffer fill.
		buf := make([]byte, 16*1024)
		for {
			n, rerr := src.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if rerr != nil {
				return
			}
		}
	}
}
