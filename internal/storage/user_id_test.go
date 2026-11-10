package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserIDRejected(t *testing.T) {
	s := NewPageStorage()
	const pid = "user-id-reject"

	page, err := s.CreatePage(pid, "body", "")
	assert.ErrorIs(t, err, ErrEmptyUserID)
	assert.Empty(t, page.ID)
	_, found := s.GetHistory(pid)
	assert.False(t, found)

	page, err = s.CreatePage(pid, "body", strings.Repeat("a", maxUserIDLen+1))
	assert.ErrorIs(t, err, ErrUserIDTooLong)
	assert.Empty(t, page.ID)
	_, found = s.GetHistory(pid)
	assert.False(t, found)

	_, err = s.CreatePage(pid, "body", " ")
	assert.NoError(t, err)
	exact, err := s.CreatePage("user-id-exact", "body", strings.Repeat("a", maxUserIDLen))
	assert.NoError(t, err)
	assert.Len(t, exact.Versions[0].UserID, maxUserIDLen)

	_, err = s.AddVersion(pid, "next", "")
	assert.ErrorIs(t, err, ErrEmptyUserID)
	_, err = s.AddVersion(pid, "next", strings.Repeat("b", maxUserIDLen+1))
	assert.ErrorIs(t, err, ErrUserIDTooLong)
	_, err = s.AddVersion("user-id-missing-page", "next", "alice")
	assert.ErrorIs(t, err, ErrPageNotFound)

	history, found := s.GetHistory(pid)
	assert.True(t, found)
	assert.Len(t, history, 1)
	assert.Equal(t, " ", history[0].UserID)
}

func TestUserIDSurvivesReloadAndCompact(t *testing.T) {
	dir := t.TempDir()
	s := NewPageStorage()
	const pid = "user-id-reload"

	authors := make([]string, 0, snapshotEvery+13)
	created, err := s.CreatePage(pid, "v1", "alice")
	assert.NoError(t, err)
	assert.Equal(t, "alice", created.Versions[0].UserID)
	authors = append(authors, "alice")
	assert.NoError(t, s.Dump(dir))

	for i := 2; i <= snapshotEvery+12; i++ {
		user := "bob"
		if i%2 == 0 {
			user = "carol"
		}
		ver, err := s.AddVersion(pid, fmt.Sprintf("v%d", i), user)
		assert.NoError(t, err)
		assert.Equal(t, user, ver.UserID)
		authors = append(authors, user)
		assert.NoError(t, s.Dump(dir))
	}

	fresh := NewPageStorage()
	assert.NoError(t, fresh.Load(dir))
	raw, err := os.ReadFile(filepath.Join(dir, shardFileName(s, pid)))
	assert.NoError(t, err)
	assert.Contains(t, string(raw), `"userId":"alice"`)
	assert.Contains(t, string(raw), `"userId":"carol"`)

	history, ok := fresh.GetHistory(pid)
	assert.True(t, ok)
	assert.Len(t, history, len(authors))
	assert.True(t, history[0].Spilled)
	for i, user := range authors {
		assert.Equal(t, user, history[i].UserID)
		got, err := fresh.GetVersion(pid, history[i].ID)
		assert.NoError(t, err)
		assert.Equal(t, user, got.UserID)
	}
}
