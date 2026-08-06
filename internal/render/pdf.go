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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// workerTimeout bounds one conversion from the API side as well, so a worker
// that never answers cannot hold an HTTP request open.
const workerTimeout = 3 * time.Minute

const (
	sandboxRoot    = "/workspace"
	sandboxSource  = "/source.docx"
	sandboxWorker  = "/frameops-render"
	sandboxOutput  = "/output"
	bubblewrapPath = "/usr/bin/bwrap"
	converterCPU   = 120
	converterFile  = 64 << 20
	converterFiles = 128
)

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
// destination. Bubblewrap gives the worker only the source, destination and
// system libraries it needs; its empty network namespace and filesystem keep it
// from reaching PostgreSQL, object storage or host credentials.
func (worker Worker) Convert(ctx context.Context, source, destination string) (Result, error) {
	if worker.command == "" {
		return Result{}, errors.New("the document worker is not configured")
	}
	info, err := os.Stat(bubblewrapPath)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return Result{}, errors.New("the document worker requires bubblewrap isolation")
	}
	output, err := os.MkdirTemp("", "frameops-pdf-output-")
	if err != nil {
		return Result{}, fmt.Errorf("create conversion output workspace: %w", err)
	}
	defer os.RemoveAll(output) //nolint:errcheck
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	destinationName := filepath.Base(destination)
	command := exec.CommandContext(ctx, bubblewrapPath,
		"--unshare-all", "--die-with-parent", "--new-session", "--clearenv",
		"--setenv", "PATH", "/usr/bin",
		"--setenv", "HOME", sandboxRoot,
		"--setenv", "TMPDIR", sandboxRoot,
		"--setenv", "LANG", "C.UTF-8",
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		"--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache",
		"--ro-bind", "/etc/libreoffice", "/etc/libreoffice",
		"--proc", "/proc", "--dev", "/dev",
		"--size", fmt.Sprint(128<<20), "--tmpfs", sandboxRoot,
		"--size", fmt.Sprint(32<<20), "--tmpfs", "/tmp", "--dir", "/var", "--size", fmt.Sprint(32<<20), "--tmpfs", "/var/tmp",
		"--ro-bind", source, sandboxSource,
		"--dir", sandboxOutput, "--bind", output, sandboxOutput,
		"--ro-bind", worker.command, sandboxWorker,
		"--chdir", sandboxRoot,
		"--", "/usr/bin/prlimit",
		fmt.Sprintf("--cpu=%d", converterCPU),
		fmt.Sprintf("--fsize=%d", converterFile),
		fmt.Sprintf("--nofile=%d", converterFiles),
		"--", sandboxWorker, "--source", sandboxSource, "--destination", filepath.Join(sandboxOutput, destinationName),
	)
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
	converted, err := os.Open(filepath.Join(output, destinationName))
	if err != nil {
		return Result{}, fmt.Errorf("read sandboxed PDF: %w", err)
	}
	defer converted.Close() //nolint:errcheck
	pdf, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("write converted PDF: %w", err)
	}
	size, err := io.Copy(pdf, converted)
	if closeErr := pdf.Close(); err == nil {
		err = closeErr
	}
	if err != nil || size != result.ByteSize {
		return Result{}, errors.New("the document worker produced an incomplete PDF")
	}
	return result, nil
}
