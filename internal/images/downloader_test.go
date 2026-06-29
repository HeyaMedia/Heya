package images

import (
	"errors"
	"net/http"
	"testing"
)

func TestStatusErrorPermanent(t *testing.T) {
	tests := []struct {
		code      int
		permanent bool
	}{
		{http.StatusNotFound, true},         // 404 — image isn't there
		{http.StatusGone, true},             // 410
		{http.StatusForbidden, true},        // 403
		{http.StatusUnauthorized, true},     // 401
		{http.StatusBadRequest, true},       // 400
		{http.StatusRequestTimeout, false},  // 408 — transient
		{http.StatusTooManyRequests, false}, // 429 — back off and retry
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, tt := range tests {
		se := &StatusError{Code: tt.code, URL: "https://media.heya.media/x.webp"}
		if got := se.Permanent(); got != tt.permanent {
			t.Errorf("StatusError{%d}.Permanent() = %v, want %v", tt.code, got, tt.permanent)
		}
	}
}

func TestStatusErrorAs(t *testing.T) {
	var err error = &StatusError{Code: http.StatusNotFound, URL: "https://media.heya.media/x.webp"}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatal("errors.As failed to unwrap *StatusError")
	}
	if se.Code != http.StatusNotFound {
		t.Errorf("Code = %d, want 404", se.Code)
	}
}
