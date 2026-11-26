package gui

import (
	"cmp"
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
	"github.com/mudkipme/maibooru/internal/settings"
)

// postsURL returns the location of the library showing what a query finds.
func postsURL(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return "/posts"
	}
	return "/posts?q=" + url.QueryEscape(q)
}

// tagQuery returns the query finding the posts of a tag.
func tagQuery(name string) string { return oxibooru.Escape(name) }

// poolQuery returns the query finding the posts of a pool, in its order.
func poolQuery(id int64) string { return "pool:" + strconv.FormatInt(id, 10) }

var dropFormats = transfer.DropOptions{Formats: []transfer.Format{transfer.FileList, transfer.URIList, transfer.PNG, transfer.Text}}

func (a *App) mainView(c *ui.Context) {
	t := c.Theme()
	a.globalShortcuts(c)
	if strings.HasPrefix(a.router.Path(), "/post/") {
		// The viewer takes the whole window.
		a.router.View(c, func(r *ui.Route) { a.page(c, r) }).Fill()
		a.dialogs(c)
		return
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(a.s.SidebarWidth).Shrink(0).Background(t.Surface)
		side.Children(func() { a.sidebar(c) })
		handle := ui.Box(c).Width(1).Background(t.Border).Shrink(0)
		a.resizer(c, handle, &a.s.SidebarWidth, 1, 170, 360)

		main := ui.Column(c).Grow(1).MinWidth(0)
		// Posts dragged out of the window don't upload again.
		formats := dropFormats
		if a.draggingOut {
			formats = transfer.DropOptions{}
		}
		offer, dragging := ui.DataDragOver(main, formats)
		dropping := (dragging && a.acceptsDrop(offer)) || (main.FileDragOver() && !a.draggingOut)
		if drop, ok := ui.DropData(main, formats); ok {
			a.received(drop.Data)
		}
		if files := main.DroppedFiles(); files != nil && !a.draggingOut {
			a.up.addFiles(files)
			a.goUploads()
		}
		main.Children(func() {
			a.toolbar(c)
			ui.Box(c).Grow(1).MinHeight(0).Children(func() {
				a.router.View(c, func(r *ui.Route) { a.page(c, r) }).Fill()
				if dropping {
					ui.Column(c).Absolute().Top(8).Left(8).Right(8).Bottom(8).Center().Gap(8).PassThrough().
						Radius(12).Border(2, t.Accent).BorderStyle(ui.BorderDashed).Background(t.Background.Alpha(0.85)).Children(func() {
						ui.Icon(c, iconUpload).FontSize(40).TextColor(t.Accent)
						ui.Text(c, "Drop to upload").FontSize(18).Bold()
						if tags := a.contextTags(); len(tags) > 0 {
							ui.Text(c, "Tagged "+strings.Join(tags, ", ")).TextColor(t.TextMuted)
						}
					})
				}
			})
		})
	})
	a.dialogs(c)
}

// dialogs builds the dialogs any page may open.
func (a *App) dialogs(c *ui.Context) {
	a.confirmView(c)
	a.searchEditorView(c)
	a.paletteView(c)
	a.shortcutsView(c)
	a.poolPickerView(c)
	a.mergeView(c)
	a.similarView(c)
}

// acceptsDrop reports whether a drag offers something to upload.
func (a *App) acceptsDrop(o transfer.Offer) bool {
	_, ok := o.Preferred(dropFormats.Formats...)
	return ok
}

// resizer makes a thin element resize a panel by dragging it: dir is 1 for a
// panel on its left, -1 for one on its right.
func (a *App) resizer(c *ui.Context, line ui.Element, width *float32, dir, lo, hi float32) {
	line.Children(func() {
		grip := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-3).Width(7).Cursor(ui.CursorResizeEW).ZIndex(5)
		if dx, _, held := grip.Dragged(); held {
			*width = min(hi, max(lo, *width+dir*dx))
		}
		if _, _, ended := grip.DragEnded(); ended {
			a.save()
		}
	})
}

// contextTags returns the tags uploads take from the page shown: those of
// the search, or none.
func (a *App) contextTags() []string {
	if a.router.Path() != "/posts" {
		return nil
	}
	var tags []string
	for _, tok := range oxibooru.Tokens(a.router.Query("q")) {
		t := oxibooru.ParseToken(tok)
		if t.Negated || (t.Key != "" && t.Key != "tag") || strings.ContainsAny(t.Value, "*,\"") {
			continue
		}
		tags = append(tags, oxibooru.Unescape(t.Value))
	}
	return tags
}

