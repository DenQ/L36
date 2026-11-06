package storage

import (
	"testing"

	"l36/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestBranchFromHead(t *testing.T) {
	s := NewPageStorage()
	const pid = "tree-page"

	created, err := s.CreatePage(pid, "A")
	assert.NoError(t, err)
	aID := created.Versions[0].ID

	b, ok := s.AddVersion(pid, "B")
	assert.True(t, ok)
	c, ok := s.AddVersion(pid, "C")
	assert.True(t, ok)
	assert.Equal(t, b.ID, c.ParentID)

	head, ok := s.SetLatest(pid, b.ID)
	assert.True(t, ok)
	assert.Equal(t, "B", head.Content)

	ba, ok := s.AddVersion(pid, "BA")
	assert.True(t, ok)
	assert.Equal(t, b.ID, ba.ParentID)
	assert.Empty(t, ba.Patch)

	gotC, err := s.GetVersion(pid, c.ID)
	assert.NoError(t, err)
	assert.Equal(t, "C", gotC.Content)

	gotB, err := s.GetVersion(pid, b.ID)
	assert.NoError(t, err)
	assert.Equal(t, "B", gotB.Content)

	gotA, err := s.GetVersion(pid, aID)
	assert.NoError(t, err)
	assert.Equal(t, "A", gotA.Content)

	history, found := s.GetHistory(pid)
	assert.True(t, found)
	assert.Len(t, history, 4)

	latest := 0
	for _, v := range history {
		if v.IsLatest {
			latest++
			assert.Equal(t, ba.ID, v.ID)
			assert.Equal(t, b.ID, v.ParentID)
		}
		if v.ID == c.ID {
			assert.False(t, v.IsLatest)
		}
	}
	assert.Equal(t, 1, latest)
}

func TestContinueFromAbandonedLeaf(t *testing.T) {
	s := NewPageStorage()
	const pid = "tree-back-to-c"

	_, err := s.CreatePage(pid, "A")
	assert.NoError(t, err)
	b, ok := s.AddVersion(pid, "B")
	assert.True(t, ok)
	c, ok := s.AddVersion(pid, "C")
	assert.True(t, ok)

	_, ok = s.SetLatest(pid, b.ID)
	assert.True(t, ok)
	ba, ok := s.AddVersion(pid, "BA")
	assert.True(t, ok)

	head, ok := s.SetLatest(pid, c.ID)
	assert.True(t, ok)
	assert.Equal(t, "C", head.Content)
	ca, ok := s.AddVersion(pid, "CA")
	assert.True(t, ok)
	assert.Equal(t, c.ID, ca.ParentID)

	for _, pair := range []struct{ id, text string }{
		{b.ID, "B"},
		{c.ID, "C"},
		{ba.ID, "BA"},
		{ca.ID, "CA"},
	} {
		got, err := s.GetVersion(pid, pair.id)
		assert.NoError(t, err)
		assert.Equal(t, pair.text, got.Content)
	}

	history, found := s.GetHistory(pid)
	assert.True(t, found)
	assert.Len(t, history, 5)
	childrenOfB := 0
	for _, v := range history {
		if v.ParentID == b.ID {
			childrenOfB++
		}
		if v.IsLatest {
			assert.Equal(t, ca.ID, v.ID)
		}
	}
	assert.Equal(t, 2, childrenOfB)
}

func TestSecondBranchFromSameHead(t *testing.T) {
	s := NewPageStorage()
	const pid = "tree-two-branches"

	_, err := s.CreatePage(pid, "A")
	assert.NoError(t, err)
	b, ok := s.AddVersion(pid, "B")
	assert.True(t, ok)
	_, ok = s.SetLatest(pid, b.ID)
	assert.True(t, ok)
	ba, ok := s.AddVersion(pid, "BA")
	assert.True(t, ok)

	_, ok = s.SetLatest(pid, b.ID)
	assert.True(t, ok)
	bb, ok := s.AddVersion(pid, "BB")
	assert.True(t, ok)
	assert.Equal(t, b.ID, ba.ParentID)
	assert.Equal(t, b.ID, bb.ParentID)
	assert.NotEqual(t, ba.ID, bb.ID)

	gotBA, err := s.GetVersion(pid, ba.ID)
	assert.NoError(t, err)
	assert.Equal(t, "BA", gotBA.Content)
	gotBB, err := s.GetVersion(pid, bb.ID)
	assert.NoError(t, err)
	assert.Equal(t, "BB", gotBB.Content)
	gotB, err := s.GetVersion(pid, b.ID)
	assert.NoError(t, err)
	assert.Equal(t, "B", gotB.Content)
}

func TestEmptyLeafCanBranch(t *testing.T) {
	s := NewPageStorage()
	const pid = "tree-empty-leaf"

	created, err := s.CreatePage(pid, "")
	assert.NoError(t, err)
	rootID := created.Versions[0].ID

	next, ok := s.AddVersion(pid, "next")
	assert.True(t, ok)
	assert.Equal(t, rootID, next.ParentID)

	root, err := s.GetVersion(pid, rootID)
	assert.NoError(t, err)
	assert.Equal(t, "", root.Content)
	gotNext, err := s.GetVersion(pid, next.ID)
	assert.NoError(t, err)
	assert.Equal(t, "next", gotNext.Content)
}

func TestMigrateLinearPatches(t *testing.T) {
	shard := &Shard{diffCache: map[uint64]string{}}
	delta := shard.diff("A", "B")
	page := &models.Page{
		ID:          "migrated",
		LatestIndex: 1,
		Versions: []models.Version{
			{ID: "a", Patch: delta},
			{ID: "b", ParentID: "a", Content: "B", IsLatest: true},
		},
	}

	migrateLinearPatches(page)

	assert.Empty(t, page.Versions[0].Patch)
	assert.Equal(t, delta, page.Versions[1].Patch)

	got, err := restoreLocked(page, "a")
	assert.NoError(t, err)
	assert.Equal(t, "A", got.Content)
}
