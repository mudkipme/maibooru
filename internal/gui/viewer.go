package gui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/egoist/mygo/ui/video"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// viewer shows a post at its size, with its details and comments beside
// it.
type viewer struct {
	id    int64
	query string
	ed    inspector

	media     *media
	zoom      float32 // 0 fits the window
	fit       int
	scroll    ui.ScrollState
	stageW    float32
	stageH    float32
	panelTab  int
	showPanel bool
	around    *oxibooru.Around
	aroundOf  int64

	notes       noteEditor
	commentTab  int
	editTab     int
	comment     string
	editing     int64
	editText    string
	commentBusy bool
}

// media is a post's content, loaded to show.
type media struct {
	postID   int64
	image    ui.ImageSource
	player   *ui.VideoPlayer
	progress float64
	err      error
	// thumbOnly is set for content no decoder here reads: the thumbnail
	// shows in its place.
	thumbOnly bool
	closed    bool
	// web is the page showing what native UI can't; blank empties it.
	web   MediaPage
	blank func()
}

func (m *media) close() {
	if m == nil || m.closed {
		return
	}
	m.closed = true
	if m.player != nil {
		m.player.Close()
	}
	if m.web != nil && m.blank != nil {
		m.blank()
	}
}

func (v *viewer) changed(p *oxibooru.Post) { v.ed.changed(p) }

func (v *viewer) reload(a *App) { v.ed.reload(a) }

// post returns the post shown.
func (v *viewer) post() *oxibooru.Post { return v.ed.post }

// show goes to a post, loading it and its content.
func (v *viewer) show(a *App, id int64, q string) {
	if v.id == id && v.query == q {
		return
	}
	v.ed.leave(a)
	v.media.close()
	v.id, v.query, v.media, v.zoom = id, q, nil, 0
	v.ed = inspector{id: id}
	v.comment, v.editing = "", 0
	v.notes = noteEditor{}
	v.ed.gen++
	v.ed.loading = true
	gen := v.ed.gen
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		return c.Post(ctx, id)
	}, func(p *oxibooru.Post, err error) {
		if gen != v.ed.gen || v.id != id {
			return
		}
		v.ed.loading = false
		if err != nil {
			v.ed.err = err
			return
		}
		v.ed.post, v.ed.draft = p, draftOf(p)
		v.load(a, p)
	})
	// Keep the library on the post, for going back to it.
	if f := a.feedOf(q); f != nil {
		if i := f.index(id); i >= 0 {
			f.cursor = i
			f.chosen.Clear()
			f.chosen.Add(id)
			f.grid.ScrollIntoView(i)
		}
	}
}

// load loads a post's content to show.
func (v *viewer) load(a *App, p *oxibooru.Post) {
	m := &media{postID: p.ID}
	v.media = m
	s := a.sess
	post := *p
	switch {
	case p.Type == oxibooru.TypeVideo:
		player, err := video.OpenURL(s.client.DataURL(p.ContentURL))
		if err != nil {
			m.err, m.thumbOnly = err, true
			return
		}
		player.SetLoop(p.HasFlag(oxibooru.FlagLoop) || !p.HasFlag(oxibooru.FlagSound)).SetMuted(a.s.MuteVideos)
		if !a.s.AutoplayVideos {
			player.Pause()
		}
		m.player = player
		return
	case p.Type == oxibooru.TypeFlash || p.Type == oxibooru.TypeDocument,
		p.MimeType == "image/avif" || p.MimeType == "image/jxl":
		if !a.webMedia(m, p) {
			m.thumbOnly = true
		}
		return
	}
	go func() {
		path, err := a.opts.Cache.Fetch(s.ctx, s.client, &post, func(f float64) {
			a.post(func() { m.progress = f })
		})
		var data []byte
		if err == nil {
			data, err = os.ReadFile(path)
		}
		var img ui.ImageSource
		var player *ui.VideoPlayer
		if err == nil {
			player, img, err = decodeContent(&post, data)
		}
		a.post(func() {
			if m.closed {
				if player != nil {
					player.Close()
				}
				return
			}
			if err != nil {
				if !a.webMedia(m, &post) {
					m.err, m.thumbOnly = err, true
				}
				return
			}
			m.image, m.player = img, player
			if player != nil {
				player.SetLoop(true).SetMuted(true)
			}
		})
	}()
}

