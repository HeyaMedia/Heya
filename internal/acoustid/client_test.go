package acoustid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupReturnsUniqueRecordingMBIDsByScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/lookup" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("client") != "client-key" || r.Form.Get("duration") != "123" || r.Form.Get("fingerprint") != "fingerprint" || r.Form.Get("meta") != "recordingids" {
			t.Fatalf("form = %#v", r.Form)
		}
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"acoustid-low","score":0.8,"recordings":[{"id":"BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB"}]},{"id":"acoustid-high","score":0.99,"recordings":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},{"id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}]}]}`))
	}))
	defer server.Close()

	client, err := New(Options{BaseURL: server.URL, APIKey: "client-key", RequestsPerSecond: 3, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := client.Lookup(context.Background(), "fingerprint", 123)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].RecordingMBID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || matches[1].Score != .99 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestLookupRequiresConfiguredApplicationKey(t *testing.T) {
	client, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lookup(context.Background(), "fingerprint", 123); err != ErrDisabled {
		t.Fatalf("error = %v", err)
	}
}
