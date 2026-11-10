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
	_, _ = s.CreatePage(pid, initialContent, "user")

	history, _ := s.GetHistory(pid)
	firstVid := history[0].ID

	for i := 1; i <= 50; i++ {
		newContent := fmt.Sprintf("Log Entry #%d: Status is Green. Energy at %d%%", i, 100-i)
		s.AddVersion(pid, newContent, "user")
	}

	firstVersion, err := s.GetVersion(pid, firstVid)

	assert.NoError(t, err, "Первая версия должна быть найдена")
	assert.Equal(t, initialContent, firstVersion.Content, "Контент первой версии должен восстановиться без искажений")

	history2, _ := s.GetHistory(pid)
	latestVersion := history2[len(history2)-1]
	assert.True(t, latestVersion.IsLatest)
	assert.NotEmpty(t, latestVersion.Content)
	assert.NotEmpty(t, latestVersion.Patch)
}

func TestPatchEdgeCases(t *testing.T) {
	s := GPageStorage
	pid := "edge-case-page"

	_, _ = s.CreatePage(pid, "First Content", "user")
	history, _ := s.GetHistory(pid)
	v1 := history[0].ID

	s.AddVersion(pid, "", "user")
	s.AddVersion(pid, "{\"json\": true}", "user")

	ver1, _ := s.GetVersion(pid, v1)
	assert.Equal(t, "First Content", ver1.Content)

	utf8Content := "L-36 🚀 Heavy Armor \n\t [Verified]"
	s.AddVersion(pid, utf8Content, "user")
	history2, _ := s.GetHistory(pid)
	vUTF8 := history2[len(history2)-1].ID

	s.AddVersion(pid, "Next", "user")
	verUTF8, _ := s.GetVersion(pid, vUTF8)
	assert.Equal(t, utf8Content, verUTF8.Content)
}

func TestDeepHistoryIntegrity(t *testing.T) {
	s := NewPageStorage()
	const pid = "deep-page-test"
	_, err := s.CreatePage(pid, "Start Line", "user")
	assert.NoError(t, err)

	for i := 0; i < 1000; i++ {
		_, err = s.AddVersion(pid, fmt.Sprintf("Line %d", i), "user")
		assert.NoError(t, err)
	}

	history, ok := s.GetHistory(pid)
	assert.True(t, ok)

	// Depth 15 sits between the snapshots at 10 and 20, so restore has to apply patches.
	const gapDepth = snapshotEvery + 5
	gap := history[gapDepth-1]
	assert.False(t, gap.Snapshot)
	assert.Empty(t, gap.Content)

	start := time.Now()
	got, err := s.GetVersion(pid, gap.ID)
	elapsed := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("Line %d", gapDepth-2), got.Content)
	fmt.Printf("\n[L-36] Restored depth %d from the nearest snapshot in %v\n", gapDepth, elapsed)
}

func TestJsonPatchCorruption(t *testing.T) {
	s := GPageStorage
	pid := "json-integrity"
	complexJSON := `{"key": "value", "nested": {"arr": [1,2,3]}, "special": "quote\" and slash\\"}`

	_, _ = s.CreatePage(pid, complexJSON, "user")
	s.AddVersion(pid, `{"key": "changed"}`, "user")

	history, _ := s.GetHistory(pid)
	v1, _ := s.GetVersion(pid, history[0].ID)

	assert.Equal(t, complexJSON, v1.Content, "JSON структура должна выжить после диффинга")
}

func TestDumpUnderConcurrency(t *testing.T) {
	s := GPageStorage
	pid := "concurrent-dump-test"
	_, _ = s.CreatePage(pid, "initial", "user")

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
				s.AddVersion(pid, "new data", "user")
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
