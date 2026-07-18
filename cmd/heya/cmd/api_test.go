package cmd

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestRenderAPIResponseStreamsRawBytesVerbatim(t *testing.T) {
	previousRaw := apiRaw
	apiRaw = true
	t.Cleanup(func() { apiRaw = previousRaw })

	body := []byte{0x00, 0x01, 0x02, 0xff}
	response := testAPIResponse(http.StatusOK, "application/octet-stream", body)
	var stdout, stderr bytes.Buffer
	nonOK, err := renderAPIResponse(response, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if nonOK {
		t.Fatal("successful response reported as non-OK")
	}
	if !bytes.Equal(stdout.Bytes(), body) {
		t.Fatalf("stdout = %v, want exact bytes %v", stdout.Bytes(), body)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRenderAPIResponseStreamsNonJSONWithoutRawFlag(t *testing.T) {
	previousRaw := apiRaw
	apiRaw = false
	t.Cleanup(func() { apiRaw = previousRaw })

	response := testAPIResponse(http.StatusOK, "audio/flac", []byte("audio-without-a-trailing-newline"))
	var stdout bytes.Buffer
	nonOK, err := renderAPIResponse(response, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if nonOK || stdout.String() != "audio-without-a-trailing-newline" {
		t.Fatalf("nonOK = %v, stdout = %q", nonOK, stdout.String())
	}
}

func TestRenderAPIResponsePrettyPrintsBoundedJSON(t *testing.T) {
	previousRaw := apiRaw
	apiRaw = false
	t.Cleanup(func() { apiRaw = previousRaw })

	response := testAPIResponse(http.StatusOK, "application/json; charset=utf-8", []byte(`{"ok":true}`))
	var stdout bytes.Buffer
	nonOK, err := renderAPIResponse(response, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if nonOK || stdout.String() != "{\n  \"ok\": true\n}\n" {
		t.Fatalf("nonOK = %v, stdout = %q", nonOK, stdout.String())
	}
}

func TestRenderAPIResponseRejectsOversizedBufferedBody(t *testing.T) {
	previousRaw := apiRaw
	apiRaw = false
	t.Cleanup(func() { apiRaw = previousRaw })

	response := testAPIResponse(http.StatusOK, "application/json", nil)
	response.ContentLength = maxBufferedAPIResponseBytes + 1
	var stdout bytes.Buffer
	_, err := renderAPIResponse(response, &stdout, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "too large to buffer") {
		t.Fatalf("error = %v, want bounded-response error", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("oversized response produced stdout: %q", stdout.String())
	}
}

func TestReadBufferedAPIResponseRejectsUnknownLengthPastLimit(t *testing.T) {
	response := testAPIResponse(http.StatusOK, "application/json", []byte("123456789"))
	response.ContentLength = -1
	_, err := readBufferedAPIResponseLimit(response, 8)
	if err == nil || !strings.Contains(err.Error(), "exceeds 8-byte") {
		t.Fatalf("error = %v, want streamed size-limit error", err)
	}
}

func TestRenderAPIResponseReportsNonOKStatusAndBody(t *testing.T) {
	previousRaw := apiRaw
	apiRaw = false
	t.Cleanup(func() { apiRaw = previousRaw })

	response := testAPIResponse(http.StatusBadGateway, "application/problem+json", []byte(`{"detail":"upstream failed"}`))
	var stdout, stderr bytes.Buffer
	nonOK, err := renderAPIResponse(response, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !nonOK {
		t.Fatal("error response did not request a non-zero exit")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	want := "HTTP 502 Bad Gateway\n{\n  \"detail\": \"upstream failed\"\n}\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func testAPIResponse(status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}
