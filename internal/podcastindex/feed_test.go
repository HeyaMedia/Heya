package podcastindex

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/karbowiak/heya/internal/publichttp"
	"github.com/karbowiak/heya/internal/safedial"
	"github.com/stretchr/testify/require"
)

func testFeedFetcher(t *testing.T, handler http.Handler) (*feedFetcher, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	dialer := &net.Dialer{}
	client := safedial.NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, serverURL.Host)
	})
	t.Cleanup(client.CloseIdleConnections)
	return newFeedFetcher(publichttp.NewFetcherWithClient(client, time.Second)), "http://feed.example.test/rss"
}

func smallFeed(title string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>` + title + `</title><description>feed</description>
<item><guid>one</guid><title>Episode</title><description>episode</description>
<enclosure url="https://cdn.example.test/one.mp3" type="audio/mpeg" length="12"/></item>
</channel></rss>`
}

func TestFetchFeedRejectsNonPublicTargetBeforeDial(t *testing.T) {
	var reached atomic.Bool
	client := &http.Client{Transport: feedRoundTripFunc(func(*http.Request) (*http.Response, error) {
		reached.Store(true)
		return nil, fmt.Errorf("unexpected request")
	})}
	fetcher := newFeedFetcher(publichttp.NewFetcherWithClient(client, time.Second))

	_, err := fetcher.Fetch(t.Context(), "http://169.254.169.254/latest/meta-data")
	require.Error(t, err)
	require.False(t, reached.Load())
}

func TestFetchFeedRejectsRedirectToNonPublicTarget(t *testing.T) {
	fetcher, feedURL := testFeedFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))

	_, err := fetcher.Fetch(t.Context(), feedURL)
	require.Error(t, err)
}

func TestFetchFeedRejectsOversizedResponse(t *testing.T) {
	fetcher, feedURL := testFeedFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(maxFeedBytes+1))
		w.WriteHeader(http.StatusOK)
	}))

	_, err := fetcher.Fetch(t.Context(), feedURL)
	require.ErrorIs(t, err, publichttp.ErrBodyTooLarge)
}

func TestFetchFeedBoundsEpisodesCategoriesAndText(t *testing.T) {
	longTitle := strings.Repeat("å", maxFeedTitleBytes)
	longDescription := "<p>" + strings.Repeat("description ", maxFeedDescriptionBytes/len("description ")+100) + "</p>"
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel>`)
	body.WriteString("<title>" + longTitle + "</title><description>" + longDescription + "</description>")
	for i := range maxFeedCategories + 10 {
		fmt.Fprintf(&body, `<itunes:category text="category-%d"/>`, i)
	}
	for i := range maxFeedEpisodes + 10 {
		description := "short"
		if i == 0 {
			description = longDescription
		}
		fmt.Fprintf(&body, `<item><guid>%d</guid><title>%s</title><description>%s</description><enclosure url="https://cdn.example.test/%d.mp3" type="audio/mpeg"/></item>`, i, longTitle, description, i)
	}
	body.WriteString("</channel></rss>")
	require.LessOrEqual(t, int64(body.Len()), maxFeedBytes)

	fetcher, feedURL := testFeedFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, body.String())
	}))
	detail, err := fetcher.Fetch(t.Context(), feedURL)
	require.NoError(t, err)
	require.Len(t, detail.Episodes, maxFeedEpisodes)
	require.Len(t, detail.Categories, maxFeedCategories)
	require.LessOrEqual(t, len(detail.Title), maxFeedTitleBytes)
	require.True(t, utf8.ValidString(detail.Title))
	require.LessOrEqual(t, len(detail.Description), maxFeedDescriptionBytes)
	require.LessOrEqual(t, len(detail.Episodes[0].Title), maxFeedTitleBytes)
	require.True(t, utf8.ValidString(detail.Episodes[0].Title))
	require.LessOrEqual(t, len(detail.Episodes[0].Description), maxFeedDescriptionBytes)
}

func TestFetchFeedCoalescesConcurrentMisses(t *testing.T) {
	var requests atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fetcher, feedURL := testFeedFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		_, _ = io.WriteString(w, smallFeed("Shared"))
	}))

	const callers = 12
	start := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			_, err := fetcher.Fetch(t.Context(), feedURL)
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	<-started
	// Give every released goroutine a chance to join the in-flight request.
	time.Sleep(20 * time.Millisecond)
	close(release)
	for range callers {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(1), requests.Load())
}

func TestFeedCacheExpiresAndStaysBounded(t *testing.T) {
	var requests atomic.Int32
	fetcher, feedURL := testFeedFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := requests.Add(1)
		_, _ = io.WriteString(w, smallFeed(fmt.Sprintf("Feed %d", count)))
	}))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	fetcher.now = func() time.Time { return now }

	first, err := fetcher.Fetch(t.Context(), feedURL)
	require.NoError(t, err)
	second, err := fetcher.Fetch(t.Context(), feedURL)
	require.NoError(t, err)
	require.Same(t, first, second)
	require.Equal(t, int32(1), requests.Load())

	now = now.Add(feedCacheTTL)
	third, err := fetcher.Fetch(t.Context(), feedURL)
	require.NoError(t, err)
	require.NotSame(t, first, third)
	require.Equal(t, int32(2), requests.Load())

	for i := range maxFeedCacheEntries + 20 {
		fetcher.store(fmt.Sprintf("feed-%d", i), &PodcastDetail{Title: fmt.Sprint(i)})
	}
	fetcher.mu.Lock()
	cacheEntries := len(fetcher.cache)
	fetcher.mu.Unlock()
	require.LessOrEqual(t, cacheEntries, maxFeedCacheEntries)
}

func TestFeedCacheHonorsApproximateByteBudget(t *testing.T) {
	fetcher := newFeedFetcher(nil)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	fetcher.now = func() time.Time { return now }
	detail := func(title string) *PodcastDetail {
		return &PodcastDetail{Title: title, Description: strings.Repeat("x", 512)}
	}
	entryWeight := podcastDetailWeight(detail("one"))
	fetcher.maxCacheBytes = entryWeight*2 + 8

	fetcher.store("one", detail("one"))
	now = now.Add(time.Second)
	fetcher.store("two", detail("two"))
	now = now.Add(time.Second)
	fetcher.store("three", detail("three"))

	fetcher.mu.Lock()
	_, retainedOldest := fetcher.cache["one"]
	cacheBytes := fetcher.bytes
	cacheEntries := len(fetcher.cache)
	fetcher.mu.Unlock()
	require.False(t, retainedOldest)
	require.LessOrEqual(t, cacheBytes, fetcher.maxCacheBytes)
	require.LessOrEqual(t, cacheEntries, 2)
}

func TestFetchFeedRejectsUniqueMissWhenConcurrencyIsFull(t *testing.T) {
	fetcher := newFeedFetcher(nil)
	for range maxFeedFetches {
		fetcher.fetchSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range maxFeedFetches {
			<-fetcher.fetchSlots
		}
	})

	_, err := fetcher.Fetch(t.Context(), "https://feed.example.test/unique.xml")
	require.ErrorContains(t, err, "too many concurrent podcast feed fetches")
}

type feedRoundTripFunc func(*http.Request) (*http.Response, error)

func (f feedRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