// decodeContent decodes a picture, or an animation into a player.
func decodeContent(p *oxibooru.Post, data []byte) (*ui.VideoPlayer, ui.ImageSource, error) {
	if p.Type == oxibooru.TypeAnimation {
		var d ui.VideoDecoder
		var err error
		switch {
		case bytes.HasPrefix(data, []byte("GIF8")):
			d, err = ui.NewGIFDecoder(data)
		case bytes.HasPrefix(data, []byte("\x89PNG")):
			d, err = ui.NewAPNGDecoder(data)
		}
		if err == nil && d != nil {
			player, err := ui.NewVideoPlayer(d)
			if err == nil {
				return player, nil, nil
			}
		}
	}
	// Huge pictures show at most 8192 pixels on their longer side, a quarter
	// of a gigabyte of pixels, rather than all of theirs.
	if bm, err := ui.DecodeBitmapScaled(data, 8192); err == nil {
		return nil, bm, nil
	}
	img, err := ui.DecodeImage(data)
	if err != nil {
		return nil, nil, fmt.Errorf("can't show %s here", cmpOr(p.MimeType, "this file"))
	}
	return nil, img, nil
}

// neighbors returns the posts before and after the one shown, in the
// order of the query it was opened from.
func (v *viewer) neighbors(a *App) (prev, next int64) {
	if f := a.feedOf(v.query); f != nil {
		if i := f.index(v.id); i >= 0 {
			if i > 0 {
				prev = f.posts[i-1].ID
			}
			if i+1 < len(f.posts) {
				next = f.posts[i+1].ID
			} else if !f.done {
				f.loadMore(a)
			}
			return prev, next
		}
	}
	if v.aroundOf != v.id {
		v.aroundOf = v.id
		id, q := v.id, v.query
		async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Around, error) {
			return c.PostAround(ctx, id, q, "id")
		}, func(r *oxibooru.Around, err error) {
			if err == nil && v.id == id {
				v.around = r
			}
		})
	}
	if v.around != nil {
		if v.around.Prev != nil {
			prev = v.around.Prev.ID
		}
		if v.around.Next != nil {
			next = v.around.Next.ID
		}
	}
	return prev, next
}

func (a *App) goPost(id int64, q string) {
	target := "/post/" + strconv.FormatInt(id, 10)
	if q != "" {
		target += "?q=" + urlQueryEscape(q)
	}
	a.router.Replace(target)
}

func (a *App) leaveViewer() {
	if a.router.CanGoBack() {
		a.router.Back()
	} else {
		a.router.Push(postsURL(a.viewer.query))
	}
}

