package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/cache"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// saveLater saves the settings once they stop changing for a moment, as a
// slider drags.
func (a *App) saveLater() { a.saveAt = time.Now().Add(time.Second) }

// flushSave saves the settings when saveLater asked for it and the moment
// passed.
func (a *App) flushSave(c *ui.Context) {
	if a.saveAt.IsZero() {
		return
	}
	if wait := time.Until(a.saveAt); wait > 0 {
		c.After(wait)
		return
	}
	a.saveAt = time.Time{}
	a.save()
}

// postsMenu fills the menu of posts chosen in the library.
func (a *App) postsMenu(m *ui.Menu, f *feed, posts []*oxibooru.Post) {
	s := a.sess
	if len(posts) == 0 {
		return
	}
	one := len(posts) == 1
	if one {
		if m.Item("Open").Shortcut(0, ui.KeyEnter).Chosen() {
			a.openPost(f, posts[0].ID)
		}
		if m.Item("Open in Default App").Chosen() {
			a.openExternally(posts[0])
		}
		if m.Item("Open in Browser").Chosen() {
			a.openURL(a.postLink(posts[0].ID))
		}
		m.Separator()
	}
	if s.user != nil && s.can("post_favorite") {
		allFav := true
		for _, p := range posts {
			allFav = allFav && p.OwnFavorite
		}
		label := "Add to Favorites"
		if allFav {
			label = "Remove from Favorites"
		}
		if m.Item(label).Shortcut(0, ui.KeyL).Chosen() {
			a.toggleFavorites(posts)
		}
	}
	if s.user != nil && s.can("post_score") && one {
		p := posts[0]
		m.Submenu("Rate", func(m *ui.Menu) {
			for _, r := range []struct {
				label string
				score int
			}{{"Like", 1}, {"Neutral", 0}, {"Dislike", -1}} {
				if m.Item(r.label).Radio(p.OwnScore == r.score).Chosen() {
					a.ratePost(p.ID, r.score)
				}
			}
		})
	}
	if s.safetyEnabled() && s.can("post_edit_safety") {
		m.Submenu("Safety", func(m *ui.Menu) {
			for _, sf := range oxibooru.Safeties {
				all := true
				for _, p := range posts {
					all = all && p.Safety == sf
				}
				if m.Item(safetyLabel(sf)).Radio(all).Chosen() {
					a.bulkEdit(posts, bulkChange{safety: sf})
				}
			}
		})
	}
	if s.can("pool_edit_post") || s.can("pool_create") {
		if m.Item("Add to Pool…").Chosen() {
			a.insp.poolPicker = poolPicker{open: true, posts: postIDs(posts)}
		}
	}
	m.Separator()
	if one {
		if m.Item("Copy Link").Shortcut(ui.Cmd, ui.KeyC).Chosen() {
			a.copyText(a.postLink(posts[0].ID))
		}
		if m.Item("Copy Content Address").Chosen() {
			a.copyText(s.client.DataURL(posts[0].ContentURL))
		}
		if m.Item("Copy ID").Chosen() {
			a.copyText(strconv.FormatInt(posts[0].ID, 10))
		}
	}
	label := "Save As…"
	if !one {
		label = fmt.Sprintf("Save %d Posts to Folder…", len(posts))
	}
	if m.Item(label).Chosen() {
		a.export(posts)
	}
	if one {
		if path, ok := a.opts.Cache.Cached(s.client.SiteURL(), posts[0]); ok {
			if m.Item("Show in Folder").Chosen() && a.opts.Desktop != nil {
				a.opts.Desktop.ShowInFolder(path)
			}
		}
	}
	if one && s.can("post_reverse_search") {
		if m.Item("Find Similar Posts").Chosen() {
			a.findSimilar(posts[0])
		}
	}
	if one {
		p := posts[0]
		content := url.QueryEscape(s.client.DataURL(p.ContentURL))
		m.Submenu("Search Elsewhere", func(m *ui.Menu) {
			if m.Item("IQDB").Chosen() {
				a.openURL("https://iqdb.org/?url=" + content)
			}
			if p.ChecksumMD5 != "" && m.Item("Danbooru").Chosen() {
				a.openURL("https://danbooru.donmai.us/posts?tags=md5:" + p.ChecksumMD5)
			}
			if m.Item("Google Lens").Chosen() {
				a.openURL("https://lens.google.com/uploadbyurl?url=" + content)
			}
		})
	}
	if one && (s.can("post_edit_content") || s.can("post_edit_thumbnail")) {
		m.Submenu("Replace", func(m *ui.Menu) {
			if s.can("post_edit_content") && m.Item("Content…").Chosen() {
				a.replaceFile(posts[0], false)
			}
			if s.can("post_edit_thumbnail") && m.Item("Thumbnail…").Chosen() {
				a.replaceFile(posts[0], true)
			}
			if s.can("post_edit_thumbnail") && posts[0].HasCustomThumbnail {
				if m.Item("Discard Custom Thumbnail").Chosen() {
					id, version := posts[0].ID, posts[0].Version
					async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
						return c.DiscardThumbnail(ctx, id, version)
					}, func(np *oxibooru.Post, err error) {
						if err != nil {
							a.fail(err)
							return
						}
						a.postChanged(np)
					})
				}
			}
		})
	}
	if s.can("post_feature") && one {
		if m.Item("Feature on Front Page").Chosen() {
			a.featurePost(posts[0].ID)
		}
	}
	if s.can("post_merge") && len(posts) == 2 {
		if m.Item("Merge…").Chosen() {
			a.insp.merge = mergeDialog{open: true, remove: *posts[1], into: *posts[0]}
		}
	}
	if s.can("post_delete") {
		m.Separator()
		label := "Delete"
		if !one {
			label = fmt.Sprintf("Delete %d Posts", len(posts))
		}
		if m.Item(label+"…").Danger().Shortcut(0, ui.KeyDelete).Chosen() {
			a.deletePosts(posts)
		}
	}
}

