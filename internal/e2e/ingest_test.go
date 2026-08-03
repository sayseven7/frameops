package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/httpapi"
	"github.com/sayseven7/frameops/internal/render"
	"github.com/sayseven7/frameops/internal/store/objectstore"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

// firstScan and secondScan are synthetic Nmap artifacts. Addresses come from the
// documentation ranges and names from `.test`: no fixture describes a real
// network.
const firstScan = `<?xml version="1.0" encoding="UTF-8"?>
<nmaprun scanner="nmap" args="-sn 198.51.100.0/24" version="7.94" xmloutputversion="1.05">
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="198.51.100.10" addrtype="ipv4"/>
    <hostnames><hostname name="api.example.test" type="PTR"/></hostnames>
  </host>
  <host>
    <status state="up" reason="echo-reply"/>
    <address addr="198.51.100.11" addrtype="ipv4"/>
  </host>
  <host>
    <status state="down" reason="no-response"/>
    <address addr="198.51.100.12" addrtype="ipv4"/>
  </host>
</nmaprun>`

const secondScan = `<?xml version="1.0" encoding="UTF-8"?>
<nmaprun scanner="nmap" args="-sn 198.51.100.0/24" version="7.94" xmloutputversion="1.05">
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="198.51.100.10" addrtype="ipv4"/>
    <hostnames><hostname name="api.example.test" type="PTR"/></hostnames>
  </host>
  <host>
    <status state="up" reason="syn-ack"/>
    <address addr="198.51.100.13" addrtype="ipv4"/>
    <hostnames><hostname name="vpn.example.test" type="PTR"/></hostnames>
  </host>
</nmaprun>`

