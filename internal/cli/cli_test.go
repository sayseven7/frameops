package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBaseURLRefusesPlaintextOffLoopback checks the transport rule the stored
// session depends on: the API cookie is Secure, so an address that would send it
// in the clear is refused before any credential is read.
func TestBaseURLRefusesPlaintextOffLoopback(t *testing.T) {
	accepted := map[string]string{
		"https://frameops.example.test":      "https://frameops.example.test",
		"https://frameops.example.test/api/": "https://frameops.example.test/api",
		"http://127.0.0.1:8080":              "http://127.0.0.1:8080",
		"http://localhost:8080":              "http://localhost:8080",
		"http://[::1]:8080":                  "http://[::1]:8080",
	}
	for raw, want := range accepted {
		base, err := baseURL(raw)
		if err != nil || base != want {
			t.Fatalf("baseURL(%q) = %q, %v, want %q", raw, base, err, want)
		}
	}
	for _, raw := range []string{
		"http://frameops.example.test",
		"http://198.51.100.10:8080",
		"ftp://frameops.example.test",
		"frameops.example.test",
		"https://operator:secret@frameops.example.test",
		"https://frameops.example.test/api?token=abc",
		"",
	} {
		if base, err := baseURL(raw); err == nil {
			t.Fatalf("baseURL(%q) = %q, want a refusal", raw, base)
		}
	}
}

func TestStoredSessionIsOperatorOnly(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "config")
	t.Setenv("FRAMEOPS_CONFIG_HOME", directory)

	if _, err := loadSession(); err == nil || !strings.Contains(err.Error(), "fops login") {
		t.Fatalf("loadSession without a session = %v, want guidance to sign in", err)
	}
	path, err := storeSession(storedSession{API: "https://frameops.example.test", Session: "session-value"})
	if err != nil {
		t.Fatalf("store session: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat session: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session file mode = %v, want 0600", info.Mode().Perm())
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat configuration directory: %v", err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("configuration directory mode = %v, want 0700", directoryInfo.Mode().Perm())
	}
	loaded, err := loadSession()
	if err != nil || loaded.Session != "session-value" || loaded.API != "https://frameops.example.test" {
		t.Fatalf("loadSession = %#v, %v", loaded, err)
	}

	// An edited session file cannot make the CLI send the session over a
	// transport it would have refused at sign-in.
	if err := os.WriteFile(path, []byte(`{"api":"http://frameops.example.test","session":"session-value"}`), 0o600); err != nil {
		t.Fatalf("rewrite session: %v", err)
	}
	if _, err := loadSession(); err == nil {
		t.Fatal("loadSession accepted a plaintext off-loopback address, want a refusal")
	}
}

func TestRunReportsUsage(t *testing.T) {
	t.Setenv("FRAMEOPS_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	for _, args := range [][]string{
		{},
		{"scan"},
		{"login", "--api", "https://frameops.example.test"},
		{"ingest"},
		{"ingest", "nuclei", "findings.json", "--engagement", "00000000-0000-0000-0000-000000000000"},
		{"ingest", "nmap", "scan.xml"},
	} {
		var stdout, stderr bytes.Buffer
		if status := Run(args, &stdout, &stderr); status != 2 {
			t.Fatalf("Run(%q) = %d, want 2 with usage on stderr: %s", args, status, stderr.String())
		}
		if !strings.Contains(stderr.String(), "fops ingest nmap FILE") {
			t.Fatalf("Run(%q) stderr = %q, want the usage text", args, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("Run(%q) wrote %q to stdout, want usage on stderr only", args, stdout.String())
		}
	}
}

// TestIngestRequiresASession checks that the online-only CLI reports a missing
// session instead of queueing the artifact anywhere.
func TestIngestRequiresASession(t *testing.T) {
	t.Setenv("FRAMEOPS_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	artifact := filepath.Join(t.TempDir(), "scan.xml")
	if err := os.WriteFile(artifact, []byte(`<nmaprun scanner="nmap" version="7.94"></nmaprun>`), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	var stdout, stderr bytes.Buffer
	status := Run([]string{"ingest", "nmap", artifact, "--engagement", "00000000-0000-0000-0000-000000000000"}, &stdout, &stderr)
	if status != 1 || !strings.Contains(stderr.String(), "no stored session") {
		t.Fatalf("Run without a session = %d, stderr = %q", status, stderr.String())
	}
}
