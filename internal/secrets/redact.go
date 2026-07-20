package secrets

import (
	"sort"
	"strings"
)

// Mask is what secret values are replaced with in captured output.
const Mask = "***"

// Redactor removes secret values from log lines.
type Redactor struct {
	values []string
}

// NewRedactor snapshots the store's values, longest first.
func NewRedactor(s *Store) *Redactor {
	var values []string
	for _, name := range s.Names() {
		if v, ok := s.Get(name); ok && v != "" {
			values = append(values, v)
		}
	}
	// Longest first: if one secret is a prefix of another, masking the short
	// one first would leave the remaining characters of the long one visible.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return &Redactor{values: values}
}

// Redact replaces every secret value in line with Mask.
func (r *Redactor) Redact(line string) string {
	for _, v := range r.values {
		line = strings.ReplaceAll(line, v, Mask)
	}
	return line
}
