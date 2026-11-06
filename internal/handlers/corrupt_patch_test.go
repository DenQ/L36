package handlers

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"l36/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestGetVersionCorruptPatchReturns500(t *testing.T) {
	const pid = "corrupt-patch-http"
	const secret = "current-secret-text"

	page := models.Page{
		ID:          pid,
		LatestIndex: 1,
		Versions: []models.Version{
			{ID: "v1", Patch: "not-a-valid-delta"},
			{ID: "v2", Content: secret, IsLatest: true},
		},
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(pid))
	idx := int(h.Sum32() % 36)

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("shard_%d.json", idx)))
	assert.NoError(t, err)
	assert.NoError(t, json.NewEncoder(f).Encode(page))
	assert.NoError(t, f.Close())

	api := newHandler()
	assert.NoError(t, api.store.Load(dir))

	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+pid+"/versions/v1", nil)
	req.SetPathValue("pid", pid)
	req.SetPathValue("vid", "v1")
	rr := httptest.NewRecorder()
	api.GetVersionHandler(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "Failed to restore version\n", rr.Body.String())
	assert.NotContains(t, rr.Body.String(), secret)
}
