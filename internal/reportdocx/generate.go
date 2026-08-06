// Package reportdocx generates the closed minimal OOXML profile accepted by the
// report ingestion boundary. Keeping the package free of database, object-store,
// network, and clock access makes the same structured source reproducible.
package reportdocx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"time"
)

const templateVersion = "frameops-structured-v1"

// Source is the normalized, ordered report content snapshot.
type Source struct {
	TemplateVersion string
	ClientName      string
	EngagementName  string
	Scope           []string
	Methodology     []string
	Findings        []Finding
}

type Finding struct {
	Title    string
	CVSS     string
	Evidence []string
	Retests  []string
}

// Generate serializes a source snapshot into a deterministic DOCX archive.
func Generate(source Source) ([]byte, error) {
	if source.TemplateVersion == "" {
		source.TemplateVersion = templateVersion
	}
	var document bytes.Buffer
	document.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range lines(source) {
		document.WriteString(`<w:p><w:r><w:t>`)
		if err := xml.EscapeText(&document, []byte(line)); err != nil {
			return nil, fmt.Errorf("escape report text: %w", err)
		}
		document.WriteString(`</w:t></w:r></w:p>`)
	}
	document.WriteString(`<w:sectPr/></w:body></w:document>`)

	parts := [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", document.String()},
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, part := range parts {
		contents := []byte(part[1])
		header := &zip.FileHeader{Name: part[0], Method: zip.Store, Modified: time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC), CRC32: crc32.ChecksumIEEE(contents), CompressedSize64: uint64(len(contents)), UncompressedSize64: uint64(len(contents))}
		entry, err := writer.CreateRaw(header)
		if err != nil {
			return nil, fmt.Errorf("create DOCX part: %w", err)
		}
		if _, err := entry.Write(contents); err != nil {
			return nil, fmt.Errorf("write DOCX part: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close DOCX: %w", err)
	}
	return archive.Bytes(), nil
}

func lines(source Source) []string {
	lines := []string{"FrameOPS report", "Template: " + source.TemplateVersion, "Client: " + source.ClientName, "Engagement: " + source.EngagementName, "Scope"}
	lines = append(lines, source.Scope...)
	lines = append(lines, "Methodology")
	lines = append(lines, source.Methodology...)
	lines = append(lines, "Findings")
	for _, finding := range source.Findings {
		lines = append(lines, finding.Title, "CVSS "+finding.CVSS, "Evidence")
		lines = append(lines, finding.Evidence...)
		lines = append(lines, "Retests")
		lines = append(lines, finding.Retests...)
	}
	return lines
}
