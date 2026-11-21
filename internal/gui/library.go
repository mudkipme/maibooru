package gui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// gridFields are the fields of posts the library shows and edits in bulk.
var gridFields = []string{
	"id", "version", "thumbnailUrl", "contentUrl", "type", "mimeType", "safety", "canvasWidth", "canvasHeight",
	"fileSize", "tags", "score", "ownScore", "ownFavorite", "favoriteCount", "commentCount", "noteCount", "flags",
	"checksum", "checksumMD5", "creationTime", "source",
}

// feed is the posts a query finds, loaded a page at a time as they scroll
// into view.
type feed struct {
	query   string
	posts   []oxibooru.Post
	total   int
	loading bool
	done    bool
	err     error
	gen     int

	grid   ui.GridState
	cursor int
	chosen ui.Selection[int64]
	// anchor is the post Shift-click chooses from.
	anchor int
}

const keepFeeds = 6

// feedOf returns the feed of a query, if one is open.
func (a *App) feedOf(q string) *feed {
	for _, f := range a.feeds {
		if f.query == q {
			return f
		}
	}
	return nil
}

// openFeed returns the feed of a query, opening it when none is.
func (a *App) openFeed(q string) *feed {
	if i := slices.IndexFunc(a.feeds, func(f *feed) bool { return f.query == q }); i >= 0 {
		f := a.feeds[i]
		if i != len(a.feeds)-1 {
			a.feeds = append(slices.Delete(a.feeds, i, i+1), f)
		}
		return f
	}
	f := &feed{query: q, cursor: -1, anchor: -1}
	f.grid.Key = func(i int) any {
		if i < len(f.posts) {
			return f.posts[i].ID
		}
		return -int64(i) - 1
	}
	f.grid.Label = func(i int) string {
		if i < len(f.posts) {
			return "Post " + strconv.FormatInt(f.posts[i].ID, 10)
		}
		return ""
	}
	f.grid.Selected = &f.cursor
	f.grid.Selection = &f.chosen
	a.feeds = append(a.feeds, f)
	if len(a.feeds) > keepFeeds {
		a.feeds = slices.Delete(a.feeds, 0, 1)
	}
	f.loadMore(a)
	return f
}

// loadMore loads the next page of the feed.
func (f *feed) loadMore(a *App) {
	if f.loading || f.done {
		return
	}
	f.loading, f.err = true, nil
	gen, offset, q := f.gen, len(f.posts), f.query
	limit := a.s.PageSize
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Page[oxibooru.Post], error) {
		return c.Posts(ctx, oxibooru.Search{Query: q, Offset: offset, Limit: limit, Fields: gridFields})
	}, func(page *oxibooru.Page[oxibooru.Post], err error) {
		if gen != f.gen {
			return
		}
		f.loading = false
		if err != nil {
			f.err = err
			return
		}
		seen := make(map[int64]bool, len(f.posts))
		for _, p := range f.posts {
			seen[p.ID] = true
		}
		for _, p := range page.Results {
			// Posts added meanwhile move the pages along: skip what came
			// already.
			if !seen[p.ID] {
				f.posts = append(f.posts, p)
			}
		}
		f.total = page.Total
		f.done = len(page.Results) == 0 || len(f.posts) >= page.Total
	})
}

// reload loads the feed again from its start, keeping the posts chosen.
func (f *feed) reload(a *App) {
	f.gen++
	f.posts, f.total, f.done, f.loading, f.err = nil, 0, false, false, nil
	f.loadMore(a)
}

// index returns the place of a post in the feed, -1 when not there.
func (f *feed) index(id int64) int {
	return slices.IndexFunc(f.posts, func(p oxibooru.Post) bool { return p.ID == id })
}

// chosenPosts returns the posts chosen, in their order.
func (f *feed) chosenPosts() []*oxibooru.Post {
	var out []*oxibooru.Post
	for i := range f.posts {
		if f.chosen.Has(f.posts[i].ID) {
			out = append(out, &f.posts[i])
		}
	}
	return out
}

