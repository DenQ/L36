package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const historyFile = "history.jsonl"

var historyMu sync.Mutex

type stressResult struct {
	Time       string  `json:"time"`
	Scenario   string  `json:"scenario"`
	Requests   int     `json:"requests"`
	DurationNs int64   `json:"duration_ns"`
	RPS        float64 `json:"rps"`
	Commit     string  `json:"commit,omitempty"`
	Dirty      bool    `json:"dirty,omitempty"`
	Go         string  `json:"go"`
	GOOS       string  `json:"goos"`
	GOARCH     string  `json:"goarch"`
	CPUs       int     `json:"cpus"`
	MaxProcs   int     `json:"maxprocs"`
}

func recordStress(scenario string, requests int, duration time.Duration, rps float64) {
	commit, dirty := gitCommit()
	rec := stressResult{
		Time:       time.Now().UTC().Format(time.RFC3339),
		Scenario:   scenario,
		Requests:   requests,
		DurationNs: duration.Nanoseconds(),
		RPS:        math.Round(rps*100) / 100,
		Commit:     commit,
		Dirty:      dirty,
		Go:         runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		CPUs:       runtime.NumCPU(),
		MaxProcs:   runtime.GOMAXPROCS(0),
	}
	if err := appendStressResult(historyFile, rec); err != nil {
		fmt.Printf("stress history: %v\n", err)
	}
}

func appendStressResult(path string, rec stressResult) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}

func gitCommit() (string, bool) {
	commit, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", false
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return strings.TrimSpace(string(commit)), false
	}
	return strings.TrimSpace(string(commit)), strings.TrimSpace(string(status)) != ""
}