func postIDs(posts []*oxibooru.Post) []int64 {
	ids := make([]int64, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return ids
}

func (a *App) copyText(s string) {
	a.clipboard = s
	a.toast("Copied")
}

func (a *App) openURL(u string) {
	if a.opts.Desktop == nil {
		return
	}
	go func() {
		if err := a.opts.Desktop.OpenURL(u); err != nil {
			a.post(func() { a.fail(err) })
		}
	}()
}

// dragData is what dragging posts out of the app gives other apps: their
// files where they are downloaded, else the addresses of their content.
func (a *App) dragData(posts []*oxibooru.Post) transfer.Data {
	site := a.sess.client.SiteURL()
	var files []string
	for _, p := range posts {
		if path, ok := a.opts.Cache.Cached(site, p); ok {
			files = append(files, path)
		}
	}
	if len(files) == len(posts) && len(files) > 0 {
		if d, err := transfer.FileData(files...); err == nil {
			return d
		}
	}
	for _, p := range posts {
		go a.opts.Cache.Fetch(a.sess.ctx, a.sess.client, p, nil)
	}
	if len(posts) == 1 {
		if d, err := transfer.URLData(a.sess.client.DataURL(posts[0].ContentURL)); err == nil {
			return d
		}
	}
	var items []transfer.Item
	for _, p := range posts {
		u := a.sess.client.DataURL(p.ContentURL)
		items = append(items, transfer.NewItem(transfer.Bytes(transfer.URIList, []byte(u)), transfer.Bytes(transfer.Text, []byte(u))))
	}
	return transfer.New(items...)
}

// toggleFavorites adds posts to the favorites, or takes them out when all
// are in already.
func (a *App) toggleFavorites(posts []*oxibooru.Post) {
	if len(posts) == 0 || a.sess.user == nil {
		return
	}
	fav := false
	for _, p := range posts {
		fav = fav || !p.OwnFavorite
	}
	ids := postIDs(posts)
	for _, id := range ids {
		async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
			return c.SetFavorite(ctx, id, fav)
		}, func(p *oxibooru.Post, err error) {
			if err != nil {
				a.fail(err)
				return
			}
			a.postChanged(p)
		})
	}
}

func (a *App) ratePost(id int64, score int) {
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		return c.RatePost(ctx, id, score)
	}, func(p *oxibooru.Post, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		a.postChanged(p)
	})
}

