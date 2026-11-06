package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffCacheIsPerShard(t *testing.T) {
	a := &Shard{diffCache: make(map[uint64]string)}
	b := &Shard{diffCache: make(map[uint64]string)}

	first := a.diff("old", "new")
	second := a.diff("old", "new")
	assert.Equal(t, first, second)
	assert.Len(t, a.diffCache, 1)
	assert.Empty(t, b.diffCache)

	other := b.diff("left", "right")
	assert.NotEmpty(t, other)
	assert.Len(t, b.diffCache, 1)
	assert.Len(t, a.diffCache, 1)
}

func TestDiffCacheResetsOnShard(t *testing.T) {
	s := &Shard{diffCache: make(map[uint64]string, maxDiffCacheEntries+1)}
	for i := 0; i < maxDiffCacheEntries+1; i++ {
		s.diffCache[uint64(i)] = "stale"
	}

	got := s.diff("alpha", "beta")
	freshDiffs := dmp.DiffMain("beta", "alpha", true)
	assert.Equal(t, dmp.DiffToDelta(freshDiffs), got)
	assert.Len(t, s.diffCache, 1)
}
