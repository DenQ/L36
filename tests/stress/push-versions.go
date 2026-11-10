package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"l36/internal/handlers"
	"l36/internal/storage"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	stressWorkers       = 100
	stressVersions      = 50
	stressSamplePages   = 3
	stressGapVersionIdx = 14 // depth 15, between snapshots kept every 10 versions
)

type stressedPage struct {
	pid      string
	contents []string
}

func PushVersions(t *testing.T) {
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, storage.NewPageStorage())
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 100
	}

	totalRequests := stressWorkers * (stressVersions + 1)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	samples := make([]stressedPage, 0, stressSamplePages)
	wg.Add(stressWorkers)

	start := time.Now()

	for w := 0; w < stressWorkers; w++ {
		go func(workerID int) {
			defer wg.Done()

			pid := fmt.Sprintf("stress-%d", workerID)
			contents := make([]string, 0, stressVersions+1)
			note := func(format string, args ...any) {
				mu.Lock()
				failures = append(failures, fmt.Sprintf(format, args...))
				mu.Unlock()
			}

			initial := "Initial content"
			contents = append(contents, initial)
			pBody, _ := json.Marshal(map[string]any{"pageId": pid, "content": initial, "userId": "stress"})
			if status, err := postStatus(client, ts.URL+"/api/pages", pBody); err != nil || status != http.StatusCreated {
				note("%s create: status %d err %v", pid, status, err)
			}

			for i := 0; i < stressVersions; i++ {
				cont := fmt.Sprintf("Content version %d with random junk %s", i, uuid.New().String())
				contents = append(contents, cont)
				vBody, _ := json.Marshal(map[string]any{"content": cont, "userId": "stress"})
				vURL := fmt.Sprintf("%s/api/pages/%s/versions", ts.URL, pid)
				if status, err := postStatus(client, vURL, vBody); err != nil || status != http.StatusCreated {
					note("%s version %d: status %d err %v", pid, i, status, err)
				}
			}

			if workerID < stressSamplePages {
				mu.Lock()
				samples = append(samples, stressedPage{pid: pid, contents: contents})
				mu.Unlock()
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(start)
	rps := float64(totalRequests) / duration.Seconds()

	fmt.Printf("\n🚀 [L-36 HEAVY LIFESTYLE STRESS COMPLETE]\n")
	fmt.Printf("   Total Requests (Create + Versions): %d\n", totalRequests)
	fmt.Printf("   Duration: %v\n", duration)
	fmt.Printf("   Real-world RPS (with Diffs): %.2f\n\n", rps)

	if len(failures) > 0 {
		t.Fatalf("%d write failures, first: %s", len(failures), failures[0])
	}
	recordStress("PushVersions", totalRequests, duration, rps)

	for _, sample := range samples {
		checkRestoredVersions(t, client, ts.URL, sample)
	}
}

func postStatus(client *http.Client, url string, body []byte) (int, error) {
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	drain(resp)
	return resp.StatusCode, nil
}

func checkRestoredVersions(t *testing.T, client *http.Client, base string, page stressedPage) {
	t.Helper()

	historyURL := fmt.Sprintf("%s/api/pages/%s/versions", base, page.pid)
	body, status, err := getBody(client, historyURL)
	if err != nil || status != http.StatusOK {
		t.Errorf("%s history: status %d err %v", page.pid, status, err)
		return
	}

	var items []struct {
		ID string `json:"versionId"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		t.Errorf("%s history json: %v", page.pid, err)
		return
	}
	if len(items) != len(page.contents) {
		t.Errorf("%s history len %d, wrote %d", page.pid, len(items), len(page.contents))
		return
	}

	latestURL := fmt.Sprintf("%s/api/pages/%s/versions/latest", base, page.pid)
	assertVersionContent(t, client, latestURL, page.contents[len(page.contents)-1])

	gapURL := fmt.Sprintf("%s/api/pages/%s/versions/%s", base, page.pid, items[stressGapVersionIdx].ID)
	assertVersionContent(t, client, gapURL, page.contents[stressGapVersionIdx])
}

func assertVersionContent(t *testing.T, client *http.Client, url, want string) {
	t.Helper()
	body, status, err := getBody(client, url)
	if err != nil || status != http.StatusOK {
		t.Errorf("GET %s: status %d err %v", url, status, err)
		return
	}
	var got struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Errorf("GET %s json: %v", url, err)
		return
	}
	if got.Content != want {
		t.Errorf("GET %s content %q, want %q", url, got.Content, want)
	}
}

func getBody(client *http.Client, url string) ([]byte, int, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}