// click chooses post i as a click does in a grid: Cmd adds it or takes it
// out, Shift chooses the posts from the one clicked before.
func (f *feed) click(i int, mods ui.Modifiers) {
	id := f.posts[i].ID
	switch {
	case mods&ui.Shift != 0 && f.anchor >= 0 && f.anchor < len(f.posts):
		f.chosen.Clear()
		for j := min(f.anchor, i); j <= max(f.anchor, i); j++ {
			f.chosen.Add(f.posts[j].ID)
		}
	case mods&ui.Cmd != 0:
		if f.chosen.Has(id) {
			f.chosen.Remove(id)
		} else {
			f.chosen.Add(id)
		}
		f.anchor = i
	default:
		f.chosen.Clear()
		f.chosen.Add(id)
		f.anchor = i
	}
	f.cursor = i
	f.grid.Focus()
}

// current returns the post the cursor is on, nil for none.
func (f *feed) current() *oxibooru.Post {
	if f.cursor < 0 || f.cursor >= len(f.posts) {
		return nil
	}
	return &f.posts[f.cursor]
}

// postChanged puts a post's new state everywhere it shows.
func (a *App) postChanged(p *oxibooru.Post) {
	for _, f := range a.feeds {
		if i := f.index(p.ID); i >= 0 {
			mergePost(&f.posts[i], p)
		}
	}
	a.insp.changed(p)
	a.viewer.changed(p)
}

// mergePost takes into dst the fields of the grid that src has.
func mergePost(dst, src *oxibooru.Post) {
	dst.Version = src.Version
	dst.ThumbnailURL, dst.ContentURL = src.ThumbnailURL, src.ContentURL
	dst.Type, dst.MimeType, dst.Safety = src.Type, src.MimeType, src.Safety
	dst.CanvasWidth, dst.CanvasHeight, dst.FileSize = src.CanvasWidth, src.CanvasHeight, src.FileSize
	dst.Tags, dst.Flags, dst.Source, dst.Checksum = src.Tags, src.Flags, src.Source, src.Checksum
	dst.Score, dst.OwnScore, dst.OwnFavorite = src.Score, src.OwnScore, src.OwnFavorite
	dst.FavoriteCount, dst.CommentCount, dst.NoteCount = src.FavoriteCount, src.CommentCount, src.NoteCount
	dst.TagCount = src.TagCount
	dst.HasCustomThumbnail = src.HasCustomThumbnail
}

// postsDeleted takes posts out of every feed.
func (a *App) postsDeleted(ids ...int64) {
	for _, f := range a.feeds {
		before := len(f.posts)
		f.posts = slices.DeleteFunc(f.posts, func(p oxibooru.Post) bool { return slices.Contains(ids, p.ID) })
		f.total -= before - len(f.posts)
		for _, id := range ids {
			f.chosen.Remove(id)
		}
		if f.cursor >= len(f.posts) {
			f.cursor = len(f.posts) - 1
		}
	}
	a.insp.deleted(ids)
}

// postsAdded shows new posts at the start of the feeds of the latest posts.
func (a *App) postsAdded() {
	for _, f := range a.feeds {
		if q := strings.TrimSpace(f.query); q == "" || !strings.Contains(q, "sort:") {
			f.reload(a)
		}
	}
	a.home.loaded = false
}

// libraryView shows the posts a query finds, in a grid beside the
// inspector.
func (a *App) libraryView(c *ui.Context, q string) {
	f := a.openFeed(q)
	if a.keys(c) && c.Shortcut(0, ui.KeyI) {
		a.s.HideInspector = !a.s.HideInspector
		a.save()
	}
	if a.keys(c) && c.Shortcut(0, ui.KeyP) && len(f.posts) > 0 {
		f.cursor = 0
		f.chosen.Clear()
		f.chosen.Add(f.posts[0].ID)
		f.grid.ScrollTo(0, ui.Start)
		f.grid.Focus()
	}
	ui.Column(c).Fill().Children(func() {
		a.libraryBar(c, f)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.grid(c, f) })
			if !a.s.HideInspector {
				line := ui.Box(c).Width(1).Background(c.Theme().Border).Shrink(0)
				a.resizer(c, line, &a.s.InspectorWidth, -1, 260, 640)
				ui.Column(c).Width(a.s.InspectorWidth).Shrink(0).Children(func() { a.inspectorView(c, f) })
			}
		})
	})
}