func (a *App) viewerView(c *ui.Context, id int64, q string) {
	v := &a.viewer
	v.show(a, id, q)
	t := c.Theme()
	prev, next := v.neighbors(a)
	if a.keys(c) {
		switch {
		case (c.Shortcut(0, ui.KeyLeft) || c.Shortcut(0, ui.KeyA)) && prev != 0:
			a.goPost(prev, q)
		case (c.Shortcut(0, ui.KeyRight) || c.Shortcut(0, ui.KeyD)) && next != 0:
			a.goPost(next, q)
		case c.Shortcut(0, ui.KeyF):
			v.cycleFit()
		case c.Shortcut(0, ui.KeyE):
			v.showPanel, v.panelTab = true, 0
		case c.Shortcut(0, ui.KeyT):
			v.showPanel, v.panelTab = true, 0
			v.ed.tagInput.Focus()
		case c.Shortcut(0, ui.KeyEscape):
			a.leaveViewer()
		case c.Shortcut(0, ui.KeyN):
			a.s.ShowNotes = !a.s.ShowNotes
			a.save()
		case c.Shortcut(0, ui.KeyI):
			v.showPanel = !v.showPanel
		case c.Shortcut(0, ui.KeyEqual) || c.Shortcut(ui.Shift, ui.KeyEqual):
			v.zoomBy(1.25)
		case c.Shortcut(0, ui.KeyMinus):
			v.zoomBy(0.8)
		case c.Shortcut(0, ui.Key0):
			v.fit, v.zoom = fitNone, 0
		case c.Shortcut(0, ui.KeyL) && v.post() != nil:
			a.toggleFavorites([]*oxibooru.Post{v.post()})
		case c.Shortcut(0, ui.KeySpace) && v.media != nil && v.media.player != nil:
			togglePlay(v.media.player)
		case c.Shortcut(0, ui.KeyDelete) && v.post() != nil:
			a.deletePosts([]*oxibooru.Post{v.post()})
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		v.ed.save(a)
	}
	root := ui.Row(c.Key("viewer")).Fill().AlignItems(ui.Stretch)
	root.OnUnmount(func() {
		// Going to the next post replaces this page, which unmounts after
		// the next one built: leave that one be.
		if v.id != id {
			return
		}
		v.ed.leave(a)
		v.media.close()
		v.media, v.id = nil, 0
	})
	root.Children(func() {
		stageBG := ui.RGB(18, 18, 20)
		ui.Column(c).Grow(1).MinWidth(0).Background(stageBG).Children(func() {
			ui.Themed(c, ui.DarkTheme().WithAccent(t.Accent)).Grow(1).Background(stageBG).Children(func() {
				a.viewerBar(c, prev, next)
				stage := ui.Box(c.Key("stage")).Grow(1).MinHeight(0)
				v.stageW, v.stageH = stage.LayoutBounds().W, stage.LayoutBounds().H
				stage.Children(func() {
					a.stage(c)
					a.navArrow(c, prev, q, true)
					a.navArrow(c, next, q, false)
				})
				if v.media != nil && v.media.player != nil && v.post() != nil && v.post().Type == oxibooru.TypeVideo {
					videoControls(c, v.media.player)
				}
			})
		})
		if v.showPanel {
			ui.Box(c).Width(1).Background(t.Border)
			ui.Column(c).Width(360).Shrink(0).Children(func() { a.viewerPanel(c) })
		}
	})
}

func (v *viewer) zoomBy(f float32) {
	if v.zoom == 0 {
		v.zoom = 1
	}
	v.fit, v.zoom = fitNone, min(8, max(0.1, v.zoom*f))
}

// Fit modes beyond fitting the window (zoom 0): the zoom follows the stage.
const (
	fitNone = iota
	fitWidth
	fitHeight
)

// cycleFit goes through fitting the window, its width, its height and the
// actual size, as the web client's F does.
func (v *viewer) cycleFit() {
	switch {
	case v.zoom == 0:
		v.fit, v.zoom = fitWidth, 1
	case v.fit == fitWidth:
		v.fit = fitHeight
	case v.fit == fitHeight:
		v.fit, v.zoom = fitNone, 1
	default:
		v.fit, v.zoom = fitNone, 0
	}
}

func togglePlay(p *ui.VideoPlayer) {
	if p.Playing() {
		p.Pause()
	} else {
		p.Play()
	}
}

func (a *App) viewerBar(c *ui.Context, prev, next int64) {
	t := c.Theme()
	v := &a.viewer
	p := v.post()
	bar := c.TitleBar()
	ui.Row(c).Height(max(44, bar.Height)).Padding(0, bar.Right+10, 0, bar.Left+10).Gap(6).AlignItems(ui.Center).DragWindow().Children(func() {
		if iconButton(c, iconChevronL, "Back to the library").Tooltip("Back (Esc)").Clicked() {
			a.leaveViewer()
		}
		title := "Post " + strconv.FormatInt(v.id, 10)
		ui.Text(c, title).Bold().SingleLine()
		if f := a.feedOf(v.query); f != nil {
			if i := f.index(v.id); i >= 0 {
				ui.Text(c, fmt.Sprintf("%s of %s", formatCount(i+1), formatCount(f.total))).FontSize(12).TextColor(t.TextMuted).FontFeatures("tnum")
			}
		}
		if p != nil {
			ui.Text(c, fmt.Sprintf("%d × %d", p.CanvasWidth, p.CanvasHeight)).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if v.media != nil && v.media.image != nil {
			if iconButton(c, iconZoomOut, "Zoom out").Tooltip("Zoom out (−)").Clicked() {
				v.zoomBy(0.8)
			}
			zl := "Fit"
			if v.zoom > 0 {
				zl = fmt.Sprintf("%d%%", int(v.zoom*100+0.5))
			}
			iw, _ := ui.ImageSize(v.media.image)
			menuButton(c.Key("fit"), nil, zl, "Zoom", func(m *ui.Menu) {
				if m.Item("Fit to Window").Shortcut(0, ui.Key0).Radio(v.zoom == 0).Chosen() {
					v.fit, v.zoom = fitNone, 0
				}
				if m.Item("Fit Width").Radio(v.fit == fitWidth).Chosen() && iw > 0 {
					v.fit, v.zoom = fitWidth, 1
				}
				if m.Item("Fit Height").Radio(v.fit == fitHeight).Chosen() && iw > 0 {
					v.fit, v.zoom = fitHeight, 1
				}
				if m.Item("Actual Size").Radio(v.zoom == 1 && v.fit == fitNone).Chosen() {
					v.fit, v.zoom = fitNone, 1
				}
				m.Separator()
				if m.Item("Enlarge Small Pictures to Fit").Checked(a.s.UpscaleSmall).Chosen() {
					a.s.UpscaleSmall = !a.s.UpscaleSmall
					a.save()
				}
				if m.Item("Checkerboard Behind Transparency").Checked(a.s.TransparencyGrid).Chosen() {
					a.s.TransparencyGrid = !a.s.TransparencyGrid
					a.save()
				}
			})
			if iconButton(c, iconZoomIn, "Zoom in").Tooltip("Zoom in (+)").Clicked() {
				v.zoomBy(1.25)
			}
		}
		if p != nil && v.media != nil && v.media.image != nil && a.sess.can("post_edit_note") {
			edit := on(iconButton(c, iconEdit, "Edit notes"), v.notes.open && v.notes.postID == p.ID, c)
			if edit.Clicked() {
				if v.notes.open {
					v.notes.open = false
				} else {
					v.notes.begin(p)
					v.zoom, v.showPanel = 0, true
				}
			}
		}
		if p != nil && len(p.Notes) > 0 {
			notes := on(toolButton(c, iconNote, strconv.Itoa(len(p.Notes))).Tooltip("Show notes (N)").Label("Notes"), a.s.ShowNotes, c)
			if notes.Clicked() {
				a.s.ShowNotes = !a.s.ShowNotes
				a.save()
			}
		}
		if p != nil {
			ic := iconHeart
			if p.OwnFavorite {
				ic = iconHeartFilled
			}
			if a.sess.user != nil {
				fav := iconButton(c, ic, "Favorite").Tooltip("Favorite (L)")
				if p.OwnFavorite {
					fav.TextColor(t.Danger)
				}
				if fav.Clicked() {
					a.toggleFavorites([]*oxibooru.Post{p})
				}
			}
			if iconButton(c, iconExternal, "Open in default app").Clicked() {
				a.openExternally(p)
			}
			iconButton(c, iconMore, "More").PopoverMenuButton(func(m *ui.Menu) {
				a.postsMenu(m, a.feedOf(v.query), []*oxibooru.Post{p})
			}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
		}
		panel := on(iconButton(c, iconPanelRight, "Details").Tooltip("Details and comments (I)"), v.showPanel, c)
		if panel.Clicked() {
			v.showPanel = !v.showPanel
		}
	})
}

// navArrow is a large button at a side of the stage, going to the post
// before or after.
func (a *App) navArrow(c *ui.Context, id int64, q string, left bool) {
	if id == 0 {
		return
	}
	label, ic := "Next post", iconChevronR
	b := ui.ButtonBase(c.Key(left)).Absolute().Top(0).Bottom(0).Width(64).Center()
	if left {
		label, ic = "Previous post", iconChevronL
		b.Left(0)
	} else {
		b.Right(0)
	}
	b.Label(label).Tooltip(label)
	hovered := b.Hovered()
	b.Children(func() {
		knob := ui.Box(c).Size(40, 40).Radius(20).Center().Background(ui.RGBA(0, 0, 0, 0.45))
		if !hovered {
			knob.Opacity(0)
		}
		knob.Children(func() { ui.Icon(c, ic).FontSize(22).TextColor(ui.RGB(255, 255, 255)) })
	})
	if b.Clicked() {
		a.goPost(id, q)
	}
}

// stage shows the post's content.
func (a *App) stage(c *ui.Context) {
	t := c.Theme()
	v := &a.viewer
	p := v.post()
	if p == nil {
		ui.Column(c).Fill().Center().Children(func() {
			if v.ed.err != nil {
				ui.Text(c, v.ed.err.Error()).TextColor(t.Danger)
			} else {
				ui.Spinner(c)
			}
		})
		return
	}
	m := v.media
	thumb := a.sess.client.DataURL(p.ThumbnailURL)
	switch {
	case m == nil:
	case m.web != nil:
		ui.WebView(c.Key("web"), m.web).Fill()
		return
	case m.player != nil:
		ui.Video(c.Key(p.ID), m.player).Fill().Fit(ui.Contain).Label("Post " + strconv.FormatInt(p.ID, 10))
		if p.Type == oxibooru.TypeVideo {
			box := ui.ButtonBase(c.Key("toggle")).Absolute().Top(0).Left(64).Right(64).Bottom(0).Label("Play or pause")
			if box.Clicked() {
				togglePlay(m.player)
			}
			if m.player.Buffering() || !m.player.Ready() {
				ui.Spinner(c).Attach(ui.AnchorCenter, ui.AnchorCenter)
			}
			if err := m.player.Err(); err != nil {
				a.contentNote(c, p, "This video doesn't play here: "+err.Error())
			}
		}
		a.notesOverlay(c, p, m, true)
		return
	case m.image != nil:
		a.imageStage(c, p, m)
		return
	}
	// The thumbnail, while the content loads or where it doesn't show.
	netimage.Image(c, thumb, netimage.Options{MaxSide: 1200, Fit: ui.Contain, NoFade: true}).Fill()
	if m != nil && m.thumbOnly {
		msg := "Shown as its thumbnail."
		switch {
		case p.Type == oxibooru.TypeFlash:
			msg = "Flash plays in other apps."
		case p.Type == oxibooru.TypeDocument:
			msg = "Documents open in other apps."
		case m.err != nil:
			msg = m.err.Error()
		}
		a.contentNote(c, p, msg)
	} else if m != nil && m.progress > 0 && m.progress < 1 {
		ui.Progress(c, m.progress).Width(200).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(24)
	}
}

// contentNote says why the content doesn't show, with a button opening it
// elsewhere.
func (a *App) contentNote(c *ui.Context, p *oxibooru.Post, msg string) {
	ui.Row(c).Gap(10).Padding(8, 8, 8, 14).Radius(10).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.7)).
		Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(24).Children(func() {
		ui.Text(c, msg).FontSize(13).MaxWidth(420)
		if ui.Button(c, "Open in Default App").Clicked() {
			a.openExternally(p)
		}
	})
}

// imageStage shows a picture to fit the stage, or zoomed in a view that
// scrolls.
func (a *App) imageStage(c *ui.Context, p *oxibooru.Post, m *media) {
	v := &a.viewer
	w, h := ui.ImageSize(m.image)
	editing := v.notes.open && v.notes.postID == p.ID
	switch {
	case v.fit == fitWidth && w > 0 && v.stageW > 0:
		v.zoom = max(0.05, (v.stageW-14)/w)
	case v.fit == fitHeight && h > 0 && v.stageH > 0:
		v.zoom = max(0.05, (v.stageH-14)/h)
	}
	if v.zoom == 0 {
		grow := a.s.UpscaleSmall
		fit := ui.ScaleDown
		if grow {
			fit = ui.Contain
		}
		if a.s.TransparencyGrid {
			checkerboard(c, w, h, grow)
		}
		img := ui.Image(c.Key(p.ID), m.image).Fill().Fit(fit).Label("Post " + strconv.FormatInt(p.ID, 10))
		if img.DoubleClicked() {
			v.fit, v.zoom = fitNone, 1
		}
		img.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputZoom || (ev.Kind == ui.InputScroll && ev.Mods&ui.Cmd != 0) {
				v.fit, v.zoom = fitNone, 1
				return true
			}
			return false
		})
		if editing {
			a.notesEditingOverlay(c, w, h, grow)
		} else {
			a.notesOverlay(c, p, m, grow)
		}
		return
	}
	sc := ui.ScrollBoth(c.Key("zoomed")).Fill().TrackScroll(&v.scroll)
	sc.HandleInput(func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputZoom:
			v.fit, v.zoom = fitNone, min(8, max(0.1, v.zoom*ev.Scale))
			return true
		case ev.Kind == ui.InputScroll && ev.Mods&ui.Cmd != 0:
			if ev.DY < 0 {
				v.zoomBy(1.1)
			} else if ev.DY > 0 {
				v.zoomBy(1 / 1.1)
			}
			return true
		}
		return false
	})
	sc.Children(func() {
		// The picture at its zoom, with its notes over it.
		ui.Box(c.Key("frame")).Size(w*v.zoom, h*v.zoom).Margin(ui.Auto).Shrink(0).Children(func() {
			if a.s.TransparencyGrid {
				checkerboard(c, w, h, true)
			}
			img := ui.Image(c.Key(p.ID), m.image).Fill().Fit(ui.FillBox).Cursor(ui.CursorGrab)
			if v.zoom >= 2 {
				if b, ok := m.image.(*ui.Bitmap); ok {
					b.Pixelated()
				}
			}
			if img.DoubleClicked() {
				v.fit, v.zoom = fitNone, 0
			}
			if dx, dy, held := img.Dragged(); held {
				v.scroll.X -= dx
				v.scroll.Y -= dy
			}
			if editing {
				a.notesEditingOverlay(c, w, h, true)
			} else {
				a.notesOverlay(c, p, m, true)
			}
		})
	})
}

