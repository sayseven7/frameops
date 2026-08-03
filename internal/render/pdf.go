// Package render converts one approved DOCX revision into its PDF by handing
// the bytes to an isolated worker process.
//
// The worker is the only component that runs a document converter, and it is
// deliberately given nothing else: no database URL, no object-storage
// credential, and no inherited environment. It receives two file paths and
// answers with the digest, the size and the identification of the converter that
// read exactly those bytes, which is the provenance recorded with the delivered
// PDF.
package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// workerTimeout bounds one conversion from the API side as well, so a worker
// that never answers cannot hold an HTTP request open.
const workerTimeout = 3 * time.Minute

type Worker struct {
	command string
}

// Result is the provenance of one conversion: the converter that produced the
// PDF and the digest and size of the bytes it wrote.
type Result struct {
	Converter string `json:"converter"`
	SHA256    string `json:"sha256"`
	ByteSize  int64  `json:"byteSize"`
}

// FromEnv reads the document worker configuration from the environment. The API
// refuses to start without it: a PDF is only ever a conversion of an approved
// DOCX, so there is no degraded mode that renders one some other way.
func FromEnv() (Worker, error) {
	return New(os.Getenv("FRAMEOPS_PDF_WORKER"))
}

func New(command string) (Worker, error) {
	if !filepath.IsAbs(command) {
		return Worker{}, errors.New("FRAMEOPS_PDF_WORKER must be the absolute path of the document worker executable")
	}
	info, err := os.Stat(command)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return Worker{}, errors.New("FRAMEOPS_PDF_WORKER must point at an executable document worker")
	}
	return Worker{command: command}, nil
}

// Convert runs the worker over the DOCX at source and writes the PDF to
// destination. The worker inherits no environment: it cannot reach PostgreSQL,
// the object store, or the network even if the document asks it to.
func (worker Worker) Convert(ctx context.Context, source, destination string) (Result, error) {
	if worker.command == "" {
		return Result{}, errors.New("the document worker is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, worker.command, "--source", source, "--destination", destination)
	command.Dir = filepath.Dir(destination)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + command.Dir, "TMPDIR=" + command.Dir}
	var answer, diagnostics strings.Builder
	command.Stdout, command.Stderr = &answer, &diagnostics
	if err := command.Run(); err != nil {
		return Result{}, fmt.Errorf("convert approved report to PDF: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	var result Result
	if err := json.Unmarshal([]byte(answer.String()), &result); err != nil {
		return Result{}, fmt.Errorf("read conversion result: %w", err)
	}
	if len(result.SHA256) != 64 || result.ByteSize <= 0 || strings.TrimSpace(result.Converter) == "" {
		return Result{}, errors.New("the document worker reported an incomplete conversion")
	}
	return result, nil
}
