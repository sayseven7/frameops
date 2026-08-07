package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseE2EEnvironment = "FRAMEOPS_RELEASE_E2E"

// TestReleaseJourney uses the shipped local-runtime launcher, whose state path
// names a fresh Compose project and therefore fresh PostgreSQL and MinIO volumes.
// It deliberately drives only the released binaries and HTTP API.
func TestReleaseJourney(t *testing.T) {
	if os.Getenv(releaseE2EEnvironment) != "1" {
		t.Skipf("%s=1 is required for the real Compose release journey", releaseE2EEnvironment)
	}

	runtime := startReleaseRuntime(t)
	if _, err := os.Stat(filepath.Join(runtime.state, "bootstrap-token")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap token still exists after clean-install bootstrap: %v", err)
	}

	admin := loginReleaseOperator(t, runtime, "admin@frameops.local", filepath.Join(runtime.state, "bootstrap-password"))
	csrf := releaseCSRF(t, runtime.api, admin.session)

	organization := releaseRequest(t, runtime.api, http.MethodGet, "/v1/organization", "", admin.session, "")
	if organization.StatusCode != http.StatusOK || !strings.Contains(organization.body, "FrameOPS Local") {
		t.Fatalf("read bootstrapped organization = %d %s", organization.StatusCode, organization.body)
	}

	memberID := releaseCreated(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/organization/members", `{"displayName":"Release Member","email":"release-member@example.test","password":"correct horse battery staple","role":"member"}`, admin.session, csrf))
	memberPassword := filepath.Join(t.TempDir(), "member-password")
	if err := os.WriteFile(memberPassword, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatalf("write member password: %v", err)
	}
	member := loginReleaseOperator(t, runtime, "release-member@example.test", memberPassword)
	memberCSRF := releaseCSRF(t, runtime.api, member.session)
	if refused := releaseRequest(t, runtime.api, http.MethodPost, "/v1/clients", `{"name":"Forbidden Client"}`, member.session, memberCSRF); refused.StatusCode != http.StatusForbidden {
		t.Fatalf("member creates client = %d %s, want 403", refused.StatusCode, refused.body)
	}
	if memberID == "" {
		t.Fatal("created member has no id")
	}

	methodologyTemplateID, methodologyID := releaseMethodology(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/methodology-templates", `{"name":"Release methodology","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after OWASP WSTG 4.2.","items":[{"title":"Authorization","objective":"Confirm tenant isolation","procedure":"Replay a request with another tenant","reference":"WSTG-ATHZ-02"}]}`, admin.session, csrf))
	if published := releaseRequest(t, runtime.api, http.MethodPost, "/v1/methodology-templates/"+url.PathEscape(methodologyTemplateID)+"/publish", "", admin.session, csrf); published.StatusCode != http.StatusOK {
		t.Fatalf("publish methodology = %d %s", published.StatusCode, published.body)
	}
	clientID := releaseCreated(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/clients", `{"name":"Release Client"}`, admin.session, csrf))
	engagementID := releaseCreated(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/clients/"+url.PathEscape(clientID)+"/engagements", `{"name":"Release Engagement","methodologyVersionId":"`+methodologyID+`"}`, admin.session, csrf))
	checklist := releaseRequest(t, runtime.api, http.MethodGet, "/v1/engagements/"+url.PathEscape(engagementID)+"/checklist", "", admin.session, "")
	if checklist.StatusCode != http.StatusOK || !strings.Contains(checklist.body, "Authorization") {
		t.Fatalf("engagement checklist = %d %s", checklist.StatusCode, checklist.body)
	}

	plan := `{"startsOn":"2026-01-01","endsOn":"2026-01-31","rulesOfEngagement":"Written authorization required.","targets":["api.example.test"],"milestones":[{"title":"Kickoff","dueOn":"2026-01-15"}]}`
	if planned := releaseRequest(t, runtime.api, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementID)+"/plan", plan, admin.session, csrf); planned.StatusCode != http.StatusCreated {
		t.Fatalf("create project plan = %d %s", planned.StatusCode, planned.body)
	}

	scan := filepath.Join(t.TempDir(), "release.xml")
	if err := os.WriteFile(scan, []byte(releaseNmapXML), 0o600); err != nil {
		t.Fatalf("write Nmap artifact: %v", err)
	}
	stdout, stderr, status := runFops(t, runtime.fops, admin.workspace, "ingest", "nmap", scan, "--engagement", engagementID)
	if status != 0 || !strings.Contains(stdout, "hosts     read 1 created 1 reused 0 ignored 0 rejected 0") {
		t.Fatalf("fops ingest = %d stdout=%q stderr=%q", status, stdout, stderr)
	}
	// fops rotates the session's CSRF token immediately before its upload.
	csrf = releaseCSRF(t, runtime.api, admin.session)

	findingID := releaseCreated(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementID)+"/findings", `{"title":"Release finding","description":"A reproducible issue","impact":"Tenant data exposure","remediation":"Authorize the request","reproduction":"Replay the request","cvssVector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}`, admin.session, csrf))
	evidence := releaseMultipart(t, runtime.api, "/v1/findings/"+url.PathEscape(findingID)+"/evidence", admin.session, csrf, "release.txt", "text/plain", []byte("release custody evidence"))
	if evidence.StatusCode != http.StatusCreated || !strings.Contains(evidence.body, `"state":"stored"`) {
		t.Fatalf("capture evidence = %d %s", evidence.StatusCode, evidence.body)
	}
	if triaged := releaseRequest(t, runtime.api, http.MethodPut, "/v1/findings/"+url.PathEscape(findingID)+"/triage", `{"validationState":"confirmed","remediationState":"open"}`, admin.session, csrf); triaged.StatusCode != http.StatusOK {
		t.Fatalf("triage finding = %d %s", triaged.StatusCode, triaged.body)
	}
	if retested := releaseRequest(t, runtime.api, http.MethodPost, "/v1/findings/"+url.PathEscape(findingID)+"/retests", `{"round":1,"resultState":"fixed","procedure":"Retested the patched request","observedResult":"The request is authorized","justification":"Verified against the fixed build"}`, admin.session, csrf); retested.StatusCode != http.StatusCreated {
		t.Fatalf("retest finding = %d %s", retested.StatusCode, retested.body)
	}

	revisionID := releaseCreated(t, releaseRequest(t, runtime.api, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementID)+"/reports/generate", "", admin.session, csrf))
	if approved := releaseRequest(t, runtime.api, http.MethodPost, "/v1/report-revisions/"+url.PathEscape(revisionID)+"/approve", "", admin.session, csrf); approved.StatusCode != http.StatusOK {
		t.Fatalf("approve DOCX = %d %s", approved.StatusCode, approved.body)
	}
	pdf := releaseRequest(t, runtime.api, http.MethodPost, "/v1/report-revisions/"+url.PathEscape(revisionID)+"/pdf", "", admin.session, csrf)
	if pdf.StatusCode != http.StatusCreated || !strings.Contains(pdf.body, `"state":"stored"`) || !strings.Contains(pdf.body, `"sourceSha256"`) {
		t.Fatalf("derive PDF delivery = %d %s", pdf.StatusCode, pdf.body)
	}

	audit := releaseRequest(t, runtime.api, http.MethodGet, "/v1/organization/audit-events?limit=100", "", admin.session, "")
	for _, action := range []string{"auth.bootstrap.succeeded", "organization.member.created", "ingestion.recorded", "evidence.capture.stored", "finding.retest.recorded", "report.pdf.stored"} {
		if audit.StatusCode != http.StatusOK || !strings.Contains(audit.body, action) {
			t.Fatalf("audit = %d %s, want %q", audit.StatusCode, audit.body, action)
		}
	}
	if logout := releaseRequest(t, runtime.api, http.MethodPost, "/v1/session/logout", "", admin.session, csrf); logout.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d %s", logout.StatusCode, logout.body)
	}
	if afterLogout := releaseRequest(t, runtime.api, http.MethodGet, "/v1/clients", "", admin.session, ""); afterLogout.StatusCode != http.StatusUnauthorized {
		t.Fatalf("request after logout = %d %s, want 401", afterLogout.StatusCode, afterLogout.body)
	}
}

