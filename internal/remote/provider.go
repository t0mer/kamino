// Package remote fetches config repo files over raw HTTPS. It never shells out
// to git and never clones: every file is retrieved as raw content on demand.
package remote

import (
	"fmt"
	"net/url"
	"strings"
)

// Provider identifies the hosting service, which determines both the raw
// content URL layout and the authentication header.
type Provider string

// Known providers.
const (
	ProviderGitHub  Provider = "github"
	ProviderGitLab  Provider = "gitlab"
	ProviderGitea   Provider = "gitea"
	ProviderGeneric Provider = "generic"
)

// DefaultRef is used when no ref is configured.
const DefaultRef = "main"

// Repo is a parsed config repo location.
type Repo struct {
	URL             string
	Ref             string
	Token           string
	RawBaseTemplate string
	Provider        Provider
	Host            string
	Owner           string
	Name            string
}

// ParseRepo resolves a repo URL into a provider-aware Repo. rawBaseTemplate
// overrides provider detection and must contain {ref} and {path} placeholders.
func ParseRepo(repoURL, ref, rawBaseTemplate string) (*Repo, error) {
	if repoURL == "" {
		return nil, fmt.Errorf("repo URL is empty")
	}
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf("parsing repo URL %q: %w", repoURL, err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("repo URL must use https, got %q", u.Scheme)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("repo URL path must be owner/repo, got %q", u.Path)
	}

	if rawBaseTemplate != "" {
		hasRef := strings.Contains(rawBaseTemplate, "{ref}")
		hasPath := strings.Contains(rawBaseTemplate, "{path}")
		if !hasRef || !hasPath {
			missing := "{ref}"
			if hasRef {
				missing = "{path}"
			}
			return nil, fmt.Errorf("raw base template %q is missing the %s placeholder; template must look like %q",
				rawBaseTemplate, missing, "https://host/{owner}/{repo}/raw/{ref}/{path}")
		}
	}

	if ref == "" {
		ref = DefaultRef
	}

	r := &Repo{
		URL:             repoURL,
		Ref:             ref,
		RawBaseTemplate: rawBaseTemplate,
		Host:            u.Host,
		Owner:           parts[0],
		Name:            strings.TrimSuffix(parts[1], ".git"),
	}

	normalizedHost := strings.ToLower(u.Hostname())
	switch {
	case normalizedHost == "github.com":
		r.Provider = ProviderGitHub
	case normalizedHost == "gitlab.com":
		r.Provider = ProviderGitLab
	default:
		// Self-hosted instances are indistinguishable by URL alone. Treat them
		// as generic and let the operator supply a raw base template.
		r.Provider = ProviderGeneric
	}

	return r, nil
}

// RawURL builds the raw content URL for path at ref. It returns an empty
// string when a generic host has no raw base template configured.
func (r *Repo) RawURL(ref, path string) string {
	if r.RawBaseTemplate != "" {
		out := strings.ReplaceAll(r.RawBaseTemplate, "{ref}", ref)
		return strings.ReplaceAll(out, "{path}", path)
	}
	switch r.Provider {
	case ProviderGitHub:
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", r.Owner, r.Name, ref, path)
	case ProviderGitLab:
		return fmt.Sprintf("https://%s/%s/%s/-/raw/%s/%s", r.Host, r.Owner, r.Name, ref, path)
	case ProviderGitea:
		return fmt.Sprintf("https://%s/%s/%s/raw/branch/%s/%s", r.Host, r.Owner, r.Name, ref, path)
	default:
		return ""
	}
}

// AuthHeader returns the header to authenticate against this provider.
// ok is false when no token is configured.
func (r *Repo) AuthHeader() (name, value string, ok bool) {
	if r.Token == "" {
		return "", "", false
	}
	switch r.Provider {
	case ProviderGitLab:
		return "PRIVATE-TOKEN", r.Token, true
	case ProviderGitHub, ProviderGitea:
		return "Authorization", "token " + r.Token, true
	default:
		return "Authorization", "Bearer " + r.Token, true
	}
}
