// Package cache keeps the content of posts on disk once downloaded, to
// show them, drag them out of the app and open them in other apps.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// Cache is a directory of posts' content.
type Cache struct {
	root string

	mu       sync.Mutex
	inflight map[string]*fetch
}

type fetch struct {
	done chan struct{}
	err  error
}

// New returns the cache in the directory root.
func New(root string) *Cache {
	return &Cache{root: root, inflight: map[string]*fetch{}}
}

// Root returns the cache's directory.
func (c *Cache) Root() string { return c.root }

// Path returns where the content of a post of a server is kept, whether or
// not it is there yet.
func (c *Cache) Path(site string, p *oxibooru.Post) string {
	// The file goes by the post's name, as it is dragged into other apps,
	// in a folder of its content's checksum, so that new content is a new
	// file.
	sum := sha256.Sum256([]byte(site))
	version := strconv.FormatInt(p.ID, 10)
	if p.Checksum != "" {
		version = p.Checksum[:min(len(p.Checksum), 16)]
	}
	return filepath.Join(c.root, hex.EncodeToString(sum[:6]), version, "post-"+strconv.FormatInt(p.ID, 10)+Ext(p))
}

// Ext returns the extension of a post's content, with its dot.
func Ext(p *oxibooru.Post) string {
	if u := p.ContentURL; u != "" {
		if i := strings.IndexAny(u, "?#"); i >= 0 {
			u = u[:i]
		}
		if ext := path.Ext(u); ext != "" && len(ext) <= 6 {
			return strings.ToLower(ext)
		}
	}
	switch p.MimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/avif":
		return ".avif"
	case "image/jxl":
		return ".jxl"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "application/pdf":
		return ".pdf"
	case "application/x-shockwave-flash":
		return ".swf"
	}
	return ""
}

// Cached returns the path of a post's content when it is on disk.
func (c *Cache) Cached(site string, p *oxibooru.Post) (string, bool) {
	file := c.Path(site, p)
	if _, err := os.Stat(file); err != nil {
		return "", false
	}
	return file, true
}

// Fetch returns the path of a post's content, downloading it first when it
// is not on disk. Fetches of the same content at once download it once.
// progress, when not nil, reports the fraction downloaded.
func (c *Cache) Fetch(ctx context.Context, client *oxibooru.Client, p *oxibooru.Post, progress func(float64)) (string, error) {
	file := c.Path(client.SiteURL(), p)
	if _, err := os.Stat(file); err == nil {
		now := time.Now()
		_ = os.Chtimes(file, now, now)
		return file, nil
	}
	c.mu.Lock()
	f, ok := c.inflight[file]
	if !ok {
		f = &fetch{done: make(chan struct{})}
		c.inflight[file] = f
		go func() {
			f.err = download(context.WithoutCancel(ctx), client, p.ContentURL, file, progress)
			c.mu.Lock()
			delete(c.inflight, file)
			c.mu.Unlock()
			close(f.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-f.done:
		return file, f.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func download(ctx context.Context, client *oxibooru.Client, ref, file string, progress func(float64)) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".download-*")
	if err != nil {
		return err
	}
	var report oxibooru.Progress
	if progress != nil {
		report = func(done, total int64) {
			if total > 0 {
				progress(float64(done) / float64(total))
			}
		}
	}
	err = client.Download(ctx, ref, tmp, report)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), file)
}

// Prune deletes the content used longest ago until the cache holds at most
// limit bytes.
func (c *Cache) Prune(limit int64) error {
	type entry struct {
		path string
		size int64
		used time.Time
	}
	var entries []entry
	var total int64
	err := filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, entry{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(a, b entry) int { return a.used.Compare(b.used) })
	for _, e := range entries {
		if total <= limit {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
			os.Remove(filepath.Dir(e.path)) // its folder, once empty
		}
	}
	return nil
}

// Size returns how many bytes the cache holds.
func (c *Cache) Size() int64 {
	var total int64
	filepath.WalkDir(c.root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Clear deletes everything cached.
func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, os.RemoveAll(filepath.Join(c.root, e.Name())))
	}
	return errors.Join(errs...)
}
