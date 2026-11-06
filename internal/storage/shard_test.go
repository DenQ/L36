package storage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShardIndexStable(t *testing.T) {
	assert.Equal(t, shardIndex("123"), shardIndex("123"))
	assert.NotEqual(t, shardIndex("123"), shardIndex("124"))

	idx := shardIndex("123")
	assert.GreaterOrEqual(t, idx, 0)
	assert.Less(t, idx, 36)
}

func TestShardIndexSpreads(t *testing.T) {
	seen := make(map[int]struct{})
	for i := 0; i < 360; i++ {
		seen[shardIndex(fmt.Sprintf("page-%d", i))] = struct{}{}
	}

	assert.Greater(t, len(seen), 30)
}