// TestFopsIngestsNmapThroughTheAPI runs the built fops binary against a real
// HTTP server carrying the real API handler and a real database. It is the
// end-to-end proof of the capture flow: every byte the CLI sends reaches
// PostgreSQL through an HTTP request, and the requests the server observed are
// asserted at the end.
func TestFopsIngestsNmapThroughTheAPI(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for end-to-end tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	const password = "correct horse battery staple"
	email := fmt.Sprintf("ingest-%d@example.test", os.Getpid())
	organizationID := provisionOperator(t, ctx, pool, email, password)

	observed := &requestLog{}
	server := httptest.NewServer(observed.wrap(httpapi.New(pool, objectstore.Bucket{}, render.Worker{})))
	t.Cleanup(server.Close)

	workspace := t.TempDir()
	binary := buildFops(t, workspace)
	passwordFile := filepath.Join(workspace, "password")
	if err := os.WriteFile(passwordFile, []byte(password+"\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	stdout, stderr, status := runFops(t, binary, workspace, "login", "--api", server.URL, "--email", email, "--password-file", passwordFile)
	if status != 0 || !strings.Contains(stdout, "signed in to "+server.URL) {
		t.Fatalf("fops login = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}

	// The CLI has no client or engagement command yet, so the engagement is
	// created over the same API with the session the CLI just stored: even the
	// test arranges its state through HTTP.
	session := storedSession(t, workspace)
	csrf := csrfToken(t, server.URL, session)
	clientID := created(t, server.URL, session, csrf, "/v1/clients", `{"name":"Ingest Client"}`)
	engagementID := created(t, server.URL, session, csrf, "/v1/clients/"+clientID+"/engagements", `{"name":"Ingest Engagement"}`)

	scan := filepath.Join(workspace, "scan.xml")
	if err := os.WriteFile(scan, []byte(firstScan), 0o600); err != nil {
		t.Fatalf("write scan artifact: %v", err)
	}
	stdout, stderr, status = runFops(t, binary, workspace, "ingest", "nmap", scan, "--engagement", engagementID)
	if status != 0 {
		t.Fatalf("fops ingest = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
	for _, fragment := range []string{
		"ingested nmap artifact scan.xml",
		"format    nmap 7.94 xmloutputversion 1.05",
		"hosts     read 3 created 2 reused 0 ignored 1 rejected 0",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("fops ingest stdout = %q, want fragment %q", stdout, fragment)
		}
	}

	assets := get(t, server.URL, session, "/v1/engagements/"+engagementID+"/assets")
	for _, fragment := range []string{`"name":"api.example.test"`, `"name":"198.51.100.11"`, `"source":"ingest"`} {
		if !strings.Contains(assets, fragment) {
			t.Fatalf("assets after ingestion = %s, want fragment %s", assets, fragment)
		}
	}
	if strings.Contains(assets, `"name":"198.51.100.12"`) {
		t.Fatalf("assets after ingestion = %s, want the host reported down to be absent", assets)
	}

	// Replaying the same artifact is refused explicitly and changes nothing.
	stdout, stderr, status = runFops(t, binary, workspace, "ingest", "nmap", scan, "--engagement", engagementID)
	if status != 1 || !strings.Contains(stderr, "already ingested this exact artifact") {
		t.Fatalf("replayed fops ingest = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
	if replayed := get(t, server.URL, session, "/v1/engagements/"+engagementID+"/assets"); replayed != assets {
		t.Fatalf("a refused replay changed the inventory:\n%s\n%s", assets, replayed)
	}

	// A later scan of the same network reuses the host it already knows and adds
	// only what is new.
	secondArtifact := filepath.Join(workspace, "scan-2.xml")
	if err := os.WriteFile(secondArtifact, []byte(secondScan), 0o600); err != nil {
		t.Fatalf("write second scan artifact: %v", err)
	}
	stdout, stderr, status = runFops(t, binary, workspace, "ingest", "nmap", secondArtifact, "--engagement", engagementID)
	if status != 0 || !strings.Contains(stdout, "hosts     read 2 created 1 reused 1 ignored 0 rejected 0") {
		t.Fatalf("second fops ingest = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
	history := get(t, server.URL, session, "/v1/engagements/"+engagementID+"/ingestions")
	var historyBody struct {
		Items []struct {
			Tool    string `json:"tool"`
			SHA256  string `json:"sha256"`
			Summary struct {
				Read, Created, Reused, Ignored, Rejected int
			} `json:"summary"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(history), &historyBody); err != nil {
		t.Fatalf("decode ingestion history = %v, body=%s", err, history)
	}
	if len(historyBody.Items) != 2 || historyBody.Items[0].Tool != "nmap" || historyBody.Items[0].SHA256 == historyBody.Items[1].SHA256 {
		t.Fatalf("ingestion history = %#v, want two distinct nmap artifacts", historyBody.Items)
	}

	// An artifact the API cannot read is refused whole: nothing is recorded.
	broken := filepath.Join(workspace, "broken.xml")
	if err := os.WriteFile(broken, []byte(`{"hosts":["198.51.100.14"]}`), 0o600); err != nil {
		t.Fatalf("write broken artifact: %v", err)
	}
	stdout, stderr, status = runFops(t, binary, workspace, "ingest", "nmap", broken, "--engagement", engagementID)
	if status != 1 || !strings.Contains(stderr, "not an accepted Nmap XML report") {
		t.Fatalf("broken fops ingest = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}

	// Another organization's engagement is indistinguishable from a missing one.
	stdout, stderr, status = runFops(t, binary, workspace, "ingest", "nmap", secondArtifact, "--engagement", "00000000-0000-0000-0000-000000000000")
	if status != 1 || !strings.Contains(stderr, "no such engagement") {
		t.Fatalf("unknown-engagement fops ingest = %d, stdout=%q, stderr=%q", status, stdout, stderr)
	}

	var recordedIngestions, ingestedAssets int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE organization_id = $1 AND action = 'ingestion.recorded'`, organizationID).Scan(&recordedIngestions); err != nil {
		t.Fatalf("count ingestion audit events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE organization_id = $1 AND action = 'asset.created' AND context->>'source' = 'ingest'`, organizationID).Scan(&ingestedAssets); err != nil {
		t.Fatalf("count ingested asset audit events: %v", err)
	}
	if recordedIngestions != 2 || ingestedAssets != 3 {
		t.Fatalf("audit events = %d ingestions and %d ingested assets, want 2 and 3", recordedIngestions, ingestedAssets)
	}

	// The traffic itself is the assertion: the CLI reached PostgreSQL only
	// through these API requests.
	for _, route := range []string{
		"POST /v1/session/login",
		"GET /v1/csrf",
		"POST /v1/engagements/" + engagementID + "/ingestions",
	} {
		if !observed.saw(route) {
			t.Fatalf("the API never received %q; observed %v", route, observed.routes())
		}
	}
}

// TestCLICarriesNoDatabaseDependency checks the boundary the product requires:
// the API is the CLI's only entry point, so the CLI package must not be able to
// reach PostgreSQL even by accident. The first-admin bootstrap is the single
// documented exception and lives outside internal/cli.
func TestCLICarriesNoDatabaseDependency(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the Go toolchain is required to inspect the CLI dependency graph")
	}
	output, err := exec.Command("go", "list", "-deps", "github.com/sayseven7/frameops/internal/cli").CombinedOutput()
	if err != nil {
		t.Fatalf("list CLI dependencies: %v: %s", err, output)
	}
	for _, forbidden := range []string{"github.com/jackc/pgx", "frameops/internal/store"} {
		if strings.Contains(string(output), forbidden) {
			t.Fatalf("internal/cli depends on %s, want the HTTP API as its only entry point", forbidden)
		}
	}
}

// provisionOperator creates one organization and one admin able to sign in. The
// first-admin bootstrap is consumed once per database, so an end-to-end run
// provisions its own operator instead of competing for it.
func provisionOperator(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email, password string) string {
	t.Helper()
	passwordHash, err := postgres.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var organizationID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('End-to-end Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (display_name, email, password_hash) VALUES ('End-to-end Operator', $1, $2) RETURNING id`, email, passwordHash).Scan(&userID); err != nil {
		t.Fatalf("create operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'admin')`, organizationID, userID); err != nil {
		t.Fatalf("create membership: %v", err)
	}
	return organizationID
}

func buildFops(t *testing.T, workspace string) string {
	t.Helper()
	binary := filepath.Join(workspace, "fops")
	build := exec.Command("go", "build", "-o", binary, "github.com/sayseven7/frameops/cmd/fops")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fops: %v: %s", err, output)
	}
	return binary
}

// runFops executes the built CLI with its own configuration directory, so the
// test never reads or writes the session of the operator running it.
func runFops(t *testing.T, binary, workspace string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), "FRAMEOPS_CONFIG_HOME="+filepath.Join(workspace, "config"))
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	var exitError *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exitError):
		return stdout.String(), stderr.String(), exitError.ExitCode()
	default:
		t.Fatalf("run fops %v: %v", args, err)
		return "", "", -1
	}
}

func storedSession(t *testing.T, workspace string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(workspace, "config", "session.json"))
	if err != nil {
		t.Fatalf("read stored session: %v", err)
	}
	var stored struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(contents, &stored); err != nil || stored.Session == "" {
		t.Fatalf("decode stored session = %v, contents=%s", err, contents)
	}
	return stored.Session
}

func csrfToken(t *testing.T, base, session string) string {
	t.Helper()
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(get(t, base, session, "/v1/csrf")), &body); err != nil || body.Token == "" {
		t.Fatalf("decode csrf token = %v", err)
	}
	return body.Token
}

func get(t *testing.T, base, session, path string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatalf("build %s request: %v", path, err)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-frameops_session", Value: session})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer response.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get %s = %d: %s", path, response.StatusCode, body)
	}
	return string(body)
}

func created(t *testing.T, base, session, csrf, path, payload string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build %s request: %v", path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(&http.Cookie{Name: "__Host-frameops_session", Value: session})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer response.Body.Close() //nolint:errcheck
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || response.StatusCode != http.StatusCreated || body.ID == "" {
		t.Fatalf("post %s = %d, decode = %v", path, response.StatusCode, err)
	}
	return body.ID
}

// requestLog records the method and path of every request the API served, so the
// end-to-end check can assert which route each step actually used.
type requestLog struct {
	mutex   sync.Mutex
	entries []string
}

func (log *requestLog) wrap(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		log.mutex.Lock()
		log.entries = append(log.entries, request.Method+" "+request.URL.Path)
		log.mutex.Unlock()
		handler.ServeHTTP(response, request)
	})
}

func (log *requestLog) saw(route string) bool {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	for _, entry := range log.entries {
		if entry == route {
			return true
		}
	}
	return false
}

func (log *requestLog) routes() []string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return append([]string(nil), log.entries...)
}
