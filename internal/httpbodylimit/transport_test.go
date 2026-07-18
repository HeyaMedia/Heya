package httpbodylimit

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTransportRejectsOversizedDeclaredLengthAndClosesBody(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("unused")}
	client := &http.Client{Transport: NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          body,
			ContentLength: 5,
		}, nil
	}), 4)}

	response, err := client.Get("http://metadata.test")
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("error = %v, want ErrResponseBodyTooLarge", err)
	}
	if !body.closed {
		t.Fatal("oversized declared response body was not closed")
	}
}

func TestTransportUnknownLengthFailsBeyondLimit(t *testing.T) {
	client := &http.Client{Transport: NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return responseWithBody("12345", -1), nil
	}), 4)}
	response, err := client.Get("http://metadata.test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("error = %v, want ErrResponseBodyTooLarge", err)
	}
	if string(body) != "1234" {
		t.Fatalf("body = %q, want bounded prefix", body)
	}
}

func TestTransportAllowsExactUnknownLength(t *testing.T) {
	client := &http.Client{Transport: NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return responseWithBody("1234", -1), nil
	}), 4)}
	response, err := client.Get("http://metadata.test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "1234" {
		t.Fatalf("body = %q, want exact body", body)
	}
}

func responseWithBody(body string, contentLength int64) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: contentLength,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}
