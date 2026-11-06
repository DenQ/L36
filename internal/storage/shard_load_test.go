package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"l36/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestReadShardLinePastBuffer(t *testing.T) {
	payload := strings.Repeat("b", 5000)
	r := bufio.NewReaderSize(strings.NewReader(payload+"\n"), 128)

	got, err := readShardLine(r, 8000)
	assert.NoError(t, err)
	assert.Equal(t, payload, string(got))
}

func TestReadShardLineRejectsOversize(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(strings.Repeat("x", 40) + "\n"))

	line, err := readShardLine(r, 16)
	assert.Nil(t, line)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "16")
}

func TestLoadKeepsEarlierPagesWhenLineTooLong(t *testing.T) {
	s := &Shard{pages: make(map[string]*models.Page)}
	body := "{\"pageId\":\"ok\"}\n" + strings.Repeat("x", 40) + "\n"

	err := s.readPages(strings.NewReader(body), 32)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "32")
	assert.Contains(t, s.pages, "ok")
	assert.Len(t, s.pages, 1)
}

func TestLoadLineBeyondTenMegabytes(t *testing.T) {
	s := &Shard{pages: make(map[string]*models.Page)}
	content := strings.Repeat("a", 11<<20)
	page := models.Page{
		ID: "big",
		Versions: []models.Version{{
			ID:       "v",
			Content:  content,
			IsLatest: true,
		}},
	}

	path := filepath.Join(t.TempDir(), "shard_0.json")
	f, err := os.Create(path)
	assert.NoError(t, err)
	assert.NoError(t, json.NewEncoder(f).Encode(&page))
	assert.NoError(t, f.Close())

	assert.NoError(t, s.load(path))
	assert.Equal(t, content, s.pages["big"].Versions[0].Content)
}

func TestDumpEmptyShardRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store := NewPageStorage()
	_, err := store.CreatePage("only-page", "text")
	assert.NoError(t, err)
	assert.NoError(t, store.Dump(dir))

	shardFile := filepath.Join(dir, fmt.Sprintf("shard_%d.json", shardIndex("only-page")))
	assert.NoError(t, os.WriteFile(shardFile+".tmp", []byte("partial"), 0o644))

	assert.True(t, store.DeletePage("only-page"))
	assert.NoError(t, store.Dump(dir))

	_, err = os.Stat(shardFile)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(shardFile + ".tmp")
	assert.ErrorIs(t, err, os.ErrNotExist)

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	_, found := fresh.GetHistory("only-page")
	assert.False(t, found)
}
