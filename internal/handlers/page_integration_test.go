package handlers

import (
	"bytes"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"l36/internal/models"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestFullPageFlow(t *testing.T) {
	testPID := "test-page-1"
	content1 := map[string]string{"text": "Version 1"}
	body, _ := json.Marshal(map[string]any{
		"pageId":  testPID,
		"content": content1,
	})

	req1 := httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body))
	rr1 := httptest.NewRecorder()
	CreatePageHandler(rr1, req1)

	assert.Equal(t, http.StatusCreated, rr1.Code)

	var page1 models.Page
	json.Unmarshal(rr1.Body.Bytes(), &page1)
	pid := page1.ID
	vid1 := page1.Versions[0].ID

	assert.NotEmpty(t, pid)
	assert.True(t, page1.Versions[0].IsLatest)

	content2 := map[string]string{"text": "Version 2"}
	body2, _ := json.Marshal(map[string]any{"content": content2})

	req2 := httptest.NewRequest("POST", "/api/pages/"+pid+"/versions", bytes.NewBuffer(body2))
	req2.SetPathValue("pid", pid)
	rr2 := httptest.NewRecorder()
	AddVersionHandler(rr2, req2)

	assert.Equal(t, http.StatusCreated, rr2.Code)

	var ver2 models.Version
	json.Unmarshal(rr2.Body.Bytes(), &ver2)
	assert.True(t, ver2.IsLatest)
	assert.Equal(t, vid1, ver2.ParentID)

	req3 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	req3.SetPathValue("pid", pid)
	rr3 := httptest.NewRecorder()
	GetHistoryHandler(rr3, req3)

	var history []models.Version
	json.Unmarshal(rr3.Body.Bytes(), &history)
	assert.Len(t, history, 2)
	assert.False(t, history[0].IsLatest, "Первая версия больше не актуальна")
	assert.True(t, history[1].IsLatest, "Вторая версия актуальна")

	req4 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions/"+vid1, nil)
	req4.SetPathValue("pid", pid)
	req4.SetPathValue("vid", vid1)
	rr4 := httptest.NewRecorder()
	GetVersionHandler(rr4, req4)

	var v1Fetched models.Version
	json.Unmarshal(rr4.Body.Bytes(), &v1Fetched)
	assert.Equal(t, vid1, v1Fetched.ID)

	req5 := httptest.NewRequest("POST", "/api/pages/"+pid+"/versions/"+vid1+"/latest", nil)
	req5.SetPathValue("pid", pid)
	req5.SetPathValue("vid", vid1)
	rr5 := httptest.NewRecorder()
	SetLatestHandler(rr5, req5)

	assert.Equal(t, http.StatusOK, rr5.Code)

	req6 := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	req6.SetPathValue("pid", pid)
	rr6 := httptest.NewRecorder()
	GetHistoryHandler(rr6, req6)

	json.Unmarshal(rr6.Body.Bytes(), &history)
	assert.True(t, history[0].IsLatest, "V1 снова актуальна")
	assert.False(t, history[1].IsLatest, "V2 больше не актуальна")
}

func TestDeletePage(t *testing.T) {
	testPID := "test-page-1234"
	content := map[string]string{"text": "To be deleted"}
	body, _ := json.Marshal(map[string]any{
		"content": content,
		"pageId":  testPID,
	})

	reqCreate := httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body))
	rrCreate := httptest.NewRecorder()
	CreatePageHandler(rrCreate, reqCreate)

	var page models.Page
	json.Unmarshal(rrCreate.Body.Bytes(), &page)
	pid := page.ID

	reqDelete := httptest.NewRequest("DELETE", "/api/pages/"+pid, nil)
	reqDelete.SetPathValue("pid", pid)
	rrDelete := httptest.NewRecorder()
	DeletePageHandler(rrDelete, reqDelete)

	assert.Equal(t, http.StatusNoContent, rrDelete.Code, "Должен быть статус 204")

	reqGet := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	reqGet.SetPathValue("pid", pid)
	rrGet := httptest.NewRecorder()
	GetHistoryHandler(rrGet, reqGet)

	assert.Equal(t, http.StatusNotFound, rrGet.Code, "После удаления страница не должна находиться")
}

func TestConcurrentVersions(t *testing.T) {
	testPID := "test-page-12345"
	content := map[string]string{"text": "Initial"}
	pBody, _ := json.Marshal(map[string]any{
		"content": content,
		"pageId":  testPID,
	})
	rrP := httptest.NewRecorder()
	CreatePageHandler(rrP, httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(pBody)))

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
			AddVersionHandler(httptest.NewRecorder(), req)
		}(i)
	}

	wg.Wait()

	reqH := httptest.NewRequest("GET", "/api/pages/"+pid+"/versions", nil)
	reqH.SetPathValue("pid", pid)
	rrH := httptest.NewRecorder()
	GetHistoryHandler(rrH, reqH)

	var history []models.Version
	json.Unmarshal(rrH.Body.Bytes(), &history)

	assert.Len(t, history, workers+1)

	latestCount := 0
	for _, v := range history {
		if v.IsLatest {
			latestCount++
		}
	}
	assert.Equal(t, 1, latestCount, "В любой момент времени должна быть только одна актуальная версия")
}

func TestSetLatestRollback(t *testing.T) {
	pID := "rollback-test-id"
	content1 := "First Strategic Data"
	body1, _ := json.Marshal(map[string]any{"pageId": pID, "content": content1})

	rr1 := httptest.NewRecorder()
	CreatePageHandler(rr1, httptest.NewRequest("POST", "/api/pages", bytes.NewBuffer(body1)))

	var page models.Page
	json.Unmarshal(rr1.Body.Bytes(), &page)
	v1ID := page.Versions[0].ID

	content2 := "Second Strategic Data"
	body2, _ := json.Marshal(map[string]any{"content": content2})

	req2 := httptest.NewRequest("POST", "/api/pages/"+pID+"/versions", bytes.NewBuffer(body2))
	req2.SetPathValue("pid", pID)
	AddVersionHandler(httptest.NewRecorder(), req2)

	reqL := httptest.NewRequest("POST", "/api/pages/"+pID+"/versions/"+v1ID+"/latest", nil)
	reqL.SetPathValue("pid", pID)
	reqL.SetPathValue("vid", v1ID)
	rrL := httptest.NewRecorder()

	SetLatestHandler(rrL, reqL)
	assert.Equal(t, http.StatusOK, rrL.Code)

	reqH := httptest.NewRequest("GET", "/api/pages/"+pID+"/versions", nil)
	reqH.SetPathValue("pid", pID)
	rrH := httptest.NewRecorder()
	GetHistoryHandler(rrH, reqH)

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