// fitRect returns where a picture shows in r: fitted, enlarged when grow,
// else no larger than its size.
func fitRect(r ui.Rect, w, h float32, grow bool) ui.Rect {
	if grow {
		return containRectAny(r, w, h)
	}
	return containRect(r, w, h)
}

// checkerboard draws squares behind a picture, so that its transparent
// parts show.
func checkerboard(c *ui.Context, w, h float32, grow bool) {
	ui.Box(c.Key("checker")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		ar := fitRect(r, w, h, grow)
		p.Fill(ar, ui.RGB(204, 204, 204), 0)
		const sq = 12
		for y := float32(0); y < ar.H; y += sq {
			for x := float32(0); x < ar.W; x += sq {
				if int(x/sq+y/sq)%2 == 0 {
					p.Fill(ui.Rect{X: ar.X + x, Y: ar.Y + y, W: min(sq, ar.W-x), H: min(sq, ar.H-y)}, ui.RGB(255, 255, 255), 0)
				}
			}
		}
	})
}

// containRect returns where a picture of an aspect ratio shows when fitted
// into r, no larger than its own size (w × h).
func containRect(r ui.Rect, w, h float32) ui.Rect {
	if w <= 0 || h <= 0 {
		return r
	}
	s := min(r.W/w, r.H/h, 1)
	dw, dh := w*s, h*s
	return ui.Rect{X: r.X + (r.W-dw)/2, Y: r.Y + (r.H-dh)/2, W: dw, H: dh}
}

