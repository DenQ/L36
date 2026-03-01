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
	"github.com/stretchr/testify/assert"
)

func TestE2ELoadStress(t *testing.T) {
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
	const requestsPerWorker = 400
	totalRequests := workers * requestsPerWorker

	var wg sync.WaitGroup
	wg.Add(workers)

	const poolSize = 1000
	payloadPool := make([][]byte, poolSize)
	for i := 0; i < poolSize; i++ {
		p := map[string]any{
			"content": fmt.Sprintf("stress test content variant %d - %s", i, uuid.New().String()),
			"pageId":  fmt.Sprintf("page-%d", i%36), // Раскидываем по разным шардам
		}
		payloadPool[i], _ = json.Marshal(p)
	}

	start := time.Now()

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < requestsPerWorker; i++ {
				data := payloadPool[(workerID+i)%poolSize]
				// resp, err := client.Post(ts.URL+"/api/pages", "application/json", bytes.NewReader(jsonBody))
				resp, err := client.Post(ts.URL+"/api/pages", "application/json", bytes.NewReader(data))

				if err != nil {
					return
				}

				assert.Equal(t, http.StatusCreated, resp.StatusCode)
				resp.Body.Close()
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(start)
	rps := float64(totalRequests) / duration.Seconds()

	fmt.Printf("\n🚀 [L-36 E2E STRESS TEST COMPLETE]\n")
	fmt.Printf("   Total Requests: %d\n", totalRequests)
	fmt.Printf("   Duration: %v\n", duration)
	fmt.Printf("   Real-world RPS: %.2f\n\n", rps)
}