const releaseNmapXML = `<?xml version="1.0"?><nmaprun scanner="nmap" args="-sn 198.51.100.0/24" version="7.94" xmloutputversion="1.05"><host><status state="up" reason="syn-ack"/><address addr="198.51.100.10" addrtype="ipv4"/><hostnames><hostname name="api.example.test" type="PTR"/></hostnames></host></nmaprun>`

type releaseRuntime struct {
	state string
	api   string
	fops  string
}

type releaseOperator struct {
	workspace string
	session   string
}

type releaseResponse struct {
	StatusCode int
	body       string
}

func startReleaseRuntime(t *testing.T) releaseRuntime {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	state := filepath.Join(t.TempDir(), "state")
	ports := make([]int, 4)
	for index := range ports {
		ports[index] = releasePort(t)
	}
	command := exec.Command("bash", "scripts/local-runtime.sh", "up")
	command.Dir = root
	command.Env = append(os.Environ(),
		"FRAMEOPS_LOCAL_STATE_DIR="+state,
		fmt.Sprintf("FRAMEOPS_POSTGRES_PORT=%d", ports[0]),
		fmt.Sprintf("FRAMEOPS_MINIO_PORT=%d", ports[1]),
		fmt.Sprintf("FRAMEOPS_API_PORT=%d", ports[2]),
		fmt.Sprintf("FRAMEOPS_UI_PORT=%d", ports[3]),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("start isolated local runtime: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		down := exec.Command("bash", "scripts/local-runtime.sh", "down")
		down.Dir, down.Env = root, command.Env
		if output, err := down.CombinedOutput(); err != nil {
			t.Errorf("stop isolated local runtime: %v\n%s", err, output)
		}
	})
	return releaseRuntime{state: state, api: fmt.Sprintf("http://127.0.0.1:%d", ports[2]), fops: filepath.Join(state, "bin", "fops")}
}

func releasePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve release port: %v", err)
	}
	defer listener.Close() //nolint:errcheck
	return listener.Addr().(*net.TCPAddr).Port
}

func loginReleaseOperator(t *testing.T, runtime releaseRuntime, email, passwordFile string) releaseOperator {
	t.Helper()
	workspace := t.TempDir()
	stdout, stderr, status := runFops(t, runtime.fops, workspace, "login", "--api", runtime.api, "--email", email, "--password-file", passwordFile)
	if status != 0 || !strings.Contains(stdout, "signed in to "+runtime.api) {
		t.Fatalf("fops login = %d stdout=%q stderr=%q", status, stdout, stderr)
	}
	return releaseOperator{workspace: workspace, session: storedSession(t, workspace)}
}

func releaseCSRF(t *testing.T, api, session string) string {
	t.Helper()
	response := releaseRequest(t, api, http.MethodGet, "/v1/csrf", "", session, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("issue CSRF = %d %s", response.StatusCode, response.body)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(response.body), &body); err != nil || body.Token == "" {
		t.Fatalf("decode CSRF = %v %s", err, response.body)
	}
	return body.Token
}

func releaseRequest(t *testing.T, api, method, path, payload, session, csrf string) releaseResponse {
	t.Helper()
	request, err := http.NewRequest(method, api+path, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if payload != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-frameops_session", Value: session})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer response.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	return releaseResponse{StatusCode: response.StatusCode, body: string(body)}
}

func releaseMultipart(t *testing.T, api, path, session, csrf, filename, contentType string, contents []byte) releaseResponse {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreatePart(map[string][]string{"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`}, "Content-Type": {contentType}})
	if err != nil {
		t.Fatalf("create evidence part: %v", err)
	}
	if _, err := part.Write(contents); err != nil {
		t.Fatalf("write evidence part: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close evidence form: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, api+path, body)
	if err != nil {
		t.Fatalf("build evidence request: %v", err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&http.Cookie{Name: "__Host-frameops_session", Value: session})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("capture evidence: %v", err)
	}
	defer response.Body.Close() //nolint:errcheck
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read evidence response: %v", err)
	}
	return releaseResponse{StatusCode: response.StatusCode, body: string(responseBody)}
}

func releaseCreated(t *testing.T, response releaseResponse) string {
	t.Helper()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create resource = %d %s", response.StatusCode, response.body)
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(response.body), &body); err != nil || body.ID == "" {
		t.Fatalf("decode created resource = %v %s", err, response.body)
	}
	return body.ID
}

func releaseMethodology(t *testing.T, response releaseResponse) (string, string) {
	t.Helper()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create methodology = %d %s", response.StatusCode, response.body)
	}
	var body struct {
		ID         string `json:"id"`
		TemplateID string `json:"templateId"`
	}
	if err := json.Unmarshal([]byte(response.body), &body); err != nil || body.ID == "" || body.TemplateID == "" {
		t.Fatalf("decode methodology = %v %s", err, response.body)
	}
	return body.TemplateID, body.ID
}
