package remote

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin joins root with the given path segments and returns the resulting
// path, rejecting any segment or combination that would resolve outside
// root. Path segments handled by this function ultimately come from the
// config repo — manifest category/profile paths, stack file names, cache
// ref/file components — which is untrusted content. Since callers of this
// package run as root, a segment containing ".." or an absolute path must
// never be allowed to make them read or write files outside the intended
// root directory.
func SafeJoin(root string, elem ...string) (string, error) {
	for _, e := range elem {
		if filepath.IsAbs(e) {
			return "", fmt.Errorf("path %q is absolute; must be relative to %q", e, root)
		}
	}

	cleanRoot := filepath.Clean(root)
	p := filepath.Join(append([]string{cleanRoot}, elem...)...)

	rel, err := filepath.Rel(cleanRoot, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", filepath.Join(elem...), root)
	}
	return p, nil
}
