package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthReturnsOKOnlyWhenDependenciesAreReady(t *testing.T) {
	for name, ready := range map[string]func() error{
		"ready":                      func() error { return nil },
		"database unavailable":       func() error { return errors.New("database unavailable") },
		"object storage unavailable": func() error { return errors.New("object storage unavailable") },
	} {
		t.Run(name, func(t *testing.T) {
			handler := Server{ready: func() error { return ready() }}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

			want := http.StatusServiceUnavailable
			if name == "ready" {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("health status = %d, want %d", response.Code, want)
			}
		})
	}
}
