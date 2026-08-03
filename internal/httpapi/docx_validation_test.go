package httpapi

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestValidateDOCXAcceptsOnlyExactMinimalOOXMLProfile(t *testing.T) {
	valid := validDOCX(t)
	if err := validateDOCX(valid, fileSize(t, valid)); err != nil {
		t.Fatalf("validate exact DOCX = %v", err)
	}
}

func TestValidateDOCXRejectsStructuralVariants(t *testing.T) {
	tests := map[string]map[string]string{
		"missing content-type override":     {"[Content_Types].xml": contentTypes(``), "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()},
		"wrong content-type namespace":      {"[Content_Types].xml": `<Types xmlns="urn:lookalike"><Override PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"/></Types>`, "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()},
		"namespaced content-type attribute": {"[Content_Types].xml": `<Types xmlns="` + ooxmlContentTypesNS + `" xmlns:x="urn:lookalike"><Override x:PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"/></Types>`, "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()},
		"external office relation":          {"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml" TargetMode="External"`), "word/document.xml": documentXML()},
		"relative office relation":          {"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="./word/document.xml"`), "word/document.xml": documentXML()},
		"wrong office relation type":        {"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": rootRelationships(`Id="rId1" Type="urn:lookalike" Target="word/document.xml"`), "word/document.xml": documentXML()},
		"extra relation":                    {"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": `<Relationships xmlns="` + ooxmlRelationshipsNS + `"><Relationship Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"/><Relationship Id="rId2" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"/></Relationships>`, "word/document.xml": documentXML()},
		"doctype":                           {"[Content_Types].xml": `<!DOCTYPE Types><Types xmlns="` + ooxmlContentTypesNS + `"><Override PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"/></Types>`, "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()},
		"malformed XML":                     {"[Content_Types].xml": `<Types xmlns="` + ooxmlContentTypesNS + `">`, "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()},
	}
	for name, parts := range tests {
		t.Run(name, func(t *testing.T) {
			file := docx(t, parts)
			if err := validateDOCX(file, fileSize(t, file)); err == nil {
				t.Fatal("validate malformed DOCX = nil, want error")
			}
		})
	}
}

func TestValidateDOCXRejectsUnsafeArchiveStructure(t *testing.T) {
	valid := map[string]string{"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()}
	for name, parts := range map[string]map[string]string{
		"extra part":       {"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word/document.xml": valid["word/document.xml"], "word/extra.xml": "x"},
		"traversal":        {"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word/document.xml": valid["word/document.xml"], "word/../evil.xml": "x"},
		"backslash":        {"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word/document.xml": valid["word/document.xml"], `word\evil.xml`: "x"},
		"noncanonical":     {"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word//document.xml": valid["word/document.xml"]},
		"missing document": {"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"]},
	} {
		t.Run(name, func(t *testing.T) {
			file := docx(t, parts)
			if err := validateDOCX(file, fileSize(t, file)); err == nil {
				t.Fatal("validate unsafe DOCX = nil, want error")
			}
		})
	}
}

func validDOCX(t *testing.T) *os.File {
	t.Helper()
	return docx(t, map[string]string{
		"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`),
		"_rels/.rels":         rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`),
		"word/document.xml":   documentXML(),
	})
}

func docx(t *testing.T, parts map[string]string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "report-*.docx")
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, contents := range parts {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return file
}

func fileSize(t *testing.T, file *os.File) int64 {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func contentTypes(attributes string) string {
	return `<Types xmlns="` + ooxmlContentTypesNS + `"><Override ` + attributes + `/></Types>`
}

func rootRelationships(attributes string) string {
	return `<Relationships xmlns="` + ooxmlRelationshipsNS + `"><Relationship ` + attributes + `/></Relationships>`
}

func documentXML() string {
	return `<w:document xmlns:w="` + ooxmlWordNS + `"><w:body/></w:document>`
}

func TestDOCXFixtureIsARealZIP(t *testing.T) {
	file := validDOCX(t)
	contents, err := os.ReadFile(file.Name())
	if err != nil || !bytes.HasPrefix(contents, []byte("PK")) || strings.Contains(string(contents), "doctype") {
		t.Fatalf("fixture must be a ZIP without injected XML directives: %v", err)
	}
}

func TestValidateDOCXRejectsDuplicateMalformedCompressedAndDeepArchives(t *testing.T) {
	valid := map[string]string{"[Content_Types].xml": contentTypes(`PartName="/word/document.xml" ContentType="` + ooxmlDocumentType + `"`), "_rels/.rels": rootRelationships(`Id="rId1" Type="` + ooxmlOfficeDocument + `" Target="word/document.xml"`), "word/document.xml": documentXML()}
	duplicate := docxSequence(t, [][2]string{{"[Content_Types].xml", valid["[Content_Types].xml"]}, {"[Content_Types].xml", valid["[Content_Types].xml"]}, {"_rels/.rels", valid["_rels/.rels"]}, {"word/document.xml", valid["word/document.xml"]}})
	deep := `<w:document xmlns:w="` + ooxmlWordNS + `">` + strings.Repeat(`<w:x>`, maxDOCXXMLDepth) + strings.Repeat(`</w:x>`, maxDOCXXMLDepth) + `</w:document>`
	for name, file := range map[string]*os.File{
		"duplicate part":       duplicate,
		"compressed expansion": docx(t, map[string]string{"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word/document.xml": strings.Repeat(" ", 1<<20) + valid["word/document.xml"]}),
		"deep XML":             docx(t, map[string]string{"[Content_Types].xml": valid["[Content_Types].xml"], "_rels/.rels": valid["_rels/.rels"], "word/document.xml": deep}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDOCX(file, fileSize(t, file)); err == nil {
				t.Fatal("validate invalid DOCX = nil, want error")
			}
		})
	}
	malformed, err := os.CreateTemp(t.TempDir(), "not-a-docx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := malformed.WriteString("not a ZIP"); err != nil {
		t.Fatal(err)
	}
	if err := validateDOCX(malformed, fileSize(t, malformed)); err == nil {
		t.Fatal("validate malformed ZIP = nil, want error")
	}
}

func docxSequence(t *testing.T, parts [][2]string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "report-*.docx")
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, part := range parts {
		entry, err := writer.Create(part[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(part[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}
