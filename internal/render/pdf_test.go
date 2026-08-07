package render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketConvertVerifiesProvenanceBeforeWritingDestination(t *testing.T) {
	workspace := t.TempDir()
	socket := filepath.Join(workspace, "render.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Frameops-Converter", "test")
		response.Header().Set("X-Frameops-SHA256", strings.Repeat("0", 64))
		response.Header().Set("X-Frameops-Byte-Size", "9")
		_, _ = response.Write([]byte("%PDF-real"))
	})}
	go server.Serve(listener) //nolint:errcheck
	t.Cleanup(func() { _ = server.Close() })

	source := filepath.Join(workspace, "approved.docx")
	destination := filepath.Join(workspace, "approved.pdf")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker, err := NewSocket(socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Convert(context.Background(), source, destination); err == nil {
		t.Fatal("Convert accepted forged renderer provenance")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination exists before provenance verification: %v", err)
	}
}

func TestSocketReadyProbesLiveRenderer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "render.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	worker, err := NewSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Ready(); err == nil {
		t.Fatal("Ready accepted a socket path without a live renderer")
	}
}

func TestConvertRunsWorkerWithoutHostServices(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, workspace, `test ! -e /run
test ! -e /home
test ! -e /proc/1/environ
printf x > "$4"
printf '{"converter":"sandbox-test","sha256":"2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881","byteSize":1}\n'`)
	if _, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf")); err != nil {
		t.Fatalf("convert inside service-free sandbox: %v", err)
	}
	if _, err := (Worker{}).Convert(context.Background(), source, filepath.Join(workspace, "unconverted.pdf")); err == nil {
		t.Fatal("conversion continued without its enforceable sandbox")
	}
}

func TestReadyRejectsUnavailableWorker(t *testing.T) {
	if err := (Worker{command: filepath.Join(t.TempDir(), "missing-worker")}).Ready(); err == nil {
		t.Fatal("Ready accepted an unavailable document worker")
	}
}

func TestConvertRejectsSandboxOutputSymlink(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	host, err := os.ReadFile("/etc/hostname")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(host)
	body := fmt.Sprintf(`ln -s /etc/hostname "$4"
printf '{"converter":"test","sha256":"%s","byteSize":%d}\n'`, hex.EncodeToString(digest[:]), len(host))
	worker := testWorker(t, workspace, body)
	if _, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf")); err == nil {
		t.Fatal("Convert accepted a sandbox output symlink")
	}
}

func TestConvertRejectsSandboxOutputFIFO(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, workspace, `mkfifo "$4"
printf '{"converter":"test","sha256":"0000000000000000000000000000000000000000000000000000000000000000","byteSize":1}\n'`)
	if _, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf")); err == nil {
		t.Fatal("Convert accepted a sandbox output FIFO")
	}
}

func TestConvertRejectsForgedOutputDigestAndSize(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, workspace, `printf 'actual PDF' > "$4"
printf '{"converter":"test","sha256":"0000000000000000000000000000000000000000000000000000000000000000","byteSize":10}\n'`)
	if _, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf")); err == nil {
		t.Fatal("Convert accepted forged output digest and size")
	}
}

func TestConvertRejectsExcessiveWorkerOutput(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("actual PDF"))
	worker := testWorker(t, workspace, `printf 'actual PDF' > "$4"
head -c 65537 /dev/zero | tr '\000' ' '
printf '{"converter":"test","sha256":"`+hex.EncodeToString(digest[:])+`","byteSize":10}\n'`)
	_, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf"))
	if err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("Convert error = %v, want bounded worker output failure", err)
	}
}

func TestConvertLimitsWorkerDiagnostics(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := testWorker(t, workspace, `head -c 65537 /dev/zero | tr '\000' x >&2
exit 1`)
	_, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf"))
	if err == nil || !strings.Contains(err.Error(), "output exceeds") || len(err.Error()) > 256 {
		t.Fatalf("Convert error length = %d, want bounded diagnostic failure: %v", len(err.Error()), err)
	}
}

func testWorker(t *testing.T, workspace, body string) Worker {
	t.Helper()
	if _, err := os.Stat(bubblewrapPath); err != nil {
		t.Skipf("bubblewrap unavailable: %v", err)
	}
	path := filepath.Join(workspace, "worker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	worker, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
