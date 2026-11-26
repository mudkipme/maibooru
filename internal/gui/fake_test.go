package gui

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
	"github.com/mudkipme/maibooru/internal/settings"
)

// fakeServer is enough of Oxibooru for the app's tests: posts, their
// updates, uploads and the user signing in.
type fakeServer struct {
	*httptest.Server
	mu      sync.Mutex
	posts   []oxibooru.Post
	updates []map[string]any
	created []map[string]any
	nextID  int64
	thumb   []byte
	// similar is what reverse searches find.
	similar []oxibooru.SimilarPost
	tags    []oxibooru.Tag
	pools   []oxibooru.Pool
}

func newFakeServer(t *testing.T, n int) *fakeServer {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	fs := &fakeServer{thumb: buf.Bytes()}
	for i := range n {
		id := int64(n - i)
		fs.posts = append(fs.posts, oxibooru.Post{
			ID: id, Version: "v1", Type: oxibooru.TypeImage, Safety: oxibooru.Safe, MimeType: "image/png",
			ThumbnailURL: fmt.Sprintf("data/generated-thumbnails/%d.png", id),
			ContentURL:   fmt.Sprintf("data/posts/%d.png", id),
			CanvasWidth:  4, CanvasHeight: 4,
			Tags: []oxibooru.MicroTag{{Names: []string{"cat"}, Category: "default", Usages: n}},
		})
	}
	fs.nextID = int64(n) + 1
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, v)
	}
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"postCount": len(fs.posts), "config": map[string]any{
			"name": "Fake", "enableSafety": true,
			"privileges": map[string]string{"post_create": "regular", "post_edit_tag": "regular", "post_delete": "moderator"},
		}})
	})
	mux.HandleFunc("GET /api/user/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			write(w, map[string]string{"name": "NotLoggedIn", "title": "Not Logged In", "description": "no"})
			return
		}
		write(w, map[string]any{"name": r.PathValue("name"), "rank": "regular", "version": "u1"})
	})
	mux.HandleFunc("POST /api/user-token/{name}", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"token": "tok-" + r.PathValue("name"), "enabled": true, "version": "t1"})
	})
	mux.HandleFunc("GET /api/tag-categories", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"results": []map[string]any{{"name": "default", "color": "default", "default": true}}})
	})
	mux.HandleFunc("GET /api/pool-categories", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"results": []map[string]any{{"name": "default", "color": "default", "default": true}}})
	})
	mux.HandleFunc("GET /api/posts/", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		end := min(len(fs.posts), offset+limit)
		page := fs.posts[min(offset, end):end]
		write(w, map[string]any{"total": len(fs.posts), "offset": offset, "limit": limit, "results": page})
	})
	mux.HandleFunc("GET /api/tags/", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		write(w, map[string]any{"total": len(fs.tags), "results": fs.tags})
	})
	mux.HandleFunc("GET /api/pools/", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		write(w, map[string]any{"total": len(fs.pools), "results": fs.pools})
	})
	mux.HandleFunc("GET /api/post/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		for _, p := range fs.posts {
			if p.ID == id {
				write(w, p)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("PUT /api/post/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		var body map[string]any
		json.UnmarshalRead(r.Body, &body)
		fs.updates = append(fs.updates, body)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		for i := range fs.posts {
			p := &fs.posts[i]
			if p.ID != id {
				continue
			}
			if tags, ok := body["tags"].([]any); ok {
				p.Tags = nil
				for _, t := range tags {
					p.Tags = append(p.Tags, oxibooru.MicroTag{Names: []string{t.(string)}, Category: "default"})
				}
			}
			if s, ok := body["safety"].(string); ok {
				p.Safety = oxibooru.Safety(s)
			}
			p.Version += "+"
			write(w, p)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/uploads", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		write(w, map[string]string{"token": "upload-token"})
	})
	mux.HandleFunc("POST /api/posts/reverse-search", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		write(w, map[string]any{"exactPost": nil, "similarPosts": append([]oxibooru.SimilarPost{}, fs.similar...)})
	})
	mux.HandleFunc("POST /api/posts/", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		var body map[string]any
		json.UnmarshalRead(r.Body, &body)
		fs.created = append(fs.created, body)
		p := oxibooru.Post{ID: fs.nextID, Version: "v1", Type: oxibooru.TypeImage, Safety: oxibooru.Safe,
			ThumbnailURL: fmt.Sprintf("data/generated-thumbnails/%d.png", fs.nextID)}
		fs.nextID++
		fs.posts = append([]oxibooru.Post{p}, fs.posts...)
		write(w, p)
	})
	mux.HandleFunc("GET /data/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(fs.thumb)
	})
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

