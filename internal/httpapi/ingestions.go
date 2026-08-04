package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

// errArtifactTooLarge separates an artifact refused for its size from one
// refused for its content, so an operator is told which limit was reached.
var errArtifactTooLarge = errors.New("artifact exceeds the accepted size")

// ingestions reads and appends the tool-import history of one engagement. The
// CLI uploads the artifact exactly as the tool wrote it; parsing, limits and
// validation happen here, at the trust boundary, so no client decides what a
// scan contributed to the inventory.
func (server Server) ingestions(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/ingestions")
	if !ok {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListIngestions(request.Context(), server.pool, session, engagementID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		server.recordIngestion(response, request, session, engagementID)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// recordIngestion imports one artifact. The artifact is read whole and hashed
// before it is parsed, so the digest always describes the exact bytes the tool
// produced, and an artifact that cannot be parsed is refused without recording
// anything: there is no partially imported scan.
func (server Server) recordIngestion(response http.ResponseWriter, request *http.Request, session postgres.Session, engagementID string) {
	upload, err := readIngestionUpload(response, request)
	switch {
	case errors.Is(err, errArtifactTooLarge), errors.Is(err, errMultipartBodyTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "artifact_too_large")
		return
	case err != nil:
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}

	report, err := readNmapReport(bytes.NewReader(upload.artifact))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_nmap_report")
		return
	}

	digest := sha256.Sum256(upload.artifact)
	recorded, err := postgres.RecordIngestion(request.Context(), server.pool, session, engagementID, postgres.Ingestion{
		Tool:          upload.tool,
		FormatVersion: report.FormatVersion,
		Filename:      upload.filename,
		SHA256:        hex.EncodeToString(digest[:]),
		ByteSize:      int64(len(upload.artifact)),
		Summary:       postgres.IngestionSummary{Read: report.Read, Ignored: report.Ignored, Rejected: report.Rejected},
	}, report.Names)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrDuplicate):
		writeJSON(response, http.StatusConflict, map[string]string{"error": "duplicate_artifact", "ingestionId": recorded.ID})
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusCreated, recorded)
	}
}

// ingestionUpload is one buffered artifact and the tool that produced it.
type ingestionUpload struct {
	tool     string
	filename string
	artifact []byte
}

// readIngestionUpload accepts one multipart import with a required `tool` field
// and a required `file` part. Only the tools this build actually parses are
// accepted; an unsupported tool is refused before its bytes are read.
func readIngestionUpload(response http.ResponseWriter, request *http.Request) (upload ingestionUpload, err error) {
	parts, err := readMultipart(response, request, maxIngestionBytes)
	if err != nil {
		return ingestionUpload{}, err
	}
	defer func() { err = normalizeMultipartError(request, err) }()
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return upload, err
		}
		switch part.FormName() {
		case "tool":
			if upload.tool != "" {
				return upload, errors.New("an import names exactly one tool")
			}
			value, err := io.ReadAll(io.LimitReader(part, maxEvidenceFieldBytes))
			if err != nil {
				return upload, err
			}
			if strings.TrimSpace(string(value)) != "nmap" {
				return upload, errors.New("unsupported ingestion tool")
			}
			upload.tool = "nmap"
		case "file":
			if upload.artifact != nil {
				return upload, errors.New("an import carries exactly one file part")
			}
			filename := strings.TrimSpace(part.FileName())
			if !plainMetadata(filename) || filename == "" || strings.ContainsAny(filename, `/\`) || filename == "." || filename == ".." {
				return upload, errors.New("an import requires a plain file name")
			}
			artifact := &bytes.Buffer{}
			size, err := io.Copy(artifact, io.LimitReader(part, maxIngestionBytes+1))
			if err != nil {
				return upload, err
			}
			if size > maxIngestionBytes {
				return upload, errArtifactTooLarge
			}
			if size == 0 {
				return upload, errors.New("an import carries at least one byte")
			}
			upload.filename = filename
			upload.artifact = artifact.Bytes()
		default:
			return upload, fmt.Errorf("unsupported import field %q", part.FormName())
		}
		if err := part.Close(); err != nil {
			return upload, err
		}
	}
	if upload.tool == "" || upload.artifact == nil {
		return upload, errors.New("an import requires one tool and one file part")
	}
	return upload, nil
}
