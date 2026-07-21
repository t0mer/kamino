// Package config resolves Kamino's own settings: where the config repo lives,
// which ref to read, and the token used to reach it.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SettingsFile is the persisted settings filename inside the data dir.
const SettingsFile = "settings.json"

// DefaultRef is used when no ref is configured anywhere.
const DefaultRef = "main"

// Settings is the persisted configuration. It holds two unrelated secrets and
// they must never be confused: RepoToken authenticates Kamino to the operator's
// config repo, while APIToken authenticates callers to Kamino's own HTTP API.
// The file is mode 0600 because of both.
type Settings struct {
	RepoURL string `json:"repo_url"`
	Ref     string `json:"ref"`
	// RepoToken is the operator's credential for a private config repo. Its
	// JSON key stays "token" so settings files written before the API existed
	// still load.
	RepoToken string `json:"token,omitempty"`
	// APIToken authenticates callers of Kamino's HTTP API. Generated on first
	// serve, never returned by any endpoint.
	APIToken        string `json:"api_token,omitempty"`
	RawBaseTemplate string `json:"raw_base_template,omitempty"`
}

// Configured reports whether a config repo has been set.
func (s Settings) Configured() bool { return s.RepoURL != "" }

// HasRepoToken reports whether a config repo credential is set, without
// exposing it.
func (s Settings) HasRepoToken() bool { return s.RepoToken != "" }

// Overrides carries one-off values from flags.
type Overrides struct {
	RepoURL         string
	Ref             string
	RepoToken       string
	RawBaseTemplate string
}

// Load reads settings.json from dataDir. A missing file yields zero Settings
// and no error: an unconfigured install is a normal first-run state.
func Load(dataDir string) (Settings, error) {
	path := filepath.Join(dataDir, SettingsFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("reading settings from %s: %w", path, err)
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, fmt.Errorf("parsing settings from %s: %w", path, err)
	}
	return s, nil
}

// Save writes settings.json atomically at mode 0600.
func Save(dataDir string, s Settings) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}

	final := filepath.Join(dataDir, SettingsFile)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("writing settings: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp) // Clean up the temp file if rename fails
		return fmt.Errorf("replacing settings: %w", err)
	}
	return nil
}

// Resolve merges saved settings with env vars and flags. Precedence is
// flags > env > saved > default, matching the documented CLI contract.
func Resolve(saved Settings, flags Overrides) Settings {
	out := saved
	pick := func(current, env, flag string) string {
		if flag != "" {
			return flag
		}
		if env != "" {
			return env
		}
		return current
	}

	out.RepoURL = pick(saved.RepoURL, os.Getenv("KAMINO_REPO"), flags.RepoURL)
	out.Ref = pick(saved.Ref, os.Getenv("KAMINO_REF"), flags.Ref)
	out.RepoToken = pick(saved.RepoToken, os.Getenv("KAMINO_TOKEN"), flags.RepoToken)
	out.RawBaseTemplate = pick(saved.RawBaseTemplate, os.Getenv("KAMINO_RAW_BASE"), flags.RawBaseTemplate)

	if out.Ref == "" {
		out.Ref = DefaultRef
	}
	return out
}

// GenerateAPIToken returns a fresh API token: 32 bytes from crypto/rand,
// hex-encoded. Callers persist it in Settings.APIToken.
func GenerateAPIToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating api token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
