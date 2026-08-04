package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

// maxEvidenceBytes bounds one captured file. The supported resource-limit matrix
// is still an open product decision, so the MVP applies this conservative
// ceiling to the stream itself and refuses the capture before anything is
// persisted or uploaded.
const maxEvidenceBytes = 32 << 20

// maxEvidenceFieldBytes bounds the short metadata fields of an upload.
const maxEvidenceFieldBytes = 255

var errEvidenceTooLarge = errors.New("evidence exceeds the accepted size")

// findingEvidence reads and appends the chain of custody of one finding.
func (server Server) findingEvidence(response http.ResponseWriter, request *http.Request) {
	findingID, ok := pathID(request.URL.Path, "/v1/findings/", "/evidence")
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
		items, err := postgres.ListEvidence(request.Context(), server.pool, session, findingID)
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
		server.captureEvidence(response, request, session, findingID)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// captureEvidence writes one capture across two systems that share no
// transaction, in an order that never reports unconfirmed bytes as evidence:
// the server first buffers and hashes the upload, then commits the custody
// metadata as a 'pending' row, then writes the object under the key the database
// derived, and only then commits the 'stored' state. A failure after the
// reservation leaves a pending row and possibly an orphan object, both of which
// remain reconcilable; this is not, and is never reported as, one atomic write.
func (server Server) captureEvidence(response http.ResponseWriter, request *http.Request, session postgres.Session, findingID string) {
	upload, err := readEvidenceUpload(response, request)
	if upload.file != nil {
		defer func() {
			_ = upload.file.Close()
			_ = os.Remove(upload.file.Name())
		}()
	}
	switch {
	case errors.Is(err, errEvidenceTooLarge), errors.Is(err, errMultipartBodyTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "evidence_too_large")
		return
	case err != nil:
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}

	reserved, err := postgres.ReserveEvidence(request.Context(), server.pool, session, findingID, upload.evidence)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := server.evidence.Put(request.Context(), reserved.StorageKey, upload.file, reserved.ByteSize, reserved.SHA256, reserved.DetectedMediaType); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	stored, err := postgres.ConfirmEvidence(request.Context(), server.pool, session, reserved.ID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(response, http.StatusCreated, stored)
}

// evidenceUpload is one buffered capture: the temporary copy of the bytes the
// server read, and the custody metadata it derived from them.
type evidenceUpload struct {
	file     *os.File
	evidence postgres.Evidence
}

// readEvidenceUpload accepts one multipart capture with a required `file` part
// and an optional `capturedAt` field. The client-reported capture instant is
// kept distinct from the instant the server receives the bytes, which the
// database records on its own.
func readEvidenceUpload(response http.ResponseWriter, request *http.Request) (upload evidenceUpload, err error) {
	parts, err := readMultipart(response, request, maxEvidenceBytes)
	if err != nil {
		return evidenceUpload{}, err
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
		case "capturedAt":
			if upload.evidence.CapturedAt != nil {
				return upload, errors.New("a capture reports at most one capture instant")
			}
			value, err := io.ReadAll(io.LimitReader(part, maxEvidenceFieldBytes))
			if err != nil {
				return upload, err
			}
			captured, err := time.Parse(time.RFC3339, strings.TrimSpace(string(value)))
			if err != nil {
				return upload, err
			}
			upload.evidence.CapturedAt = &captured
		case "file":
			if upload.file != nil {
				return upload, errors.New("a capture carries exactly one file part")
			}
			if err := upload.read(part); err != nil {
				return upload, err
			}
		default:
			return upload, fmt.Errorf("unsupported capture field %q", part.FormName())
		}
		if err := part.Close(); err != nil {
			return upload, err
		}
	}
	if upload.file == nil {
		return upload, errors.New("a capture requires one file part")
	}
	return upload, nil
}

// read buffers the uploaded bytes once, under an explicit limit, hashing them as
// they are written. The media type is then detected from the buffered copy
// rather than from the request stream, so detection cannot consume bytes the
// persisted object would be missing, and the digest always describes exactly the
// bytes that are uploaded.
func (upload *evidenceUpload) read(part *multipart.Part) error {
	filename := strings.TrimSpace(part.FileName())
	if !plainMetadata(filename) || filename == "" || strings.ContainsAny(filename, `/\`) || filename == "." || filename == ".." {
		return errors.New("a capture requires a plain file name")
	}
	declared := strings.TrimSpace(part.Header.Get("Content-Type"))
	if !plainMetadata(declared) {
		return errors.New("the declared media type is not a plain header value")
	}
	file, err := os.CreateTemp("", "frameops-evidence-")
	if err != nil {
		return err
	}
	upload.file = file
	digest := sha256.New()
	size, err := io.Copy(file, io.TeeReader(io.LimitReader(part, maxEvidenceBytes+1), digest))
	if err != nil {
		return err
	}
	if size > maxEvidenceBytes {
		return errEvidenceTooLarge
	}
	if size == 0 {
		return errors.New("a capture carries at least one byte")
	}
	header := make([]byte, 512)
	read, err := file.ReadAt(header, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	upload.evidence.Filename = filename
	upload.evidence.DeclaredMediaType = declared
	upload.evidence.DetectedMediaType = http.DetectContentType(header[:read])
	upload.evidence.SHA256 = hex.EncodeToString(digest.Sum(nil))
	upload.evidence.ByteSize = size
	return nil
}

// plainMetadata accepts one short printable client-supplied value that is
// echoed back to other operators.
func plainMetadata(value string) bool {
	if len(value) > maxEvidenceFieldBytes {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