var sortOptions = []struct{ label, token string }{
	{"Newest", ""},
	{"Oldest", "id,asc"},
	{"Recently Edited", "edit-date"},
	{"Highest Score", "score"},
	{"Most Favorited", "fav-count"},
	{"Most Tags", "tag-count"},
	{"Fewest Tags", "tag-count,asc"},
	{"Largest Files", "file-size"},
	{"Largest Images", "area"},
	{"Random", "random"},
}

var typeFilters = []struct{ label, value string }{
	{"All", ""}, {"Images", "image"}, {"Animations", "animation"}, {"Videos", "video"}, {"Other", "flash,document"},
}

// libraryBar shows how many posts the query finds, and filters and sorts
// them.
func (a *App) libraryBar(c *ui.Context, f *feed) {
	t := c.Theme()
	q := f.query
	ui.Row(c).Padding(6, 12).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		count := "Loading…"
		switch {
		case f.err != nil && len(f.posts) == 0:
			count = "Couldn't load"
		case !f.loading || len(f.posts) > 0:
			count = pluralize(f.total, "post", "posts")
		}
		if n := f.chosen.Len(); n > 1 {
			count = fmt.Sprintf("%d of %s chosen", n, pluralize(f.total, "post", "posts"))
		}
		ui.Text(c, count).TextColor(t.TextMuted).SingleLine().FontFeatures("tnum")

		ui.Spacer(c)

		typ := 0
		cur := oxibooru.TokenValue(q, "type")
		if cur == "flash" {
			cur = "flash,document"
		}
		for i, tf := range typeFilters {
			if tf.value == cur {
				typ = i
			}
		}
		labels := make([]string, len(typeFilters))
		for i, tf := range typeFilters {
			labels[i] = tf.label
		}
		if ui.Segmented(c.Key("type"), &typ, labels...).Label("Type").Changed() {
			value := typeFilters[typ].value
			if value == "flash,document" && a.sess.info.Config.Szurubooru() {
				value = "flash" // Szurubooru keeps no documents
			}
			a.router.Replace(postsURL(oxibooru.SetToken(q, "type", value)))
		}

		if a.sess.safetyEnabled() {
			a.safetyFilter(c, q)
		}

		sortLabel := "Newest"
		curSort := oxibooru.TokenValue(q, "sort")
		for _, so := range sortOptions {
			if so.token == curSort {
				sortLabel = so.label
			}
		}
		if curSort != "" && sortLabel == "Newest" {
			sortLabel = curSort
		}
		menuButton(c.Key("sort"), iconSort, sortLabel, "Sort", func(m *ui.Menu) {
			for _, so := range sortOptions {
				if m.Item(so.label).Radio(so.token == curSort).Chosen() {
					a.router.Replace(postsURL(oxibooru.SetToken(q, "sort", so.token)))
				}
			}
		})

		if strings.TrimSpace(q) != "" {
			if iconButton(c.Key("save"), iconBookmark, "Save this search in the sidebar").Clicked() {
				a.saveSearch = saveSearchDialog{open: true, name: q, query: q, index: -1}
			}
		}

		size := a.s.ThumbnailSize
		if ui.Slider(c.Key("size"), &size, 100, 360).Width(110).Label("Thumbnail size").Tooltip("Thumbnail size").Changed() {
			a.s.ThumbnailSize = size
			a.saveLater()
		}

		panel := on(iconButton(c.Key("inspector"), iconPanelRight, "Show or hide the inspector (I)"), !a.s.HideInspector, c)
		if panel.Clicked() {
			a.s.HideInspector = !a.s.HideInspector
			a.save()
		}
	})
}

