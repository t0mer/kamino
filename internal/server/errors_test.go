package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSONSetsStatusAndContentType(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, http.StatusCreated, map[string]string{"ok": "yes"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "yes", body["ok"])
}

func TestWriteJSONWithNilBodyWritesNoBody(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, http.StatusNoContent, nil)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
}

func TestWriteErrorRendersTheSingleErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()

	writeError(rec, http.StatusBadRequest, "bad request")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "bad request", body.Error)
	assert.Empty(t, body.Details)
}

func TestWriteErrorIncludesDetailsWhenGiven(t *testing.T) {
	rec := httptest.NewRecorder()

	writeError(rec, http.StatusUnprocessableEntity, "invalid config", "dev/go: unknown arch", "tools/docker: cycle")

	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid config", body.Error)
	assert.Equal(t, []string{"dev/go: unknown arch", "tools/docker: cycle"}, body.Details)
}
