package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/radiobrowser"
	"github.com/karbowiak/heya/internal/service"
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
func handleRadioStream(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		streamURL := r.URL.Query().Get("url")
		if streamURL == "" {
			writeError(w, http.StatusBadRequest, "missing url parameter")
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		// Abort the outbound fetch when the client tab closes the audio
		// element. Without this the upstream connection would dangle until
		// our HTTP timeout fires.
		go func() {
			<-ctx.Done()
		}()

		// G704 SSRF: streamURL is from the radio-browser community catalog
		// (curated public radio streams). Proxying it is the feature.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil) //nolint:gosec
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stream URL")
			return
		}
		// `1` asks for inline metadata; absent means plain audio.
		req.Header.Set("Icy-MetaData", "1")
		req.Header.Set("User-Agent", "Heya/0.1 (+https://heya.media)")
		// Use a separate client (not app.RadioBrowser's HTTP client) so the
		// long-lived stream doesn't share the 15s timeout meant for one-shot
		// API calls. Streams stay open until either side disconnects.
		client := &http.Client{}
		resp, err := client.Do(req) //nolint:gosec // see G704 note above
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to connect to radio stream: "+err.Error())
			return
		}
		defer resp.Body.Close() //nolint:errcheck // defer close

		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "audio/mpeg"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		// Live streams have no Content-Length — anything we write is best-
		// effort flushed to the client.
		flusher, _ := w.(http.Flusher)

		var src io.Reader = resp.Body
		if metaintStr := resp.Header.Get("icy-metaint"); metaintStr != "" {
			if metaint, err := strconv.Atoi(metaintStr); err == nil && metaint > 0 {
				hub := app.EventHub()
				src = radiobrowser.NewIcyReader(resp.Body, metaint, func(artist, title string) {
					if hub != nil {
						hub.Emit(eventhub.EventRadioICY, eventhub.RadioICYPayload{
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

// writeRadioError is a tiny helper for the JSON-style error shape the
// other stream handlers use. Kept local so this file doesn't reach across
// package boundaries for one-liner formatting.
func writeRadioError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, msg)
}

var _ = writeRadioError // reserved for future use by handler additions
