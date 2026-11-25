package gui

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// The ways the library lays its posts out (Settings.Layout).
const (
	layoutGrid      = ""
	layoutJustified = "justified"
	layoutMasonry   = "masonry"
	layoutList      = "list"
)

var layouts = []struct {
	id, label string
	icon      *ui.SVG
}{
	{layoutGrid, "Grid", iconLayoutGrid},
	{layoutJustified, "Justified", iconLayoutRows},
	{layoutMasonry, "Masonry", iconMasonry},
	{layoutList, "List", iconList},
}

// flowState is where the posts of a justified or masonry layout are, and
// how far they are scrolled.
type flowState struct {
	scroll ui.ScrollState
	view   ui.Handle
	rects  []ui.Rect
	// reveal is the post to scroll into view once laid out, -1 for none.
	reveal int
}

// layoutMenu chooses how the library lays its posts out.
func (a *App) layoutMenu(c *ui.Context) {
	cur := layouts[0]
	for _, l := range layouts {
		if l.id == a.s.Layout {
			cur = l
		}
	}
	menuButton(c.Key("layout"), cur.icon, "", "Layout", func(m *ui.Menu) {
		for _, l := range layouts {
			if m.Item(l.label).Radio(l.id == a.s.Layout).Chosen() {
				a.s.Layout = l.id
				a.save()
			}
		}
	})
}

// focus gives the posts' view the keyboard focus.
func (f *feed) focus() {
	switch f.layout {
	case layoutList:
		f.list.Handle.Focus()
	case layoutJustified, layoutMasonry:
		f.flow.view.Focus()
	default:
		f.grid.Focus()
	}
}

// scrollIntoView scrolls the posts' view as little as shows post i.
func (f *feed) scrollIntoView(i int) {
	switch f.layout {
	case layoutList:
		f.list.ScrollIntoView(i)
	case layoutJustified, layoutMasonry:
		f.flow.reveal = i
	default:
		f.grid.ScrollIntoView(i)
	}
}

// scrollToStart scrolls the posts' view to its first post.
func (f *feed) scrollToStart() {
	switch f.layout {
	case layoutList:
		f.list.ScrollTo(0, ui.Start)
	case layoutJustified, layoutMasonry:
		f.flow.scroll.Y = 0
	default:
		f.grid.ScrollTo(0, ui.Start)
	}
}

// aspect is a post's width over its height, 1 when unknown.
func aspect(p *oxibooru.Post) float32 {
	if p == nil || p.CanvasWidth <= 0 || p.CanvasHeight <= 0 {
		return 1
	}
	return float32(p.CanvasWidth) / float32(p.CanvasHeight)
}

func clamp32(v, lo, hi float32) float32 { return min(max(v, lo), hi) }

// justify lays items of the aspects given out in rows filling width, about
// height high, gap apart: the last row, short of the width, keeps the
// height. It returns their places and how high they reach.
func justify(aspects []float32, width, height, gap float32) ([]ui.Rect, float32) {
	rects := make([]ui.Rect, len(aspects))
	var y float32
	for start := 0; start < len(aspects); {
		var sum float32
		end, full := start, false
		for end < len(aspects) && !full {
			sum += clamp32(aspects[end], 0.25, 4)
			end++
			full = sum*height+gap*float32(end-start-1) >= width
		}
		h := height
		if full {
			h = (width - gap*float32(end-start-1)) / sum
		}
		var x float32
		for i := start; i < end; i++ {
			w := clamp32(aspects[i], 0.25, 4) * h
			if full && i == end-1 {
				w = width - x
			}
			rects[i] = ui.Rect{X: x, Y: y, W: w, H: h}
			x += w + gap
		}
		y += h + gap
		start = end
	}
	return rects, max(y-gap, 0)
}

// masonry lays items of the aspects given out in columns about width wide,
// filling width, each item going to the shortest column.
func masonry(aspects []float32, width, colWidth, gap float32) ([]ui.Rect, float32) {
	cols := max(1, int(math.Floor(float64((width+gap)/(colWidth+gap)))))
	w := (width - gap*float32(cols-1)) / float32(cols)
	heights := make([]float32, cols)
	rects := make([]ui.Rect, len(aspects))
	for i, a := range aspects {
		col := 0
		for j, h := range heights {
			if h < heights[col]-0.5 {
				col = j
			}
		}
		h := w / clamp32(a, 0.33, 3)
		rects[i] = ui.Rect{X: float32(col) * (w + gap), Y: heights[col], W: w, H: h}
		heights[col] += h + gap
	}
	var bottom float32
	for _, h := range heights {
		bottom = max(bottom, h-gap)
	}
	return rects, bottom
}

