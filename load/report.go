package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// vegetaReport is the JSON shape of `vegeta report --type=json`.
type vegetaReport struct {
	Latencies struct {
		P99 time.Duration `json:"99th"`
	} `json:"latencies"`
	Duration    time.Duration  `json:"duration"`
	Requests    uint64         `json:"requests"`
	Rate        float64        `json:"rate"`
	Throughput  float64        `json:"throughput"`
	Success     float64        `json:"success"`
	StatusCodes map[string]int `json:"status_codes"`
	Errors      []string       `json:"errors"`
}

func parseReport(raw []byte) (vegetaReport, error) {
	var report vegetaReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return vegetaReport{}, fmt.Errorf("vegeta report: %w", err)
	}
	if report.Requests == 0 {
		return vegetaReport{}, fmt.Errorf("vegeta report: no requests")
	}
	return report, nil
}

func formatResult(scenario string, report vegetaReport) string {
	line := fmt.Sprintf("%s  requests %d  rps %.0f  2xx %.2f%%  p99 %s  %s",
		scenario,
		report.Requests,
		report.Rate,
		report.Success*100,
		report.Latencies.P99.Round(time.Microsecond),
		formatStatus(report.StatusCodes),
	)
	if extra := extraErrors(report.Errors); len(extra) > 0 {
		line += "\n  " + strings.Join(extra, "\n  ")
	}
	return line
}

func formatStatus(codes map[string]int) string {
	if len(codes) == 0 {
		return "[]"
	}
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, code := range keys {
		parts[i] = fmt.Sprintf("%s:%d", code, codes[code])
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// extraErrors drops vegeta's "409 Conflict" style strings. Those are already in the status list.
func extraErrors(errs []string) []string {
	var out []string
	for _, err := range errs {
		if err == "" || err[0] >= '0' && err[0] <= '9' {
			continue
		}
		out = append(out, err)
	}
	return out
}
