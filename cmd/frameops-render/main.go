// Package main is the FrameOPS document renderer. In Compose it listens only on
// a Unix socket in a networkless, credential-free sidecar; local process mode is
// retained for development. It converts one DOCX file to one PDF file and
// returns the digest, size, and converter identification.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// maxSourceBytes matches the largest DOCX revision the API accepts, and
	// maxOutputBytes bounds what a converted document may expand into.
	maxSourceBytes       = 32 << 20
	maxOutputBytes       = 64 << 20
	converterOutputLimit = 64 << 10
	convertTimeout       = 2 * time.Minute
	pdfMagic             = "%PDF-"
)

var (
	conversionSlot             = make(chan struct{}, 1)
	errConverterOutputTooLarge = errors.New("converter output exceeds the accepted limit")
)

type tailBuffer struct {
	bytes    []byte
	limit    int
	exceeded bool
}

func (buffer *tailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if written >= buffer.limit {
		hadOutput := len(buffer.bytes) > 0
		buffer.bytes = append(buffer.bytes[:0], p[written-buffer.limit:]...)
		buffer.exceeded = buffer.exceeded || hadOutput || written > buffer.limit
		return written, nil
	}
	if overflow := len(buffer.bytes) + written - buffer.limit; overflow > 0 {
		copy(buffer.bytes, buffer.bytes[overflow:])
		buffer.bytes = buffer.bytes[:len(buffer.bytes)-overflow]
		buffer.exceeded = true
	}
	buffer.bytes = append(buffer.bytes, p...)
	return written, nil
}

func (buffer *tailBuffer) String() string { return string(buffer.bytes) }

// allowedEnvironment is the closed set of variables the worker may run with. Any
// other variable is refused rather than ignored: a worker that can see a
// database URL or an object-storage key is no longer isolated, and failing at
// start-up is the only way to notice that the boundary was crossed.
var allowedEnvironment = map[string]bool{"PATH": true, "HOME": true, "HOSTNAME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "TZ": true, "PWD": true, "FRAMEOPS_RENDER_SOCKET": true}

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "--serve":
		err = serveSocket(os.Getenv("FRAMEOPS_RENDER_SOCKET"))
	case len(os.Args) == 2 && os.Args[1] == "--healthcheck":
		err = checkSocket(os.Getenv("FRAMEOPS_RENDER_SOCKET"))
	default:
		err = run(context.Background(), os.Args[1:], os.Environ(), os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serveSocket(socket string) error {
	if err := isolatedEnvironment(os.Environ()); err != nil {
		return err
	}
	if !filepath.IsAbs(socket) {
		return errors.New("FRAMEOPS_RENDER_SOCKET must be an absolute Unix socket path")
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing to replace a non-socket renderer path")
		}
		if err := os.Remove(socket); err != nil {
			return fmt.Errorf("remove stale renderer socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect renderer socket: %w", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on renderer socket: %w", err)
	}
	defer listener.Close()  //nolint:errcheck
	defer os.Remove(socket) //nolint:errcheck
	if err := os.Chmod(socket, 0o600); err != nil {
		return fmt.Errorf("protect renderer socket: %w", err)
	}
	return (&http.Server{Handler: http.HandlerFunc(renderHandler), ReadHeaderTimeout: 5 * time.Second}).Serve(listener)
}

func checkSocket(socket string) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	response, err := client.Get("http://renderer/health")
	if err != nil {
		return err
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("renderer health status %d", response.StatusCode)
	}
	return nil
}

func renderHandler(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/health" {
		workspace, err := os.MkdirTemp("", "frameops-render-health-")
		if err == nil {
			defer os.RemoveAll(workspace) //nolint:errcheck
			_, err = converterVersion(request.Context(), workspace)
		}
		if err != nil {
			http.Error(response, "renderer unavailable", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost || request.URL.Path != "/render" {
		http.NotFound(response, request)
		return
	}
	if request.ContentLength > maxSourceBytes {
		http.Error(response, "document too large", http.StatusRequestEntityTooLarge)
		return
	}
	select {
	case conversionSlot <- struct{}{}:
		defer func() { <-conversionSlot }()
	default:
		http.Error(response, "renderer busy", http.StatusServiceUnavailable)
		return
	}
	workspace, err := os.MkdirTemp("", "frameops-render-request-")
	if err != nil {
		http.Error(response, "create conversion workspace", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(workspace) //nolint:errcheck
	source, destination := filepath.Join(workspace, "source.docx"), filepath.Join(workspace, "output.pdf")
	input, err := os.OpenFile(source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		http.Error(response, "create source document", http.StatusInternalServerError)
		return
	}
	size, copyErr := io.Copy(input, io.LimitReader(request.Body, maxSourceBytes+1))
	closeErr := input.Close()
	if size > maxSourceBytes {
		http.Error(response, "document too large", http.StatusRequestEntityTooLarge)
		return
	}
	if copyErr != nil || closeErr != nil || size == 0 {
		http.Error(response, "invalid source document", http.StatusBadRequest)
		return
	}
	var answer strings.Builder
	if err := run(request.Context(), []string{"--source", source, "--destination", destination}, os.Environ(), &answer); err != nil {
		http.Error(response, "conversion failed", http.StatusUnprocessableEntity)
		return
	}
	var result struct {
		Converter string `json:"converter"`
		SHA256    string `json:"sha256"`
		ByteSize  int64  `json:"byteSize"`
	}
	if err := json.Unmarshal([]byte(answer.String()), &result); err != nil {
		http.Error(response, "invalid conversion provenance", http.StatusInternalServerError)
		return
	}
	output, err := os.Open(destination)
	if err != nil {
		http.Error(response, "read converted PDF", http.StatusInternalServerError)
		return
	}
	defer output.Close() //nolint:errcheck
	response.Header().Set("Content-Type", "application/pdf")
	response.Header().Set("X-Frameops-Converter", result.Converter)
	response.Header().Set("X-Frameops-SHA256", result.SHA256)
	response.Header().Set("X-Frameops-Byte-Size", strconv.FormatInt(result.ByteSize, 10))
	_, _ = io.Copy(response, io.LimitReader(output, maxOutputBytes+1))
}

func run(ctx context.Context, args, environment []string, out io.Writer) error {
	flags := flag.NewFlagSet("frameops-render", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "path of the DOCX file to convert")
	destination := flags.String("destination", "", "path the converted PDF is written to")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("usage: frameops-render --source FILE --destination FILE")
	}
	if *source == "" || *destination == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: frameops-render --source FILE --destination FILE")
	}
	if err := isolatedEnvironment(environment); err != nil {
		return err
	}

	workspace, err := os.MkdirTemp("", "frameops-render-")
	if err != nil {
		return fmt.Errorf("create conversion workspace: %w", err)
	}
	defer os.RemoveAll(workspace) //nolint:errcheck

	ctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	converter, err := converterVersion(ctx, workspace)
	if err != nil {
		return err
	}
	produced, err := convert(ctx, workspace, *source)
	if err != nil {
		return err
	}
	digest, size, err := copyPDF(produced, *destination)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"converter": converter, "sha256": digest, "byteSize": size})
}

func isolatedEnvironment(environment []string) error {
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !allowedEnvironment[name] {
			return fmt.Errorf("the document worker refuses to run with %s in its environment: it must reach no database, object store, or service", name)
		}
	}
	return nil
}

// isolate runs one converter invocation inside the caller's empty sandbox. The
// worker fails closed: when the sandbox filesystem is not present there is no
// conversion, and therefore no artifact that could be mistaken for a delivered
// PDF. Converter diagnostics are kept to a bounded tail, so a failure is
// reported without buffering an unbounded amount of converter output.
func isolate(ctx context.Context, workspace string, arguments ...string) (string, error) {
	if os.Getenv("FRAMEOPS_RENDER_SOCKET") == "" {
		for _, path := range []string{"/run", "/home"} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				return "", errors.New("the document worker requires the outer sandbox")
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, arguments[0], arguments[1:]...)
	command.Dir = workspace
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + workspace, "TMPDIR=" + workspace}
	output := tailBuffer{limit: converterOutputLimit}
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		if output.exceeded {
			return output.String(), errConverterOutputTooLarge
		}
		if err := ctx.Err(); err != nil {
			return output.String(), err
		}
		return output.String(), fmt.Errorf("%w: %s", err, lastLine(output.String()))
	}
	if output.exceeded {
		return output.String(), errConverterOutputTooLarge
	}
	return output.String(), nil
}

