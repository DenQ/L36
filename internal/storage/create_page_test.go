package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreatePageRejectsEmptyID(t *testing.T) {
	page, err := GPageStorage.CreatePage("", "should not stick")
	assert.ErrorIs(t, err, ErrEmptyPageID)
	assert.Empty(t, page.ID)

	_, found := GPageStorage.GetHistory("")
	assert.False(t, found)
}

func TestCreatePageRejectsDuplicate(t *testing.T) {
	const pid = "create-page-duplicate"

	first, err := GPageStorage.CreatePage(pid, "original")
	assert.NoError(t, err)
	assert.Equal(t, "original", first.Versions[0].Content)

	again, err := GPageStorage.CreatePage(pid, "replacement")
	assert.ErrorIs(t, err, ErrPageExists)
	assert.Empty(t, again.ID)

	history, found := GPageStorage.GetHistory(pid)
	assert.True(t, found)
	assert.Equal(t, "original", history[0].Content)
}
