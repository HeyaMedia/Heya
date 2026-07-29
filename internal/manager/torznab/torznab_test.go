package torznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Trimmed from a live Prowlarr 2.5.2 t=caps response.
const capsFixture = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Prowlarr" />
  <limits default="100" max="100" />
  <searching>
    <search available="yes" supportedParams="q" />
    <tv-search available="yes" supportedParams="q,season,ep,imdbid,tvdbid" />
    <movie-search available="yes" supportedParams="q,imdbid" />
    <music-search available="no" supportedParams="q" />
    <book-search available="no" supportedParams="q" />
  </searching>
  <categories>
    <category id="2000" name="Movies">
      <subcat id="2040" name="Movies/HD" />
      <subcat id="2045" name="Movies/UHD" />
    </category>
    <category id="5000" name="TV" />
  </categories>
</caps>`

// Newznab-flavoured search RSS: attrs carry the newznab namespace, the size
// rides both the <size> element and an enclosure.
const searchFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>Dune.Part.Two.2024.2160p.WEB-DL.DV.HDR.DDP5.1.H.265-GROUP</title>
      <guid>https://indexer.example/details/abc123</guid>
      <link>https://indexer.example/getnzb/abc123.nzb</link>
      <comments>https://indexer.example/details/abc123#comments</comments>
      <pubDate>Mon, 27 Jul 2026 08:12:00 +0000</pubDate>
      <enclosure url="https://indexer.example/getnzb/abc123.nzb" length="34805436000" type="application/x-nzb" />
      <newznab:attr name="category" value="2000" />
      <newznab:attr name="category" value="2045" />
      <newznab:attr name="grabs" value="118" />
    </item>
    <item>
      <title>Severance.S02E07.1080p.WEB.H264-GROUP</title>
      <guid>https://indexer.example/details/def456</guid>
      <link>https://indexer.example/getnzb/def456.nzb</link>
      <pubDate>Tue, 28 Jul 2026 21:00:00 +0000</pubDate>
      <size>4402341478</size>
      <torznab:attr name="category" value="5000" />
      <torznab:attr name="seeders" value="12" />
      <torznab:attr name="peers" value="14" />
    </item>
  </channel>
</rss>`

const errorFixture = `<?xml version="1.0" encoding="UTF-8"?>
<error code="100" description="Invalid API Key" />`

func fixtureServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(respond))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-key")
}

func TestCaps(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("t"); got != "caps" {
			t.Errorf("t param = %q, want caps", got)
		}
		if got := r.URL.Query().Get("apikey"); got != "test-key" {
			t.Errorf("apikey param = %q, want test-key", got)
		}
		w.Write([]byte(capsFixture))
	})

	caps, err := client.Caps(context.Background())
	if err != nil {
		t.Fatalf("Caps: %v", err)
	}
	if caps.ServerTitle != "Prowlarr" {
		t.Errorf("ServerTitle = %q", caps.ServerTitle)
	}
	available := map[string]bool{}
	for _, mode := range caps.Searching {
		available[mode.Name] = mode.Available
	}
	if !available["tv-search"] || !available["movie-search"] || available["music-search"] {
		t.Errorf("search modes wrong: %+v", available)
	}
	if len(caps.Categories) != 2 || caps.Categories[0].ID != 2000 || len(caps.Categories[0].Subcats) != 2 {
		t.Errorf("categories wrong: %+v", caps.Categories)
	}
}

func TestSearch(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "movie" || q.Get("imdbid") != "15239678" || q.Get("cat") != "2000,2045" {
			t.Errorf("unexpected query: %v", q)
		}
		w.Write([]byte(searchFixture))
	})

	releases, err := client.Search(context.Background(), Query{
		Type:   "movie",
		ImdbID: "tt15239678", // tt prefix must be stripped
		Cats:   []int{2000, 2045},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2", len(releases))
	}

	dune := releases[0]
	if dune.DownloadURL != "https://indexer.example/getnzb/abc123.nzb" {
		t.Errorf("DownloadURL = %q", dune.DownloadURL)
	}
	// No <size> element — falls back to the enclosure length.
	if dune.Size != 34805436000 {
		t.Errorf("Size = %d, want enclosure fallback 34805436000", dune.Size)
	}
	// newznab:attr and torznab:attr both parse (namespace-agnostic).
	if len(dune.Categories) != 2 || dune.Attrs["grabs"] != "118" {
		t.Errorf("newznab attrs wrong: cats=%v attrs=%v", dune.Categories, dune.Attrs)
	}
	sev := releases[1]
	if sev.Seeders != 12 || sev.Peers != 14 || sev.Size != 4402341478 {
		t.Errorf("torznab attrs wrong: %+v", sev)
	}
	if sev.PublishDate.IsZero() {
		t.Error("PublishDate not parsed")
	}
}

func TestErrorEnvelope(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(errorFixture))
	})
	_, err := client.Caps(context.Background())
	if err == nil {
		t.Fatal("want error for <error> document")
	}
	if got := err.Error(); got != "torznab: indexer error 100: Invalid API Key" {
		t.Errorf("error = %q", got)
	}
}