func (a *App) featurePost(id int64) {
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		return c.FeaturePost(ctx, id)
	}, func(p *oxibooru.Post, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		a.toast("Featured on the front page")
		a.home.loaded = false
	})
}

// deletePosts asks, then deletes posts.
func (a *App) deletePosts(posts []*oxibooru.Post) {
	if len(posts) == 0 || !a.sess.can("post_delete") {
		return
	}
	title := "Delete this post?"
	if len(posts) > 1 {
		title = fmt.Sprintf("Delete %d posts?", len(posts))
	}
	type target struct {
		id      int64
		version oxibooru.Version
	}
	var targets []target
	for _, p := range posts {
		targets = append(targets, target{p.ID, p.Version})
	}
	a.confirm(title, "The server deletes them for everyone. This can't be undone.", "Delete", func() {
		async(a, func(ctx context.Context, c *oxibooru.Client) ([]int64, error) {
			var done []int64
			var errs []error
			for _, t := range targets {
				version := t.version
				if version == "" {
					p, err := c.Post(ctx, t.id, "version")
					if err != nil {
						errs = append(errs, err)
						continue
					}
					version = p.Version
				}
				if err := c.DeletePost(ctx, t.id, version); err != nil {
					errs = append(errs, fmt.Errorf("post %d: %w", t.id, err))
					continue
				}
				done = append(done, t.id)
			}
			return done, errors.Join(errs...)
		}, func(done []int64, err error) {
			a.postsDeleted(done...)
			if err != nil {
				a.fail(err)
			} else {
				a.toast(pluralize(len(done), "post", "posts") + " deleted")
			}
			if p := a.viewer.post(); p != nil && slices.Contains(done, p.ID) {
				a.router.Back()
			}
		})
	})
}

// export saves posts' content: one where the user chooses, several into a
// folder.
func (a *App) export(posts []*oxibooru.Post) {
	desk := a.opts.Desktop
	if desk == nil || len(posts) == 0 {
		return
	}
	snapshot := make([]oxibooru.Post, len(posts))
	for i, p := range posts {
		snapshot[i] = *p
	}
	s := a.sess
	go func() {
		var dests []string
		if len(snapshot) == 1 {
			dest, err := desk.SaveFile(fileName(&snapshot[0]))
			if err != nil || dest == "" {
				a.post(func() { a.fail(err) })
				return
			}
			dests = []string{dest}
		} else {
			dir, err := desk.ChooseFolder()
			if err != nil || dir == "" {
				a.post(func() { a.fail(err) })
				return
			}
			for i := range snapshot {
				dests = append(dests, filepath.Join(dir, fileName(&snapshot[i])))
			}
		}
		var errs []error
		for i := range snapshot {
			src, err := a.opts.Cache.Fetch(s.ctx, s.client, &snapshot[i], nil)
			if err == nil {
				err = copyFile(src, dests[i])
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("post %d: %w", snapshot[i].ID, err))
			}
		}
		a.post(func() {
			if err := errors.Join(errs...); err != nil {
				a.fail(err)
				return
			}
			a.toast("Saved " + pluralize(len(snapshot), "file", "files"))
		})
	}()
}

