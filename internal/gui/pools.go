package gui

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type poolsPage struct {
	search string
	help   bool
	cat    string
	pg     pager[oxibooru.Pool]
	grid   ui.GridState
	cursor int

	newOpen bool
	newName string
	newCat  string
	newDesc string
	busy    bool

	detail poolDetail
}

// poolDetail is the page of a pool: its posts, in their order, and its
// details.
type poolDetail struct {
	id      int64
	pool    *oxibooru.Pool
	loading bool
	err     error
	gen     int

	grid   ui.GridState
	cursor int
	chosen ui.Selection[int64]

	editing     bool
	names       []string
	category    string
	description string
	mergeInto   string
	mergeTC     completer
	busy        bool
}

func (pp *poolsPage) query() string {
	q := strings.TrimSpace(pp.search)
	if q != "" && !strings.ContainsAny(q, "*:") {
		q = "*" + oxibooru.Escape(q) + "*"
	}
	if pp.cat != "" {
		q = strings.TrimSpace(q + " category:" + oxibooru.Escape(pp.cat))
	}
	return strings.TrimSpace(q + " sort:last-edit-date")
}

func (pp *poolsPage) reload(a *App) {
	pp.pg.reload(a)
	if pp.detail.id != 0 {
		pp.detail.load(a)
	}
}

// find returns a pool loaded, by its ID.
func (pp *poolsPage) find(id int64) *oxibooru.Pool {
	if pp.detail.pool != nil && pp.detail.pool.ID == id {
		return pp.detail.pool
	}
	for i := range pp.pg.items {
		if pp.pg.items[i].ID == id {
			return &pp.pg.items[i]
		}
	}
	return nil
}

// changed shows a pool's new state.
func (pp *poolsPage) changed(pool *oxibooru.Pool) {
	for i := range pp.pg.items {
		if pp.pg.items[i].ID == pool.ID {
			pp.pg.items[i] = *pool
		}
	}
	if pp.detail.id == pool.ID {
		pp.detail.pool = pool
	}
}

func (a *App) poolsView(c *ui.Context) {
	pp := &a.pools
	t := c.Theme()
	s := a.sess
	if pp.pg.fetch == nil {
		pp.cursor = -1
		pp.pg.fetch = func(ctx context.Context, cl *oxibooru.Client, q oxibooru.Search) (*oxibooru.Page[oxibooru.Pool], error) {
			return cl.Pools(ctx, q)
		}
	}
	pp.pg.search(a, pp.query())
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.SearchField(c.Key("q"), &pp.search).Placeholder("Find pools").Label("Find pools").Width(240)
			syntaxHelp(c, &pp.help, poolSyntax, &pp.search)
			var cats []string
			for _, pc := range s.poolCats {
				cats = append(cats, pc.Name)
			}
			cats = append([]string{"All Categories"}, cats...)
			cat := pp.cat
			if cat == "" {
				cat = "All Categories"
			}
			if ui.Select(c.Key("cat"), &cat, cats).Label("Category").Changed() {
				pp.cat = cat
				if cat == "All Categories" {
					pp.cat = ""
				}
			}
			ui.Spacer(c)
			ui.Text(c, pluralize(pp.pg.total, "pool", "pools")).TextColor(t.TextMuted).SingleLine()
			if s.can("pool_create") {
				nb := toolButton(c.Key("new"), iconPlus, "New Pool")
				if nb.Clicked() {
					pp.newOpen = true
					pp.newCat = s.defaultPoolCategory()
				}
			}
		})
		if pp.pg.err != nil && len(pp.pg.items) == 0 {
			ui.Text(c, pp.pg.err.Error()).TextColor(t.Danger).Padding(16)
			return
		}
		if pp.pg.done && len(pp.pg.items) == 0 {
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Icon(c, iconLayers).FontSize(40).TextColor(t.TextMuted)
				ui.Text(c, "No pools").FontSize(16).Bold()
				ui.Text(c, "Pools keep posts in an order, as the pages of a comic.").TextColor(t.TextMuted)
			})
			return
		}
		pp.grid.Selected = &pp.cursor
		pp.grid.Key = func(i int) any { return pp.pg.items[i].ID }
		pp.grid.Label = func(i int) string { return pp.pg.items[i].Name() }
		g := ui.GridView(c.Key("pools"), &pp.grid, len(pp.pg.items), 200, 230, func(i int) {
			pp.pg.near(a, i)
			a.poolCard(c, &pp.pg.items[i])
		}).Grow(1).Label("Pools")
		if g.Submitted() && pp.cursor >= 0 && pp.cursor < len(pp.pg.items) {
			a.router.Push("/pool/" + strconv.FormatInt(pp.pg.items[pp.cursor].ID, 10))
		}
	})
	a.newPoolView(c)
}