func converterVersion(ctx context.Context, workspace string) (string, error) {
	output, err := isolate(ctx, workspace, "soffice", "--headless", "-env:UserInstallation=file://"+filepath.Join(workspace, "profile"), "--version")
	if err != nil {
		return "", fmt.Errorf("read converter version: %w", err)
	}
	version := lastLine(output)
	if version == "" {
		return "", errors.New("the converter did not report a version")
	}
	return version, nil
}

func convert(ctx context.Context, workspace, source string) (string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("read source document: %w", err)
	}
	if info.Size() == 0 || info.Size() > maxSourceBytes {
		return "", errors.New("the source document is empty or larger than the accepted size")
	}
	output := filepath.Join(workspace, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		return "", fmt.Errorf("create conversion output directory: %w", err)
	}
	if _, err := isolate(ctx, workspace, "soffice", "--headless", "--norestore", "--nolockcheck", "--nodefault",
		"-env:UserInstallation=file://"+filepath.Join(workspace, "profile"), "--convert-to", "pdf", "--outdir", output, source); err != nil {
		return "", fmt.Errorf("convert document to PDF: %w", err)
	}
	produced := filepath.Join(output, strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))+".pdf")
	if _, err := os.Stat(produced); err != nil {
		return "", errors.New("the converter produced no PDF")
	}
	return produced, nil
}

// copyPDF writes the converted bytes to their destination and reports the digest
// the caller records as provenance, computed over exactly the bytes written.
func copyPDF(produced, destination string) (string, int64, error) {
	reader, err := os.Open(produced)
	if err != nil {
		return "", 0, fmt.Errorf("read converted PDF: %w", err)
	}
	defer reader.Close() //nolint:errcheck
	magic := make([]byte, len(pdfMagic))
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != pdfMagic {
		return "", 0, errors.New("the converter produced a file that is not a PDF")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("read converted PDF: %w", err)
	}
	writer, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("write converted PDF: %w", err)
	}
	defer writer.Close() //nolint:errcheck
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(writer, digest), io.LimitReader(reader, maxOutputBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("write converted PDF: %w", err)
	}
	if size > maxOutputBytes {
		return "", 0, errors.New("the converted PDF is larger than the accepted size")
	}
	if err := writer.Close(); err != nil {
		return "", 0, fmt.Errorf("write converted PDF: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