// flowView shows the feed's posts laid out as layout says, justified or
// masonry, building only those near the view. It reports Enter or a double
// click on the cursor's post.
func (a *App) flowView(c *ui.Context, f *feed, n int, layout string) (ui.Element, bool) {
	t := c.Theme()
	fs := &f.flow
	size := float32(a.s.ThumbnailSize)
	gap, pad := t.Space(2), t.Space(1)
	sc := ui.Scroll(c.Key("flow:" + layout + ":" + f.query)).TrackScroll(&fs.scroll).Grow(1).Focusable().Label("Posts")
	sc.Bind(&fs.view)
	box := sc.LayoutBounds()
	width := box.W - 2*pad
	if width <= 0 {
		w, _ := c.Size()
		width = w / 2
		c.Invalidate()
	}
	aspects := make([]float32, n)
	for i := range aspects {
		aspects[i] = 1
		if i < len(f.posts) {
			aspects[i] = aspect(&f.posts[i])
		}
	}
	var height float32
	if layout == layoutMasonry {
		fs.rects, height = masonry(aspects, width, size, gap)
	} else {
		fs.rects, height = justify(aspects, width, size, gap)
	}

	submitted := a.flowKeys(c, sc, f)
	if i := fs.reveal; i >= 0 && i < len(fs.rects) && box.H > 0 {
		r := fs.rects[i]
		if r.Y < fs.scroll.Y {
			fs.scroll.Y = r.Y
		} else if bottom := r.Y + r.H + 2*pad; bottom > fs.scroll.Y+box.H {
			fs.scroll.Y = bottom - box.H
		}
		fs.reveal = -1
	}

	viewH := box.H
	if viewH <= 0 {
		_, viewH = c.Size()
	}
	top, bottom := fs.scroll.Y-size, fs.scroll.Y+viewH+size
	sc.Children(func() {
		ui.Box(c).Height(height + 2*pad).Shrink(0).Children(func() {
			for i, r := range fs.rects {
				if r.Y+r.H < top || r.Y > bottom {
					continue
				}
				if i >= len(f.posts)-24 {
					f.loadMore(a)
				}
				cell := ui.Column(c.Key(f.grid.Key(i))).Absolute().Top(r.Y+pad).Left(r.X+pad).Size(r.W, r.H).AlignItems(ui.Stretch)
				cell.Children(func() {
					if i >= len(f.posts) {
						ui.Box(c).Grow(1).Radius(t.Radius).Background(t.Surface)
						return
					}
					// Sizes in steps, so that reflowing rows keep the pictures
					// loaded.
					a.thumbnail(c, f, i, float32(math.Ceil(float64(max(r.W, r.H))/64)*64))
				})
			}
		})
	})
	return sc, submitted
}

// flowKeys moves the cursor of a justified or masonry layout with the
// arrows, Home and End, Shift choosing the posts from the anchor, and
// reports Enter.
func (a *App) flowKeys(c *ui.Context, sc ui.Element, f *feed) bool {
	n := len(f.posts)
	if n == 0 {
		return false
	}
	if sc.Shortcut(ui.Cmd, ui.KeyA) {
		for _, p := range f.posts {
			f.chosen.Add(p.ID)
		}
	}
	for _, k := range []ui.Key{ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd} {
		extend := sc.Shortcut(ui.Shift, k)
		if !extend && !sc.Shortcut(0, k) {
			continue
		}
		to := neighbor(f.flow.rects[:n], f.cursor, k)
		if to < 0 {
			continue
		}
		if extend && f.anchor >= 0 && f.anchor < n {
			f.chosen.Clear()
			for j := min(f.anchor, to); j <= max(f.anchor, to); j++ {
				f.chosen.Add(f.posts[j].ID)
			}
		} else {
			f.chosen.Clear()
			f.chosen.Add(f.posts[to].ID)
			f.anchor = to
		}
		f.cursor = to
		f.flow.reveal = to
	}
	return f.cursor >= 0 && f.cursor < n && sc.Shortcut(0, ui.KeyEnter)
}

// neighbor returns the item key moves to from item at among rects: the
// one before or after it, the first or the last, or the nearest above or
// below that it overlaps sideways, -1 for none.
func neighbor(rects []ui.Rect, at int, key ui.Key) int {
	n := len(rects)
	if at < 0 || at >= n {
		return 0
	}
	switch key {
	case ui.KeyLeft:
		return max(at-1, 0)
	case ui.KeyRight:
		return min(at+1, n-1)
	case ui.KeyHome:
		return 0
	case ui.KeyEnd:
		return n - 1
	}
	cur := rects[at]
	mid := cur.X + cur.W/2
	best := -1
	var bestOverlap bool
	var bestV, bestH float32
	for j, r := range rects {
		var v float32
		switch {
		case key == ui.KeyUp && r.Y+r.H <= cur.Y+1:
			v = cur.Y - (r.Y + r.H)
		case key == ui.KeyDown && r.Y >= cur.Y+cur.H-1:
			v = r.Y - (cur.Y + cur.H)
		default:
			continue
		}
		overlap := r.X < cur.X+cur.W && cur.X < r.X+r.W
		h := float32(math.Abs(float64(r.X + r.W/2 - mid)))
		better := best < 0 || overlap && !bestOverlap
		if !better && overlap == bestOverlap {
			better = v < bestV-1 || v <= bestV+1 && h < bestH
		}
		if better {
			best, bestOverlap, bestV, bestH = j, overlap, v, h
		}
	}
	if best < 0 {
		return at
	}
	return best
}