// contextPool returns the pool uploads go into from the page shown, 0 for
// none.
func (a *App) contextPool() int64 {
	if after, ok := strings.CutPrefix(a.router.Path(), "/pool/"); ok {
		id, _ := strconv.ParseInt(after, 10, 64)
		return id
	}
	if a.router.Path() == "/posts" {
		if v := oxibooru.TokenValue(a.router.Query("q"), "pool"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			return id
		}
	}
	return 0
}

// received takes what was dropped or pasted: files, links and pictures, to
// upload.
func (a *App) received(d transfer.Data) {
	if files, err := d.Files(); err == nil && len(files) > 0 {
		a.up.addFiles(files)
		a.goUploads()
		return
	}
	if urls, err := d.URLs(); err == nil && len(urls) > 0 {
		var web []string
		for _, u := range urls {
			if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				web = append(web, u)
			}
		}
		if len(web) > 0 {
			a.up.addURLs(web)
			a.goUploads()
			return
		}
	}
	if png, err := d.Read(transfer.PNG); err == nil && len(png) > 0 {
		a.up.addImage(png)
		a.goUploads()
		return
	}
	if text, err := d.Read(transfer.Text); err == nil {
		var web []string
		for f := range strings.FieldsSeq(string(text)) {
			if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
				web = append(web, f)
			}
		}
		if len(web) > 0 {
			a.up.addURLs(web)
			a.goUploads()
		}
	}
}

func (a *App) goUploads() {
	if a.router.Path() != "/uploads" {
		a.up.defaults(a.contextTags(), a.contextPool())
		a.router.Push("/uploads")
	}
}

func (a *App) globalShortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyF) || (a.keys(c) && (c.Shortcut(0, ui.KeySlash) || c.Shortcut(0, ui.KeyQ))) {
		a.searchFocus.SelectAll()
	}
	if c.Shortcut(ui.Cmd, ui.KeyJ) {
		a.openPalette()
	}
	if c.Shortcut(ui.Cmd, ui.KeyU) {
		a.chooseUploads()
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.router.Push("/settings")
	}
	if c.Shortcut(ui.Cmd, ui.KeyR) || c.Shortcut(0, ui.KeyF5) {
		a.refresh()
	}
	if a.keys(c) && c.Shortcut(ui.Shift, ui.KeySlash) {
		a.shortcutsOpen = true
	}
	if !c.FocusTakesText() && c.Shortcut(ui.Cmd, ui.KeyV) {
		a.paste(c)
	}
}

// keys reports whether single keys act as shortcuts now: the settings let
// them, and the focus takes no text.
func (a *App) keys(c *ui.Context) bool {
	return !a.s.NoKeyShortcuts && !c.FocusTakesText()
}

// paste uploads what the clipboard holds: files, a picture or links.
func (a *App) paste(c *ui.Context) {
	d := c.Services().ReadClipboardData(transfer.FileList, transfer.PNG, transfer.URIList, transfer.Text)
	a.received(d)
}

// chooseUploads asks for files to upload.
func (a *App) chooseUploads() {
	desk := a.opts.Desktop
	if desk == nil {
		return
	}
	go func() {
		files, err := desk.OpenFiles()
		a.post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			if len(files) > 0 {
				a.up.addFiles(files)
				a.goUploads()
			}
		})
	}()
}

// refresh loads the page shown again.
func (a *App) refresh() {
	switch p := a.router.Path(); {
	case p == "/posts":
		if f := a.feedOf(a.router.Query("q")); f != nil {
			f.reload(a)
		}
		a.insp.reload(a)
	case strings.HasPrefix(p, "/post/"):
		a.viewer.reload(a)
	case p == "/tags":
		a.tags.reload(a)
	case p == "/pools":
		a.pools.reload(a)
	case p == "/":
		a.home.loaded = false
	case p == "/history":
		a.history.reload(a)
	case p == "/comments":
		a.comms.reload(a)
	case p == "/users":
		a.users.reload(a)
	}
	a.refreshCategories()
}

