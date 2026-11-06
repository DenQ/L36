package storage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnapshotKeepsEveryTenthVersion(t *testing.T) {
	s := NewPageStorage()
	const pid = "snapshot-chain"

	_, err := s.CreatePage(pid, "v1")
	assert.NoError(t, err)
	for i := 2; i <= snapshotEvery+5; i++ {
		_, ok := s.AddVersion(pid, fmt.Sprintf("v%d", i))
		assert.True(t, ok)
	}

	history, ok := s.GetHistory(pid)
	assert.True(t, ok)

	assert.True(t, history[0].Snapshot)
	assert.Equal(t, "v1", history[0].Content)

	anchor := history[snapshotEvery-1]
	assert.True(t, anchor.Snapshot)
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery), anchor.Content)
	assert.NotEmpty(t, anchor.Patch)

	mid := history[4]
	assert.False(t, mid.Snapshot)
	assert.Empty(t, mid.Content)

	got, err := s.GetVersion(pid, mid.ID)
	assert.NoError(t, err)
	assert.Equal(t, "v5", got.Content)
	assert.False(t, got.Snapshot)

	shard := s.getShard(pid)
	shard.mu.Lock()
	shard.pages[pid].Versions[snapshotEvery].Patch = "not-a-valid-delta"
	shard.mu.Unlock()

	got, err = s.GetVersion(pid, mid.ID)
	assert.NoError(t, err)
	assert.Equal(t, "v5", got.Content)

	got, err = s.GetVersion(pid, anchor.ID)
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery), got.Content)

	shard.mu.Lock()
	shard.pages[pid].Versions[snapshotEvery-1].Patch = "not-a-valid-delta"
	shard.mu.Unlock()

	_, err = s.GetVersion(pid, mid.ID)
	assert.ErrorIs(t, err, ErrCorruptPatch)

	got, err = s.GetVersion(pid, anchor.ID)
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery), got.Content)
}

func TestSnapshotDepthFollowsParentChain(t *testing.T) {
	s := NewPageStorage()
	const pid = "snapshot-branch"

	_, err := s.CreatePage(pid, "v1")
	assert.NoError(t, err)

	var sideParent string
	for i := 2; i <= snapshotEvery+1; i++ {
		ver, ok := s.AddVersion(pid, fmt.Sprintf("v%d", i))
		assert.True(t, ok)
		if i == 3 {
			sideParent = ver.ID
		}
	}

	_, ok := s.SetLatest(pid, sideParent)
	assert.True(t, ok)
	_, ok = s.AddVersion(pid, "side")
	assert.True(t, ok)

	history, ok := s.GetHistory(pid)
	assert.True(t, ok)

	var anchorContent string
	for _, v := range history {
		if v.ID == sideParent {
			assert.False(t, v.Snapshot)
			assert.Empty(t, v.Content)
		}
		if v.Content == fmt.Sprintf("v%d", snapshotEvery) {
			assert.True(t, v.Snapshot)
			assert.NotEmpty(t, v.Patch)
			anchorContent = v.Content
		}
	}
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery), anchorContent)
}

func TestEmptySnapshotReloads(t *testing.T) {
	s := NewPageStorage()
	const pid = "snapshot-empty"

	created, err := s.CreatePage(pid, "")
	assert.NoError(t, err)
	_, ok := s.AddVersion(pid, "next")
	assert.True(t, ok)

	dir := t.TempDir()
	assert.NoError(t, s.Dump(dir))

	loaded := NewPageStorage()
	assert.NoError(t, loaded.Load(dir))

	history, ok := loaded.GetHistory(pid)
	assert.True(t, ok)
	assert.True(t, history[0].Snapshot)
	assert.Empty(t, history[0].Content)

	got, err := loaded.GetVersion(pid, created.Versions[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, "", got.Content)
}
