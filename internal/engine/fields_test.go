package engine_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/manifest"
)

// expandedFields are the manifest.Item fields that Resolve runs through
// {version} / {secret:NAME} expansion.
var expandedFields = []string{
	"Check",
	"CheckContains",
	"Packages",
	"PostInstall",
	"PreInstall",
	"Source",
}

// verbatimFields are the manifest.Item fields Resolve copies or consumes
// without expansion. SHA256 belongs here deliberately: a checksum is an opaque
// literal, and templating one would be meaningless.
var verbatimFields = []string{
	"Arch",
	"CategoryID",
	"DependsOn",
	"EnvFile",
	"Files",
	"ID",
	"InstallDir",
	"Name",
	"Path",
	"PathExport",
	"Python",
	"Repo",
	"SHA256",
	"Secrets",
	"Timeout",
	"Type",
	"Version",
}

// TestResolveCoversEveryManifestItemField fails when manifest.Item gains or
// loses a field.
//
// Resolve is the single place templating happens — every runner consumes a
// ResolvedItem and never sees a placeholder. A newly added field that nobody
// remembers to expand would reach a root shell as a literal "{version}" or, far
// worse, as an unexpanded "{secret:TOKEN}". Nothing else in the suite would
// notice, so this test exists to force the decision at the moment the field is
// added.
func TestResolveCoversEveryManifestItemField(t *testing.T) {
	var known []string
	known = append(known, expandedFields...)
	known = append(known, verbatimFields...)
	sort.Strings(known)

	var actual []string
	itemType := reflect.TypeOf(manifest.Item{})
	for i := 0; i < itemType.NumField(); i++ {
		actual = append(actual, itemType.Field(i).Name)
	}
	sort.Strings(actual)

	assert.Equal(t, known, actual,
		"manifest.Item's fields changed.\n"+
			"Decide whether each added or renamed field can contain a {version} or\n"+
			"{secret:NAME} placeholder:\n"+
			"  - if it CAN, expand it in engine.Resolve (internal/engine/item.go) and\n"+
			"    add its name to expandedFields below;\n"+
			"  - if it CANNOT, add its name to verbatimFields below.\n"+
			"Skipping this decision lets an unexpanded placeholder reach a root shell.")
}
