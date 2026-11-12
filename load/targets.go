package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
)

// pageSlots is the number of distinct page ids in the pages scenario.
// Vegeta cycles them, so after the first pass the server answers 409.
const pageSlots = 36

// versionPages is how many pages receive new versions. Created before the attack.
const versionPages = 100

type target struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Body   []byte      `json:"body,omitempty"`
	Header http.Header `json:"header,omitempty"`
}

func newTargetEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

func pageTarget(base string, i int) target {
	body := fmt.Sprintf(
		`{"pageId":"load-page-%d","content":"load","userId":"load"}`,
		i%pageSlots,
	)
	return postTarget(base+"/api/pages", body)
}

func versionTarget(base string, i int) target {
	body := fmt.Sprintf(`{"content":"v-%d","userId":"load"}`, i)
	url := fmt.Sprintf("%s/api/pages/load-ver-%d/versions", base, i%versionPages)
	return postTarget(url, body)
}

func postTarget(url, body string) target {
	return target{
		Method: http.MethodPost,
		URL:    url,
		Body:   []byte(body),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
}

// streamTargets writes targets until ctx is canceled or the reader goes away.
func streamTargets(ctx context.Context, w io.Writer, next func(i int) target) error {
	enc := newTargetEncoder(w)
	for i := 0; ; i++ {
		if ctx.Err() != nil {
			return nil
		}
		if err := enc.Encode(next(i)); err != nil {
			if closedPipe(err) {
				return nil
			}
			return err
		}
	}
}

func closedPipe(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF)
}