// page builds the page of a route.
func (a *App) page(c *ui.Context, r *ui.Route) {
	switch {
	case r.Match("/"):
		r.Title("Overview")
		a.homeView(c)
	case r.Match("/posts"):
		r.Title("Posts")
		a.libraryView(c, a.router.Query("q"))
	case r.Match("/post/{id}"):
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		r.Title("Post " + r.Param("id"))
		a.viewerView(c, id, a.router.Query("q"))
	case r.Match("/uploads"):
		r.Title("Uploads")
		a.uploadsView(c)
	case r.Match("/tags"):
		r.Title("Tags")
		a.tagsView(c)
	case r.Match("/pools"):
		r.Title("Pools")
		a.poolsView(c)
	case r.Match("/pool/{id}"):
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		r.Title("Pool")
		a.poolView(c, id)
	case r.Match("/categories"):
		r.Title("Categories")
		a.categoriesView(c)
	case r.Match("/comments"):
		r.Title("Comments")
		a.commentsView(c)
	case r.Match("/history"):
		r.Title("History")
		a.historyView(c)
	case r.Match("/users"):
		r.Title("Users")
		a.usersView(c)
	case r.Match("/user/{name}"):
		r.Title(r.Param("name"))
		a.userView(c, r.Param("name"))
	case r.Match("/settings"):
		r.Title("Settings")
		a.settingsView(c)
	case r.Match("/help"):
		r.Title("Help")
		a.helpView(c)
	default:
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "Nothing here").TextColor(c.Theme().TextMuted)
		})
	}
}

func (a *App) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	path := a.router.Path()
	if path == "/posts" {
		if q := a.router.Query("q"); q != a.lastQuery || !a.queryShown {
			a.lastQuery, a.searchText, a.queryShown = q, q, true
		}
	} else if a.queryShown {
		a.searchText, a.queryShown = "", false
	}
	ui.Row(c).Height(max(48, bar.Height)).Shrink(0).Padding(0, max(12, bar.Right+8), 0, 12).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
		ui.BackButton(c, a.router)
		ui.ForwardButton(c, a.router)
		a.searchField(c)
		ui.Spacer(c)
		up := ui.PrimaryButton(c, "").Label("Upload").Tooltip("Upload files (Ctrl+U)").Children(func() {
			ui.Icon(c, iconUpload)
			ui.Text(c, "Upload").SingleLine()
		})
		if !a.sess.can("post_create") {
			up.Disabled(true)
		}
		if up.Clicked() {
			if len(a.up.items) > 0 && path != "/uploads" {
				a.goUploads()
			} else {
				a.chooseUploads()
			}
		}
	})
}

func (a *App) searchField(c *ui.Context) {
	prefix, term, ok := lastToken(a.searchText)
	var suggestions []string
	if ok {
		for _, n := range a.searchTags.names() {
			if n != term {
				suggestions = append(suggestions, prefix+oxibooru.Escape(n))
			}
		}
	}
	f := ui.Autocomplete(c.Key("search"), &a.searchText, suggestions).Placeholder("Search tags, or id:12 type:video sort:score…").
		Label("Search").Width(420).MinWidth(160).Shrink(1).Bind(&a.searchFocus)
	if ok {
		a.searchTags.Query = term
	} else {
		a.searchTags.Query = ""
	}
	a.searchTags.update(a, c, f.Changed())
	if f.Submitted() {
		q := strings.TrimSpace(a.searchText)
		if a.router.Path() != "/posts" || q != a.router.Query("q") {
			a.router.Push(postsURL(q))
		}
	}
	help := iconButton(c.Key("searchhelp"), iconInfo, "Search syntax")
	if help.Clicked() {
		a.searchHelp = !a.searchHelp
	}
	ui.Popover(c, help, &a.searchHelp, func() { a.searchHelpView(c) })
}

var searchExamples = []struct{ token, about string }{
	{"cat dog", "posts tagged both cat and dog"},
	{"-dog", "not tagged dog"},
	{"cat*", "a tag starting with cat"},
	{"type:video", "image, animation, video, flash or document"},
	{"safety:safe,sketchy", "of these safeties"},
	{"pool:12", "in pool 12, in its order"},
	{"uploader:ada", "uploaded by ada"},
	{"fav:ada", "in ada's favorites"},
	{"special:fav", "in your favorites"},
	{"tag-count:0", "without tags"},
	{"score:5..", "scored 5 or more"},
	{"date:2024-06", "posted in June 2024"},
	{"width:1920.. ar:1.7..1.8", "wide pictures of about 16:9"},
	{"file-size:..1MB", "files of 1 MB at most"},
	{"sort:random", "sort: random, score, fav-count, file-size, edit-date…"},
	{`re\:zero`, "a backslash escapes : * , and a leading -"},
}

