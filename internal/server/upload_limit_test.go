package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

type uploadLimitForm struct {
	File huma.FormFile `form:"file" required:"true"`
}

func TestImageUploadContentLengthRejectedBeforeHandler(t *testing.T) {
	t.Parallel()
	for _, operationID := range []string{"upload-media-asset", "upload-playlist-cover"} {
		operationID := operationID
		t.Run(operationID, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			api := newHumaAPI(mux, fakeSessions{}, nil)
			called := false
			huma.Register(api, secured(op(http.MethodPost, "/"+operationID, operationID, "upload", "Test")),
				func(context.Context, *struct{}) (*struct{}, error) {
					called = true
					return nil, nil
				})

			request := httptest.NewRequest(http.MethodPost, "/"+operationID, bytes.NewReader([]byte("small placeholder")))
			request.Header.Set("Authorization", "Bearer user-token")
			request.ContentLength = maxImageMultipartBytes + 1
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413", response.Code)
			}
			if called {
				t.Fatal("oversized request reached handler")
			}
		})
	}
}

func TestChunkedImageMultipartIsCappedBeforeHandler(t *testing.T) {
	mux := http.NewServeMux()
	api := newHumaAPI(mux, fakeSessions{}, nil)
	called := false
	huma.Register(api, secured(op(http.MethodPost, "/chunked-upload", "upload-media-asset", "upload", "Test")),
		func(_ context.Context, _ *struct {
			RawBody huma.MultipartFormFiles[uploadLimitForm]
		}) (*struct{}, error) {
			called = true
			return nil, nil
		})

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("file", "oversized.png")
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte{'x'}, 1<<20)
	remaining := maxImageMultipartBytes + 1
	for remaining > 0 {
		write := int64(len(chunk))
		if write > remaining {
			write = remaining
		}
		if _, err := part.Write(chunk[:write]); err != nil {
			t.Fatal(err)
		}
		remaining -= write
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/chunked-upload", &body)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.ContentLength = -1 // exercise MaxBytesReader, not the early header check
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	// Huma parses multipart bodies before invoking the operation handler and
	// maps MaxBytesReader's bounded read failure to its 422 validation response.
	// The explicit Content-Length path above remains a 413.
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 from bounded multipart parsing", response.Code)
	}
	if called {
		t.Fatal("oversized chunked multipart reached handler")
	}
}