// fileName names a post's file when saved.
func fileName(p *oxibooru.Post) string {
	return "post-" + strconv.FormatInt(p.ID, 10) + cache.Ext(p)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// openExternally opens a post's content in the app the system has for it,
// downloading it first.
func (a *App) openExternally(p *oxibooru.Post) {
	desk := a.opts.Desktop
	if desk == nil {
		return
	}
	post := *p
	s := a.sess
	a.toast("Opening post " + strconv.FormatInt(post.ID, 10) + "…")
	go func() {
		path, err := a.opts.Cache.Fetch(s.ctx, s.client, &post, nil)
		if err == nil {
			err = desk.OpenPath(path)
		}
		if err != nil {
			a.post(func() { a.fail(err) })
		}
	}()
}

// replaceFile asks for a file, and makes it the post's content, or its
// thumbnail.
func (a *App) replaceFile(p *oxibooru.Post, thumbnail bool) {
	desk := a.opts.Desktop
	if desk == nil {
		return
	}
	id := p.ID
	go func() {
		files, err := desk.OpenFiles()
		if err != nil || len(files) == 0 {
			a.post(func() { a.fail(err) })
			return
		}
		path := files[0]
		a.post(func() {
			a.toast("Uploading " + filepath.Base(path) + "…")
			async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				info, err := f.Stat()
				if err != nil {
					return nil, err
				}
				token, err := c.Upload(ctx, path, f, info.Size(), nil)
				if err != nil {
					return nil, err
				}
				cur, err := c.Post(ctx, id, "id", "version")
				if err != nil {
					return nil, err
				}
				fields := oxibooru.PostFields{Version: cur.Version}
				if thumbnail {
					fields.ThumbnailToken = token
				} else {
					fields.ContentToken = token
				}
				return c.UpdatePost(ctx, id, fields)
			}, func(np *oxibooru.Post, err error) {
				if err != nil {
					a.fail(err)
					return
				}
				a.postChanged(np)
				if v := a.viewer.post(); v != nil && v.ID == np.ID {
					a.viewer.id = 0
				}
				a.toast("Replaced")
			})
		})
	}()
}

// findSimilar searches for the posts that look like a post.
func (a *App) findSimilar(p *oxibooru.Post) {
	sd := &a.insp.similar
	*sd = similarDialog{open: true, post: *p, busy: true}
	u := a.sess.client.DataURL(p.ContentURL)
	if p.Type != oxibooru.TypeImage && p.Type != oxibooru.TypeAnimation {
		u = a.sess.client.DataURL(p.ThumbnailURL)
	}
	id := p.ID
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.ImageSearch, error) {
		return c.ReverseSearchURL(ctx, u)
	}, func(r *oxibooru.ImageSearch, err error) {
		if sd.post.ID != id {
			return
		}
		sd.busy, sd.err = false, err
		if err == nil {
			for _, s := range r.SimilarPosts {
				if s.Post.ID != id {
					sd.found = append(sd.found, s)
				}
			}
			if r.ExactPost != nil && r.ExactPost.ID != id {
				sd.found = append([]oxibooru.SimilarPost{{Post: *r.ExactPost}}, sd.found...)
			}
		}
	})
}

// bulkChange is what editing several posts at once changes.
type bulkChange struct {
	add, remove []string
	safety      oxibooru.Safety
}

// bulkEdit applies a change to posts, one after another.
func (a *App) bulkEdit(posts []*oxibooru.Post, ch bulkChange) {
	ids := postIDs(posts)
	a.insp.bulkBusy = true
	async(a, func(ctx context.Context, c *oxibooru.Client) ([]*oxibooru.Post, error) {
		var out []*oxibooru.Post
		var errs []error
		for _, id := range ids {
			p, err := c.Post(ctx, id, "id", "version", "tags", "safety")
			if err != nil {
				errs = append(errs, err)
				continue
			}
			var f oxibooru.PostFields
			f.Version = p.Version
			if len(ch.add) > 0 || len(ch.remove) > 0 {
				tags := p.TagNames()
				tags = slices.DeleteFunc(tags, func(t string) bool { return slices.Contains(ch.remove, t) })
				for _, t := range ch.add {
					if !slices.Contains(tags, t) {
						tags = append(tags, t)
					}
				}
				f.Tags = &tags
			}
			if ch.safety != "" && ch.safety != p.Safety {
				f.Safety = &ch.safety
			}
			if f.Tags == nil && f.Safety == nil {
				continue
			}
			np, err := c.UpdatePost(ctx, id, f)
			if err != nil {
				errs = append(errs, fmt.Errorf("post %d: %w", id, err))
				continue
			}
			out = append(out, np)
		}
		return out, errors.Join(errs...)
	}, func(changed []*oxibooru.Post, err error) {
		a.insp.bulkBusy = false
		for _, p := range changed {
			a.postChanged(p)
		}
		if len(ch.add) > 0 {
			a.s.AddRecentTags(a.sess.profile.ID, ch.add...)
			a.save()
		}
		if err != nil {
			a.fail(err)
			return
		}
		a.toast("Changed " + pluralize(len(changed), "post", "posts"))
	})
}
