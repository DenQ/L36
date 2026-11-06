package storage

import (
	"bufio"
	"encoding/json"
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
