package manifest

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// decodeStrict decodes YAML and rejects unknown fields, so a typo in a config
// repo is reported rather than silently ignored.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

// ParseManifest decodes a manifest.yaml.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := decodeStrict(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

// ParseCategory decodes a category file and stamps each item's CategoryID.
func ParseCategory(data []byte) (*Category, error) {
	var c Category
	if err := decodeStrict(data, &c); err != nil {
		return nil, fmt.Errorf("parsing category: %w", err)
	}
	for i := range c.Items {
		c.Items[i].CategoryID = c.ID
	}
	return &c, nil
}

// ParseProfile decodes a profile file.
func ParseProfile(data []byte) (*Profile, error) {
	var p Profile
	if err := decodeStrict(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile: %w", err)
	}
	return &p, nil
}
