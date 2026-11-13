package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestPageTargetRoundTrip(t *testing.T) {
	got := pageTarget("http://127.0.0.1:12360", 37)
	if got.Method != "POST" {
		t.Fatalf("method %s", got.Method)
	}
	if got.URL != "http://127.0.0.1:12360/api/pages" {
		t.Fatalf("url %s", got.URL)
	}
	if string(got.Body) != `{"pageId":"load-page-1","content":"load","userId":"load"}` {
		t.Fatalf("body %s", got.Body)
	}

	var buf bytes.Buffer
	if err := newTargetEncoder(&buf).Encode(got); err != nil {
		t.Fatal(err)
	}
	var decoded target
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Body) != string(got.Body) {
		t.Fatalf("decoded body %s", decoded.Body)
	}
	if decoded.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type %s", decoded.Header.Get("Content-Type"))
	}
}

func TestVersionTargetCyclesPages(t *testing.T) {
	first := versionTarget("http://127.0.0.1:9", 0)
	wrapped := versionTarget("http://127.0.0.1:9", versionPages)
	if first.URL != "http://127.0.0.1:9/api/pages/load-ver-0/versions" {
		t.Fatalf("url %s", first.URL)
	}
	if wrapped.URL != first.URL {
		t.Fatalf("wrapped url %s", wrapped.URL)
	}
	if string(wrapped.Body) == string(first.Body) {
		t.Fatal("cycled page reused the same content")
	}
}

func TestStreamTargetsStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if err := streamTargets(ctx, &buf, func(i int) target { return pageTarget("http://127.0.0.1", i) }); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote %d bytes after cancel", buf.Len())
	}
}
