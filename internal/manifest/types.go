// Package manifest defines the config repo schema and parses it from YAML.
package manifest

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Arches lists every architecture Kamino supports.
var Arches = []string{"amd64", "arm64"}

// ItemType identifies which runner installs an item.
type ItemType string

// Supported item types.
const (
	ItemApt          ItemType = "apt"
	ItemDeb          ItemType = "deb"
	ItemTarball      ItemType = "tarball"
	ItemBinary       ItemType = "binary"
	ItemPip          ItemType = "pip"
	ItemScript       ItemType = "script"
	ItemComposeStack ItemType = "compose_stack"
	ItemSnap         ItemType = "snap"
)

// Valid reports whether t is a known item type.
func (t ItemType) Valid() bool {
	switch t {
	case ItemApt, ItemDeb, ItemTarball, ItemBinary, ItemPip, ItemScript, ItemComposeStack, ItemSnap:
		return true
	}
	return false
}

// Source maps an architecture to a download URL. A bare YAML string is
// expanded to every supported architecture.
type Source map[string]string

// UnmarshalYAML accepts either a bare string or a per-arch mapping.
func (s *Source) UnmarshalYAML(node *yaml.Node) error {
	out := Source{}
	if node.Kind == yaml.ScalarNode {
		var v string
		if err := node.Decode(&v); err != nil {
			return err
		}
		for _, a := range Arches {
			out[a] = v
		}
		*s = out
		return nil
	}
	var m map[string]string
	if err := node.Decode(&m); err != nil {
		return err
	}
	for k, v := range m {
		out[k] = v
	}
	*s = out
	return nil
}

// Defaults are manifest-wide settings applied to every item unless overridden.
type Defaults struct {
	AptUpdateBeforeRun bool          `yaml:"apt_update_before_run"`
	Timeout            time.Duration `yaml:"timeout"`
}

// Manifest is the config repo entry point.
type Manifest struct {
	Schema     int      `yaml:"schema"`
	Name       string   `yaml:"name"`
	Defaults   Defaults `yaml:"defaults"`
	Categories []string `yaml:"categories"`
	Profiles   []string `yaml:"profiles"`
}

// Item is a single installable unit.
type Item struct {
	ID            string        `yaml:"id"`
	Name          string        `yaml:"name"`
	Type          ItemType      `yaml:"type"`
	Version       string        `yaml:"version"`
	Source        Source        `yaml:"source"`
	SHA256        Source        `yaml:"sha256"`
	Packages      []string      `yaml:"packages"`
	Repo          string        `yaml:"repo"`
	Python        string        `yaml:"python"`
	InstallDir    string        `yaml:"install_dir"`
	PathExport    string        `yaml:"path_export"`
	Path          string        `yaml:"path"`
	Files         []string      `yaml:"files"`
	EnvFile       string        `yaml:"env_file"`
	DependsOn     []string      `yaml:"depends_on"`
	Check         string        `yaml:"check"`
	CheckContains string        `yaml:"check_contains"`
	PreInstall    []string      `yaml:"pre_install"`
	PostInstall   []string      `yaml:"post_install"`
	Timeout       time.Duration `yaml:"timeout"`
	Arch          []string      `yaml:"arch"`
	Secrets       []string      `yaml:"secrets"`

	// CategoryID is populated by ParseCategory, not by YAML.
	CategoryID string `yaml:"-"`
}

// Ref returns the canonical "category/item" reference for the item.
func (i Item) Ref() string { return fmt.Sprintf("%s/%s", i.CategoryID, i.ID) }

// SupportsArch reports whether the item may be installed on arch.
func (i Item) SupportsArch(arch string) bool {
	if len(i.Arch) == 0 {
		return true
	}
	for _, a := range i.Arch {
		if a == arch {
			return true
		}
	}
	return false
}

// Category groups related items.
type Category struct {
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Order int    `yaml:"order"`
	Items []Item `yaml:"items"`
}

// Override pins or replaces item fields from a profile.
type Override struct {
	Version string `yaml:"version"`
}

// Profile selects a set of items and may override their fields.
type Profile struct {
	ID        string              `yaml:"id"`
	Name      string              `yaml:"name"`
	Include   []string            `yaml:"include"`
	Exclude   []string            `yaml:"exclude"`
	Overrides map[string]Override `yaml:"overrides"`
}

// Resolved is a fully fetched and parsed config repo.
type Resolved struct {
	Manifest   Manifest
	Categories []Category
	Profiles   []Profile
	SHA        string
	Stale      bool
	FetchedAt  time.Time
}

// Item looks up an item by its "category/item" ref.
func (r *Resolved) Item(ref string) (Item, bool) {
	for _, c := range r.Categories {
		for _, it := range c.Items {
			if it.Ref() == ref {
				return it, true
			}
		}
	}
	return Item{}, false
}
