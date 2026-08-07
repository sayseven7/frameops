package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsolateRejectsHostRuntime(t *testing.T) {
	_, err := isolate(context.Background(), t.TempDir(), "/usr/bin/true")
	if err == nil || !strings.Contains(err.Error(), "outer sandbox") {
		t.Fatalf("isolate error = %v, want outer sandbox refusal", err)
	}
}

func TestRendererRejectsConcurrentRenderWhileBusy(t *testing.T) {
	conversionSlot <- struct{}{}
	t.Cleanup(func() { <-conversionSlot })
	request := httptest.NewRequest(http.MethodPost, "/render", strings.NewReader("docx"))
	response := httptest.NewRecorder()
	renderHandler(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestRendererHealthSucceedsWhileConversionBusy(t *testing.T) {
	t.Setenv("FRAMEOPS_RENDER_SOCKET", "/run/frameops/render.sock")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "soffice"), []byte("#!/bin/sh\necho 'LibreOffice test'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	conversionSlot <- struct{}{}
	t.Cleanup(func() { <-conversionSlot })

	response := httptest.NewRecorder()
	renderHandler(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestRendererHealthDoesNotMakeConcurrentRenderBusy(t *testing.T) {
	t.Setenv("FRAMEOPS_RENDER_SOCKET", "/run/frameops/render.sock")
	bin, signal := t.TempDir(), t.TempDir()
	started, release := filepath.Join(signal, "started"), filepath.Join(signal, "release")
	script := "#!/bin/sh\ncase \"$HOME\" in\n  *frameops-render-health-*)\n    : > " + strconv.Quote(started) + "\n    while [ ! -f " + strconv.Quote(release) + " ]; do sleep 0.01; done\n    ;;\nesac\necho 'LibreOffice test'\n"
	if err := os.WriteFile(filepath.Join(bin, "soffice"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	health := make(chan int, 1)
	go func() {
		response := httptest.NewRecorder()
		renderHandler(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		health <- response.Code
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health converter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })

	response := httptest.NewRecorder()
	renderHandler(response, httptest.NewRequest(http.MethodPost, "/render", strings.NewReader("docx")))
	if response.Code == http.StatusServiceUnavailable {
		t.Fatalf("render status = %d, want a conversion result", response.Code)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := <-health; status != http.StatusNoContent {
		t.Fatalf("health status = %d, want %d", status, http.StatusNoContent)
	}
}

func TestTailBufferAcceptsExactLimit(t *testing.T) {
	buffer := tailBuffer{limit: 4}
	if _, err := buffer.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if buffer.exceeded || buffer.String() != "1234" {
		t.Fatalf("buffer = %q, exceeded = %t", buffer.String(), buffer.exceeded)
	}
}

func TestIsolateBoundsConverterDiagnostics(t *testing.T) {
	t.Setenv("FRAMEOPS_RENDER_SOCKET", "/run/frameops/render.sock")
	script := filepath.Join(t.TempDir(), "noisy")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%070000d' 0\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := isolate(context.Background(), t.TempDir(), script)
	if !errors.Is(err, errConverterOutputTooLarge) {
		t.Fatalf("isolate error = %v, want %v", err, errConverterOutputTooLarge)
	}
	if len(output) > converterOutputLimit {
		t.Fatalf("diagnostic length = %d, want <= %d", len(output), converterOutputLimit)
	}
}

func TestConverterVersionStopsWhenRequestIsCancelled(t *testing.T) {
	t.Setenv("FRAMEOPS_RENDER_SOCKET", "/run/frameops/render.sock")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "soffice"), []byte("#!/bin/sh\n: > \"$HOME/started\"\nwhile :; do :; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	workspace := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := converterVersion(ctx, workspace)
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(workspace, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("converter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("converterVersion error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled converter kept running")
	}
}

func TestRendererRejectsOversizedDocumentBeforeConversion(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/render", strings.NewReader("x"))
	request.ContentLength = maxSourceBytes + 1
	response := httptest.NewRecorder()
	renderHandler(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}