// notesOverlay draws the post's notes over its picture, the text of the
// one under the pointer beside it.
func (a *App) notesOverlay(c *ui.Context, p *oxibooru.Post, m *media, grow bool) {
	if !a.s.ShowNotes || len(p.Notes) == 0 {
		return
	}
	w, h := float32(p.CanvasWidth), float32(p.CanvasHeight)
	if m.image != nil {
		w, h = ui.ImageSize(m.image)
	} else if m.player != nil {
		if pw, ph := m.player.Size(); pw > 0 {
			w, h = float32(pw), float32(ph)
		}
	}
	notes := p.Notes
	overlay := ui.Box(c.Key("notes")).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	px, py, over := overlay.PointerPosition()
	box := overlay.LayoutBounds()
	grow = grow || m.player != nil
	area := fitRect(ui.Rect{W: box.W, H: box.H}, w, h, grow)
	hovered := -1
	if over {
		for i, n := range notes {
			if pointInNote(n, (px-area.X)/area.W, (py-area.Y)/area.H) {
				hovered = i
			}
		}
	}
	overlay.PassThrough().Draw(func(pt *ui.Painter, r ui.Rect) {
		ar := fitRect(r, w, h, grow)
		for i, n := range notes {
			path := notePath(n, ar)
			if path == nil {
				continue
			}
			fill := ui.RGBA(255, 236, 140, 0.18)
			if i == hovered {
				fill = ui.RGBA(255, 236, 140, 0.35)
			}
			pt.FillPath(path, fill)
			pt.StrokePath(path, 1.5, ui.RGBA(255, 214, 10, 0.9))
		}
	})
	if hovered >= 0 {
		n := notes[hovered]
		ui.Column(c.Key("note")).Absolute().Left(min(px+14, box.W-300)).Top(py+14).MaxWidth(300).Padding(8, 10).Radius(8).
			Background(ui.RGBA(255, 250, 220, 0.97)).TextColor(ui.RGB(30, 30, 30)).PassThrough().Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.3)).
			Children(func() {
				a.richText(c, "note", n.Text)
			})
	}
}

