package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthReturnsOKOnlyWhenDependenciesAreReady(t *testing.T) {
	for name, ready := range map[string]func() error{
		"ready":                      func() error { return nil },
		"database unavailable":       func() error { return errors.New("database unavailable") },
		"object storage unavailable": func() error { return errors.New("object storage unavailable") },
		"PDF worker unavailable":     func() error { return errors.New("PDF worker unavailable") },
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

func TestReadinessProbeGetsDeadlineBelowComposeHealthcheck(t *testing.T) {
	err := readinessProbe(func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("readiness probe has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining >= 3*time.Second {
			t.Fatalf("readiness deadline remaining = %s, want between zero and Compose timeout", remaining)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("readiness probe error = %v", err)
	}
}