func (a *App) searchHelpView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Gap(8).Width(440).Children(func() {
		ui.Text(c, "Search").Bold()
		ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(14).GapY(6).Children(func() {
			for _, ex := range searchExamples {
				tok := ex.token
				if ui.Link(c.Key(tok), tok, "").FontSize(13).Clicked() {
					a.searchText = strings.TrimSpace(a.searchText + " " + tok)
					a.searchHelp = false
					a.searchFocus.Focus()
				}
				ui.Text(c, ex.about).FontSize(12).TextColor(t.TextMuted)
			}
		})
		ui.Text(c, "Tokens join with spaces. A minus before any token excludes it; ranges take a..b, lists a,b,c.").
			FontSize(12).TextColor(t.TextMuted)
	})
}

// sidebar lists the places of the library and the server.
func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	s := a.sess
	bar := c.TitleBar()
	ui.Column(c).Height(max(bar.Bottom, 0)).Shrink(0).DragWindow()
	ui.Row(c).Padding(10, 14, 6).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Icon(c, iconLibrary).TextColor(t.Accent)
		ui.Text(c, cmp.Or(s.profile.Name, "Oxibooru")).Bold().SingleLine().Grow(1)
	})
	loc := a.router.Location()
	switch {
	case strings.HasPrefix(loc, "/pool/"):
		loc = "/pools"
	case strings.HasPrefix(loc, "/user/"):
		loc = "/users"
	}
	chosen := loc
	me := ""
	if s.user != nil {
		me = s.user.Name
	}
	side := ui.Sidebar(c, &chosen, func() {
		ui.SidebarSection(c, "Library", nil, func() {
			ui.SidebarItem(c, "/", iconHome, "Overview")
			ui.SidebarItem(c, "/posts", iconLibrary, "All Posts")
			if me != "" {
				ui.SidebarItem(c, postsURL("special:fav"), iconHeart, "Favorites")
				ui.SidebarItem(c, postsURL("special:liked"), iconThumbUp, "Liked")
				ui.SidebarItem(c, postsURL("uploader:"+oxibooru.Escape(me)), iconUser, "My Uploads")
			}
			ui.SidebarItem(c, postsURL("sort:random"), iconShuffle, "Random")
			up := ui.SidebarItem(c, "/uploads", iconUpload, "Uploads")
			if n := a.up.pendingCount(); n > 0 {
				up.Children(func() { ui.Badge(c, strconv.Itoa(n)) })
			}
		})
		a.savedSearches(c, loc, c.Focused(a.sideFocus))
		ui.SidebarSection(c, "Organize", nil, func() {
			ui.SidebarItem(c, "/tags", iconTag, "Tags")
			ui.SidebarItem(c, "/pools", iconLayers, "Pools")
			if s.can("tag_category_list") {
				ui.SidebarItem(c, "/categories", iconPalette, "Categories")
			}
		})
		ui.SidebarSection(c, "Server", nil, func() {
			ui.SidebarItem(c, "/comments", iconComment, "Comments")
			if s.can("snapshot_list") || s.rank().Level() >= oxibooru.Power.Level() {
				ui.SidebarItem(c, "/history", iconHistory, "History")
			}
			if s.can("user_list") {
				ui.SidebarItem(c, "/users", iconUsers, "Users")
			}
			ui.SidebarItem(c, "/help", iconInfo, "Help")
		})
	}).Grow(1).Label("Places").Bind(&a.sideFocus)
	switch {
	case !side.Changed() || chosen == a.router.Location():
	case chosen == newSearchID:
		a.editSearch(-1, settings.SavedSearch{})
	default:
		a.router.Push(chosen)
	}
	a.accountButton(c)
}

