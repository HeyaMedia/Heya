package heyamedia

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeRT is a scripted inner RoundTripper: it returns responses[i] (or errs[i])
// on the i-th call, defaulting to 200 once the script is exhausted.
type fakeRT struct {
	responses []*http.Response
	errs      []error
	calls     int
}

func (f *fakeRT) RoundTrip(*http.Request) (*http.Response, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return resp(http.StatusOK, ""), nil
}

func resp(code int, retryAfter string) *http.Response {
	h := http.Header{}
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader("body"))}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestRetryTransport_RetriesThenSucceeds(t *testing.T) {
	inner := &fakeRT{responses: []*http.Response{resp(http.StatusTooManyRequests, ""), resp(http.StatusOK, "")}}
	rt := newRetryTransport(inner)
	req, _ := http.NewRequest(http.MethodGet, "http://x/api/v1/movie/1", nil)

	r, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", r.StatusCode)
	}
	if inner.calls != 2 {
		t.Fatalf("want 2 inner calls (429 then 200), got %d", inner.calls)
	}
}

func TestRetryTransport_TerminalNoRetry(t *testing.T) {
	inner := &fakeRT{responses: []*http.Response{resp(http.StatusNotFound, "")}}
	rt := newRetryTransport(inner)
	req, _ := http.NewRequest(http.MethodGet, "http://x/api/v1/movie/1", nil)

	r, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", r.StatusCode)
	}
	if inner.calls != 1 {
		t.Fatalf("404 is terminal; want 1 inner call, got %d", inner.calls)
	}
}

func TestRetryTransport_ContextCancelBails(t *testing.T) {
	// Retryable statuses forever — a cancelled context must stop the loop
	// promptly instead of sleeping through every backoff.
	inner := &fakeRT{}
	for i := 0; i < 10; i++ {
		inner.responses = append(inner.responses, resp(http.StatusServiceUnavailable, ""))
	}
	rt := newRetryTransport(inner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://x/api/v1/movie/1", nil)

	done := make(chan struct{})
	go func() {
		_, err := rt.RoundTrip(req)
		if err == nil {
			t.Error("want a context error, got nil")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RoundTrip did not bail on a cancelled context")
	}
	if inner.calls > 1 {
		t.Fatalf("cancelled ctx should stop after at most one attempt, got %d", inner.calls)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{408, 429, 500, 502, 503, 504}
	terminal := []int{200, 400, 401, 404, 422, 501}
	for _, c := range retryable {
		if !isRetryableStatus(c) {
			t.Errorf("status %d should be retryable", c)
		}
	}
	for _, c := range terminal {
		if isRetryableStatus(c) {
			t.Errorf("status %d should be terminal", c)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	if got := retryAfter(resp(200, "2")); got != 2*time.Second {
		t.Errorf("delta-seconds: want 2s, got %v", got)
	}
	if got := retryAfter(resp(200, "9999")); got != heyaMaxRetryAfter {
		t.Errorf("over-cap: want %v, got %v", heyaMaxRetryAfter, got)
	}
	if got := retryAfter(resp(200, "")); got != 0 {
		t.Errorf("absent: want 0, got %v", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := retryAfter(resp(200, past)); got != 0 {
		t.Errorf("past date: want 0, got %v", got)
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil should not be retryable")
	}
	if IsRetryable(context.Canceled) {
		t.Error("context.Canceled should not be retryable (shutdown)")
	}
	if !IsRetryable(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded (client timeout) should be retryable")
	}
	if !IsRetryable(timeoutErr{}) {
		t.Error("net timeout should be retryable")
	}
	retryable := []int{408, 429, 500, 502, 503, 504}
	for _, s := range retryable {
		if !IsRetryable(&UpstreamError{Status: s}) {
			t.Errorf("UpstreamError %d should be retryable", s)
		}
	}
	terminal := []int{400, 404, 422, 501}
	for _, s := range terminal {
		if IsRetryable(&UpstreamError{Status: s}) {
			t.Errorf("UpstreamError %d should be terminal", s)
		}
	}
}
