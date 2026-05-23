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
	if u := os.Getenv("HEYA_MEDIA_URL"); u != "" {
		return u
	}
	return "https://heya.media"
}

func skipIfHeyaDown(t *testing.T) {
	t.Helper()
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(heyaURL() + "/api/v1/health")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("heya media server not reachable at " + heyaURL())
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

	detail, err := p.GetDetail(context.Background(), "heya:oshi-no-ko-2023", nil)
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
