package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

const (
	maxDOCXEntries        = 3
	maxDOCXExpandedBytes  = 128 << 20
	maxDOCXXMLPartBytes   = 8 << 20
	maxDOCXExpansionRatio = 100
	maxDOCXXMLDepth       = 64
	ooxmlContentTypesNS   = "http://schemas.openxmlformats.org/package/2006/content-types"
	ooxmlRelationshipsNS  = "http://schemas.openxmlformats.org/package/2006/relationships"
	ooxmlWordNS           = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	ooxmlDocumentType     = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	ooxmlOfficeDocument   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
)

var errInvalidDOCX = errors.New("report is not an accepted DOCX archive")

// validateDOCX accepts the smallest OOXML package FrameOPS imports: the content
// types part, the package root relationship, and the main document part. A
// closed profile removes alternate paths and relationship graphs from this
// untrusted archive boundary.
func validateDOCX(file *os.File, size int64) error {
	archive, err := zip.NewReader(file, size)
	if err != nil || len(archive.File) != maxDOCXEntries {
		return errInvalidDOCX
	}

	parts := map[string]bool{
		"[Content_Types].xml": false,
		"_rels/.rels":         false,
		"word/document.xml":   false,
	}
	var expanded uint64
	for _, entry := range archive.File {
		if !canonicalDOCXName(entry.Name) || entry.FileInfo().IsDir() {
			return errInvalidDOCX
		}
		seen, allowed := parts[entry.Name]
		if !allowed || seen || entry.UncompressedSize64 > maxDOCXExpandedBytes || exceedsDOCXRatio(entry) {
			return errInvalidDOCX
		}
		parts[entry.Name] = true
		expanded += entry.UncompressedSize64
		if expanded > maxDOCXExpandedBytes {
			return errInvalidDOCX
		}

		contents, err := readDOCXXML(entry)
		if err != nil || !validDOCXPart(entry.Name, contents) {
			return errInvalidDOCX
		}
	}
	for _, present := range parts {
		if !present {
			return errInvalidDOCX
		}
	}
	return nil
}

func canonicalDOCXName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `\\`) && !strings.ContainsRune(name, 0) && !strings.HasPrefix(name, "/") && path.Clean(name) == name
}

func exceedsDOCXRatio(entry *zip.File) bool {
	return entry.UncompressedSize64 > entry.CompressedSize64*maxDOCXExpansionRatio
}

func readDOCXXML(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close() //nolint:errcheck
	contents, err := io.ReadAll(io.LimitReader(reader, maxDOCXXMLPartBytes+1))
	if err != nil || len(contents) > maxDOCXXMLPartBytes {
		return nil, errInvalidDOCX
	}
	return contents, nil
}

func validDOCXPart(name string, contents []byte) bool {
	switch name {
	case "[Content_Types].xml":
		return validOOXMLRelationship(contents,
			xml.Name{Space: ooxmlContentTypesNS, Local: "Types"},
			xml.Name{Space: ooxmlContentTypesNS, Local: "Override"},
			map[string]string{"PartName": "/word/document.xml", "ContentType": ooxmlDocumentType})
	case "_rels/.rels":
		return validOOXMLRelationship(contents,
			xml.Name{Space: ooxmlRelationshipsNS, Local: "Relationships"},
			xml.Name{Space: ooxmlRelationshipsNS, Local: "Relationship"},
			map[string]string{"Type": ooxmlOfficeDocument, "Target": "word/document.xml"}, "Id")
	case "word/document.xml":
		return validOOXMLDocument(contents)
	default:
		return false
	}
}

func validOOXMLRelationship(contents []byte, root, child xml.Name, expected map[string]string, optional ...string) bool {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	depth, children := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return depth == 0 && children == 1
		}
		if err != nil {
			return false
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				if token.Name != root || !declaresOnlyNamespace(token.Attr, root.Space) {
					return false
				}
				continue
			}
			if depth != 2 || token.Name != child || !exactOOXMLAttributes(token.Attr, expected, optional...) {
				return false
			}
			children++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return false
			}
		case xml.CharData:
			if strings.TrimSpace(string(token)) != "" {
				return false
			}
		default:
			return false
		}
	}
}

func declaresOnlyNamespace(attributes []xml.Attr, namespace string) bool {
	return len(attributes) == 1 && attributes[0].Name == (xml.Name{Local: "xmlns"}) && attributes[0].Value == namespace
}

func exactOOXMLAttributes(attributes []xml.Attr, expected map[string]string, optional ...string) bool {
	optionalSet := make(map[string]bool, len(optional))
	for _, name := range optional {
		optionalSet[name] = true
	}
	if len(attributes) < len(expected) || len(attributes) > len(expected)+len(optionalSet) {
		return false
	}
	seen := make(map[string]bool, len(attributes))
	for _, attribute := range attributes {
		if attribute.Name.Space != "" || seen[attribute.Name.Local] {
			return false
		}
		seen[attribute.Name.Local] = true
		if value, ok := expected[attribute.Name.Local]; ok {
			if attribute.Value != value {
				return false
			}
			continue
		}
		if !optionalSet[attribute.Name.Local] || attribute.Value == "" {
			return false
		}
	}
	for name := range expected {
		if !seen[name] {
			return false
		}
	}
	return true
}

func validOOXMLDocument(contents []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return depth == 0
		}
		if err != nil {
			return false
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth > maxDOCXXMLDepth || depth == 1 && token.Name != (xml.Name{Space: ooxmlWordNS, Local: "document"}) {
				return false
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				return false
			}
		case xml.CharData:
			// Character data is allowed in the document part only.
		default:
			return false
		}
	}
}
