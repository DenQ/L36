package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"l36/internal/models"
	"l36/internal/storage"

	"github.com/stretchr/testify/assert"
)

func newHandler() *Handler {
	return &Handler{store: storage.NewPageStorage()}
}

func TestFullPageFlow(t *testing.T) {
	h := newHandler()
	testPID := "test-page-1"
	content1 := map[string]string{"text": "Version 1"}
	body, _ := json.Marshal(map[string]any{
		"pageId":  testPID,
		"content": content1,
	})

	req1 := httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body))
	rr1 := httptest.NewRecorder()
	h.CreatePageHandler(rr1, req1)

	assert.Equal(t, http.StatusCreated, rr1.Code)

	var page1 models.Page
	json.Unmarshal(rr1.Body.Bytes(), &page1)
	pid := page1.ID
	vid1 := page1.Versions[0].ID

	assert.NotEmpty(t, pid)
	assert.True(t, page1.Versions[0].IsLatest)
	assert.Equal(t, `{"text":"Version 1"}`, page1.Versions[0].Content)

	content2 := map[string]string{"text": "Version 2"}
	body2, _ := json.Marshal(map[string]any{"content": content2})

	req2 := httptest.NewRequest("POST", "/api/pages/"+pid+"/versions", bytes.NewBuffer(body2))
	req2.SetPathValue("pid", pid)
	rr2 := httptest.NewRecorder()
	h.AddVersionHandler(rr2, req2)

	assert.Equal(t, http.StatusCreated, rr2.Code)

	var ver2 models.Version
	json.Unmarshal(rr2.Body.Bytes(), &ver2)
	assert.True(t, ver2.IsLatest)
	assert.Equal(t, vid1, ver2.ParentID)
	assert.Equal(t, `{"text":"Version 2"}`, ver2.Content)

	req3 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	req3.SetPathValue("pid", pid)
	rr3 := httptest.NewRecorder()
	h.GetHistoryHandler(rr3, req3)

	var history []models.Version
	json.Unmarshal(rr3.Body.Bytes(), &history)
	assert.Len(t, history, 2)
	assert.False(t, history[0].IsLatest, "Первая версия больше не актуальна")
	assert.True(t, history[1].IsLatest, "Вторая версия актуальна")

	req4 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions/"+vid1, nil)
	req4.SetPathValue("pid", pid)
	req4.SetPathValue("vid", vid1)
	rr4 := httptest.NewRecorder()
	h.GetVersionHandler(rr4, req4)

	var v1Fetched models.Version
	json.Unmarshal(rr4.Body.Bytes(), &v1Fetched)
	assert.Equal(t, vid1, v1Fetched.ID)
	assert.Equal(t, `{"text":"Version 1"}`, v1Fetched.Content)

	req5 := httptest.NewRequest("POST", "/api/pages/"+pid+"/versions/"+vid1+"/latest", nil)
	req5.SetPathValue("pid", pid)
	req5.SetPathValue("vid", vid1)
	rr5 := httptest.NewRecorder()
	h.SetLatestHandler(rr5, req5)

	assert.Equal(t, http.StatusOK, rr5.Code)

	req6 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	req6.SetPathValue("pid", pid)
	rr6 := httptest.NewRecorder()
	h.GetHistoryHandler(rr6, req6)

	json.Unmarshal(rr6.Body.Bytes(), &history)
	assert.True(t, history[0].IsLatest, "V1 снова актуальна")
	assert.False(t, history[1].IsLatest, "V2 больше не актуальна")
}

