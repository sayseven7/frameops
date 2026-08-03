package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sessionCookieName is the cookie the API issues. It is a `__Host-` cookie and
// therefore marked Secure, which is why the CLI refuses a plaintext API URL
// outside loopback rather than replaying the session over an open network.
const sessionCookieName = "__Host-frameops_session"

// apiClient never follows a redirect: the session cookie is attached by hand to
// every request, and a redirect could carry it to a host the operator did not
// name.
var apiClient = &http.Client{
	Timeout: 2 * time.Minute,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("the API answered with a redirect, which is not followed")
	},
}

// ingestion is the record the API returns for one imported artifact.
type ingestion struct {
	ID            string `json:"id"`
	EngagementID  string `json:"engagementId"`
	Tool          string `json:"tool"`
	FormatVersion string `json:"formatVersion"`
	Filename      string `json:"filename"`
	SHA256        string `json:"sha256"`
	ByteSize      int64  `json:"byteSize"`
	Summary       struct {
		Read     int `json:"read"`
		Created  int `json:"created"`
		Reused   int `json:"reused"`
		Ignored  int `json:"ignored"`
		Rejected int `json:"rejected"`
	} `json:"summary"`
}

// baseURL accepts the address of one FrameOPS API. Plaintext HTTP is accepted
// only for loopback, where it is the local development server; anywhere else it
// would publish the session cookie.
func baseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid API URL: %w", err)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("the API URL must be a plain scheme://host[/path] address")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !loopback(parsed.Hostname()) {
			return "", errors.New("the API URL must use https outside loopback, because the session cookie is Secure")
		}
	default:
		return "", errors.New("the API URL must use https, or http on loopback")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// authenticate signs in through the API and returns the session it issued.
func authenticate(base, email, password string) (string, error) {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return "", fmt.Errorf("encode credentials: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/v1/session/login", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build sign-in request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := apiClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("reach the API: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusNoContent {
		return "", apiError(response)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie.Value, nil
		}
	}
	return "", errors.New("the API accepted the credentials without issuing a session")
}

// uploadIngestion sends one artifact through the API. The CSRF token is issued
// for the stored session immediately before the upload, so the CLI holds no
// second credential between commands.
func uploadIngestion(stored storedSession, engagementID, filename string, artifact []byte) (ingestion, error) {
	token, err := csrfToken(stored)
	if err != nil {
		return ingestion{}, err
	}
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("tool", "nmap"); err != nil {
		return ingestion{}, fmt.Errorf("build upload: %w", err)
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return ingestion{}, fmt.Errorf("build upload: %w", err)
	}
	if _, err := part.Write(artifact); err != nil {
		return ingestion{}, fmt.Errorf("build upload: %w", err)
	}
	if err := form.Close(); err != nil {
		return ingestion{}, fmt.Errorf("build upload: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, stored.API+"/v1/engagements/"+url.PathEscape(engagementID)+"/ingestions", body)
	if err != nil {
		return ingestion{}, fmt.Errorf("build ingestion request: %w", err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: stored.Session})
	response, err := apiClient.Do(request)
	if err != nil {
		return ingestion{}, fmt.Errorf("reach the API: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusCreated {
		return ingestion{}, apiError(response)
	}
	var recorded ingestion
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&recorded); err != nil {
		return ingestion{}, fmt.Errorf("read the recorded ingestion: %w", err)
	}
	return recorded, nil
}

func csrfToken(stored storedSession) (string, error) {
	request, err := http.NewRequest(http.MethodGet, stored.API+"/v1/csrf", nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: stored.Session})
	response, err := apiClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("reach the API: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	if response.StatusCode != http.StatusOK {
		return "", apiError(response)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil || body.Token == "" {
		return "", errors.New("the API did not issue a request token")
	}
	return body.Token, nil
}

// apiError turns one refusal into the sentence an operator can act on, without
// inventing a cause the API did not report.
func apiError(response *http.Response) error {
	var body struct {
		Error       string `json:"error"`
		IngestionID string `json:"ingestionId"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body)
	switch body.Error {
	case "invalid_credentials":
		return errors.New("the API refused those credentials")
	case "unauthorized":
		return errors.New("the stored session is no longer valid; run `fops login` again")
	case "duplicate_artifact":
		return fmt.Errorf("this engagement already ingested this exact artifact as ingestion %s", body.IngestionID)
	case "invalid_nmap_report":
		return errors.New("the API refused the artifact: it is not an accepted Nmap XML report")
	case "artifact_too_large":
		return errors.New("the API refused the artifact: it is larger than the accepted size")
	case "not_found":
		return errors.New("no such engagement, or it belongs to another organization")
	case "":
		return fmt.Errorf("the API answered %s", response.Status)
	default:
		return fmt.Errorf("the API refused the request: %s (%s)", body.Error, response.Status)
	}
}
