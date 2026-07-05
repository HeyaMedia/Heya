package heyamedia

import (
	"net/http"
	"time"

	gen "github.com/karbowiak/heya/clients/heyamedia"
)

// Client is the typed wrapper around the generated heya.media OpenAPI
// client. External callers receive it via NewClient(baseURL) and pass it
// into NewHeyaProvider; package-level helpers like GetPersonFromHeya
// also take *Client so workers that don't hold a provider can still
// issue targeted lookups.
//
// The struct deliberately keeps the underlying generated client
// unexported — code outside this package should go through HeyaProvider
// or the package-level helpers, not pluck the raw client off Client.gen.
// That way swapping the codegen out later (or wrapping the transport
// with retry / caching middleware) stays a single-package change.
type Client struct {
	gen *gen.ClientWithResponses
}

// NewClient constructs a Client pointed at the given heya.media base URL.
// The HTTP client carries a 5-minute ceiling so a hung upstream can't
// pin a worker forever; callers cancel sooner via ctx where appropriate.
//
// heya.media's artist endpoint can legitimately take 60-120s on cold
// cache (rate-limited upstream MusicBrainz / Last.fm), so the timeout
// has to be generous. Search calls return in seconds.
//
// The transport is wrapped with loggingTransport so every upstream call
// gets a DEBUG-level trace (method, path, status, duration) — see
// transport.go for why that's useful and what it deliberately avoids
// logging.
func NewClient(baseURL string) *Client {
	httpClient := &http.Client{Timeout: 5 * time.Minute, Transport: newLoggingTransport(nil)}
	c, err := gen.NewClientWithResponses(baseURL, gen.WithHTTPClient(httpClient))
	if err != nil {
		// NewClientWithResponses only errors when a ClientOption rejects
		// the URL. We don't pass any URL-validating options, so this is
		// effectively unreachable — but panic loudly so a hypothetical
		// future config mishap surfaces at startup, not on first request.
		panic("heyamedia: NewClientWithResponses unexpectedly failed: " + err.Error())
	}
	return &Client{gen: c}
}
