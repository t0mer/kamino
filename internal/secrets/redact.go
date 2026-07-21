package secrets

import (
	"log/slog"
	"sort"
	"strings"
)

// Mask is what secret values are replaced with in captured output.
const Mask = "***"

// shortSecretWarnThreshold is the value length (in bytes) below which
// NewRedactor warns that a secret will heavily mask log output. Below this
// length, the value is likely to appear as an ordinary substring of normal
// command output (single characters, two- or three-character abbreviations
// like "id" or "ok" show up constantly in real logs), so every occurrence of
// that substring gets masked, not just the actual secret. Four bytes is
// short enough to catch the pathological single-character/word case the
// warning exists for, while not nagging about typical tokens/passwords that
// are safely much longer.
const shortSecretWarnThreshold = 4

// Redactor removes secret values from log lines.
//
// A Redactor is a point-in-time snapshot: NewRedactor copies the values held
// by the Store at the moment it is called, and never reads the Store again.
// Any secret registered with Store.Set after NewRedactor returns will NOT be
// masked by that Redactor instance. Callers must register every secret for a
// run before constructing the run's Redactor. This snapshot tradeoff is
// deliberate: a live-reading redactor would need to take a lock on the store
// for every single captured log line, on the hot path of streaming output.
type Redactor struct {
	values []string
}

// NewRedactor snapshots the store's values, longest first.
//
// Longest first: if one secret's value is a prefix, suffix, or infix of
// another's, masking the shorter one first would leave the remaining
// characters of the longer one visible in the output.
//
// See the Redactor doc comment: this snapshot is taken once, here, and does
// not observe later calls to Store.Set.
//
// If a secret's value is shorter than shortSecretWarnThreshold, NewRedactor
// emits a slog.Warn naming the secret (never its value) so the operator
// knows that secret will mask every occurrence of its characters in the
// run's output. The value is still redacted regardless of length — a short
// secret is still a secret, and an unreadable log is preferable to a leaked
// credential.
func NewRedactor(s *Store) *Redactor {
	type entry struct {
		name  string
		value string
	}

	var entries []entry
	for _, name := range s.Names() {
		v, ok := s.Get(name)
		if !ok || v == "" {
			continue
		}
		if len(v) < shortSecretWarnThreshold {
			slog.Warn("secret value is very short; it will heavily mask log output",
				"name", name, "length", len(v))
		}
		entries = append(entries, entry{name: name, value: v})
	}

	sort.Slice(entries, func(i, j int) bool { return len(entries[i].value) > len(entries[j].value) })

	values := make([]string, len(entries))
	for i, e := range entries {
		values[i] = e.value
	}
	return &Redactor{values: values}
}

// Redact replaces every secret value in line with Mask.
func (r *Redactor) Redact(line string) string {
	for _, v := range r.values {
		line = strings.ReplaceAll(line, v, Mask)
	}
	return line
}
