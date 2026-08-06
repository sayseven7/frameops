package reportdocx

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sayseven7/frameops/internal/render"
)

func TestGenerateIsDeterministicAndIncludesStructuredSource(t *testing.T) {
	source := Source{
		TemplateVersion: "frameops-structured-v1",
		ClientName:      "Acme",
		EngagementName:  "External assessment",
		Scope:           []string{"app.example.test"},
		Methodology:     []string{"Authorization: replay request"},
		Findings: []Finding{{
			Title: "Missing tenant authorization", CVSS: "9.8", Evidence: []string{"request.png"}, Retests: []string{"Round 1: fixed"},
		}},
	}

	first, err := Generate(source)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := Generate(source)
	if err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Generate() produced different bytes for the same source")
	}

	archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatalf("open generated DOCX = %v", err)
	}
	for _, part := range archive.File {
		if part.Name != "word/document.xml" {
			continue
		}
		reader, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		document, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Acme", "app.example.test", "Authorization: replay request", "Missing tenant authorization", "CVSS 9.8", "request.png", "Round 1: fixed", "frameops-structured-v1"} {
			if !bytes.Contains(document, []byte(want)) {
				t.Errorf("generated document does not contain %q", want)
			}
		}
	}
}

func TestGenerateUsesLibreOfficeCompatibleOOXMLProfile(t *testing.T) {
	docx, err := Generate(Source{})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatalf("open generated DOCX = %v", err)
	}
	parts := map[string][]byte{}
	for _, part := range archive.File {
		reader, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		parts[part.Name], err = io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range [][]byte{
		[]byte(`Default Extension="xml" ContentType="application/xml"`),
		[]byte(`Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"`),
	} {
		if !bytes.Contains(parts["[Content_Types].xml"], want) {
			t.Errorf("content types missing %q", want)
		}
	}
	if !bytes.Contains(parts["word/document.xml"], []byte(`<w:sectPr/>`)) {
		t.Error("document is missing the required section properties")
	}
}

func TestGenerateConvertsWithRealLibreOfficeWorker(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skipf("soffice unavailable: %v", err)
	}
	if _, err := os.Stat("/usr/bin/bwrap"); err != nil {
		t.Skipf("bubblewrap unavailable: %v", err)
	}
	workspace := t.TempDir()
	source := filepath.Join(workspace, "approved.docx")
	docx, err := Generate(Source{ClientName: "Acme", EngagementName: "Assessment"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := os.WriteFile(source, docx, 0o600); err != nil {
		t.Fatal(err)
	}
	workerPath := filepath.Join(workspace, "frameops-render")
	if output, err := exec.Command("go", "build", "-o", workerPath, "../../cmd/frameops-render").CombinedOutput(); err != nil {
		t.Fatalf("build real document worker: %v: %s", err, output)
	}
	worker, err := render.New(workerPath)
	if err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(workspace, "approved.pdf")
	if _, err := worker.Convert(t.Context(), source, pdfPath); err != nil {
		t.Fatalf("convert generated DOCX with real worker: %v", err)
	}
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("real worker did not produce a PDF")
	}
}
