package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetVersionRejectsCorruptPatch(t *testing.T) {
	const pid = "corrupt-patch-storage"
	_, err := GPageStorage.CreatePage(pid, "original")
	assert.NoError(t, err)
	_, ok := GPageStorage.AddVersion(pid, "next")
	assert.True(t, ok)

	history, found := GPageStorage.GetHistory(pid)
	assert.True(t, found)

	shard := GPageStorage.getShard(pid)
	shard.mu.Lock()
	shard.pages[pid].Versions[0].Patch = "not-a-valid-delta"
	shard.mu.Unlock()

	got, err := GPageStorage.GetVersion(pid, history[0].ID)
	assert.ErrorIs(t, err, ErrCorruptPatch)
	assert.Empty(t, got.Content)

	latest, err := GPageStorage.GetVersion(pid, history[1].ID)
	assert.NoError(t, err)
	assert.Equal(t, "next", latest.Content)
}
