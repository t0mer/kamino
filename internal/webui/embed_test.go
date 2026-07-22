package webui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/webui"
)

func TestHandlerWithoutABuildReturnsAnError(t *testing.T) {
	// Before any `npm run build`, dist holds only .gitkeep. Handler must
	// report that rather than panic, so the server can still serve the API.
	_, err := webui.Handler()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index.html")
}
