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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	workerOutput   = 64 << 10
	maxSourceBytes = 32 << 20
)

var errWorkerOutputTooLarge = errors.New("document worker output exceeds the accepted limit")

type limitedBuffer struct {
	strings.Builder
	limit    int
	exceeded bool
}

func (buffer *limitedBuffer) Write(bytes []byte) (int, error) {
	if buffer.Len()+len(bytes) > buffer.limit {
		buffer.exceeded = true
		return 0, errWorkerOutputTooLarge
	}
	return buffer.Builder.Write(bytes)
}

type Worker struct {
	command string
	socket  string
	client  *http.Client
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
	if socket := os.Getenv("FRAMEOPS_PDF_SOCKET"); socket != "" {
		if os.Getenv("FRAMEOPS_PDF_WORKER") != "" {
			return Worker{}, errors.New("configure only one of FRAMEOPS_PDF_SOCKET and FRAMEOPS_PDF_WORKER")
		}
		return NewSocket(socket)
	}
	return New(os.Getenv("FRAMEOPS_PDF_WORKER"))
}

func NewSocket(socket string) (Worker, error) {
	if !filepath.IsAbs(socket) {
		return Worker{}, errors.New("FRAMEOPS_PDF_SOCKET must be an absolute Unix socket path")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return Worker{socket: socket, client: &http.Client{Transport: transport}}, nil
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

func (worker Worker) Ready() error {
	if worker.socket != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://renderer/health", nil)
		response, err := worker.client.Do(request)
		if err != nil {
			return fmt.Errorf("document renderer is unavailable: %w", err)
		}
		defer response.Body.Close() //nolint:errcheck
		if response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("document renderer is unavailable: status %d", response.StatusCode)
		}
		return nil
	}
	for _, path := range []string{worker.command, bubblewrapPath, "/usr/bin/prlimit", "/usr/bin/soffice"} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("document worker runtime is unavailable: %s", path)
		}
	}
	for _, path := range []string{"/usr", "/lib", "/lib64", "/etc/ld.so.cache", "/etc/libreoffice"} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("document worker runtime is unavailable: %s", path)
		}
	}
	return nil
}

func (worker Worker) Convert(ctx context.Context, source, destination string) (Result, error) {
	if worker.socket != "" {
		return worker.convertSocket(ctx, source, destination)
	}
	return worker.convertProcess(ctx, source, destination)
}

func (worker Worker) convertSocket(ctx context.Context, source, destination string) (Result, error) {
	input, err := os.Open(source)
	if err != nil {
		return Result{}, fmt.Errorf("read source document: %w", err)
	}
	defer input.Close() //nolint:errcheck
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxSourceBytes {
		return Result{}, errors.New("the source document is empty, invalid, or larger than the accepted size")
	}
	ctx, cancel := context.WithTimeout(ctx, workerTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://renderer/render", io.NopCloser(io.LimitReader(input, maxSourceBytes+1)))
	if err != nil {
		return Result{}, fmt.Errorf("create render request: %w", err)
	}
	request.ContentLength = info.Size()
	request.Header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	response, err := worker.client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("render approved report: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusOK {
		diagnostic, _ := io.ReadAll(io.LimitReader(response.Body, workerOutput))
		return Result{}, fmt.Errorf("render approved report: status %d: %s", response.StatusCode, strings.TrimSpace(string(diagnostic)))
	}
	result := Result{Converter: response.Header.Get("X-Frameops-Converter"), SHA256: response.Header.Get("X-Frameops-SHA256")}
	result.ByteSize, err = strconv.ParseInt(response.Header.Get("X-Frameops-Byte-Size"), 10, 64)
	decoded, digestErr := hex.DecodeString(result.SHA256)
	if err != nil || digestErr != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != result.SHA256 || result.ByteSize <= 0 || result.ByteSize > converterFile || strings.TrimSpace(result.Converter) == "" || len(result.Converter) > 1024 {
		return Result{}, errors.New("the document renderer reported invalid provenance")
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".frameops-pdf-")
	if err != nil {
		return Result{}, fmt.Errorf("stage converted PDF: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName) //nolint:errcheck
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return Result{}, fmt.Errorf("stage converted PDF: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(response.Body, converterFile+1))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || size != result.ByteSize || size > converterFile || hex.EncodeToString(digest.Sum(nil)) != result.SHA256 {
		return Result{}, errors.New("the document renderer returned bytes that do not match their provenance")
	}
	staged, err := os.Open(temporaryName)
	if err != nil {
		return Result{}, fmt.Errorf("read verified PDF: %w", err)
	}
	defer staged.Close() //nolint:errcheck
	pdf, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("write converted PDF: %w", err)
	}
	complete := false
	defer func() {
		_ = pdf.Close()
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(pdf, staged); err != nil {
		return Result{}, fmt.Errorf("write converted PDF: %w", err)
	}
	if err := pdf.Close(); err != nil {
		return Result{}, fmt.Errorf("write converted PDF: %w", err)
	}
	complete = true
	return result, nil
}

// Convert runs the local worker over the DOCX at source and writes the PDF to
// destination. This process mode remains available for non-Compose development.
func (worker Worker) convertProcess(ctx context.Context, source, destination string) (Result, error) {
	if err := worker.Ready(); err != nil {
		return Result{}, err
	}
	var info os.FileInfo
	var err error
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
	answer := limitedBuffer{limit: workerOutput}
	diagnostics := limitedBuffer{limit: workerOutput}
	command.Stdout, command.Stderr = &answer, &diagnostics
	if err := command.Run(); err != nil {
		if answer.exceeded || diagnostics.exceeded {
			return Result{}, errWorkerOutputTooLarge
		}
		return Result{}, fmt.Errorf("convert approved report to PDF: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	if answer.exceeded || diagnostics.exceeded {
		return Result{}, errWorkerOutputTooLarge
	}
	var result Result
	if err := json.Unmarshal([]byte(answer.String()), &result); err != nil {
		return Result{}, fmt.Errorf("read conversion result: %w", err)
	}
	if len(result.SHA256) != 64 || result.ByteSize <= 0 || strings.TrimSpace(result.Converter) == "" {
		return Result{}, errors.New("the document worker reported an incomplete conversion")
	}
	converted, err := os.OpenFile(filepath.Join(output, destinationName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Result{}, fmt.Errorf("read sandboxed PDF: %w", err)
	}
	defer converted.Close() //nolint:errcheck
	info, err = converted.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Result{}, errors.New("the document worker produced an invalid PDF output")
	}
	pdf, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("write converted PDF: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(pdf, digest), io.LimitReader(converted, converterFile+1))
	if closeErr := pdf.Close(); err == nil {
		err = closeErr
	}
	if err != nil || size > converterFile || size != result.ByteSize || hex.EncodeToString(digest.Sum(nil)) != result.SHA256 {
		return Result{}, errors.New("the document worker produced an incomplete PDF")
	}
	complete = true
	return result, nil
}
