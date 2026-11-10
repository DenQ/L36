package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetVersionRejectsCorruptPatch(t *testing.T) {
	const pid = "corrupt-patch-storage"
	_, err := GPageStorage.CreatePage(pid, "original", "user")
	assert.NoError(t, err)
	_, err = GPageStorage.AddVersion(pid, "middle", "user")
	assert.NoError(t, err)
	_, err = GPageStorage.AddVersion(pid, "next", "user")
	assert.NoError(t, err)

	history, found := GPageStorage.GetHistory(pid)
	assert.True(t, found)

	shard := GPageStorage.getShard(pid)
	shard.mu.Lock()
	shard.pages[pid].Versions[2].Patch = "not-a-valid-delta"
	shard.mu.Unlock()

	got, err := GPageStorage.GetVersion(pid, history[1].ID)
	assert.ErrorIs(t, err, ErrCorruptPatch)
	assert.Empty(t, got.Content)

	root, err := GPageStorage.GetVersion(pid, history[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, "original", root.Content)

	latest, err := GPageStorage.GetVersion(pid, history[2].ID)
	assert.NoError(t, err)
	assert.Equal(t, "next", latest.Content)
}
