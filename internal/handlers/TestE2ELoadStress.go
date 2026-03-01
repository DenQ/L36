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

	"github.com/google/uuid"
)

func TestE2EFullLifecycleStress(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 100
	}

	const workers = 100
	const versionsPerPage = 50
	totalRequests := workers * (versionsPerPage + 1)

	var wg sync.WaitGroup
	wg.Add(workers)

	alphabet := "0123456789abcdefghijklmnopqrstuvwxyz"

	start := time.Now()

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()

			shardChar := string(alphabet[workerID%len(alphabet)])
			pid := fmt.Sprintf("%s-stress-%d", shardChar, workerID)

			pBody, _ := json.Marshal(map[string]any{"pageId": pid, "content": "Initial content"})
			resp, err := client.Post(ts.URL+"/api/pages", "application/json", bytes.NewReader(pBody))
			if err == nil {
				resp.Body.Close()
			}

			for i := 0; i < versionsPerPage; i++ {
				cont := fmt.Sprintf("Content version %d with random junk %s", i, uuid.New().String())
				vBody, _ := json.Marshal(map[string]any{"content": cont})

				vUrl := fmt.Sprintf("%s/api/pages/%s/versions", ts.URL, pid)
				vResp, err := client.Post(vUrl, "application/json", bytes.NewReader(vBody))

				if err == nil {
					vResp.Body.Close()
				}
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
}
