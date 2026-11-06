package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentString(t *testing.T) {
	assert.Equal(t, "hello", contentString("hello"))
	assert.Equal(t, "null", contentString("null"))
	assert.Equal(t, "", contentString(nil))
	assert.Equal(t, `{"text":"v"}`, contentString(map[string]string{"text": "v"}))
	assert.Equal(t, "", contentString((*int)(nil)))
	assert.Equal(t, "", contentString(make(chan int)))
}
