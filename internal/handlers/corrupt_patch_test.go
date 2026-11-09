package handlers

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetVersionCorruptPatchReturns500(t *testing.T) {
	const pid = "corrupt-patch-http"
	const secret = "current-secret-text"

	h := fnv.New32a()
	_, _ = h.Write([]byte(pid))
	idx := int(h.Sum32() % 36)

	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("shard_%d.log", idx)))
	assert.NoError(t, err)
	writeLogFrame(t, f, map[string]any{
		"op": "create", "pageId": pid, "versionId": "v1", "dt": 1, "latest": true, "snapshot": true,
	}, map[string]any{"content": "original"})
	writeLogFrame(t, f, map[string]any{
		"op": "add", "pageId": pid, "versionId": "v2", "parentId": "v1", "dt": 2, "latest": true, "snapshot": true,
	}, map[string]any{"content": "middle", "patch": "=1"})
	writeLogFrame(t, f, map[string]any{
		"op": "add", "pageId": pid, "versionId": "v3", "parentId": "v2", "dt": 3, "latest": true, "snapshot": true,
	}, map[string]any{"content": secret, "patch": "not-a-valid-delta"})
	assert.NoError(t, f.Close())

	api := newHandler()
	assert.NoError(t, api.store.Load(dir))

	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+pid+"/versions/v2", nil)
	req.SetPathValue("pid", pid)
	req.SetPathValue("vid", "v2")
	rr := httptest.NewRecorder()
	api.GetVersionHandler(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "Failed to restore version\n", rr.Body.String())
	assert.NotContains(t, rr.Body.String(), secret)
}

func writeLogFrame(t *testing.T, f *os.File, header, body map[string]any) {
	t.Helper()
	hdr, err := json.Marshal(header)
	assert.NoError(t, err)
	payload, err := json.Marshal(body)
	assert.NoError(t, err)

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(hdr)))
	_, err = f.Write(lenBuf[:])
	assert.NoError(t, err)
	_, err = f.Write(hdr)
	assert.NoError(t, err)
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	_, err = f.Write(lenBuf[:])
	assert.NoError(t, err)
	_, err = f.Write(payload)
	assert.NoError(t, err)
}