func (a *App) accountButton(c *ui.Context) {
	t := c.Theme()
	s := a.sess
	name, sub := "Guest", s.profile.URL
	if s.user != nil {
		name, sub = s.user.Name, string(s.user.Rank)
	}
	row := ui.ButtonBase(c.Key("account")).Row().Padding(8, 10).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Label("Account menu")
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		var pic ui.ImageSource
		if s.user != nil && s.user.AvatarURL != "" {
			pic, _ = netimage.Load(c, s.client.DataURL(s.user.AvatarURL), netimage.Options{MaxSide: 96})
		}
		avatar(c, name, pic, 28)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).Bold().SingleLine()
			ui.Text(c, sub).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		ui.Icon(c, iconChevronD).TextColor(t.TextMuted)
	})
	row.PopoverMenuButton(func(m *ui.Menu) {
		if s.user != nil && m.Item("Account").Chosen() {
			a.router.Push("/user/" + url.PathEscape(s.user.Name))
		}
		if m.Item("Settings").Shortcut(ui.Cmd, ui.KeyComma).Chosen() {
			a.router.Push("/settings")
		}
		if m.Item("Help").Chosen() {
			a.router.Push("/help")
		}
		if m.Item("Keyboard Shortcuts").Shortcut(ui.Shift, ui.KeySlash).Chosen() {
			a.shortcutsOpen = true
		}
		m.Separator()
		if m.Item("Switch Server…").Chosen() {
			a.switchServer()
		}
		if s.user != nil {
			if m.Item("Sign Out").Chosen() {
				a.signOut()
			}
		} else if m.Item("Sign In…").Chosen() {
			p := s.profile
			a.switchServer()
			a.connect = connectPage{step: stepSignIn, url: p.URL, profile: p.ID, endpoint: &oxibooru.Endpoint{API: p.API, Site: p.Site}, serverName: p.Name}
		}
	}, ui.PopoverMenuButtonOptions{At: ui.AnchorTopStart, Self: ui.AnchorBottomStart})
}

// avatar shows a user's picture, or their initials while it loads.
func avatar(c *ui.Context, name string, pic ui.ImageSource, size float32) ui.Element {
	t := c.Theme()
	if pic != nil {
		return ui.Image(c, pic).Size(size, size).Radius(size / 2).Fit(ui.Cover).Shrink(0)
	}
	initial := "?"
	if name != "" {
		initial = strings.ToUpper(string([]rune(name)[:1]))
	}
	return ui.Box(c).Size(size, size).Radius(size / 2).Shrink(0).Center().Background(t.Accent.Alpha(0.2)).Children(func() {
		ui.Text(c, initial).FontSize(size * 0.45).Bold().TextColor(t.Accent)
	})
}

// shortcutsView lists the keyboard's shortcuts.
func (a *App) shortcutsView(c *ui.Context) {
	if !a.shortcutsOpen {
		return
	}
	t := c.Theme()
	rows := shortcutRows
	ui.Modal(c, &a.shortcutsOpen, func() {
		ui.Text(c, "Keyboard Shortcuts").FontSize(16).Bold()
		ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(24).GapY(8).Width(380).Children(func() {
			for _, r := range rows {
				ui.Text(c, r[0]).FontFeatures("tnum").TextColor(t.TextMuted)
				ui.Text(c, r[1])
			}
		})
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "Close").Clicked() {
				a.shortcutsOpen = false
			}
		})
	})
}

// shortcutRows are the keys of the app, and what they do.
var shortcutRows = [][2]string{
	{"Ctrl+F, / or Q", "Search"},
	{"Ctrl+J", "Go to a pool, saved search or tag"},
	{"P", "Choose the first post"},
	{"Ctrl+U", "Upload files"},
	{"Ctrl+V", "Upload what the clipboard holds"},
	{"Ctrl+R or F5", "Reload"},
	{"Enter or double-click", "Open the post"},
	{"Ctrl+A", "Choose every post shown"},
	{"Delete", "Delete the posts chosen"},
	{"L", "Add to or take out of favorites"},
	{"I", "Show or hide the inspector, or the viewer's details"},
	{"← → or A D", "Previous and next post"},
	{"F", "Fit the window, its width, its height, or the actual size"},
	{"+ − 0", "Zoom in, out, to fit"},
	{"E, T", "Edit the post, its tags"},
	{"Space", "Play or pause"},
	{"N", "Show or hide notes"},
	{"Escape", "Back to the library"},
	{"Ctrl+,", "Settings"},
}
