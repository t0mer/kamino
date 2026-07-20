// Package sysinfo detects the host Kamino is running on.
package sysinfo

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// Info describes the host.
type Info struct {
	OS        string `json:"os"`
	Distro    string `json:"distro"`
	VersionID string `json:"version_id"`
	Arch      string `json:"arch"`
	Hostname  string `json:"hostname"`
	Root      bool   `json:"root"`
}

// Detect inspects the running host.
func Detect() (Info, error) {
	info := Info{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
		Root: os.Geteuid() == 0,
	}

	host, err := os.Hostname()
	if err != nil {
		return Info{}, fmt.Errorf("reading hostname: %w", err)
	}
	info.Hostname = host

	f, err := os.Open("/etc/os-release")
	if err != nil {
		// Not fatal on its own: RequireUbuntuRoot turns it into a clear refusal.
		return info, nil
	}
	defer func() { _ = f.Close() }()

	distro, versionID, err := ParseOSRelease(f)
	if err != nil {
		return Info{}, fmt.Errorf("parsing /etc/os-release: %w", err)
	}
	info.Distro = distro
	info.VersionID = versionID
	return info, nil
}

// ParseOSRelease extracts ID and VERSION_ID from an os-release stream.
// Values may be quoted with either double or single quotes; matching quote pairs
// are stripped. The returned distro ID is normalized to lowercase per the
// os-release specification.
func ParseOSRelease(r io.Reader) (distro, versionID string, err error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		// Skip comments and empty lines.
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = unquoteValue(value)
		switch strings.TrimSpace(key) {
		case "ID":
			distro = strings.ToLower(value)
		case "VERSION_ID":
			versionID = value
		}
	}
	if err := s.Err(); err != nil {
		return "", "", fmt.Errorf("scanning os-release: %w", err)
	}
	return distro, versionID, nil
}

// unquoteValue removes a matching pair of surrounding quotes (either " or ')
// from a value. Only strips if the value is quoted with matching pairs;
// unmatched or interior quotes are preserved.
func unquoteValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// RequireUbuntuRoot returns an actionable error unless the host is Ubuntu and
// the process is root. Installing packages needs both, and failing early with
// a clear message beats failing later inside apt.
func (i Info) RequireUbuntuRoot() error {
	if i.Distro != "ubuntu" {
		got := i.Distro
		if got == "" {
			got = "unknown"
		}
		return fmt.Errorf("kamino apply requires Ubuntu, detected %q", got)
	}
	if !i.Root {
		return fmt.Errorf("kamino apply must run as root: re-run with sudo")
	}
	return nil
}