func (a *App) poolCard(c *ui.Context, p *oxibooru.Pool) {
	t := c.Theme()
	card := ui.Column(c).Grow(1).Gap(6).Padding(4)
	if card.DoubleClicked() {
		a.router.Push("/pool/" + strconv.FormatInt(p.ID, 10))
	}
	card.Children(func() {
		ui.Box(c).Grow(1).Radius(t.Radius).Clip().Background(t.Surface).Children(func() {
			if len(p.Posts) > 0 {
				netimage.Image(c, a.sess.client.DataURL(p.Posts[0].ThumbnailURL), netimage.Options{MaxSide: 400}).Fill()
			} else {
				ui.Icon(c, iconLayers).FontSize(32).TextColor(t.TextMuted).Margin(ui.Auto)
			}
		})
		ui.Text(c, p.Name()).Bold().SingleLine().TextColor(a.poolColor(p.Category, t))
		ui.Text(c, pluralize(p.PostCount, "post", "posts")+" · "+p.Category).FontSize(12).TextColor(t.TextMuted).SingleLine()
	})
}

func (a *App) newPoolView(c *ui.Context) {
	pp := &a.pools
	if !pp.newOpen {
		return
	}
	var cats []string
	for _, pc := range a.sess.poolCats {
		cats = append(cats, pc.Name)
	}
	ui.Modal(c, &pp.newOpen, func() {
		ui.Text(c, "New Pool").FontSize(16).Bold()
		ui.TextInput(c.Key("name"), &pp.newName).Placeholder("Name").Label("Name").AutoFocus().Width(360)
		if len(cats) > 0 {
			ui.Select(c.Key("cat"), &pp.newCat, cats).Label("Category").Width(360)
		}
		ui.TextArea(c.Key("desc"), &pp.newDesc).Placeholder("Description (Markdown)").Label("Description").Lines(3, 8).Width(360)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				pp.newOpen = false
			}
			ok := strings.TrimSpace(pp.newName) != "" && !pp.busy
			if ui.PrimaryButton(c, "Create").Disabled(!ok).Clicked() {
				name := strings.ReplaceAll(strings.TrimSpace(pp.newName), " ", "_")
				f := oxibooru.PoolFields{Names: &[]string{name}, Category: &pp.newCat}
				if d := strings.TrimSpace(pp.newDesc); d != "" {
					f.Description = &d
				}
				pp.busy = true
				async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
					return cl.CreatePool(ctx, f)
				}, func(p *oxibooru.Pool, err error) {
					pp.busy = false
					if err != nil {
						a.fail(err)
						return
					}
					pp.newOpen, pp.newName, pp.newDesc = false, "", ""
					pp.pg.reload(a)
					a.router.Push("/pool/" + strconv.FormatInt(p.ID, 10))
				})
			}
		})
	})
}

func (pd *poolDetail) load(a *App) {
	pd.gen++
	gen, id := pd.gen, pd.id
	pd.loading = true
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
		return cl.Pool(ctx, id)
	}, func(p *oxibooru.Pool, err error) {
		if gen != pd.gen {
			return
		}
		pd.loading, pd.err = false, err
		if err == nil {
			pd.pool = p
		}
	})
}