// containRectAny fits a picture into r at any scale, as a video shows.
func containRectAny(r ui.Rect, w, h float32) ui.Rect {
	if w <= 0 || h <= 0 {
		return r
	}
	s := min(r.W/w, r.H/h)
	dw, dh := w*s, h*s
	return ui.Rect{X: r.X + (r.W-dw)/2, Y: r.Y + (r.H-dh)/2, W: dw, H: dh}
}

func notePath(n oxibooru.Note, r ui.Rect) *ui.Path {
	if len(n.Polygon) < 3 {
		return nil
	}
	path := &ui.Path{}
	for i, pt := range n.Polygon {
		x, y := r.X+float32(pt[0])*r.W, r.Y+float32(pt[1])*r.H
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	return path.Close()
}

// pointInNote reports whether a point, in fractions of the picture, is in a
// note's polygon.
func pointInNote(n oxibooru.Note, x, y float32) bool {
	in := false
	pts := n.Polygon
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi := float32(pts[i][0]), float32(pts[i][1])
		xj, yj := float32(pts[j][0]), float32(pts[j][1])
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// markdownEditor writes Markdown in a text area, or shows what it makes,
// as the web client's Write and Preview tabs.
func (a *App) markdownEditor(c *ui.Context, key string, text *string, tab *int, placeholder string, focus bool) {
	ui.Column(c.Key(key)).Gap(6).Children(func() {
		ui.Segmented(c.Key("tab"), tab, "Write", "Preview").Label("Write or preview")
		if *tab == 1 {
			ui.Column(c.Key("preview")).MinHeight(48).Padding(8).Radius(c.Theme().Radius).Border(1, c.Theme().Border).Children(func() {
				if strings.TrimSpace(*text) == "" {
					ui.Text(c, "Nothing to preview").TextColor(c.Theme().TextMuted)
				} else {
					a.richText(c, "preview:"+key, *text)
				}
			})
			return
		}
		ta := ui.TextArea(c.Key("text"), text).Placeholder(placeholder).Label("Comment").Lines(2, 10)
		if focus {
			ta.AutoFocus()
		}
	})
}

// videoControls plays, pauses, seeks and mutes a video.
func videoControls(c *ui.Context, p *ui.VideoPlayer) {
	ui.Row(c).Height(44).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
		ic, label := iconPlay, "Play"
		if p.Playing() {
			ic, label = iconPause, "Pause"
		}
		if iconButton(c, ic, label).Tooltip(label + " (Space)").Clicked() {
			togglePlay(p)
		}
		pos, dur := p.Position(), p.Duration()
		ui.Text(c, formatDuration(pos)).FontSize(12).FontFeatures("tnum")
		at := pos.Seconds()
		if ui.Slider(c.Key("seek"), &at, 0, max(dur.Seconds(), 0.001)).Grow(1).Label("Position").Changed() {
			p.Seek(time.Duration(at * float64(time.Second)))
		}
		ui.Text(c, formatDuration(dur)).FontSize(12).FontFeatures("tnum")
		loop := p.Loop()
		lb := iconButton(c, iconRepeat, "Loop")
		if loop {
			lb.Background(c.Theme().SurfacePressed)
		}
		if lb.Clicked() {
			p.SetLoop(!loop)
		}
		mic, ml := iconVolume, "Mute"
		if p.Muted() {
			mic, ml = iconMute, "Unmute"
		}
		if iconButton(c, mic, ml).Clicked() {
			p.SetMuted(!p.Muted())
		}
		vol := p.Volume()
		if ui.Slider(c.Key("vol"), &vol, 0, 1).Width(80).Label("Volume").Changed() {
			p.SetVolume(vol)
			p.SetMuted(false)
		}
	})
}

