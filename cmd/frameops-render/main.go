// Package main is the isolated FrameOPS document worker. It converts one DOCX
// file to one PDF file and nothing else: it never opens the database, never
// speaks to the object store, and runs the converter inside a network namespace
// with no interfaces, so a document that tries to reach the network during
// conversion cannot. The caller hands it two paths and reads back the digest,
// the size and the converter identification the conversion produced.
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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// maxSourceBytes matches the largest DOCX revision the API accepts, and
	// maxOutputBytes bounds what a converted document may expand into.
	maxSourceBytes = 32 << 20
	maxOutputBytes = 64 << 20
	convertTimeout = 2 * time.Minute
	pdfMagic       = "%PDF-"
)

// allowedEnvironment is the closed set of variables the worker may run with. Any
// other variable is refused rather than ignored: a worker that can see a
// database URL or an object-storage key is no longer isolated, and failing at
// start-up is the only way to notice that the boundary was crossed.
var allowedEnvironment = map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true, "TZ": true, "PWD": true}

func main() {
	if err := run(os.Args[1:], os.Environ(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args, environment []string, out io.Writer) error {
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

	converter, err := converterVersion(workspace)
	if err != nil {
		return err
	}
	produced, err := convert(workspace, *source)
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

// isolate runs one converter invocation inside an empty network namespace. The
// worker fails closed: when the namespace cannot be created there is no
// conversion, and therefore no artifact that could be mistaken for a delivered
// PDF. Converter diagnostics are kept to their final line, so a failure is
// reported without echoing an unbounded amount of converter output.
func isolate(workspace string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), convertTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "unshare", append([]string{"--net", "--map-root-user", "--"}, arguments...)...)
	command.Dir = workspace
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + workspace, "TMPDIR=" + workspace}
	var output strings.Builder
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the conversion did not finish within the accepted time")
		}
		return "", fmt.Errorf("%w: %s", err, lastLine(output.String()))
	}
	return output.String(), nil
}

func converterVersion(workspace string) (string, error) {
	output, err := isolate(workspace, "soffice", "--headless", "-env:UserInstallation=file://"+filepath.Join(workspace, "profile"), "--version")
	if err != nil {
		return "", fmt.Errorf("read converter version: %w", err)
	}
	version := lastLine(output)
	if version == "" {
		return "", errors.New("the converter did not report a version")
	}
	return version, nil
}

func convert(workspace, source string) (string, error) {
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
	if _, err := isolate(workspace, "soffice", "--headless", "--norestore", "--nolockcheck", "--nodefault",
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
