// Package plan turns a profile into an ordered, executable list of steps.
package plan

import "github.com/t0mer/kamino/internal/manifest"

// Step is one item to install, in execution order.
type Step struct {
	Ref      string        `json:"ref"`
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	Version  string        `json:"version,omitempty"`
	Implicit bool          `json:"implicit,omitempty"`
	Item     manifest.Item `json:"-"`
}

// Plan is the full, ordered execution plan for a profile on one architecture.
type Plan struct {
	ProfileID string   `json:"profile"`
	ConfigSHA string   `json:"config_sha"`
	Arch      string   `json:"arch"`
	Steps     []Step   `json:"steps"`
	Warnings  []string `json:"warnings,omitempty"`
	Stale     bool     `json:"stale,omitempty"`
}

// Selection is the set of item refs a profile resolves to, before ordering.
type Selection struct {
	Refs     []string
	Implicit map[string]bool
}