func formatDuration(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// viewerPanel shows the post's details, editable, and its comments.
func (a *App) viewerPanel(c *ui.Context) {
	v := &a.viewer
	p := v.post()
	t := c.Theme()
	if p == nil {
		return
	}
	if v.notes.open && v.notes.postID == p.ID {
		ui.Row(c).Padding(12, 14, 4).Children(func() { ui.Text(c, "Notes").FontSize(16).Bold() })
		a.notesPanel(c, p)
		return
	}
	ui.Row(c).Padding(8, 12).Children(func() {
		ui.Tabs(c, &v.panelTab, "Details", fmt.Sprintf("Comments (%d)", len(p.Comments))).FillWidth()
	})
	switch v.panelTab {
	case 0:
		ui.Scroll(c.Key("details")).Grow(1).Children(func() {
			ui.Column(c).Padding(4, 14, 14).Gap(14).Children(func() {
				if p.Description != "" && v.ed.draft.description == p.Description {
					a.richText(c, "desc"+strconv.FormatInt(p.ID, 10), p.Description)
				}
				a.postEditor(c, &v.ed, a.feedOf(v.query), p, false)
			})
		})
		if v.ed.draft.fields(p) != nil {
			ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
				ui.Text(c, "Edited").FontSize(12).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c, "Revert").Clicked() {
					v.ed.draft = draftOf(p)
				}
				if ui.PrimaryButton(c, "Save").Disabled(v.ed.saving).Clicked() {
					v.ed.save(a)
				}
			})
		}
	case 1:
		a.commentsPanel(c, p)
	}
}

