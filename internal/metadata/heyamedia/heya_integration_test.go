package heyamedia_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

func heyaURL() string {
	return os.Getenv("HEYA_MEDIA_URL")
}

// skipIfHeyaDown gates the integration tests on an explicit HEYA_MEDIA_URL
// (typically a local v0.3.0 heya.media instance). Production may run an
// older API version, so we don't fall back to it implicitly.
func skipIfHeyaDown(t *testing.T) {
	t.Helper()
	u := heyaURL()
	if u == "" {
		t.Skip("HEYA_MEDIA_URL not set — skipping heya.media integration tests")
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(u + "/api/v1/health/live")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("heya media server not reachable at " + u)
	}
}

func TestHeyaProvider_Search(t *testing.T) {
	skipIfHeyaDown(t)
	c := heyamedia.NewClient(heyaURL())
	p := heyamedia.NewHeyaProvider(c)

	results, err := p.Search(context.Background(), metadata.KindTV, metadata.SearchQuery{Title: "Elfen Lied", Year: "2004"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results for 'Elfen Lied'")
	}
	t.Logf("Search: %d results", len(results))
	for _, r := range results {
		t.Logf("  %s (%s) conf=%.2f id=%s", r.Title, r.Year, r.Confidence, r.ProviderID)
	}
}

func TestHeyaProvider_LookupByNFO(t *testing.T) {
	skipIfHeyaDown(t)
	c := heyamedia.NewClient(heyaURL())
	p := heyamedia.NewHeyaProvider(c)

	detail, pid, err := p.LookupByNFO(context.Background(), metadata.KindTV, metadata.NFOIDs{
		IMDBID: "tt0480489",
		TMDBID: "42671",
		TVDBID: "75941",
	}, nil)
	if err != nil {
		t.Fatalf("LookupByNFO failed: %v", err)
	}
	if detail == nil {
		t.Fatal("LookupByNFO returned nil detail")
	}
	t.Logf("NFO Lookup: pid=%s title=%s year=%s seasons=%d", pid, detail.Title, detail.Year, len(detail.Seasons))
	t.Logf("  ExternalIDs: %v", detail.ExternalIDs)
	t.Logf("  Cast: %d, Crew: %d", len(detail.Cast), len(detail.Crew))
	t.Logf("  Titles: %d, Overviews: %d", len(detail.Titles), len(detail.Overviews))
}

func TestHeyaProvider_GetDetail(t *testing.T) {
	skipIfHeyaDown(t)
	c := heyamedia.NewClient(heyaURL())
	p := heyamedia.NewHeyaProvider(c)

	// Elfen Lied (TMDB 42671) — the same fixture LookupByNFO covers, with
	// the v0.3.0 providerID shape: heya:<kind>:<provider>:<value>.
	detail, err := p.GetDetail(context.Background(), "heya:tv:tmdb:42671", nil)
	if err != nil {
		t.Fatalf("GetDetail failed: %v", err)
	}
	if detail == nil {
		t.Fatal("GetDetail returned nil")
	}
	t.Logf("Detail: %s (%s) - %s", detail.Title, detail.Year, detail.HeyaSlug)
	t.Logf("  Seasons: %d, Cast: %d, Crew: %d", len(detail.Seasons), len(detail.Cast), len(detail.Crew))
	t.Logf("  Titles: %d, Overviews: %d", len(detail.Titles), len(detail.Overviews))
	for i, s := range detail.Seasons {
		fmt.Printf("  Season %d: %s (%d episodes)\n", s.Number, s.Title, len(s.Episodes))
		if i >= 3 {
			break
		}
	}
}
