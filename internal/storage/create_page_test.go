package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreatePageRejectsEmptyID(t *testing.T) {
	page, ok := GPageStorage.CreatePage("", "should not stick")
	assert.False(t, ok)
	assert.Empty(t, page.ID)

	_, found := GPageStorage.GetHistory("")
	assert.False(t, found)
}
