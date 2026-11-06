package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppendStressResultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	first := stressResult{
		Time:       "2026-11-06T09:00:00Z",
		Scenario:   "PushPages",
		Requests:   10000,
		DurationNs: 1_000_000_000,
		RPS:        10000,
		Commit:     "abc1234",
		Go:         "go1.25.0",
		GOOS:       "linux",
		GOARCH:     "amd64",
		CPUs:       8,
		MaxProcs:   8,
	}
	second := stressResult{
		Time:       "2026-11-06T09:00:05Z",
		Scenario:   "PushVersions",
		Requests:   5100,
		DurationNs: 2_000_000_000,
		RPS:        2550.5,
		Commit:     "abc1234",
		Dirty:      true,
		Go:         "go1.25.0",
		GOOS:       "linux",
		GOARCH:     "amd64",
		CPUs:       8,
		MaxProcs:   8,
	}

	assert.NoError(t, appendStressResult(path, first))
	assert.NoError(t, appendStressResult(path, second))

	data, err := os.ReadFile(path)
	assert.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	assert.Len(t, lines, 2)

	var gotFirst, gotSecond stressResult
	assert.NoError(t, json.Unmarshal([]byte(lines[0]), &gotFirst))
	assert.NoError(t, json.Unmarshal([]byte(lines[1]), &gotSecond))
	assert.Equal(t, first, gotFirst)
	assert.Equal(t, second, gotSecond)
}
