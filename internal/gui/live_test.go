package gui

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// TestLiveApp goes through the app against the server at
// $MAIBOORU_TEST_SERVER as the user $MAIBOORU_TEST_USER, whose password is
// $MAIBOORU_TEST_PASSWORD, saving what it shows into $MAIBOORU_SHOTS.
func TestLiveApp(t *testing.T) {
	server, user, pass := os.Getenv("MAIBOORU_TEST_SERVER"), os.Getenv("MAIBOORU_TEST_USER"), os.Getenv("MAIBOORU_TEST_PASSWORD")
	if server == "" || user == "" {
		t.Skip("MAIBOORU_TEST_SERVER and MAIBOORU_TEST_USER are not set")
	}
	h := newHarness(t, nil)
	a := h.app
	h.shot("01-connect")

	a.connect.url = server
	a.discover()
	h.until("the server", func() bool { return a.connect.step == stepSignIn })
	h.shot("02-signin")

	a.connect.user, a.connect.password = user, pass
	a.signIn(false)
	h.until("signing in", func() bool { return a.sess != nil && a.sess.ready })
	if a.sess.client.Credentials().Token == "" {
		t.Fatal("no token after signing in")
	}

	f := a.openFeed("")
	h.until("the posts", func() bool { return len(f.posts) > 0 })
	h.settle(1500 * time.Millisecond)
	h.shot("03-library")

	first := f.posts[0]
	f.cursor = 0
	f.chosen.Add(first.ID)
	h.until("the inspector", func() bool { return a.insp.post != nil && a.insp.post.ID == first.ID })
	h.settle(800 * time.Millisecond)
	h.shot("04-inspector")

	// Edit the tags in the inspector, and save.
	tag := "maibooru_ui_" + strconv.FormatInt(time.Now().UnixNano()%1e6, 10)
	a.insp.draft.tags = append(a.insp.draft.tags, tag)
	a.insp.save(a)
	h.until("saving", func() bool { return !a.insp.saving })
	if !slices.Contains(a.insp.post.TagNames(), tag) {
		t.Errorf("tags after saving: %v", a.insp.post.TagNames())
	}

	// Several chosen: the bulk editor.
	for _, p := range f.posts[1:4] {
		f.chosen.Add(p.ID)
	}
	h.settle(500 * time.Millisecond)
	h.shot("05-bulk")
	f.chosen.Clear()
	f.chosen.Add(first.ID)

	a.openPost(f, first.ID)
	h.until("the viewer", func() bool {
		m := a.viewer.media
		return m != nil && (m.image != nil || m.player != nil || m.thumbOnly)
	})
	a.viewer.showPanel = true
	h.settle(500 * time.Millisecond)
	h.shot("06-viewer")

	// An animation plays as a video of its frames, and a video with the
	// system's media framework.
	for _, typ := range []oxibooru.PostType{oxibooru.TypeAnimation, oxibooru.TypeVideo} {
		i := slices.IndexFunc(f.posts, func(p oxibooru.Post) bool { return p.Type == typ })
		if i < 0 {
			continue
		}
		a.openPost(f, f.posts[i].ID)
		h.until("the "+string(typ), func() bool {
			return a.viewer.media != nil && (a.viewer.media.player != nil || a.viewer.media.thumbOnly)
		})
		if typ == oxibooru.TypeAnimation && a.viewer.media.player == nil {
			t.Errorf("the animation doesn't play: %v", a.viewer.media.err)
		}
		h.settle(1500 * time.Millisecond)
		h.shot("06-" + string(typ))
	}

	h.tt.SetDark(true)
	a.router.Push(postsURL(""))
	h.settle(800 * time.Millisecond)
	h.shot("07-dark")
	a.openPost(f, first.ID)
	h.settle(800 * time.Millisecond)
	h.shot("07-dark-viewer")
	h.tt.SetDark(false)

	a.router.Push(postsURL("sort:score type:image"))
	h.settle(1200 * time.Millisecond)
	h.shot("07-search")

	a.router.Push("/tags")
	h.until("the tags", func() bool { return len(a.tags.pg.items) > 0 })
	a.editTag(&a.tags.pg.items[0])
	h.settle(800 * time.Millisecond)
	h.shot("08-tags")

	a.router.Push("/pools")
	h.until("the pools", func() bool { return a.pools.pg.started && !a.pools.pg.loading })
	h.settle(1000 * time.Millisecond)
	h.shot("09-pools")
	if len(a.pools.pg.items) > 0 {
		a.router.Push("/pool/" + strconv.FormatInt(a.pools.pg.items[0].ID, 10))
		h.until("the pool", func() bool { return a.pools.detail.pool != nil })
		h.settle(1000 * time.Millisecond)
		h.shot("10-pool")
	}

	a.router.Push("/categories")
	h.settle(300 * time.Millisecond)
	h.shot("11-categories")

	a.router.Push("/history")
	h.until("the history", func() bool { return len(a.history.pg.items) > 0 })
	h.shot("12-history")

	a.router.Push("/user/" + user)
	h.until("the account", func() bool { return a.account.user != nil })
	h.settle(500 * time.Millisecond)
	h.shot("13-account")

	a.router.Push("/")
	h.until("the overview", func() bool { return len(a.home.recent) > 0 })
	h.settle(1000 * time.Millisecond)
	h.shot("14-overview")

	// Upload a picture made here, with a tag, as dropping it does.
	dir := t.TempDir()
	file := filepath.Join(dir, "made.png")
	img := image.NewRGBA(image.Rect(0, 0, 120, 90))
	seed := uint8(time.Now().UnixNano())
	for y := range 90 {
		for x := range 120 {
			img.Set(x, y, color.RGBA{uint8(x * 2), seed, uint8(y * 2), 255})
		}
	}
	out, _ := os.Create(file)
	png.Encode(out, img)
	out.Close()
	drop, err := transfer.FileData(file)
	if err != nil {
		t.Fatal(err)
	}
	a.received(drop)
	if a.router.Path() != "/uploads" || len(a.up.items) != 1 {
		t.Fatalf("after the drop: page %s, %d uploads", a.router.Path(), len(a.up.items))
	}
	a.up.tags = []string{"maibooru_upload_test"}
	h.settle(300 * time.Millisecond)
	h.shot("15-uploads")
	a.up.start()
	h.until("the upload", func() bool { return !a.up.running })
	it := a.up.items[0]
	if it.state != upDone || it.post == nil {
		t.Fatalf("upload state %d: %v", it.state, it.err)
	}
	h.settle(500 * time.Millisecond)
	h.shot("16-uploaded")

	// The same file again is a duplicate.
	a.up.addFiles([]string{file})
	a.up.start()
	h.until("the second upload", func() bool { return !a.up.running })
	if last := a.up.items[len(a.up.items)-1]; last.state != upDuplicate || last.exact == nil || last.exact.ID != it.post.ID {
		t.Errorf("second upload: state %d, exact %v", last.state, last.exact)
	}

	// Delete the post made.
	id, version := it.post.ID, it.post.Version
	a.do(func(ctx context.Context, c *oxibooru.Client) error { return c.DeletePost(ctx, id, version) }, nil)
	h.settle(300 * time.Millisecond)

	a.router.Push("/settings")
	h.settle(300 * time.Millisecond)
	h.shot("17-settings")
	a.router.Push("/help")
	h.settle(300 * time.Millisecond)
	h.shot("18-help")

	a.signOut()
	h.settle(300 * time.Millisecond)
	if a.sess != nil || a.connect.step != stepSignIn {
		t.Errorf("after signing out: session %v, step %d", a.sess != nil, a.connect.step)
	}
}