// signedIn returns a harness signed in to the fake server.
func signedIn(t *testing.T, fs *fakeServer) *harness {
	t.Helper()
	h := newHarness(t, nil)
	signIn(t, h, fs)
	return h
}

// signIn signs a harness in to the fake server, as the user would.
func signIn(t *testing.T, h *harness, fs *fakeServer) {
	t.Helper()
	a := h.app
	a.connect.url = fs.URL
	a.discover()
	h.until("the server", func() bool { return a.connect.step == stepSignIn || a.connect.err != "" })
	if a.connect.err != "" {
		t.Fatal(a.connect.err)
	}
	a.connect.user, a.connect.password = "ada", "secret"
	a.signIn(false)
	h.until("signing in", func() bool { return (a.sess != nil && a.sess.ready) || a.connect.err != "" })
	if a.connect.err != "" {
		t.Fatal(a.connect.err)
	}
}

func TestFilesOpenedAtLaunchWaitForTheServer(t *testing.T) {
	fs := newFakeServer(t, 1)
	h := newHarness(t, nil)
	file := filepath.Join(t.TempDir(), "opened.png")
	os.WriteFile(file, fs.thumb, 0o600)
	h.app.Upload(file)
	signIn(t, h, fs)
	if h.app.router.Path() != "/uploads" || len(h.app.up.items) != 1 || h.app.up.items[0].path != file {
		t.Errorf("page %s, %d uploads", h.app.router.Path(), len(h.app.up.items))
	}
}

func TestSignInKeepsATokenAndAProfile(t *testing.T) {
	fs := newFakeServer(t, 3)
	h := signedIn(t, fs)
	a := h.app
	if got := a.sess.client.Credentials(); got.Token != "tok-ada" || got.Password != "" {
		t.Errorf("credentials %+v", got)
	}
	p, ok := a.s.Profile(a.s.Active)
	if !ok || p.User != "ada" || p.Name != "Fake" || p.API != fs.URL+"/api/" {
		t.Errorf("profile %+v", p)
	}
	tok, _ := a.opts.Secrets.Get(tokenKey(p.ID))
	if string(tok) != "tok-ada" {
		t.Errorf("secret %q", tok)
	}

	// Another start resumes with the token, without asking.
	h2 := newHarness(t, a.s)
	h2.app.opts.Secrets.Set(tokenKey(p.ID), tok)
	h2.app.resume(*p)
	h2.until("resuming", func() bool { return h2.app.sess != nil && h2.app.sess.ready })
	if h2.app.sess.user == nil || h2.app.sess.user.Name != "ada" {
		t.Errorf("resumed as %+v", h2.app.sess.user)
	}
}

func TestLibraryLoadsPagesAsTheyShow(t *testing.T) {
	fs := newFakeServer(t, 250)
	h := signedIn(t, fs)
	a := h.app
	f := a.openFeed("")
	h.until("the first page", func() bool { return len(f.posts) > 0 })
	if len(f.posts) != a.s.PageSize || f.total != 250 {
		t.Fatalf("%d posts of %d after the first page", len(f.posts), f.total)
	}
	if !h.tt.HasText("250 posts") {
		t.Error("the count isn't shown")
	}
	// Scrolling to the end loads the rest.
	for range 40 {
		h.tt.Scroll(500, 400, 0, 4000)
		h.settle(0)
		if f.done {
			break
		}
	}
	h.until("every page", func() bool { return f.done })
	if len(f.posts) != 250 {
		t.Errorf("%d posts loaded", len(f.posts))
	}
}