// safetyFilter toggles the safeties the query keeps.
func (a *App) safetyFilter(c *ui.Context, q string) {
	cur := oxibooru.TokenValue(q, "safety")
	if cur == "" {
		cur = oxibooru.TokenValue(q, "rating")
	}
	on := map[oxibooru.Safety]bool{}
	if cur == "" {
		for _, s := range oxibooru.Safeties {
			on[s] = true
		}
	} else {
		for s := range strings.SplitSeq(cur, ",") {
			on[oxibooru.Safety(s)] = true
		}
	}
	changed := false
	ui.ToggleGroup(c.Key("safety"), func() {
		for _, s := range oxibooru.Safeties {
			v := on[s]
			tg := ui.Toggle(c.Key(s), &v, safetyLabel(s)).Tooltip("Show " + string(s) + " posts")
			if tg.Changed() {
				on[s] = v
				changed = true
			}
		}
	}).Label("Safety")
	if changed {
		var keep []string
		for _, s := range oxibooru.Safeties {
			if on[s] {
				keep = append(keep, string(s))
			}
		}
		value := strings.Join(keep, ",")
		if len(keep) == len(oxibooru.Safeties) || len(keep) == 0 {
			value = ""
		}
		q = oxibooru.SetToken(q, "rating", "")
		a.router.Replace(postsURL(oxibooru.SetToken(q, "safety", value)))
	}
}

func safetyLabel(s oxibooru.Safety) string {
	switch s {
	case oxibooru.Safe:
		return "Safe"
	case oxibooru.Sketchy:
		return "Sketchy"
	case oxibooru.Unsafe:
		return "Unsafe"
	}
	return string(s)
}

// safetyColor is the color that marks a safety.
func safetyColor(s oxibooru.Safety, t *ui.Theme) ui.Color {
	switch s {
	case oxibooru.Sketchy:
		return t.Warning
	case oxibooru.Unsafe:
		return t.Danger
	}
	return t.Success
}

// grid shows the feed's posts, loading more as they scroll into view.
func (a *App) grid(c *ui.Context, f *feed) {
	t := c.Theme()
	if f.err != nil && len(f.posts) == 0 {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Icon(c, iconAlert).FontSize(32).TextColor(t.Danger)
			ui.Text(c, f.err.Error()).Wrap().MaxWidth(420).TextAlign(ui.Center)
			if ui.Button(c, "Try Again").Clicked() {
				f.reload(a)
			}
		})
		return
	}
	if !f.loading && f.done && len(f.posts) == 0 {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Icon(c, iconImage).FontSize(40).TextColor(t.TextMuted)
			if strings.TrimSpace(f.query) == "" {
				ui.Text(c, "No posts yet").FontSize(18).Bold()
				ui.Text(c, "Drop files here, paste them, or click Upload.").TextColor(t.TextMuted)
			} else {
				ui.Text(c, "Nothing matches “"+f.query+"”").FontSize(16).Bold()
			}
		})
		return
	}
	size := float32(a.s.ThumbnailSize)
	n := len(f.posts)
	if !f.done {
		// A row of placeholders that load the next page as they show.
		n += min(f.total-len(f.posts), 12)
		n = max(n, len(f.posts))
	}
	grid := ui.GridView(c.Key("grid:"+f.query), &f.grid, n, size, size, func(i int) {
		if i >= len(f.posts)-24 {
			f.loadMore(a)
		}
		if i >= len(f.posts) {
			ui.Box(c).Grow(1).Radius(t.Radius).Background(t.Surface)
			return
		}
		a.thumbnail(c, f, i, size)
	}).Grow(1).Label("Posts")
	if grid.Changed() {
		f.anchor = f.cursor
		a.insp.follow(a, f)
	}
	if grid.Submitted() {
		if p := f.current(); p != nil {
			a.openPost(f, p.ID)
		}
	}
	if grid.Shortcut(0, ui.KeyDelete) || grid.Shortcut(0, ui.KeyBackspace) {
		a.deletePosts(a.actionPosts(f, nil))
	}
	if a.keys(c) && grid.Shortcut(0, ui.KeyL) {
		a.toggleFavorites(a.actionPosts(f, nil))
	}
	if grid.Shortcut(ui.Cmd, ui.KeyC) {
		if p := f.current(); p != nil {
			c.WriteClipboard(a.postLink(p.ID))
			a.toast("Copied the link")
		}
	}
	if f.loading && len(f.posts) == 0 {
		ui.Row(c).Absolute().Top(12).Left(12).Gap(8).Children(func() {
			ui.Spinner(c)
		})
	}
	a.insp.follow(a, f)
}

