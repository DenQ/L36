package main

import (
	"testing"
	"time"
)

func TestSaveInterval(t *testing.T) {
	if got := saveInterval(""); got != time.Minute {
		t.Fatalf("empty: got %s", got)
	}
	if got := saveInterval("15"); got != 15*time.Second {
		t.Fatalf("15: got %s", got)
	}
	if got := saveInterval(" 90 "); got != 90*time.Second {
		t.Fatalf("trimmed: got %s", got)
	}
	if got := saveInterval("0"); got != time.Minute {
		t.Fatalf("zero: got %s", got)
	}
	if got := saveInterval("nope"); got != time.Minute {
		t.Fatalf("bad: got %s", got)
	}
}