// tableSorts are the sorts of the search the list's columns stand for.
var tableSorts = map[string]string{
	"post": "id", "tags": "tag-count", "type": "type", "dimensions": "area", "size": "file-size",
	"safety": "safety", "score": "score", "favorites": "fav-count", "posted": "date",
}

// sortOrderOf returns the list's column the query sorts by.
func sortOrderOf(q string) ui.SortOrder {
	v := oxibooru.TokenValue(q, "sort")
	if v == "" {
		return ui.SortOrder{Column: "post", Descending: true}
	}
	token, dir, _ := strings.Cut(v, ",")
	for col, s := range tableSorts {
		if s == token {
			return ui.SortOrder{Column: col, Descending: dir != "asc"}
		}
	}
	return ui.SortOrder{}
}

// tableView shows the feed's posts in a table of their details. It reports
// a new choice, and Enter or a double click on a post.
func (a *App) tableView(c *ui.Context, f *feed) (ui.Element, bool, bool) {
	t := c.Theme()
	cols := []ui.TableColumn{
		{ID: "thumb", Width: 52, Fixed: true},
		{Title: "Post", ID: "post", Width: 72, Sortable: true},
		{Title: "Tags", ID: "tags", MinWidth: 120, Sortable: true},
		{Title: "Type", ID: "type", Width: 60, Sortable: true},
		{Title: "Dimensions", ID: "dimensions", Width: 96, Sortable: true},
		{Title: "Size", ID: "size", Width: 80, Align: ui.End, Sortable: true},
	}
	if a.sess.safetyEnabled() {
		cols = append(cols, ui.TableColumn{Title: "Safety", ID: "safety", Width: 72, Sortable: true})
	}
	cols = append(cols,
		ui.TableColumn{Title: "Favorites", ID: "favorites", Width: 80, Align: ui.End, Sortable: true},
		ui.TableColumn{Title: "Posted", ID: "posted", Width: 100, Sortable: true},
	)
	sort := sortOrderOf(f.query)
	l := &f.list
	l.Key, l.Label = f.grid.Key, f.grid.Label
	l.Selected, l.Selection = &f.cursor, &f.chosen
	l.Sort, l.RowHeight = &sort, 44
	l.CellMenu = func(m *ui.Menu, row, _ int) {
		if row < len(f.posts) {
			a.postsMenu(m, f, a.actionPosts(f, &f.posts[row]))
		}
	}
	tbl := ui.Table(c.Key("table:"+f.query), l, cols, len(f.posts), func(row, col int) {
		if row >= len(f.posts)-24 {
			f.loadMore(a)
		}
		p := &f.posts[row]
		// Chosen rows show in the accent color, which the muted text
		// would be lost on.
		muted := func(e ui.Element, color ui.Color) {
			if !f.chosen.Has(p.ID) {
				e.TextColor(color)
			}
		}
		switch cols[col].ID {
		case "thumb":
			cell := ui.Box(c).Size(36, 36).Radius(4).Clip()
			a.dragOut(cell, func() []*oxibooru.Post { return a.actionPosts(f, p) })
			cell.Children(func() {
				img := netimage.Image(c, a.sess.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 96, Fit: ui.Cover}).Fill()
				if a.s.BlurUnsafe && p.Safety == oxibooru.Unsafe {
					img.Blur(6)
				}
			})
		case "post":
			ui.Text(c, "#"+strconv.FormatInt(p.ID, 10)).FontFeatures("tnum")
		case "tags":
			ui.Row(c).Gap(6).Clip().Children(func() {
				for _, tag := range p.Tags {
					muted(ui.Text(c, a.tagLabel(tag.Name())).SingleLine().Shrink(0), a.sess.tagColor(tag.Category, t))
				}
			})
		case "type":
			muted(ui.Text(c, strings.ToUpper(strings.TrimPrefix(extOf(p), "."))), t.TextMuted)
		case "dimensions":
			if p.CanvasWidth > 0 {
				muted(ui.Text(c, strconv.Itoa(p.CanvasWidth)+" × "+strconv.Itoa(p.CanvasHeight)).FontFeatures("tnum"), t.TextMuted)
			}
		case "size":
			if p.FileSize > 0 {
				muted(ui.Text(c, formatBytes(p.FileSize)).SingleLine().FontFeatures("tnum"), t.TextMuted)
			}
		case "safety":
			muted(ui.Text(c, safetyLabel(p.Safety)), safetyColor(p.Safety, t))
		case "favorites":
			ui.Text(c, formatCount(p.FavoriteCount)).FontFeatures("tnum")
		case "posted":
			muted(ui.Text(c, formatTime(p.CreationTime)).SingleLine(), t.TextMuted)
		}
	}).Grow(1).Label("Posts")
	if now := sortOrderOf(f.query); sort != now && sort.Column != "" {
		token := tableSorts[sort.Column]
		if !sort.Descending {
			token += ",asc"
		}
		if token == "id" {
			token = ""
		}
		a.router.Replace(postsURL(oxibooru.SetToken(f.query, "sort", token)))
	}
	return tbl, tbl.Changed(), tbl.Submitted()
}