func (pd *poolDetail) startEditing() {
	p := pd.pool
	pd.editing = true
	pd.names, pd.category, pd.description = slices.Clone(p.Names), p.Category, p.Description
}

func (a *App) poolView(c *ui.Context, id int64) {
	pd := &a.pools.detail
	t := c.Theme()
	s := a.sess
	if pd.id != id {
		*pd = poolDetail{id: id, cursor: -1, gen: pd.gen}
		pd.load(a)
	}
	p := pd.pool
	if p == nil {
		ui.Column(c).Fill().Center().Children(func() {
			if pd.err != nil {
				ui.Text(c, pd.err.Error()).TextColor(t.Danger)
			} else {
				ui.Spinner(c)
			}
		})
		return
	}
	canEditPosts := s.can("pool_edit_post")
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).Padding(14, 16).Gap(6).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconLayers).FontSize(22).TextColor(a.poolColor(p.Category, t))
					ui.Text(c, p.Name()).FontSize(20).Bold().SingleLine()
					if len(p.Names) > 1 {
						ui.Text(c, "also "+strings.Join(p.Names[1:], ", ")).TextColor(t.TextMuted).SingleLine()
					}
					ui.Spacer(c)
					ui.Link(c, "Show in Library", postsURL(poolQuery(p.ID)))
					if !pd.editing && (s.can("pool_edit_name") || s.can("pool_edit_description") || s.can("pool_edit_category")) {
						if ui.Button(c, "Edit").Clicked() {
							pd.startEditing()
						}
					}
				})
				ui.Text(c, pluralize(p.PostCount, "post", "posts")+" · "+p.Category+" · edited "+formatTime(p.LastEditTime)).FontSize(12).TextColor(t.TextMuted)
				if p.Description != "" {
					a.richText(c, "pool"+strconv.FormatInt(p.ID, 10), p.Description)
				}
				if canEditPosts {
					ui.Text(c, "Drag posts to put them in another order; drop files here to add them.").FontSize(12).TextColor(t.TextMuted)
				}
			})
			a.poolPostsGrid(c, pd, canEditPosts)
		})
		if pd.editing {
			ui.Box(c).Width(1).Background(t.Border)
			ui.Scroll(c.Key("edit")).Width(360).Shrink(0).Children(func() {
				ui.Column(c).Padding(14).Gap(14).Children(func() { a.poolEditor(c, pd) })
			})
		}
	})
}

func (a *App) poolPostsGrid(c *ui.Context, pd *poolDetail, editable bool) {
	t := c.Theme()
	p := pd.pool
	posts := p.Posts
	if len(posts) == 0 {
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Text(c, "This pool is empty").FontSize(16).Bold()
			ui.Text(c, "Add posts from the library's menu, or drop files here.").TextColor(t.TextMuted)
		})
		return
	}
	pd.grid.Key = func(i int) any { return posts[i].ID }
	pd.grid.Label = func(i int) string { return "Post " + strconv.FormatInt(posts[i].ID, 10) }
	pd.grid.Selected = &pd.cursor
	pd.grid.Selection = &pd.chosen
	pd.grid.Reorder = nil
	if editable {
		pd.grid.Reorder = func(items []int, to int) {
			ids := p.PostIDs()
			moved := make([]int64, len(items))
			for i, it := range items {
				moved[i] = ids[it]
			}
			rest := slices.DeleteFunc(slices.Clone(ids), func(id int64) bool { return slices.Contains(moved, id) })
			at := to
			for _, it := range items {
				if it < to {
					at--
				}
			}
			at = max(0, min(at, len(rest)))
			order := slices.Insert(rest, at, moved...)
			a.setPoolPosts(pd, order)
		}
	}
	size := float32(a.s.ThumbnailSize)
	q := poolQuery(p.ID)
	g := ui.GridView(c.Key("poolposts"), &pd.grid, len(posts), size, size, func(i int) {
		mp := posts[i]
		cell := ui.Box(c).Grow(1).Radius(t.Radius - 1).Clip()
		if cell.DoubleClicked() {
			a.goPostFrom(mp.ID, q)
		}
		cell.ContextMenu(func(m *ui.Menu) {
			if m.Item("Open").Chosen() {
				a.goPostFrom(mp.ID, q)
			}
			if editable {
				ids := pd.chosenOr(mp.ID)
				m.Separator()
				if m.Item("Move to Start").Chosen() {
					a.movePoolPosts(pd, ids, true)
				}
				if m.Item("Move to End").Chosen() {
					a.movePoolPosts(pd, ids, false)
				}
				if m.Item("Remove from Pool").Danger().Chosen() {
					a.removeFromPool(pd, ids)
				}
			}
		})
		cell.Children(func() {
			netimage.Image(c, a.sess.client.DataURL(mp.ThumbnailURL), netimage.Options{MaxSide: int(size * 2)}).Fill()
			ui.Text(c, strconv.Itoa(i+1)).FontSize(11).Padding(1, 6).Radius(6).Background(ui.RGBA(0, 0, 0, 0.6)).
				TextColor(ui.RGB(255, 255, 255)).Absolute().Left(6).Top(6).PassThrough()
		})
	}).Grow(1).Label("Posts of the pool")
	if g.Submitted() && pd.cursor >= 0 && pd.cursor < len(posts) {
		a.goPostFrom(posts[pd.cursor].ID, q)
	}
	if editable && (g.Shortcut(0, ui.KeyDelete) || g.Shortcut(0, ui.KeyBackspace)) {
		if pd.cursor >= 0 && pd.cursor < len(posts) {
			a.removeFromPool(pd, pd.chosenOr(posts[pd.cursor].ID))
		}
	}
}

