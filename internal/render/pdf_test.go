package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConvertRunsWorkerWithoutHostServices(t *testing.T) {
	workerPath := filepath.Join(t.TempDir(), "worker")
	if err := os.WriteFile(workerPath, []byte(`#!/usr/bin/sh
set -eu
test ! -e /run
test ! -e /home
interfaces=0
while IFS= read -r line; do
  case "$line" in *:*) interfaces=$((interfaces + 1));; esac
done </proc/net/dev
test "$interfaces" = 1
printf 'x' > "$4"
printf '{"converter":"sandbox-test","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","byteSize":1}\n'
`), 0o700); err != nil {
		t.Fatalf("write worker: %v", err)
	}
	worker, err := New(workerPath)
	if err != nil {
		t.Fatalf("configure worker: %v", err)
	}
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	if err := os.WriteFile(source, []byte("approved"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := worker.Convert(context.Background(), source, filepath.Join(workspace, "approved.pdf")); err != nil {
		t.Fatalf("convert inside service-free sandbox: %v", err)
	}
	if _, err := (Worker{}).Convert(context.Background(), source, filepath.Join(workspace, "unconverted.pdf")); err == nil {
		t.Fatal("conversion continued without its enforceable sandbox")
	}
}
