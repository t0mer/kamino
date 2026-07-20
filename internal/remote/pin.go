package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Pinner resolves a mutable ref to an immutable commit SHA, so that a push to
// the config repo mid-run cannot produce a half-old, half-new plan.
type Pinner interface {
	Pin(ctx context.Context, repo *Repo) (string, error)
}

// APIPinner resolves refs using each provider's REST API.
type APIPinner struct {
	client  *http.Client
	apiBase string
}

// NewAPIPinner builds a pinner. An empty apiBase uses the provider default;
// tests pass a local server URL.
func NewAPIPinner(client *http.Client, apiBase string) *APIPinner {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &APIPinner{client: client, apiBase: apiBase}
}

// Pin resolves repo.Ref to a commit SHA. For providers with no usable API it
// returns the ref unchanged; callers record the fetch timestamp instead.
func (p *APIPinner) Pin(ctx context.Context, repo *Repo) (string, error) {
	endpoint, decode := p.endpointFor(repo)
	if endpoint == "" {
		return repo.Ref, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building pin request: %w", err)
	}
	if name, value, ok := repo.AuthHeader(); ok {
		req.Header.Set(name, value)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolving ref %q: %w", repo.Ref, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolving ref %q: unexpected status %d", repo.Ref, resp.StatusCode)
	}

	sha, err := decode(resp)
	if err != nil {
		return "", fmt.Errorf("resolving ref %q: %w", repo.Ref, err)
	}
	return sha, nil
}

type shaDecoder func(*http.Response) (string, error)

func (p *APIPinner) endpointFor(repo *Repo) (string, shaDecoder) {
	switch repo.Provider {
	case ProviderGitHub:
		base := p.apiBase
		if base == "" {
			base = "https://api.github.com"
		}
		return fmt.Sprintf("%s/repos/%s/%s/commits/%s", base, repo.Owner, repo.Name, repo.Ref),
			decodeField("sha")
	case ProviderGitLab:
		base := p.apiBase
		if base == "" {
			base = "https://" + repo.Host
		}
		return fmt.Sprintf("%s/api/v4/projects/%s%%2F%s/repository/commits/%s",
			base, repo.Owner, repo.Name, repo.Ref), decodeField("id")
	case ProviderGitea:
		base := p.apiBase
		if base == "" {
			base = "https://" + repo.Host
		}
		return fmt.Sprintf("%s/api/v1/repos/%s/%s/commits/%s", base, repo.Owner, repo.Name, repo.Ref),
			decodeField("sha")
	default:
		return "", nil
	}
}

func decodeField(field string) shaDecoder {
	return func(resp *http.Response) (string, error) {
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", fmt.Errorf("decoding commit response: %w", err)
		}
		sha, _ := payload[field].(string)
		if sha == "" {
			return "", fmt.Errorf("commit response has no %q field", field)
		}
		return sha, nil
	}
}
