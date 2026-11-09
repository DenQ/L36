package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogDropsStaleFullText(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStorage()
	const pid = "compact-page"
	big := strings.Repeat("a", 32*1024)

	_, err := s.CreatePage(pid, big)
	assert.NoError(t, err)
	assert.NoError(t, s.Dump(dir))
	for i := 2; i <= snapshotEvery+12; i++ {
		_, ok := s.AddVersion(pid, big+fmt.Sprintf("-%d", i))
		assert.True(t, ok)
		assert.NoError(t, s.Dump(dir))
	}

	info, err := os.Stat(filepath.Join(dir, shardFileName(s, pid)))
	assert.NoError(t, err)
	// Snapshots keep the full text. At most snapshotEvery stale copies sit
	// in the log until the next rewrite.
	assert.Less(t, info.Size(), int64(len(big)*(snapshotEvery+4)))

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	history, ok := fresh.GetHistory(pid)
	assert.True(t, ok)
	got, err := fresh.GetVersion(pid, history[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, big, got.Content)
	got, err = fresh.GetVersion(pid, history[4].ID)
	assert.NoError(t, err)
	assert.Equal(t, big+"-5", got.Content)
	got, err = fresh.GetLatestVersion(pid)
	assert.NoError(t, err)
	assert.Equal(t, big+fmt.Sprintf("-%d", snapshotEvery+12), got.Content)
}

func TestSpillKeepsColdVersionReadable(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStorage()
	const pid = "spill-chain"

	_, err := s.CreatePage(pid, "v1")
	assert.NoError(t, err)
	for i := 2; i <= snapshotEvery+5; i++ {
		_, ok := s.AddVersion(pid, fmt.Sprintf("v%d", i))
		assert.True(t, ok)
	}
	assert.NoError(t, s.Dump(dir))

	history, ok := s.GetHistory(pid)
	assert.True(t, ok)
	assert.True(t, history[0].Spilled)
	assert.Empty(t, history[0].Content)
	assert.True(t, history[snapshotEvery-1].Snapshot)
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery), history[snapshotEvery-1].Content)
	assert.False(t, history[snapshotEvery-1].Spilled)

	got, err := s.GetVersion(pid, history[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, "v1", got.Content)

	mid := history[4]
	got, err = s.GetVersion(pid, mid.ID)
	assert.NoError(t, err)
	assert.Equal(t, "v5", got.Content)

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	loaded, ok := fresh.GetHistory(pid)
	assert.True(t, ok)
	assert.True(t, loaded[0].Spilled)
	got, err = fresh.GetVersion(pid, loaded[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, "v1", got.Content)
	got, err = fresh.GetVersion(pid, loaded[len(loaded)-1].ID)
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("v%d", snapshotEvery+5), got.Content)
}

func TestBranchFromSpilledVersionReloads(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStorage()
	const pid = "spill-branch"

	created, err := s.CreatePage(pid, "v1")
	assert.NoError(t, err)
	var second string
	for i := 2; i <= snapshotEvery+5; i++ {
		ver, ok := s.AddVersion(pid, fmt.Sprintf("v%d", i))
		assert.True(t, ok)
		if i == 2 {
			second = ver.ID
		}
	}
	assert.NoError(t, s.Dump(dir))

	_, ok := s.SetLatest(pid, second)
	assert.True(t, ok)
	side, ok := s.AddVersion(pid, "side")
	assert.True(t, ok)
	assert.Equal(t, second, side.ParentID)
	assert.NoError(t, s.Dump(dir))

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	got, err := fresh.GetVersion(pid, created.Versions[0].ID)
	assert.NoError(t, err)
	assert.Equal(t, "v1", got.Content)
	got, err = fresh.GetVersion(pid, second)
	assert.NoError(t, err)
	assert.Equal(t, "v2", got.Content)
	got, err = fresh.GetVersion(pid, side.ID)
	assert.NoError(t, err)
	assert.Equal(t, "side", got.Content)

	latest, err := fresh.GetLatestVersion(pid)
	assert.NoError(t, err)
	assert.Equal(t, side.ID, latest.ID)
}

func TestSetLatestSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStorage()
	const pid = "spill-latest"

	_, err := s.CreatePage(pid, "a")
	assert.NoError(t, err)
	second, ok := s.AddVersion(pid, "b")
	assert.True(t, ok)
	_, ok = s.AddVersion(pid, "c")
	assert.True(t, ok)
	_, ok = s.SetLatest(pid, second.ID)
	assert.True(t, ok)
	assert.NoError(t, s.Dump(dir))

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	latest, err := fresh.GetLatestVersion(pid)
	assert.NoError(t, err)
	assert.Equal(t, "b", latest.Content)
}
