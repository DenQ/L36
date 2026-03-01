package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"l36/internal/models"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestE2EPageFlow(t *testing.T) {
	// t.Parallel()
	testPID := "test-page-36"
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	ts := httptest.NewServer(Logger(mux))
	defer ts.Close()

	client := ts.Client()

	content := map[string]string{"title": "E2E Test"}
	body, _ := json.Marshal(map[string]any{
		"content": content,
		"pageId":  testPID,
	})

	resp, err := client.Post(ts.URL+"/api/pages", "application/json", bytes.NewBuffer(body))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var page models.Page
	json.NewDecoder(resp.Body).Decode(&page)
	resp.Body.Close()

	pid := page.ID
	assert.NotEmpty(t, pid)

	versionContent := map[string]string{"title": "Updated by E2E"}
	vBody, _ := json.Marshal(map[string]any{"content": versionContent})

	vUrl := fmt.Sprintf("%s/api/pages/%s/versions", ts.URL, pid)
	vResp, err := client.Post(vUrl, "application/json", bytes.NewBuffer(vBody))

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, vResp.StatusCode)

	var version models.Version
	json.NewDecoder(vResp.Body).Decode(&version)
	vResp.Body.Close()

	assert.True(t, version.IsLatest)
	fmt.Printf("\n[E2E] Successfully tested Page %s with Version %s\n", pid, version.ID)

	reqDel, _ := http.NewRequest("DELETE", ts.URL+"/api/pages/"+pid, nil)
	respDel, err := client.Do(reqDel)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, respDel.StatusCode)

	respGet, _ := client.Get(ts.URL + "/api/pages/" + pid + "/versions")
	assert.Equal(t, http.StatusNotFound, respGet.StatusCode)
}
