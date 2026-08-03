package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// storedSession is everything the CLI keeps between commands: which API it
// signed in to and the session that API issued. No password and no request token
// is ever written to disk.
type storedSession struct {
	API     string `json:"api"`
	Session string `json:"session"`
}

// sessionDirectory resolves the operator-owned directory that holds the session.
// FRAMEOPS_CONFIG_HOME takes precedence so a test or a scripted run can keep its
// session out of the operator's own configuration.
func sessionDirectory() (string, error) {
	if configured := os.Getenv("FRAMEOPS_CONFIG_HOME"); configured != "" {
		return configured, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "frameops"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the configuration directory: %w", err)
	}
	return filepath.Join(home, ".config", "frameops"), nil
}

// storeSession writes the session for the current operator only, replacing any
// earlier one. The file is created 0600 inside a 0700 directory: it carries an
// authenticated session to client findings and evidence.
func storeSession(session storedSession) (string, error) {
	directory, err := sessionDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create the configuration directory: %w", err)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("encode the session: %w", err)
	}
	path := filepath.Join(directory, "session.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write the session: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("restrict the session file: %w", err)
	}
	return path, nil
}

func loadSession() (storedSession, error) {
	directory, err := sessionDirectory()
	if err != nil {
		return storedSession{}, err
	}
	contents, err := os.ReadFile(filepath.Join(directory, "session.json"))
	if errors.Is(err, os.ErrNotExist) {
		return storedSession{}, errors.New("no stored session; run `fops login` first")
	}
	if err != nil {
		return storedSession{}, fmt.Errorf("read the stored session: %w", err)
	}
	var session storedSession
	if err := json.Unmarshal(contents, &session); err != nil {
		return storedSession{}, fmt.Errorf("read the stored session: %w", err)
	}
	if session.Session == "" {
		return storedSession{}, errors.New("the stored session is empty; run `fops login` again")
	}
	// The stored address is validated again on use, so an edited session file
	// cannot make the CLI send the session somewhere it would refuse to sign in.
	base, err := baseURL(session.API)
	if err != nil {
		return storedSession{}, err
	}
	session.API = base
	return session, nil
}
