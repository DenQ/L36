package handlers

import (
	"testing"
	"time"
)

func TestSequence(t *testing.T) {
	t.Run("PushPages", PushPages)

	time.Sleep(5 * time.Second)

	t.Run("PushVersions", PushVersions)
}