func (a *App) goPostFrom(id int64, q string) {
	target := "/post/" + strconv.FormatInt(id, 10) + "?q=" + urlQueryEscape(q)
	a.router.Push(target)
}

// chosenOr returns the posts chosen when id is one of them, else id.
func (pd *poolDetail) chosenOr(id int64) []int64 {
	if pd.chosen.Has(id) {
		var ids []int64
		for _, p := range pd.pool.Posts {
			if pd.chosen.Has(p.ID) {
				ids = append(ids, p.ID)
			}
		}
		return ids
	}
	return []int64{id}
}

func (a *App) movePoolPosts(pd *poolDetail, ids []int64, start bool) {
	rest := slices.DeleteFunc(pd.pool.PostIDs(), func(id int64) bool { return slices.Contains(ids, id) })
	if start {
		a.setPoolPosts(pd, append(slices.Clone(ids), rest...))
	} else {
		a.setPoolPosts(pd, append(rest, ids...))
	}
}

func (a *App) removeFromPool(pd *poolDetail, ids []int64) {
	order := slices.DeleteFunc(pd.pool.PostIDs(), func(id int64) bool { return slices.Contains(ids, id) })
	for _, id := range ids {
		pd.chosen.Remove(id)
	}
	a.setPoolPosts(pd, order)
}

// setPoolPosts changes the pool's posts, showing the new order at once.
func (a *App) setPoolPosts(pd *poolDetail, order []int64) {
	p := pd.pool
	byID := map[int64]oxibooru.MicroPost{}
	for _, mp := range p.Posts {
		byID[mp.ID] = mp
	}
	optimistic := *p
	optimistic.Posts = nil
	for _, id := range order {
		optimistic.Posts = append(optimistic.Posts, byID[id])
	}
	optimistic.PostCount = len(order)
	pd.pool = &optimistic
	version, id := p.Version, p.ID
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
		return cl.UpdatePool(ctx, id, oxibooru.PoolFields{Version: version, Posts: &order})
	}, func(np *oxibooru.Pool, err error) {
		if err != nil {
			a.fail(err)
			pd.load(a)
			return
		}
		a.pools.changed(np)
		for _, f := range a.feeds {
			if f.query == poolQuery(id) {
				f.reload(a)
			}
		}
	})
}

