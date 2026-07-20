package sysinfo_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/sysinfo"
)

func TestParseOSRelease(t *testing.T) {
	in := `PRETTY_NAME="Ubuntu 24.04.3 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
ID=ubuntu
ID_LIKE=debian
`
	distro, versionID, err := sysinfo.ParseOSRelease(strings.NewReader(in))

	require.NoError(t, err)
	assert.Equal(t, "ubuntu", distro)
	assert.Equal(t, "24.04", versionID)
}

func TestParseOSReleaseHandlesUnquotedValues(t *testing.T) {
	distro, versionID, err := sysinfo.ParseOSRelease(strings.NewReader("ID=debian\nVERSION_ID=12\n"))

	require.NoError(t, err)
	assert.Equal(t, "debian", distro)
	assert.Equal(t, "12", versionID)
}

func TestDetectReportsArchAndHostname(t *testing.T) {
	got, err := sysinfo.Detect()

	require.NoError(t, err)
	assert.Equal(t, runtime.GOARCH, got.Arch)
	assert.NotEmpty(t, got.Hostname)
}

func TestRequireUbuntuRootRejectsNonUbuntu(t *testing.T) {
	err := sysinfo.Info{Distro: "debian", Root: true}.RequireUbuntuRoot()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Ubuntu")
}

func TestRequireUbuntuRootRejectsNonRoot(t *testing.T) {
	err := sysinfo.Info{Distro: "ubuntu", Root: false}.RequireUbuntuRoot()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sudo")
}

func TestRequireUbuntuRootAcceptsUbuntuRoot(t *testing.T) {
	assert.NoError(t, sysinfo.Info{Distro: "ubuntu", Root: true}.RequireUbuntuRoot())
}
