package exec

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadLineCapBoundary pins the cap accounting at its edges. The newline
// delimiter must not count against the cap: counting it marked a line whose
// content landed exactly on the cap as truncated, which would attach a
// misleading marker to output that was in fact captured whole.
func TestReadLineCapBoundary(t *testing.T) {
	tests := []struct {
		name          string
		contentLen    int
		wantTruncated bool
		wantLen       int
	}{
		{"one byte under the cap", maxLineBytes - 1, false, maxLineBytes - 1},
		{"exactly at the cap", maxLineBytes, false, maxLineBytes},
		{"one byte over the cap", maxLineBytes + 1, true, maxLineBytes},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Repeat("x", tc.contentLen) + "\nfollowing\n"
			br := bufio.NewReaderSize(strings.NewReader(input), 64*1024)

			line, truncated, err := readLine(br)

			require.NoError(t, err)
			assert.Equal(t, tc.wantTruncated, truncated)
			assert.Len(t, line, tc.wantLen)

			// Whatever happened to the long line, the stream must keep flowing.
			next, _, err := readLine(br)
			require.NoError(t, err)
			assert.Equal(t, "following", next)
		})
	}
}
