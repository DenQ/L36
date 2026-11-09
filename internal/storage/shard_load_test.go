package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogRejectsHugeRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shard_0.log")
	f, err := os.Create(path)
	assert.NoError(t, err)

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(maxShardLineSize+1))
	_, err = f.Write(hdr[:])
	assert.NoError(t, err)
	assert.NoError(t, f.Close())

	s := NewPageStorage()
	err = s.Load(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestLogTruncatesTornTail(t *testing.T) {
	dir := t.TempDir()
	store := NewPageStorage()
	_, err := store.CreatePage("kept", "body")
	assert.NoError(t, err)
	assert.NoError(t, store.Dump(dir))

	path := filepath.Join(dir, shardFileName(store, "kept"))
	before, err := os.Stat(path)
	assert.NoError(t, err)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	assert.NoError(t, err)
	_, err = f.Write([]byte{0x01, 0x02})
	assert.NoError(t, err)
	assert.NoError(t, f.Close())

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	history, found := fresh.GetHistory("kept")
	assert.True(t, found)
	assert.Equal(t, "body", history[0].Content)

	info, err := os.Stat(path)
	assert.NoError(t, err)
	assert.Equal(t, before.Size(), info.Size())
}

func TestLoadLineBeyondTenMegabytes(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("a", 11<<20)
	store := NewPageStorage()
	_, err := store.CreatePage("big", content)
	assert.NoError(t, err)
	assert.NoError(t, store.Dump(dir))

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	history, found := fresh.GetHistory("big")
	assert.True(t, found)
	assert.Equal(t, content, history[0].Content)
}

func TestDeleteStaysGoneAfterReload(t *testing.T) {
	dir := t.TempDir()
	store := NewPageStorage()
	_, err := store.CreatePage("only-page", "text")
	assert.NoError(t, err)
	assert.NoError(t, store.Dump(dir))

	assert.True(t, store.DeletePage("only-page"))
	assert.NoError(t, store.Dump(dir))

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	_, found := fresh.GetHistory("only-page")
	assert.False(t, found)
}

func shardFileName(store *PageStorage, pid string) string {
	return filepath.Base(store.shards[shardIndex(pid)].path)
}
