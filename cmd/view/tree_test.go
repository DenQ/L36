package main

import (
	"bytes"
	"testing"

	"l36/internal/models"
)

func TestWritePageShowsBranch(t *testing.T) {
	page := models.Page{
		ID: "seed-notes",
		Versions: []models.Version{
			{ID: "aaaaaaaa-1111-2222-3333-444444444444", Content: "root"},
			{ID: "bbbbbbbb-1111-2222-3333-444444444444", ParentID: "aaaaaaaa-1111-2222-3333-444444444444"},
			{ID: "cccccccc-1111-2222-3333-444444444444", ParentID: "bbbbbbbb-1111-2222-3333-444444444444", Content: "Финал линейной ветки"},
			{ID: "dddddddd-1111-2222-3333-444444444444", ParentID: "bbbbbbbb-1111-2222-3333-444444444444", Content: "Ответвление", IsLatest: true},
		},
	}
	var buf bytes.Buffer
	writePage(&buf, page)

	got := buf.String()
	want := "" +
		"seed-notes\n" +
		"└─ aaaaaaaa-1111-2222-3333-444444444444  root\n" +
		"   └─ bbbbbbbb-1111-2222-3333-444444444444\n" +
		"      ├─ cccccccc-1111-2222-3333-444444444444  Финал линейной ветки\n" +
		"      └─ dddddddd-1111-2222-3333-444444444444 *  Ответвление\n"
	if got != want {
		t.Fatalf("tree:\n%s\nwant:\n%s", got, want)
	}
}
