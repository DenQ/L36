package main

import "testing"

func TestParseReport(t *testing.T) {
	raw := []byte(`{
		"latencies": {"99th": 1500000},
		"duration": 3000000000,
		"requests": 100,
		"rate": 33.3,
		"throughput": 30,
		"success": 0.9,
		"status_codes": {"201": 90, "409": 10},
		"errors": ["409 Conflict"]
	}`)
	report, err := parseReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.Requests != 100 || report.Rate != 33.3 {
		t.Fatalf("requests %d rate %v", report.Requests, report.Rate)
	}
	if report.Latencies.P99.Microseconds() != 1500 {
		t.Fatalf("p99 %s", report.Latencies.P99)
	}
	line := formatResult("Pages", report)
	if want := "Pages  requests 100  rps 33  2xx 90.00%  p99 1.5ms  [201:90 409:10]"; line != want {
		t.Fatalf("line %q", line)
	}
}

func TestParseReportRejectsEmpty(t *testing.T) {
	if _, err := parseReport([]byte(`{"requests":0}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtraErrorsSkipsStatusText(t *testing.T) {
	got := extraErrors([]string{"409 Conflict", "dial tcp: connection refused"})
	if len(got) != 1 || got[0] != "dial tcp: connection refused" {
		t.Fatalf("errors %v", got)
	}
}
