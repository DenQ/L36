package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"l36/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestE2EPageFlow(t *testing.T) {
	t.Parallel()
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
	respGet.Body.Close()
}

func TestHTTPRoutesCoverGaps(t *testing.T) {
	pid := fmt.Sprintf("http-gap-%d", time.Now().UnixNano())
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := ts.Client()

	bad, err := client.Post(ts.URL+"/api/pages", "application/json", strings.NewReader("{"))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, bad.StatusCode)
	bad.Body.Close()

	created := postJSON(t, client, ts.URL+"/api/pages", `{"pageId":"`+pid+`","content":"first text"}`)
	assert.Equal(t, http.StatusCreated, created.StatusCode)
	var page models.Page
	assert.NoError(t, json.NewDecoder(created.Body).Decode(&page))
	created.Body.Close()
	assert.Equal(t, "first text", page.Versions[0].Content)
	v1 := page.Versions[0].ID

	latest := getJSON(t, client, ts.URL+"/api/pages/"+pid+"/versions/latest")
	assert.Equal(t, http.StatusOK, latest.StatusCode)
	assert.Equal(t, "first text", decodeVersion(t, latest).Content)

	added := postJSON(t, client, ts.URL+"/api/pages/"+pid+"/versions", `{"content":"second text"}`)
	assert.Equal(t, http.StatusCreated, added.StatusCode)
	assert.Equal(t, "second text", decodeVersion(t, added).Content)

	badVersion, err := client.Post(ts.URL+"/api/pages/"+pid+"/versions", "application/json", strings.NewReader("{"))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, badVersion.StatusCode)
	badVersion.Body.Close()

	byID := getJSON(t, client, ts.URL+"/api/pages/"+pid+"/versions/"+v1)
	assert.Equal(t, http.StatusOK, byID.StatusCode)
	assert.Equal(t, "first text", decodeVersion(t, byID).Content)

	unknownVersion := getJSON(t, client, ts.URL+"/api/pages/"+pid+"/versions/missing-version")
	assert.Equal(t, http.StatusNotFound, unknownVersion.StatusCode)
	unknownVersion.Body.Close()

	setLatest, err := http.NewRequest(http.MethodPost, ts.URL+"/api/pages/"+pid+"/versions/"+v1+"/latest", nil)
	assert.NoError(t, err)
	setResp, err := client.Do(setLatest)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, setResp.StatusCode)
	setResp.Body.Close()

	rolled := getJSON(t, client, ts.URL+"/api/pages/"+pid+"/versions/latest")
	assert.Equal(t, http.StatusOK, rolled.StatusCode)
	assert.Equal(t, "first text", decodeVersion(t, rolled).Content)

	unknownPage := getJSON(t, client, ts.URL+"/api/pages/missing-http-page/versions")
	assert.Equal(t, http.StatusNotFound, unknownPage.StatusCode)
	unknownPage.Body.Close()

	unknownLatest := getJSON(t, client, ts.URL+"/api/pages/missing-http-page/versions/latest")
	assert.Equal(t, http.StatusNotFound, unknownLatest.StatusCode)
	unknownLatest.Body.Close()

	del, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/pages/missing-http-page", nil)
	assert.NoError(t, err)
	delResp, err := client.Do(del)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, delResp.StatusCode)
	delResp.Body.Close()

	firstDel, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/pages/"+pid, nil)
	assert.NoError(t, err)
	firstDelResp, err := client.Do(firstDel)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, firstDelResp.StatusCode)
	firstDelResp.Body.Close()

	secondDel, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/pages/"+pid, nil)
	assert.NoError(t, err)
	secondDelResp, err := client.Do(secondDel)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, secondDelResp.StatusCode)
	secondDelResp.Body.Close()
}

func postJSON(t *testing.T, client *http.Client, url, body string) *http.Response {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	assert.NoError(t, err)
	return resp
}

func getJSON(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	assert.NoError(t, err)
	return resp
}

func decodeVersion(t *testing.T, resp *http.Response) models.Version {
	t.Helper()
	defer resp.Body.Close()
	var version models.Version
	assert.NoError(t, json.NewDecoder(resp.Body).Decode(&version))
	return version
}
