// Package version exposes the build-injected version string.
package version

import "fmt"

// Version is the release version, injected at build time with
// -ldflags "-X github.com/t0mer/kamino/internal/version.Version=<v>".
// It is empty in development builds.
var Version string

// String returns a human-readable version banner.
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	return fmt.Sprintf("kamino %s", v)
}