// thumbnail is the cell of a post in the grid.
func (a *App) thumbnail(c *ui.Context, f *feed, i int, size float32) {
	t := c.Theme()
	p := &f.posts[i]
	cell := ui.Box(c).Grow(1).Radius(t.Radius - 1).Clip()
	cell.ContextMenu(func(m *ui.Menu) {
		if !f.chosen.Has(p.ID) {
			f.chosen.Clear()
			f.chosen.Add(p.ID)
			f.cursor = i
		}
		a.postsMenu(m, f, a.actionPosts(f, p))
	})
	// The drag source takes the press from the grid's cell, so it chooses
	// and opens the post as the cell would.
	a.dragOut(cell, func() []*oxibooru.Post { return a.actionPosts(f, p) })
	if cell.Clicked() {
		f.click(i, cell.ClickModifiers())
	}
	if cell.DoubleClicked() {
		a.openPost(f, p.ID)
	}
	scale := float32(2)
	cell.Children(func() {
		img := netimage.Image(c, a.sess.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: int(size * scale), Fit: ui.Cover}).Fill()
		if a.s.BlurUnsafe && p.Safety == oxibooru.Unsafe {
			img.Blur(14)
		}
		ui.Row(c).Absolute().Left(6).Bottom(6).Gap(4).PassThrough().Children(func() {
			badge := func(s *ui.SVG, label string) {
				ui.Row(c).Gap(3).Padding(2, 5).Radius(6).Background(ui.RGBA(0, 0, 0, 0.6)).TextColor(ui.RGB(255, 255, 255)).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, s).FontSize(12)
					if label != "" {
						ui.Text(c, label).FontSize(11).SingleLine()
					}
				})
			}
			switch p.Type {
			case oxibooru.TypeVideo:
				badge(iconVideo, "")
			case oxibooru.TypeAnimation:
				badge(iconPlay, "GIF")
			case oxibooru.TypeFlash, oxibooru.TypeDocument:
				badge(iconFile, strings.ToUpper(strings.TrimPrefix(extOf(p), ".")))
			}
			if p.OwnFavorite {
				badge(iconHeartFilled, "")
			}
			if p.NoteCount > 0 {
				badge(iconNote, "")
			}
		})
		if f.chosen.Has(p.ID) {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(t.Radius-1).Border(3, t.Accent).PassThrough()
			ui.Box(c).Absolute().Top(6).Left(6).Size(20, 20).Radius(10).Center().Background(t.Accent).PassThrough().Children(func() {
				ui.Icon(c, iconCheck).FontSize(12).TextColor(t.AccentText)
			})
		}
		if a.sess.safetyEnabled() && p.Safety != oxibooru.Safe && p.Safety != "" {
			ui.Box(c).Absolute().Top(6).Right(6).Size(10, 10).Radius(5).Background(safetyColor(p.Safety, t)).
				Border(1.5, ui.RGBA(255, 255, 255, 0.85)).PassThrough()
		}
	})
}

// actionPosts returns the posts an action on post p (the one the cursor is
// on when nil) applies to: those chosen, when p is one of them.
func (a *App) actionPosts(f *feed, p *oxibooru.Post) []*oxibooru.Post {
	if p == nil {
		p = f.current()
	}
	chosen := f.chosenPosts()
	if p == nil {
		return chosen
	}
	if f.chosen.Has(p.ID) && len(chosen) > 0 {
		return chosen
	}
	return []*oxibooru.Post{p}
}

// openPost shows a post in the viewer, going through the feed's posts.
func (a *App) openPost(f *feed, id int64) {
	target := "/post/" + strconv.FormatInt(id, 10)
	if f != nil && f.query != "" {
		target += "?q=" + urlQueryEscape(f.query)
	}
	a.router.Push(target)
}

// postLink returns the address of a post in the server's web client.
func (a *App) postLink(id int64) string {
	return strings.TrimSuffix(a.sess.client.SiteURL(), "/") + "/post/" + strconv.FormatInt(id, 10)
}

func extOf(p *oxibooru.Post) string {
	if i := strings.LastIndexByte(p.ContentURL, '.'); i >= 0 && len(p.ContentURL)-i <= 6 {
		return p.ContentURL[i:]
	}
	return ""
}
