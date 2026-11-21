package gui

import (
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/cache"
	"github.com/mudkipme/maibooru/internal/settings"
)

type memSecrets struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memSecrets) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key], nil
}

func (s *memSecrets) Set(key string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = v
	return nil
}

func (s *memSecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// harness runs the app's view without a window, the test's goroutine
// standing in for the main thread.
type harness struct {
	t     *testing.T
	app   *App
	tt    *ui.Tester
	queue chan func()
}

func newHarness(t *testing.T, s *settings.Settings) *harness {
	t.Helper()
	q := make(chan func(), 4096)
	if s == nil {
		s = &settings.Settings{}
	}
	s.Defaults()
	a := New(Options{
		Settings: s,
		Secrets:  &memSecrets{m: map[string][]byte{}},
		Cache:    cache.New(t.TempDir()),
		Post:     func(fn func()) { q <- fn },
	})
	h := &harness{t: t, app: a, queue: q}
	h.tt = ui.NewTester(a.View, 1280, 820)
	return h
}

// until runs what goroutines posted and builds frames until cond holds.
func (h *harness) until(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		h.drain()
		h.tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		select {
		case fn := <-h.queue:
			fn()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (h *harness) drain() {
	for {
		select {
		case fn := <-h.queue:
			fn()
		default:
			return
		}
	}
}

// settle runs frames for a while, as pictures arrive.
func (h *harness) settle(d time.Duration) {
	end := time.Now().Add(d)
	h.until("settling", func() bool { return time.Now().After(end) })
}

// shot saves what the window shows, into $MAIBOORU_SHOTS.
func (h *harness) shot(name string) {
	dir := os.Getenv("MAIBOORU_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		h.t.Fatal(err)
	}
}