func TestInspectorSavesOnlyWhatChanged(t *testing.T) {
	fs := newFakeServer(t, 5)
	h := signedIn(t, fs)
	a := h.app
	f := a.openFeed("")
	h.until("the posts", func() bool { return len(f.posts) == 5 })
	f.cursor = 0
	h.until("the inspector", func() bool { return a.insp.post != nil })
	a.insp.draft.tags = append(a.insp.draft.tags, "dog")
	a.insp.draft.safety = oxibooru.Sketchy
	h.tt.Frame()
	if err := h.tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	h.until("saving", func() bool { return len(fs.updates) == 1 && !a.insp.saving })
	got := fs.updates[0]
	if got["version"] != "v1" || got["safety"] != "sketchy" || len(got) != 3 {
		t.Errorf("update %v", got)
	}
	if tags := got["tags"].([]any); len(tags) != 2 || tags[1] != "dog" {
		t.Errorf("tags %v", tags)
	}
	h.until("the grid to follow", func() bool { return len(f.posts[0].Tags) == 2 })
	if f.posts[0].Safety != oxibooru.Sketchy {
		t.Errorf("the grid's post %+v", f.posts[0])
	}
	if recent := a.recentTags(); !slices.Contains(recent, "dog") {
		t.Errorf("recent tags %v", recent)
	}
}

func TestBulkEditAddsTagsToEveryPost(t *testing.T) {
	fs := newFakeServer(t, 4)
	h := signedIn(t, fs)
	a := h.app
	f := a.openFeed("")
	h.until("the posts", func() bool { return len(f.posts) == 4 })
	for _, p := range f.posts[:3] {
		f.chosen.Add(p.ID)
	}
	h.settle(0)
	if !h.tt.HasText("3 posts chosen") {
		t.Fatalf("no bulk editor: %q", h.tt.Texts())
	}
	a.bulkEdit(f.chosenPosts(), bulkChange{add: []string{"bird"}})
	h.until("the bulk edit", func() bool { return !a.insp.bulkBusy })
	if len(fs.updates) != 3 {
		t.Fatalf("%d updates", len(fs.updates))
	}
	for _, p := range f.posts[:3] {
		if !slices.Contains(p.TagNames(), "bird") {
			t.Errorf("post %d tags %v", p.ID, p.TagNames())
		}
	}
	if slices.Contains(f.posts[3].TagNames(), "bird") {
		t.Error("the post not chosen changed")
	}
}