// commentsPanel lists a post's comments, and writes new ones.
func (a *App) commentsPanel(c *ui.Context, p *oxibooru.Post) {
	v := &a.viewer
	t := c.Theme()
	s := a.sess
	ui.Scroll(c.Key("comments")).Grow(1).Children(func() {
		ui.Column(c).Padding(4, 14, 14).Gap(12).Children(func() {
			if len(p.Comments) == 0 {
				ui.Text(c, "No comments yet").TextColor(t.TextMuted)
			}
			for i := range p.Comments {
				cm := &p.Comments[i]
				a.commentView(c, cm, func(updated *oxibooru.Comment) {
					if updated == nil {
						p.Comments = slices.DeleteFunc(p.Comments, func(x oxibooru.Comment) bool { return x.ID == cm.ID })
						p.CommentCount = len(p.Comments)
					} else {
						*cm = *updated
					}
				})
			}
		})
	})
	if s.user != nil && s.can("comment_create") {
		ui.Column(c).Padding(10, 14).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			a.markdownEditor(c, "new", &v.comment, &v.commentTab, "Write a comment (Markdown)", false)
			ui.Row(c).Justify(ui.End).Children(func() {
				ok := strings.TrimSpace(v.comment) != "" && !v.commentBusy
				if ui.PrimaryButton(c, "Comment").Disabled(!ok).KeyShortcuts(ui.Cmd, ui.KeyEnter).Clicked() || (ok && c.Shortcut(ui.Cmd, ui.KeyEnter)) {
					text, id := v.comment, p.ID
					v.commentBusy = true
					async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Comment, error) {
						return cl.CreateComment(ctx, id, text)
					}, func(cm *oxibooru.Comment, err error) {
						v.commentBusy = false
						if err != nil {
							a.fail(err)
							return
						}
						v.comment = ""
						if cur := v.post(); cur != nil && cur.ID == id {
							cur.Comments = append(cur.Comments, *cm)
							cur.CommentCount = len(cur.Comments)
						}
					})
				}
			})
		})
	}
}

// commentView shows a comment, with its score and, for its author, Edit and
// Delete. done takes its new state, nil once deleted.
func (a *App) commentView(c *ui.Context, cm *oxibooru.Comment, done func(*oxibooru.Comment)) {
	t := c.Theme()
	s := a.sess
	v := &a.viewer
	author := "Anonymous"
	var avatarURL string
	if cm.User != nil {
		author, avatarURL = cm.User.Name, cm.User.AvatarURL
	}
	mine := cm.User != nil && s.isMe(cm.User.Name)
	ui.Column(c.Key(cm.ID)).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			var pic ui.ImageSource
			if avatarURL != "" {
				pic, _ = netimage.Load(c, s.client.DataURL(avatarURL), netimage.Options{MaxSide: 64})
			}
			avatar(c, author, pic, 22)
			ui.Text(c, author).Bold().FontSize(13)
			ui.Text(c, formatTime(cm.CreationTime)).FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			if s.user != nil && s.can("comment_score") {
				up := ui.Button(c.Key("up"), "").Label("Like").Children(func() { ui.Icon(c, iconThumbUp).FontSize(12) })
				if cm.OwnScore > 0 {
					up.TextColor(t.Accent)
				}
				ui.Text(c, strconv.Itoa(cm.Score)).FontSize(12).FontFeatures("tnum")
				down := ui.Button(c.Key("down"), "").Label("Dislike").Children(func() { ui.Icon(c, iconThumbDown).FontSize(12) })
				if cm.OwnScore < 0 {
					down.TextColor(t.Danger)
				}
				score := 0
				switch {
				case up.Clicked():
					score = map[bool]int{true: 0, false: 1}[cm.OwnScore > 0]
				case down.Clicked():
					score = map[bool]int{true: 0, false: -1}[cm.OwnScore < 0]
				default:
					score = 99
				}
				if score != 99 {
					id := cm.ID
					async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Comment, error) {
						return cl.RateComment(ctx, id, score)
					}, func(n *oxibooru.Comment, err error) {
						if err != nil {
							a.fail(err)
							return
						}
						done(n)
					})
				}
			}
			canEdit := mine && s.can("comment_edit_own") || s.can("comment_edit_any")
			canDelete := mine && s.can("comment_delete_own") || s.can("comment_delete_any")
			if canEdit || canDelete {
				iconButton(c.Key("menu"), iconMore, "Comment actions").PopoverMenuButton(func(m *ui.Menu) {
					if canEdit && m.Item("Edit").Chosen() {
						v.editing, v.editText = cm.ID, cm.Text
					}
					if canDelete && m.Item("Delete…").Danger().Chosen() {
						snapshot := *cm
						a.confirm("Delete this comment?", "This can't be undone.", "Delete", func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								return cl.DeleteComment(ctx, &snapshot)
							}, func() { done(nil) })
						})
					}
				}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
			}
		})
		if v.editing == cm.ID {
			a.markdownEditor(c, "edit", &v.editText, &v.editTab, "Markdown", true)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					v.editing = 0
				}
				if ui.PrimaryButton(c, "Save").Clicked() {
					snapshot, text := *cm, v.editText
					async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Comment, error) {
						return cl.UpdateComment(ctx, &snapshot, text)
					}, func(n *oxibooru.Comment, err error) {
						if err != nil {
							a.fail(err)
							return
						}
						v.editing = 0
						done(n)
					})
				}
			})
		} else {
			a.richText(c, "comment"+strconv.FormatInt(cm.ID, 10), cm.Text)
		}
	})
}
