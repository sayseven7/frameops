package reportdocx

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
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
	if len(archive.File) != 3 {
		t.Fatalf("generated DOCX has %d parts, want 3", len(archive.File))
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