func TestDeletePage(t *testing.T) {
	h := newHandler()
	testPID := "test-page-1234"
	content := map[string]string{"text": "To be deleted"}
	body, _ := json.Marshal(map[string]any{
		"content": content,
		"pageId":  testPID,
	})

	reqCreate := httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body))
	rrCreate := httptest.NewRecorder()
	h.CreatePageHandler(rrCreate, reqCreate)

	var page models.Page
	json.Unmarshal(rrCreate.Body.Bytes(), &page)
	pid := page.ID

	reqDelete := httptest.NewRequest("DELETE", "/api/pages/"+pid, nil)
	reqDelete.SetPathValue("pid", pid)
	rrDelete := httptest.NewRecorder()
	h.DeletePageHandler(rrDelete, reqDelete)

	assert.Equal(t, http.StatusNoContent, rrDelete.Code, "Должен быть статус 204")

	reqGet := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	reqGet.SetPathValue("pid", pid)
	rrGet := httptest.NewRecorder()
	h.GetHistoryHandler(rrGet, reqGet)

	assert.Equal(t, http.StatusNotFound, rrGet.Code, "После удаления страница не должна находиться")
}

func TestConcurrentVersions(t *testing.T) {
	h := newHandler()
	testPID := fmt.Sprintf("test-page-concurrent-%d", time.Now().UnixNano())
	content := map[string]string{"text": "Initial"}
	pBody, _ := json.Marshal(map[string]any{
		"content": content,
		"pageId":  testPID,
	})
	rrP := httptest.NewRecorder()
	h.CreatePageHandler(rrP, httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(pBody)))
	assert.Equal(t, http.StatusCreated, rrP.Code, rrP.Body.String())

	var page models.Page
	json.Unmarshal(rrP.Body.Bytes(), &page)
	pid := page.ID

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(val int) {
			defer wg.Done()
			c := map[string]int{"val": val}
			b, _ := json.Marshal(map[string]any{"content": c})

			req := httptest.NewRequest("POST", "/api/pages/"+pid+"/versions", bytes.NewBuffer(b))
			req.SetPathValue("pid", pid)
			h.AddVersionHandler(httptest.NewRecorder(), req)
		}(i)
	}

	wg.Wait()

	reqH := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	reqH.SetPathValue("pid", pid)
	rrH := httptest.NewRecorder()
	h.GetHistoryHandler(rrH, reqH)

	var history []models.Version
	json.Unmarshal(rrH.Body.Bytes(), &history)

	assert.Len(t, history, workers+1, rrH.Body.String())

	latestCount := 0
	got := make(map[string]int, len(history))
	for _, v := range history {
		if v.IsLatest {
			latestCount++
		}
		reqV := httptest.NewRequest(http.MethodGet, "/api/pages/"+pid+"/versions/"+v.ID, nil)
		reqV.SetPathValue("pid", pid)
		reqV.SetPathValue("vid", v.ID)
		rrV := httptest.NewRecorder()
		h.GetVersionHandler(rrV, reqV)
		assert.Equal(t, http.StatusOK, rrV.Code)

		var restored models.Version
		assert.NoError(t, json.Unmarshal(rrV.Body.Bytes(), &restored))
		got[restored.Content]++
	}
	assert.Equal(t, 1, latestCount, "В любой момент времени должна быть только одна актуальная версия")

	assert.Equal(t, 1, got[`{"text":"Initial"}`])
	for i := 0; i < workers; i++ {
		assert.Equal(t, 1, got[fmt.Sprintf(`{"val":%d}`, i)])
	}
}

func TestSetLatestRollback(t *testing.T) {
	h := newHandler()
	pID := "rollback-test-id"
	content1 := "First Strategic Data"
	body1, _ := json.Marshal(map[string]any{"pageId": pID, "content": content1})

	rr1 := httptest.NewRecorder()
	h.CreatePageHandler(rr1, httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body1)))

	var page models.Page
	json.Unmarshal(rr1.Body.Bytes(), &page)
	v1ID := page.Versions[0].ID

	content2 := "Second Strategic Data"
	body2, _ := json.Marshal(map[string]any{"content": content2})

	req2 := httptest.NewRequest("POST", "/api/pages/"+pID+"/versions", bytes.NewBuffer(body2))
	req2.SetPathValue("pid", pID)
	h.AddVersionHandler(httptest.NewRecorder(), req2)

	reqL := httptest.NewRequest("POST", "/api/pages/"+pID+"/versions/"+v1ID+"/latest", nil)
	reqL.SetPathValue("pid", pID)
	reqL.SetPathValue("vid", v1ID)
	rrL := httptest.NewRecorder()

	h.SetLatestHandler(rrL, reqL)
	assert.Equal(t, http.StatusOK, rrL.Code)

	reqH := httptest.NewRequest("GET", "/api/pages/"+pID+"/versions", nil)
	reqH.SetPathValue("pid", pID)
	rrH := httptest.NewRecorder()
	h.GetHistoryHandler(rrH, reqH)

	var history []models.Version
	json.Unmarshal(rrH.Body.Bytes(), &history)

	assert.Len(t, history, 2)

	for _, v := range history {
		if v.ID == v1ID {
			assert.True(t, v.IsLatest, "V1 должна стать Latest после отката")
		} else {
			assert.False(t, v.IsLatest, "V2 должна потерять статус Latest")
		}
	}
}

