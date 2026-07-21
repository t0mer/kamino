// Package secrets holds run-scoped secret values, expands them into commands,
// and redacts them from captured output. Values live in memory only: they are
// never written to sqlite or to disk.
package secrets

import (
	"sort"
	"sync"
)

// Store is an in-memory set of secret values for one run.
type Store struct {
	mu     sync.RWMutex
	values map[string]string
}

// New builds an empty store.
func New() *Store { return &Store{values: map[string]string{}} }

// Set records a secret value.
func (s *Store) Set(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[name] = value
}

// Get returns a secret value.
func (s *Store) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[name]
	return v, ok
}

// Names returns every secret name held, sorted.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.values))
	for k := range s.values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Missing returns the names in refs that the store has no value for.
func Missing(refs []string, s *Store) []string {
	var out []string
	for _, name := range refs {
		if _, ok := s.Get(name); !ok {
			out = append(out, name)
		}
	}
	return out
}
