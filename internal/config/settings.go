// Package config resolves Kamino's own settings: where the config repo lives,
// which ref to read, and the token used to reach it.
package config

import (
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

// Settings is the persisted configuration. It may hold a token, so the file it
// is written to is mode 0600.
type Settings struct {
	RepoURL         string `json:"repo_url"`
	Ref             string `json:"ref"`
	Token           string `json:"token,omitempty"`
	RawBaseTemplate string `json:"raw_base_template,omitempty"`
}

// Configured reports whether a config repo has been set.
func (s Settings) Configured() bool { return s.RepoURL != "" }

// Overrides carries one-off values from flags.
type Overrides struct {
	RepoURL         string
	Ref             string
	Token           string
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
	out.Token = pick(saved.Token, os.Getenv("KAMINO_TOKEN"), flags.Token)
	out.RawBaseTemplate = pick(saved.RawBaseTemplate, os.Getenv("KAMINO_RAW_BASE"), flags.RawBaseTemplate)

	if out.Ref == "" {
		out.Ref = DefaultRef
	}
	return out
}
