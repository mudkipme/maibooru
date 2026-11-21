package gui

import (
	"testing"
	"time"
)

func TestZZClickSelects(t *testing.T) {
	fs := newFakeServer(t, 6)
	h := signedIn(t, fs)
	h.app.router.Push("/posts")
	h.settle(500 * time.Millisecond)
	f := h.app.feeds[len(h.app.feeds)-1]
	t.Logf("before: cursor %d chosen %d insp %d", f.cursor, f.chosen.Len(), h.app.insp.id)
	if err := h.tt.Click("Post 3"); err != nil {
		t.Fatal(err)
	}
	h.settle(300 * time.Millisecond)
	t.Logf("after: cursor %d chosen %d insp %d", f.cursor, f.chosen.Len(), h.app.insp.id)
}
