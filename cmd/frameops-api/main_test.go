package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestServeShutsDownOnCancellation(t *testing.T) {
	server := &http.Server{Addr: "127.0.0.1:0"}

	context, cancel := context.WithCancel(context.Background())
	defer cancel()
	errch := make(chan error, 1)
	go func() { errch <- serve(context, server) }()
	cancel()

	if err := <-errch; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve() error = %v, want graceful shutdown", err)
	}
}
