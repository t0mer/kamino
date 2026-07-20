package remote

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeJoin joins root with the given path segments and returns the resulting
// path, performing a LEXICAL check to reject any segment or combination that
// would resolve outside root. Path segments handled by this function ultimately
// come from the config repo — manifest category/profile paths, stack file names,
// cache ref/file components — which is untrusted content. Since callers of this
// package run as root, a segment containing ".." or an absolute path must never
// be allowed to make them read or write files outside the intended root
// directory.
//
// The check is purely lexical: it uses filepath.Clean and filepath.Rel, not
// filepath.EvalSymlinks. A symlink placed inside the root that points outside it
// will not be detected. This gap is acceptable because exploiting it requires
// write access to the root directory itself — a stronger threat model than the
// untrusted-manifest-string attack this guard defends against.
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
