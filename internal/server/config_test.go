package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/server"
)

// loadFixtureConfig reads testdata/config the same way the CLI's --config-dir
// does, so handler tests need no network.
func loadFixtureConfig(t *testing.T) func(context.Context) (*manifest.Resolved, error) {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "config")

	return func(context.Context) (*manifest.Resolved, error) {
		mb, err := os.ReadFile(filepath.Join(root, "manifest.yaml"))
		if err != nil {
			return nil, err
		}
		m, err := manifest.ParseManifest(mb)
		if err != nil {
			return nil, err
		}
		out := &manifest.Resolved{Manifest: *m, SHA: "fixture"}
		for _, p := range m.Categories {
			b, err := os.ReadFile(filepath.Join(root, p))
			if err != nil {
				return nil, err
			}
			c, err := manifest.ParseCategory(b)
			if err != nil {
				return nil, err
			}
			out.Categories = append(out.Categories, *c)
		}
		for _, p := range m.Profiles {
			b, err := os.ReadFile(filepath.Join(root, p))
			if err != nil {
				return nil, err
			}
			pr, err := manifest.ParseProfile(b)
			if err != nil {
				return nil, err
			}
			out.Profiles = append(out.Profiles, *pr)
		}
		return out, nil
	}
}

func newConfigServer(t *testing.T) http.Handler {
	t.Helper()
	return server.New(server.Deps{
		DataDir:    t.TempDir(),
		APIToken:   testToken,
		LoadConfig: loadFixtureConfig(t),
	}).Handler()
}

func TestGetConfigReturnsCategoriesAndProfiles(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/config", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Name       string `json:"name"`
		SHA        string `json:"sha"`
		Categories []struct {
			ID    string `json:"id"`
			Items []struct {
				Ref string `json:"ref"`
			} `json:"items"`
		} `json:"categories"`
		Profiles []struct {
			ID string `json:"id"`
		} `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	assert.Equal(t, "kamino-testdata", got.Name)
	assert.Equal(t, "fixture", got.SHA)
	assert.Len(t, got.Categories, 3)
	assert.Len(t, got.Profiles, 3)
	assert.Equal(t, "dev/go", got.Categories[0].Items[0].Ref)
}

func TestPostPlanReturnsOrderedSteps(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/plan", `{"profile":"dev","arch":"amd64"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Profile string `json:"profile"`
		Steps   []struct {
			Ref string `json:"ref"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	assert.Equal(t, "dev", got.Profile)
	require.Len(t, got.Steps, 4)
	assert.Equal(t, "dev/go", got.Steps[0].Ref)
}

func TestPostPlanSurfacesSafetyWarnings(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/plan", `{"profile":"production","arch":"amd64"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	require.NotEmpty(t, got.Warnings,
		"the UI's confirmation screen is the only place an operator sees these")
	joined := ""
	for _, wn := range got.Warnings {
		joined += wn + "\n"
	}
	assert.Contains(t, joined, "sha256")
	assert.Contains(t, joined, "runs a shell script as root")
}

func TestPostPlanUnknownProfileIs404(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/plan", `{"profile":"nope","arch":"amd64"}`)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "nope")
}

func TestPostPlanMalformedBodyIs400(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/plan", "{not json")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostPlanDefaultsArchToTheHost(t *testing.T) {
	h := newConfigServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/plan", `{"profile":"test"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Arch string `json:"arch"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got.Arch)
}

func TestGetConfigIncludesFetchedAtWhenKnown(t *testing.T) {
	fetched := time.Date(2026, 7, 20, 14, 22, 0, 0, time.UTC)
	load := func(context.Context) (*manifest.Resolved, error) {
		return &manifest.Resolved{
			Manifest:  manifest.Manifest{Schema: 1, Name: "t"},
			SHA:       "abc",
			Stale:     true,
			FetchedAt: fetched,
		}, nil
	}
	h := server.New(server.Deps{DataDir: t.TempDir(), APIToken: testToken, LoadConfig: load}).Handler()

	rec := do(t, h, http.MethodGet, "/api/v1/config", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Stale     bool   `json:"stale"`
		FetchedAt string `json:"fetched_at"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, got.Stale)
	assert.Equal(t, "2026-07-20T14:22:00Z", got.FetchedAt,
		"the UI pairs fetched_at with stale to show how old the cached config is")
}