func TestMissingVersionSaysVersionNotFound(t *testing.T) {
	h := newHandler()
	const pid = "missing-page-404-phrases"
	const want = "Version not found\n"

	reqVersion := httptest.NewRequest(http.MethodGet, "/api/pages/"+pid+"/versions/nope", nil)
	reqVersion.SetPathValue("pid", pid)
	reqVersion.SetPathValue("vid", "nope")
	rrVersion := httptest.NewRecorder()
	h.GetVersionHandler(rrVersion, reqVersion)
	assert.Equal(t, http.StatusNotFound, rrVersion.Code)
	assert.Equal(t, want, rrVersion.Body.String())

	reqLatest := httptest.NewRequest(http.MethodPost, "/api/pages/"+pid+"/versions/nope/latest", nil)
	reqLatest.SetPathValue("pid", pid)
	reqLatest.SetPathValue("vid", "nope")
	rrLatest := httptest.NewRecorder()
	h.SetLatestHandler(rrLatest, reqLatest)
	assert.Equal(t, http.StatusNotFound, rrLatest.Code)
	assert.Equal(t, want, rrLatest.Body.String())

	reqGetLatest := httptest.NewRequest(http.MethodGet, "/api/pages/"+pid+"/versions/latest", nil)
	reqGetLatest.SetPathValue("pid", pid)
	rrGetLatest := httptest.NewRecorder()
	h.GetLatestVersionHandler(rrGetLatest, reqGetLatest)
	assert.Equal(t, http.StatusNotFound, rrGetLatest.Code)
	assert.Equal(t, want, rrGetLatest.Body.String())
}

func TestCreatePageRejectsEmptyID(t *testing.T) {
	h := newHandler()
	bodies := []string{
		`{"pageId":"","content":"nope"}`,
		`{"content":"nope"}`,
	}

	for _, body := range bodies {
		req := httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewBufferString(body))
		rr := httptest.NewRecorder()
		h.CreatePageHandler(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	}

	_, found := h.store.GetHistory("")
	assert.False(t, found)
}

func TestCreatePageConflict(t *testing.T) {
	h := newHandler()
	const pid = "handler-create-duplicate"
	first := `{"pageId":"` + pid + `","content":"original"}`
	second := `{"pageId":"` + pid + `","content":"replacement"}`

	req1 := httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewBufferString(first))
	rr1 := httptest.NewRecorder()
	h.CreatePageHandler(rr1, req1)
	assert.Equal(t, http.StatusCreated, rr1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewBufferString(second))
	rr2 := httptest.NewRecorder()
	h.CreatePageHandler(rr2, req2)
	assert.Equal(t, http.StatusConflict, rr2.Code)
	assert.Equal(t, "Page already exists\n", rr2.Body.String())

	history, found := h.store.GetHistory(pid)
	assert.True(t, found)
	assert.Equal(t, "original", history[0].Content)
}

