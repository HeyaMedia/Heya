package acoustid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestLookupClassifiesTransientAndConfigurationFailures(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		retryAfter    string
		transient     bool
		configuration bool
	}{
		{name: "service unavailable", status: http.StatusServiceUnavailable, retryAfter: "17", transient: true},
		{name: "bad HTTP credentials", status: http.StatusForbidden, configuration: true},
		{name: "bad application key HTTP 400 envelope", status: http.StatusBadRequest, body: `{"status":"error","error":{"code":4,"message":"invalid client key"}}`, configuration: true},
		{name: "bad application key envelope", status: http.StatusOK, body: `{"status":"error","error":{"code":4,"message":"invalid client key"}}`, configuration: true},
		{name: "internal envelope failure", status: http.StatusOK, body: `{"status":"error","error":{"code":10,"message":"internal error"}}`, transient: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := New(Options{BaseURL: server.URL, APIKey: "client-key", RequestsPerSecond: 3, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Lookup(context.Background(), "fingerprint", 123)
			if err == nil || IsTransient(err) != test.transient || IsConfiguration(err) != test.configuration {
				t.Fatalf("error classification = %v transient=%v configuration=%v", err, IsTransient(err), IsConfiguration(err))
			}
			if test.retryAfter != "" && ErrorRetryAfter(err) != 17*time.Second {
				t.Fatalf("retry after = %s", ErrorRetryAfter(err))
			}
		})
	}
}

func TestLookupNoMatchIsSuccessfulEmptyEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer server.Close()
	client, err := New(Options{BaseURL: server.URL, APIKey: "client-key", RequestsPerSecond: 3, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := client.Lookup(context.Background(), "fingerprint", 123)
	if err != nil || len(matches) != 0 {
		t.Fatalf("no-match lookup = %#v, %v", matches, err)
	}
}
