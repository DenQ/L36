package storage

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func TestPatchIntegrityChain(t *testing.T) {
	s := GPageStorage
	pid := "integrity-test-page"

	initialContent := "Original Ship Logs of L-36"
	s.CreatePage(pid, initialContent)

	history, _ := s.GetHistory(pid)
	firstVid := history[0].ID

	for i := 1; i <= 50; i++ {
		newContent := fmt.Sprintf("Log Entry #%d: Status is Green. Energy at %d%%", i, 100-i)
		s.AddVersion(pid, newContent)
	}

	firstVersion, ok := s.GetVersion(pid, firstVid)

	assert.True(t, ok, "Первая версия должна быть найдена")
	assert.Equal(t, initialContent, firstVersion.Content, "Контент первой версии должен восстановиться без искажений")

	history2, _ := s.GetHistory(pid)
	latestVersion := history2[len(history2)-1]
	assert.True(t, latestVersion.IsLatest)
	assert.NotEmpty(t, latestVersion.Content)
	assert.Empty(t, latestVersion.Patch)
}

func TestPatchEdgeCases(t *testing.T) {
	s := GPageStorage
	pid := "edge-case-page"

	s.CreatePage(pid, "First Content")
	history, _ := s.GetHistory(pid)
	v1 := history[0].ID

	s.AddVersion(pid, "")
	s.AddVersion(pid, "{\"json\": true}")

	ver1, _ := s.GetVersion(pid, v1)
	assert.Equal(t, "First Content", ver1.Content)

	utf8Content := "L-36 🚀 Heavy Armor \n\t [Verified]"
	s.AddVersion(pid, utf8Content)
	history2, _ := s.GetHistory(pid)
	vUTF8 := history2[len(history2)-1].ID

	s.AddVersion(pid, "Next")
	verUTF8, _ := s.GetVersion(pid, vUTF8)
	assert.Equal(t, utf8Content, verUTF8.Content)
}

func TestDeepHistoryIntegrity(t *testing.T) {
	s := GPageStorage
	pid := "deep-page-test"
	initialText := "Start Line"
	s.CreatePage(pid, initialText)

	for i := 0; i < 1000; i++ {
		s.AddVersion(pid, fmt.Sprintf("Line %d", i))
	}

	history, _ := s.GetHistory(pid)
	firstVid := history[0].ID

	start := time.Now()
	v1, ok := s.GetVersion(pid, firstVid)
	elapsed := time.Since(start)

	assert.True(t, ok)
	assert.Equal(t, initialText, v1.Content)
	fmt.Printf("\n[L-36] Restored version 1 from 1000 patches in %v\n", elapsed)
}

func TestJsonPatchCorruption(t *testing.T) {
	s := GPageStorage
	pid := "json-integrity"
	complexJSON := `{"key": "value", "nested": {"arr": [1,2,3]}, "special": "quote\" and slash\\"}`

	s.CreatePage(pid, complexJSON)
	s.AddVersion(pid, `{"key": "changed"}`)

	history, _ := s.GetHistory(pid)
	v1, _ := s.GetVersion(pid, history[0].ID)

	assert.Equal(t, complexJSON, v1.Content, "JSON структура должна выжить после диффинга")
}

func TestDumpUnderConcurrency(t *testing.T) {
	s := GPageStorage
	pid := "concurrent-dump-test"
	s.CreatePage(pid, "initial")

	stop := make(chan bool)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.AddVersion(pid, "new data")
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_ = s.Dump("data/test_stress.json")
			time.Sleep(10 * time.Millisecond)
		}
		stop <- true
	}()

	wg.Wait()
}