func TestHistoryListIsFlat(t *testing.T) {
	h := newHandler()
	create := httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewBufferString(`{"pageId":"flat-tree","content":"A"}`))
	rrCreate := httptest.NewRecorder()
	h.CreatePageHandler(rrCreate, create)
	assert.Equal(t, http.StatusCreated, rrCreate.Code)

	var page models.Page
	assert.NoError(t, json.Unmarshal(rrCreate.Body.Bytes(), &page))

	add := httptest.NewRequest(http.MethodPost, "/api/pages/flat-tree/versions", bytes.NewBufferString(`{"content":"B"}`))
	add.SetPathValue("pid", "flat-tree")
	rrAdd := httptest.NewRecorder()
	h.AddVersionHandler(rrAdd, add)
	assert.Equal(t, http.StatusCreated, rrAdd.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/pages/flat-tree/versions", nil)
	req.SetPathValue("pid", "flat-tree")
	rr := httptest.NewRecorder()
	h.GetHistoryHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), `"patch"`)
	assert.NotContains(t, rr.Body.String(), `"content"`)

	var items []models.VersionListItem
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &items))
	assert.Len(t, items, 2)
	assert.Equal(t, page.Versions[0].ID, items[1].ParentID)
	assert.True(t, items[1].IsLatest)
	assert.False(t, items[0].IsLatest)
}

func TestHistoryListShowsBothBranches(t *testing.T) {
	h := newHandler()
	create := httptest.NewRequest(http.MethodPost, "/api/pages", bytes.NewBufferString(`{"pageId":"flat-branches","content":"A"}`))
	rrCreate := httptest.NewRecorder()
	h.CreatePageHandler(rrCreate, create)
	assert.Equal(t, http.StatusCreated, rrCreate.Code)

	var page models.Page
	assert.NoError(t, json.Unmarshal(rrCreate.Body.Bytes(), &page))
	rootID := page.Versions[0].ID

	addB := httptest.NewRequest(http.MethodPost, "/api/pages/flat-branches/versions", bytes.NewBufferString(`{"content":"B"}`))
	addB.SetPathValue("pid", "flat-branches")
	rrB := httptest.NewRecorder()
	h.AddVersionHandler(rrB, addB)
	assert.Equal(t, http.StatusCreated, rrB.Code)
	var b models.Version
	assert.NoError(t, json.Unmarshal(rrB.Body.Bytes(), &b))

	addC := httptest.NewRequest(http.MethodPost, "/api/pages/flat-branches/versions", bytes.NewBufferString(`{"content":"C"}`))
	addC.SetPathValue("pid", "flat-branches")
	rrC := httptest.NewRecorder()
	h.AddVersionHandler(rrC, addC)
	assert.Equal(t, http.StatusCreated, rrC.Code)

	move := httptest.NewRequest(http.MethodPost, "/api/pages/flat-branches/versions/"+b.ID+"/latest", nil)
	move.SetPathValue("pid", "flat-branches")
	move.SetPathValue("vid", b.ID)
	rrMove := httptest.NewRecorder()
	h.SetLatestHandler(rrMove, move)
	assert.Equal(t, http.StatusOK, rrMove.Code)
	var moved models.Version
	assert.NoError(t, json.Unmarshal(rrMove.Body.Bytes(), &moved))
	assert.Equal(t, "B", moved.Content)

	addBA := httptest.NewRequest(http.MethodPost, "/api/pages/flat-branches/versions", bytes.NewBufferString(`{"content":"BA"}`))
	addBA.SetPathValue("pid", "flat-branches")
	rrBA := httptest.NewRecorder()
	h.AddVersionHandler(rrBA, addBA)
	assert.Equal(t, http.StatusCreated, rrBA.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/pages/flat-branches/versions", nil)
	req.SetPathValue("pid", "flat-branches")
	rr := httptest.NewRecorder()
	h.GetHistoryHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), `"patch"`)
	assert.NotContains(t, rr.Body.String(), `"content"`)

	var items []models.VersionListItem
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &items))
	assert.Len(t, items, 4)

	childrenOfB := 0
	var root models.VersionListItem
	for _, item := range items {
		if item.ID == rootID {
			root = item
		}
		if item.ParentID == b.ID {
			childrenOfB++
		}
	}
	assert.Empty(t, root.ParentID)
	assert.False(t, root.IsLatest)
	assert.Equal(t, 2, childrenOfB)
}