func TestDroppedFilesUploadWithTheSearchTags(t *testing.T) {
	fs := newFakeServer(t, 2)
	h := signedIn(t, fs)
	a := h.app
	a.router.Push(postsURL("cat type:image"))
	h.settle(0)

	dir := t.TempDir()
	var files []string
	for _, name := range []string{"a.png", "b.jpg", "notes.txt"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, fs.thumb, 0o600)
		files = append(files, p)
	}
	d, err := transfer.FileData(filepath.Join(dir, "a.png"), filepath.Join(dir, "b.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	a.received(d)
	// The same file dropped twice is queued once; folders bring their media.
	a.up.addFiles([]string{files[0], dir})
	if a.router.Path() != "/uploads" {
		t.Fatalf("page %s", a.router.Path())
	}
	if len(a.up.items) != 2 {
		t.Fatalf("%d uploads queued", len(a.up.items))
	}
	if !slices.Equal(a.up.tags, []string{"cat"}) {
		t.Errorf("tags of the uploads %v", a.up.tags)
	}
	h.settle(0)
	if err := h.tt.Click("Upload 2 Files"); err != nil {
		t.Fatal(err)
	}
	h.until("the uploads", func() bool { return !a.up.running && len(fs.created) == 2 })
	for _, c := range fs.created {
		if c["contentToken"] != "upload-token" || c["safety"] != "safe" {
			t.Errorf("created %v", c)
		}
		if tags := c["tags"].([]any); len(tags) != 1 || tags[0] != "cat" {
			t.Errorf("tags %v", tags)
		}
	}
	for _, it := range a.up.items {
		if it.state != upDone {
			t.Errorf("%s: state %d, %v", it.name, it.state, it.err)
		}
	}
	if !strings.Contains(strings.Join(h.tt.Texts(), " "), "Open") {
		t.Error("the finished uploads don't open their posts")
	}
}

func TestViewerGoesThroughTheSearch(t *testing.T) {
	fs := newFakeServer(t, 3)
	h := signedIn(t, fs)
	a := h.app
	f := a.openFeed("")
	h.until("the posts", func() bool { return len(f.posts) == 3 })
	a.openPost(f, f.posts[0].ID)
	h.until("the picture", func() bool { return a.viewer.media != nil && a.viewer.media.image != nil })

	h.tt.Key(0, ui.KeyRight)
	next := f.posts[1].ID
	h.until("the next picture", func() bool {
		m := a.viewer.media
		return a.viewer.id == next && m != nil && m.postID == next && m.image != nil
	})
	m := a.viewer.media
	h.settle(200 * time.Millisecond)
	if a.viewer.media != m || m.closed {
		t.Error("the next post's picture was dropped")
	}
	if f.cursor != 1 || !f.chosen.Has(next) {
		t.Errorf("the library's cursor %d", f.cursor)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.settle(0)
	if a.router.Path() != "/posts" {
		t.Errorf("Escape went to %s", a.router.Path())
	}
}

func TestFilesDroppedOnTheWindowQueueUploads(t *testing.T) {
	fs := newFakeServer(t, 1)
	h := signedIn(t, fs)
	a := h.app
	h.settle(0)
	file := filepath.Join(t.TempDir(), "dropped.png")
	os.WriteFile(file, fs.thumb, 0o600)
	if !h.tt.DropFiles(700, 400, file) {
		t.Fatal("the window took no files")
	}
	h.settle(0)
	if a.router.Path() != "/uploads" || len(a.up.items) != 1 || a.up.items[0].path != file {
		t.Errorf("page %s, %d uploads", a.router.Path(), len(a.up.items))
	}
}

func TestUploadsLikePostsThereWaitForTheUser(t *testing.T) {
	fs := newFakeServer(t, 1)
	fs.similar = []oxibooru.SimilarPost{{Distance: 0.1, Post: oxibooru.Post{ID: 1, ThumbnailURL: "data/t.png",
		Tags: []oxibooru.MicroTag{{Names: []string{"cat"}}, {Names: []string{"orange"}}}}}}
	h := signedIn(t, fs)
	a := h.app
	file := filepath.Join(t.TempDir(), "alike.png")
	os.WriteFile(file, fs.thumb, 0o600)
	a.up.addFiles([]string{file})
	a.up.start()
	h.until("the review", func() bool { return !a.up.running })
	it := a.up.items[0]
	if it.state != upReview || len(it.lookalikes) != 1 || !it.lookalikes[0].relate || len(fs.created) != 0 {
		t.Fatalf("state %d, lookalikes %+v, %d made", it.state, it.lookalikes, len(fs.created))
	}
	a.router.Push("/uploads")
	h.settle(0)
	if !h.tt.HasText("Copy tags") || !h.tt.HasText("Upload 1 File") {
		t.Errorf("no review: %q", h.tt.Texts())
	}
	it.lookalikes[0].copyTags = true
	a.up.start()
	h.until("the upload", func() bool { return !a.up.running })
	if it.state != upDone || len(fs.created) != 1 {
		t.Fatalf("state %d: %v", it.state, it.err)
	}
	made := fs.created[0]
	if rel := made["relations"].([]any); len(rel) != 1 || rel[0] != float64(1) {
		t.Errorf("relations %v", made["relations"])
	}
	if tags := made["tags"].([]any); len(tags) != 2 || tags[0] != "cat" {
		t.Errorf("tags %v", tags)
	}
}

func TestHelpShowsTheServerAndTheSyntax(t *testing.T) {
	fs := newFakeServer(t, 2)
	h := signedIn(t, fs)
	if err := h.tt.Click("Help"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if !h.tt.HasText("Fake") || !h.tt.HasText(fs.URL+"/") {
		t.Errorf("no server: %q", h.tt.Texts())
	}
	h.app.help.tab = 1
	h.settle(0)
	if !h.tt.HasText("special:tumbleweed") {
		t.Errorf("no syntax: %q", h.tt.Texts())
	}
}

func TestLockWaitsForThePIN(t *testing.T) {
	fs := newFakeServer(t, 1)
	s := &settings.Settings{PINHash: hashPIN("1234")}
	h := newHarness(t, s)
	a := h.app
	if !a.lock.locked {
		t.Fatal("a PIN doesn't lock the app as it starts")
	}
	signIn(t, h, fs)
	file := filepath.Join(t.TempDir(), "opened.png")
	os.WriteFile(file, fs.thumb, 0o600)
	a.Upload(file)
	h.tt.Frame()
	if len(a.up.items) != 0 {
		t.Fatal("files upload while locked")
	}

	a.lock.pin = "4321"
	a.tryPIN()
	if !a.lock.locked || a.lock.err == "" {
		t.Fatal("a wrong PIN unlocks")
	}
	a.lock.pin = "1234"
	a.tryPIN()
	if a.lock.locked {
		t.Fatal("the PIN doesn't unlock")
	}
	if a.router.Path() != "/uploads" || len(a.up.items) != 1 {
		t.Errorf("page %s, %d uploads after unlocking", a.router.Path(), len(a.up.items))
	}

	a.Lock()
	if !a.lock.locked {
		t.Error("Lock doesn't lock")
	}
	a.s.PINHash = ""
	a.unlock()
	a.Lock()
	if a.lock.locked {
		t.Error("Lock locks without a PIN")
	}
}

func TestClickingAThumbnailChoosesIt(t *testing.T) {
	fs := newFakeServer(t, 6)
	h := signedIn(t, fs)
	a := h.app
	a.router.Push("/posts")
	h.until("the posts", func() bool { _, ok := h.tt.Find("Post 5"); return ok })
	f := a.feeds[len(a.feeds)-1]
	click := func(mods ui.Modifiers, label string) {
		t.Helper()
		r, ok := h.tt.Find(label)
		if !ok {
			t.Fatalf("no %s", label)
		}
		h.tt.ClickAtWith(mods, r.X+r.W/2, r.Y+r.H/2)
		h.tt.Frame()
	}
	chosen := func() []int64 { return slices.Sorted(f.chosen.All()) }

	click(0, "Post 3")
	if p := f.current(); p == nil || p.ID != 3 || !slices.Equal(chosen(), []int64{3}) || a.insp.id != 3 {
		t.Fatalf("a click chose %v, the inspector shows %d", chosen(), a.insp.id)
	}
	click(ui.Cmd, "Post 5")
	if !slices.Equal(chosen(), []int64{3, 5}) {
		t.Errorf("Cmd-click chose %v", chosen())
	}
	click(ui.Shift, "Post 1")
	if !slices.Equal(chosen(), []int64{1, 2, 3, 4, 5}) {
		t.Errorf("Shift-click chose %v", chosen())
	}
	click(0, "Post 2")
	click(0, "Post 2")
	if a.router.Path() != "/post/2" {
		t.Errorf("a double click went to %s", a.router.Path())
	}
}

func TestDraggingPostsOutGivesTheirFiles(t *testing.T) {
	fs := newFakeServer(t, 3)
	h := signedIn(t, fs)
	a := h.app
	a.router.Push("/posts")
	h.until("the posts", func() bool { _, ok := h.tt.Find("Post 2"); return ok })

	r, _ := h.tt.Find("Post 2")
	h.tt.Press(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Move(r.X+r.W/2+40, r.Y+r.H/2+40)
	h.tt.Frame()
	if !a.draggingOut {
		t.Fatal("dragging a post doesn't start a drag")
	}
	dropped := filepath.Join(t.TempDir(), "post-2.png")
	os.WriteFile(dropped, fs.thumb, 0o600)
	h.tt.DropFiles(r.X+r.W/2+40, r.Y+r.H/2+40, dropped)
	h.tt.Frame()
	if len(a.up.items) != 0 {
		t.Errorf("a post dragged onto the window uploads again")
	}

	f := a.feeds[len(a.feeds)-1]
	d := a.dragData([]*oxibooru.Post{&f.posts[f.index(2)]})
	files, err := d.Files()
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "post-2.png" {
		t.Fatalf("dragged %v, %v", files, err)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Errorf("the dragged file isn't there: %v", err)
	}
}

func TestFilterBarSetsTokensOfTheSearch(t *testing.T) {
	fs := newFakeServer(t, 3)
	h := signedIn(t, fs)
	a := h.app
	a.router.Replace(postsURL("cat aspect-ratio:1..2"))
	h.settle(0)
	if err := h.tt.Click("Show or hide filters"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if !h.tt.HasText("Shape 1..2") {
		t.Error("a shape typed in the search isn't shown")
	}
	if err := h.tt.Click("Shape"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if err := h.tt.ChooseMenuItem("Portrait"); err != nil {
		t.Fatalf("%v; menu %v", err, h.tt.Menu())
	}
	h.settle(0)
	if q := a.router.Query("q"); q != "cat ar:..0.95" {
		t.Errorf("query %q", q)
	}
	if err := h.tt.Click("Notes"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	h.tt.ChooseMenuItem("Without Notes")
	h.settle(0)
	h.shot("filters")
	if err := h.tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if q := a.router.Query("q"); q != "cat" {
		t.Errorf("query %q after clearing", q)
	}
}

func TestLayoutsShowThePosts(t *testing.T) {
	fs := newFakeServer(t, 40)
	thumb := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range 64 {
		v := uint8(150)
		if i%8 == 0 || i%8 == 7 || i < 8 || i >= 56 {
			v = 60
		}
		thumb.Set(i%8, i/8, color.RGBA{v, v, 200, 255})
	}
	var buf bytes.Buffer
	png.Encode(&buf, thumb)
	fs.thumb = buf.Bytes()
	for i := range fs.posts {
		fs.posts[i].FileSize = int64(1000 * (i + 1) * 937)
		fs.posts[i].CanvasWidth, fs.posts[i].CanvasHeight = []int{400, 300, 200, 600}[i%4], []int{300, 300, 400, 200}[i%4]
	}
	h := signedIn(t, fs)
	a := h.app
	a.router.Replace(postsURL(""))
	f := a.openFeed("")
	h.until("the posts", func() bool { return len(f.posts) == 40 })

	for _, layout := range []string{layoutJustified, layoutMasonry} {
		a.s.Layout = layout
		h.settle(100 * time.Millisecond)
		h.shot("layout-" + layout)
		if len(f.flow.rects) != 40 {
			t.Fatalf("%s placed %d posts", layout, len(f.flow.rects))
		}
		f.click(0, 0)
		h.settle(0)
		h.tt.Key(0, ui.KeyDown)
		h.settle(0)
		first, below := f.flow.rects[0], f.flow.rects[f.cursor]
		if f.cursor <= 0 || below.Y <= first.Y || below.X >= first.X+first.W {
			t.Errorf("%s: Down went to post %d at %+v from %+v", layout, f.cursor, below, first)
		}
		h.tt.Key(ui.Shift, ui.KeyRight)
		h.settle(0)
		if f.chosen.Len() != 2 {
			t.Errorf("%s: Shift+Right chose %d posts", layout, f.chosen.Len())
		}
	}

	a.s.Layout = layoutList
	h.settle(100 * time.Millisecond)
	h.shot("layout-list")
	if !h.tt.HasText("#40") || !h.tt.HasText("400 × 300") {
		t.Error("the list doesn't show the posts' details")
	}
	if err := h.tt.Click("Dimensions"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if q := a.router.Query("q"); q != "sort:area,asc" {
		t.Errorf("sorting by dimensions searched %q", q)
	}
}

func TestSavedSearchEditorMakesTheQuery(t *testing.T) {
	fs := newFakeServer(t, 3)
	h := signedIn(t, fs)
	a := h.app
	a.router.Replace(postsURL("cat"))
	h.settle(0)
	if err := h.tt.Click("Save this search in the sidebar"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	d := &a.saveSearch
	if len(d.rules) != 1 || d.rules[0].field.label != "Tag" || !slices.Equal(d.rules[0].values, []string{"cat"}) {
		t.Fatalf("the editor read %d rules from the search", len(d.rules))
	}
	if err := h.tt.Click("Field"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if err := h.tt.ChooseMenuItem("Type"); err != nil {
		t.Fatalf("%v; menu %v", err, h.tt.Menu())
	}
	h.settle(0)
	if err := h.tt.Click("Video"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if err := h.tt.Click("Match"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	h.tt.ChooseMenuItem("Is Not")
	h.settle(0)
	if err := h.tt.Click("Add Rule"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	d.rules[1].values = []string{"cat"}
	d.name = "Stills"
	if err := h.tt.Click("Icon and color"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	h.tt.Click("Star")
	h.tt.Click("Green")
	h.settle(0)
	h.tt.Key(0, ui.KeyEscape)
	h.settle(time.Second)
	h.shot("saved-search-editor")
	if err := h.tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	saved := a.s.Saved[a.sess.profile.ID]
	want := settings.SavedSearch{Name: "Stills", Query: "-type:video cat", Icon: "star", Color: "green"}
	if len(saved) != 1 || saved[0] != want {
		t.Fatalf("saved %+v", saved)
	}
	h.until("the count", func() bool { n, ok := a.postCount(want.Query); return ok && n == 3 })
	h.settle(0)
	h.shot("saved-search-sidebar")

	if err := h.tt.RightClick("Stills"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	h.tt.ChooseMenuItem("Edit…")
	h.settle(0)
	if r := d.rules[0]; !d.open || d.index != 0 || r.field.label != "Type" || r.op != "not" {
		t.Errorf("editing read the rules %+v", d.rules)
	}
	h.tt.Click("Cancel")
	h.settle(0)
	if err := h.tt.Click("New Saved Search…"); err != nil {
		t.Fatal(err)
	}
	h.settle(0)
	if !d.open || d.index != -1 || d.name != "" {
		t.Error("New Saved Search didn't open an empty editor")
	}
}

func TestPaletteGoesToWhatItFinds(t *testing.T) {
	fs := newFakeServer(t, 3)
	fs.tags = []oxibooru.Tag{
		{Names: []string{"cat"}, Category: "default", Usages: 50},
		{Names: []string{"black_cat"}, Category: "default", Usages: 3},
		{Names: []string{"sky", "blue_sky"}, Category: "default", Usages: 9},
	}
	fs.pools = []oxibooru.Pool{{ID: 7, Names: []string{"Summer_Trip"}, Category: "default", PostCount: 12}}
	h := signedIn(t, fs)
	a := h.app
	a.s.Saved[a.sess.profile.ID] = []settings.SavedSearch{{Name: "Best Cats", Query: "cat score:5.."}}
	find := func(q string) []paletteItem {
		t.Helper()
		h.tt.Key(ui.Cmd, ui.KeyJ)
		h.settle(0)
		if !a.pal.open {
			t.Fatal("Ctrl+J didn't open the palette")
		}
		h.tt.Type(q)
		h.tt.Advance(time.Second)
		h.until("the palette's search", func() bool { return a.pal.asked == q && len(a.pal.tags) > 0 })
		h.settle(0)
		var items []paletteItem
		for _, g := range a.paletteGroups(ui.LightTheme()) {
			items = append(items, g.items...)
		}
		return items
	}

	items := find("bc")
	var names []string
	for _, it := range items {
		names = append(names, it.name)
	}
	if !slices.Equal(names, []string{"Best Cats", "black_cat"}) {
		t.Fatalf("found %q", names)
	}
	h.shot("palette")
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyEnter)
	h.settle(0)
	if a.pal.open || a.router.Query("q") != "black_cat" {
		t.Errorf("went to %q", a.router.Location())
	}

	if items := find("bluesky"); len(items) != 1 || items[0].name != "sky" || items[0].alias != "blue_sky" {
		t.Errorf("an alias found %+v", items)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.settle(0)

	find("sumtrip")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(0)
	if a.router.Location() != "/pool/7" {
		t.Errorf("went to %q", a.router.Location())
	}
}