func (a *App) poolEditor(c *ui.Context, pd *poolDetail) {
	s := a.sess
	t := c.Theme()
	p := pd.pool
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Edit Pool").FontSize(16).Bold().Grow(1)
		if iconButton(c, iconClose, "Close").Clicked() {
			pd.editing = false
		}
	})
	section(c, "Names", func() {
		ui.TokenField(c.Key("names"), &pd.names, nil).Label("Names").Placeholder("Add a name…").Disabled(!s.can("pool_edit_name"))
	})
	var cats []string
	for _, pc := range s.poolCats {
		cats = append(cats, pc.Name)
	}
	section(c, "Category", func() {
		ui.Select(c.Key("cat"), &pd.category, cats).Label("Category").Disabled(!s.can("pool_edit_category"))
	})
	section(c, "Description", func() {
		ui.TextArea(c.Key("desc"), &pd.description).Label("Description").Placeholder("Markdown").Lines(3, 12).ReadOnly(!s.can("pool_edit_description"))
	})
	ui.Row(c).Justify(ui.End).Children(func() {
		if ui.PrimaryButton(c, "Save").Disabled(pd.busy || len(uniq(pd.names)) == 0).Clicked() {
			f := oxibooru.PoolFields{Version: p.Version}
			if names := uniq(pd.names); !slices.Equal(names, p.Names) {
				f.Names = &names
			}
			if pd.category != p.Category {
				cat := pd.category
				f.Category = &cat
			}
			if pd.description != p.Description {
				d := pd.description
				f.Description = &d
			}
			pd.busy = true
			id := p.ID
			async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
				return cl.UpdatePool(ctx, id, f)
			}, func(np *oxibooru.Pool, err error) {
				pd.busy = false
				if err != nil {
					a.fail(err)
					return
				}
				pd.editing = false
				a.pools.changed(np)
				a.toast("Saved " + np.Name())
			})
		}
	})
	if s.can("pool_merge") {
		section(c, "Merge Into", func() {
			ui.Text(c, "Moves this pool's posts to another pool, then deletes it.").FontSize(12).TextColor(t.TextMuted)
			pd.mergeTC.pools = true
			ui.Row(c).Gap(8).Children(func() {
				in := ui.Autocomplete(c.Key("merge"), &pd.mergeInto, pd.mergeTC.names()).Placeholder("Pool").Label("Pool to merge into").Grow(1)
				pd.mergeTC.Query = pd.mergeInto
				pd.mergeTC.update(a, c, in.Changed())
				if ui.Button(c, "Merge…").Disabled(strings.TrimSpace(pd.mergeInto) == "" || pd.busy).Clicked() {
					src, target := *p, strings.TrimSpace(pd.mergeInto)
					a.confirm("Merge "+src.Name()+" into "+target+"?", "This can't be undone.", "Merge", func() {
						pd.busy = true
						async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
							page, err := cl.Pools(ctx, oxibooru.Search{Query: "name:" + oxibooru.Escape(target), Limit: 1})
							if err != nil {
								return nil, err
							}
							if len(page.Results) == 0 {
								return nil, errNotFound("pool " + target)
							}
							return cl.MergePools(ctx, &src, &page.Results[0])
						}, func(np *oxibooru.Pool, err error) {
							pd.busy = false
							if err != nil {
								a.fail(err)
								return
							}
							a.pools.pg.reload(a)
							a.router.Replace("/pool/" + strconv.FormatInt(np.ID, 10))
						})
					})
				}
			})
		})
	}
	if s.can("pool_delete") {
		section(c, "Delete", func() {
			if ui.Button(c, "Delete Pool…").TextColor(t.Danger).Disabled(pd.busy).Clicked() {
				src := *p
				a.confirm("Delete the pool "+src.Name()+"?", "Its posts stay. This can't be undone.", "Delete", func() {
					a.do(func(ctx context.Context, cl *oxibooru.Client) error {
						return cl.DeletePool(ctx, src.ID, src.Version)
					}, func() {
						a.pools.pg.reload(a)
						a.pools.detail = poolDetail{}
						a.router.Replace("/pools")
					})
				})
			}
		})
	}
}
