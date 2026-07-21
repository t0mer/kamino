package secrets

import (
	"fmt"
	"regexp"
	"strings"
)

var secretPattern = regexp.MustCompile(`\{secret:([A-Za-z0-9_]+)\}`)

// Expand substitutes {version} and {secret:NAME} placeholders in.
//
// An unknown secret is an error rather than an empty expansion: silently
// dropping a token would produce a command that runs as root and fails in a
// way that is very hard to diagnose.
func Expand(in, version string, s *Store) (string, error) {
	out := strings.ReplaceAll(in, "{version}", version)

	var missing []string
	out = secretPattern.ReplaceAllStringFunc(out, func(match string) string {
		name := secretPattern.FindStringSubmatch(match)[1]
		value, ok := s.Get(name)
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("missing secret(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}
