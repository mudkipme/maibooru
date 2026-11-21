package cache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mudkipme/maibooru/internal/oxibooru"
)

func TestFetchDownloadsOnce(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	defer srv.Close()
	client, err := oxibooru.New(srv.URL+"/api", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := New(t.TempDir())
	p := &oxibooru.Post{ID: 7, Checksum: "abcdef0123456789abcdef", ContentURL: "data/posts/7_abc.webm?x=1"}

	var wg sync.WaitGroup
	paths := make([]string, 4)
	for i := range paths {
		wg.Go(func() {
			path, err := c.Fetch(t.Context(), client, p, nil)
			if err != nil {
				t.Error(err)
			}
			paths[i] = path
		})
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Errorf("%d downloads", hits.Load())
	}
	for _, path := range paths[1:] {
		if path != paths[0] {
			t.Errorf("paths %v", paths)
		}
	}
	if filepath.Base(paths[0]) != "post-7.webm" || filepath.Base(filepath.Dir(paths[0])) != "abcdef0123456789" {
		t.Errorf("path %s", paths[0])
	}
	if got, ok := c.Cached(srv.URL+"/", p); !ok || got != paths[0] {
		t.Errorf("Cached %q %v", got, ok)
	}
	if _, err := c.Fetch(t.Context(), client, p, nil); err != nil || hits.Load() != 1 {
		t.Errorf("a cached post downloads again: %v", err)
	}
}

func TestPruneKeepsTheNewest(t *testing.T) {
	root := t.TempDir()
	c := New(root)
	dir := filepath.Join(root, "site")
	os.MkdirAll(dir, 0o700)
	old := time.Now().Add(-time.Hour)
	for i, name := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, make([]byte, 100), 0o600)
		ts := old.Add(time.Duration(i) * time.Minute)
		os.Chtimes(path, ts, ts)
	}
	if err := c.Prune(150); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "c" {
		t.Errorf("left %v", entries)
	}
	if c.Size() != 100 {
		t.Errorf("size %d", c.Size())
	}
	if err := c.Clear(); err != nil || c.Size() != 0 {
		t.Errorf("clear: %v, %d left", err, c.Size())
	}
}

func TestExt(t *testing.T) {
	for _, tc := range []struct {
		p    oxibooru.Post
		want string
	}{
		{oxibooru.Post{ContentURL: "data/posts/1_x.PNG"}, ".png"},
		{oxibooru.Post{MimeType: "video/mp4"}, ".mp4"},
		{oxibooru.Post{MimeType: "image/jxl"}, ".jxl"},
		{oxibooru.Post{}, ""},
	} {
		if got := Ext(&tc.p); got != tc.want {
			t.Errorf("Ext(%+v) = %q", tc.p, got)
		}
	}
}
