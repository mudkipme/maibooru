package gui

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
	"github.com/mudkipme/maibooru/internal/settings"
)

var searchIcons = []struct {
	name string
	svg  *ui.SVG
}{
	{"Bookmark", iconBookmark}, {"Folder", iconFolder}, {"Star", iconStar}, {"Heart", iconHeart},
	{"Tag", iconTag}, {"Image", iconImage}, {"Video", iconVideo}, {"File", iconFile},
	{"Layers", iconLayers}, {"Eye", iconEye}, {"Comment", iconComment}, {"Note", iconNote},
	{"User", iconUser}, {"Sound", iconSound}, {"Shield", iconShield}, {"History", iconHistory},
}

var searchColors = []struct{ name, hex string }{
	{"Red", "#e5484d"}, {"Orange", "#f76b15"}, {"Yellow", "#e2a336"}, {"Green", "#30a46c"}, {"Teal", "#12a594"},
	{"Blue", "#3e63dd"}, {"Purple", "#8e4ec6"}, {"Pink", "#d6409f"}, {"Gray", "#8b8d98"},
}

func searchIcon(name string) *ui.SVG {
	for _, ic := range searchIcons {
		if strings.EqualFold(ic.name, name) {
			return ic.svg
		}
	}
	return iconBookmark
}

// searchColor returns a saved search's color, false for the accent.
func searchColor(name string) (ui.Color, bool) {
	for _, sc := range searchColors {
		if strings.EqualFold(sc.name, name) {
			return ui.Hex(sc.hex), true
		}
	}
	return ui.Color{}, false
}

// postCount is how many posts a query found when last asked.
type postCount struct {
	n      int
	at     time.Time
	asking bool
}

const countsKeep = 5 * time.Minute

// postCount returns how many posts a query finds, asking the server when it
// hasn't for a while.
func (a *App) postCount(q string) (int, bool) {
	s := a.sess
	if s.counts == nil {
		s.counts = map[string]*postCount{}
	}
	pc := s.counts[q]
	if pc == nil {
		pc = &postCount{n: -1}
		s.counts[q] = pc
	}
	if !pc.asking && time.Since(pc.at) > countsKeep {
		pc.asking = true
		async(a, func(ctx context.Context, cl *oxibooru.Client) (int, error) {
			page, err := cl.Posts(ctx, oxibooru.Search{Query: q, Limit: 1, Fields: []string{"id"}})
			if err != nil {
				return 0, err
			}
			return page.Total, nil
		}, func(n int, err error) {
			pc.asking, pc.at = false, time.Now()
			if err == nil {
				pc.n = n
			}
		})
	}
	return pc.n, pc.n >= 0
}

// noteCount keeps how many posts a query found.
func (a *App) noteCount(q string, n int) {
	if a.sess.counts == nil {
		a.sess.counts = map[string]*postCount{}
	}
	if pc := a.sess.counts[q]; pc != nil && pc.asking {
		pc.n = n
		return
	}
	a.sess.counts[q] = &postCount{n: n, at: time.Now()}
}

const newSearchID = "#new-search"

// savedSearches lists the saved searches in the sidebar, in their icons and
// colors, with how many posts each finds.
func (a *App) savedSearches(c *ui.Context, chosen string, focused bool) {
	t := c.Theme()
	saved := a.s.Saved[a.sess.profile.ID]
	ui.SidebarSection(c, "Saved Searches", &a.savedOpen, func() {
		for i, ss := range saved {
			id := postsURL(ss.Query)
			highlighted := id == chosen && focused
			item := func() {
				it := ui.SidebarItem(c.Key(i), id, searchIcon(ss.Icon), ss.Name).Tooltip(ss.Query)
				if n, ok := a.postCount(ss.Query); ok {
					it.Children(func() {
						count := ui.Text(c, formatCount(n)).FontSize(11).FontFeatures("tnum").SingleLine()
						if !highlighted {
							count.TextColor(t.TextMuted)
						}
					})
				}
				it.ContextMenu(func(m *ui.Menu) {
					if m.Item("Edit…").Chosen() {
						a.editSearch(i, ss)
					}
					if m.Item("Remove").Danger().Chosen() {
						a.removeSavedSearch(i)
					}
				})
			}
			if col, ok := searchColor(ss.Color); ok && !highlighted {
				th := *t
				th.Accent, th.InactiveAccent = col, col
				ui.Themed(c.Key(i), &th).Background(ui.Transparent).Children(item)
			} else {
				item()
			}
		}
		ui.SidebarItem(c, newSearchID, iconPlus, "New Saved Search…")
	})
}

// searchEditor edits a saved search: its name, looks, and the rules making
// its query.
type searchEditor struct {
	open  bool
	index int // the search edited, -1 for a new one
	name  string
	icon  string
	color string
	rules []*rule
	sort  string
	looks bool
	// query is the query of the rules, edited at editedAt.
	query    string
	editedAt time.Time
	count    string
}

func (a *App) editSearch(index int, ss settings.SavedSearch) {
	rules, sort := parseRules(ss.Query, a.filterEnv())
	if len(rules) == 0 {
		rules = []*rule{newRule(ruleFields[0])}
	}
	a.saveSearch = searchEditor{open: true, index: index, name: ss.Name, icon: ss.Icon, color: ss.Color,
		rules: rules, sort: sort, query: ss.Query}
}

func (a *App) searchEditorView(c *ui.Context) {
	d := &a.saveSearch
	if !d.open {
		return
	}
	t := c.Theme()
	env := a.filterEnv()
	q := buildQuery(d.rules, d.sort, env)
	if q != d.query {
		d.query, d.editedAt = q, c.Now()
	}
	if wait := typingPause*2 - c.Now().Sub(d.editedAt); wait > 0 {
		c.After(wait)
	} else if n, ok := a.postCount(q); ok {
		d.count = pluralize(n, "post", "posts")
	}
	ui.Modal(c, &d.open, func() {
		title := "New Saved Search"
		if d.index >= 0 {
			title = "Edit Saved Search"
		}
		ui.Column(c).Gap(12).Width(620).Children(func() {
			ui.Text(c, title).FontSize(16).Bold()
			var name ui.Element
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.looksButton(c, d)
				name = ui.TextInput(c, &d.name).Placeholder("Name").Label("Name").AutoFocus().Grow(1)
			})
			ui.Text(c, "Posts match all of these rules.").FontSize(12).TextColor(t.TextMuted)
			remove := -1
			ui.Column(c).Gap(6).Children(func() {
				for i, r := range d.rules {
					if a.ruleRow(c.Key(r), r, env) {
						remove = i
					}
				}
			})
			if remove >= 0 {
				d.rules = slices.Delete(d.rules, remove, remove+1)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if toolButton(c.Key("add"), iconPlus, "Add Rule").Clicked() {
					d.rules = append(d.rules, newRule(ruleFields[0]))
				}
				ui.Spacer(c)
				sortLabel := d.sort
				for _, so := range sortOptions {
					if so.token == d.sort {
						sortLabel = so.label
					}
				}
				menuButton(c.Key("sort"), iconSort, sortLabel, "Sort", func(m *ui.Menu) {
					for _, so := range sortOptions {
						if m.Item(so.label).Radio(so.token == d.sort).Chosen() {
							d.sort = so.token
						}
					}
				})
			})
			ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(8, 10).Radius(t.Radius).Background(t.SurfaceHover).Children(func() {
				ui.Text(c, cmp.Or(q, "All posts")).FontSize(12).TextColor(t.TextMuted).Wrap().Grow(1).MinWidth(0)
				ui.Text(c, d.count).FontSize(12).FontFeatures("tnum").SingleLine()
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					d.open = false
				}
				ok := strings.TrimSpace(d.name) != "" && q != ""
				if (ui.PrimaryButton(c, "Save").Disabled(!ok).Clicked() || name.Submitted()) && ok {
					a.keepSearch(d.index, settings.SavedSearch{Name: strings.TrimSpace(d.name), Query: q, Icon: d.icon, Color: d.color})
					d.open = false
				}
			})
		})
	})
}

// keepSearch saves a search, in place of the one at index, or as a new one.
func (a *App) keepSearch(index int, ss settings.SavedSearch) {
	id := a.sess.profile.ID
	if index >= 0 && index < len(a.s.Saved[id]) {
		a.s.Saved[id][index] = ss
	} else {
		a.s.Saved[id] = append(a.s.Saved[id], ss)
	}
	a.savedOpen = true
	a.save()
}

func (a *App) removeSavedSearch(i int) {
	id := a.sess.profile.ID
	saved := a.s.Saved[id]
	if i < 0 || i >= len(saved) {
		return
	}
	a.s.Saved[id] = append(saved[:i:i], saved[i+1:]...)
	a.save()
}

// looksButton shows the saved search's icon in its color, and picks them.
func (a *App) looksButton(c *ui.Context, d *searchEditor) {
	t := c.Theme()
	col, ok := searchColor(d.color)
	if !ok {
		col = t.Accent
	}
	b := ui.ButtonBase(c.Key("looks")).Size(34, 34).Shrink(0).Radius(t.Radius).Center().
		BorderWidth(1).BorderColor(t.Border).Label("Icon and color").Tooltip("Icon and color")
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() { ui.Icon(c, searchIcon(d.icon)).FontSize(18).TextColor(col) })
	if b.Clicked() {
		d.looks = !d.looks
	}
	ui.Popover(c, b, &d.looks, func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.Row(c).Wrap().Gap(4).Width(8*32 + 7*4).Children(func() {
				for _, ic := range searchIcons {
					chosen := searchIcon(d.icon) == ic.svg
					ib := on(iconButton(c.Key(ic.name), ic.svg, ic.name), chosen, c).Size(32, 32)
					if ib.Clicked() {
						d.icon = strings.ToLower(ic.name)
						if ic.svg == iconBookmark {
							d.icon = ""
						}
					}
				}
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				swatch := func(name string, col ui.Color) {
					sb := ui.ButtonBase(c.Key(name)).Size(22, 22).Radius(11).Background(col).Label(name).Tooltip(name)
					if strings.EqualFold(name, d.color) || (name == "Accent" && d.color == "") {
						sb.BorderWidth(2).BorderColor(t.Text)
					}
					if sb.Clicked() {
						d.color = strings.ToLower(name)
						if name == "Accent" {
							d.color = ""
						}
					}
				}
				swatch("Accent", t.Accent)
				for _, sc := range searchColors {
					swatch(sc.name, ui.Hex(sc.hex))
				}
			})
		})
	})
}

var fieldSubmenus = []string{"Size", "Count", "Date"}

// ruleRow edits a rule, reporting whether it is to be removed.
func (a *App) ruleRow(c *ui.Context, r *rule, env filterEnv) (remove bool) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		menuButton(c.Key("field"), nil, r.field.label, "Field", func(m *ui.Menu) {
			item := func(m *ui.Menu, rf *ruleField) {
				if m.Item(rf.label).Radio(r.field == rf).Chosen() && r.field != rf {
					*r = *newRule(rf)
				}
			}
			prev, prevSub, first := "", false, true
			for _, rf := range ruleFields {
				if !rf.available(env) {
					continue
				}
				sub := slices.Contains(fieldSubmenus, rf.group)
				if !first && rf.group != prev && !(sub && prevSub) {
					m.Separator()
				}
				switch {
				case !sub:
					item(m, rf)
				case rf.group != prev:
					g := rf.group
					m.Submenu(g, func(sm *ui.Menu) {
						for _, f := range ruleFields {
							if f.group == g && f.available(env) {
								item(sm, f)
							}
						}
					})
				}
				prev, prevSub, first = rf.group, sub, false
			}
		})
		if ops := ruleOps[r.field.kind]; len(ops) > 0 {
			label := ops[0].label
			if i := slices.IndexFunc(ops, func(o filterOption) bool { return o.value == r.op }); i >= 0 {
				label = ops[i].label
			}
			menuButton(c.Key("op"), nil, label, "Match", func(m *ui.Menu) {
				for _, o := range ops {
					if m.Item(o.label).Radio(o.value == r.op).Chosen() {
						r.op = o.value
					}
				}
			})
		}
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
			a.ruleValue(c, r, env, t)
		})
		remove = iconButton(c.Key("remove"), iconMinus, "Remove rule").Clicked()
	})
	return remove
}

func (a *App) ruleValue(c *ui.Context, r *rule, env filterEnv, t *ui.Theme) {
	rf := r.field
	switch rf.kind {
	case kindTags:
		a.tagField(c, &r.values, &r.tags, "Tags").Grow(1).MinWidth(0)
	case kindChoice:
		for _, o := range rf.choices(env) {
			chosen := slices.Contains(r.values, o.value)
			if on(toolButton(c.Key(o.value), nil, o.label), chosen, c).Clicked() {
				switch {
				case rf.single:
					r.values = []string{o.value}
				case chosen:
					r.values = slices.DeleteFunc(slices.Clone(r.values), func(v string) bool { return v == o.value })
				default:
					r.values = append(slices.Clone(r.values), o.value)
				}
			}
		}
	case kindNames:
		placeholder := "Add a name…"
		if rf.key() == "pool" {
			placeholder = "Add a pool ID…"
		}
		ui.TokenField(c, &r.values, nil).Label(rf.label).Placeholder(placeholder).Grow(1).MinWidth(0)
	case kindText:
		ui.TextInput(c, &r.a).Label(rf.label).Grow(1).MinWidth(0)
	case kindNumber, kindDate:
		placeholder := ""
		if rf.kind == kindDate {
			placeholder = "2024-06-30"
		}
		ui.TextInput(c.Key("a"), &r.a).Label(rf.label).Placeholder(placeholder).Width(110)
		if r.op == "between" {
			ui.Text(c, "and")
			ui.TextInput(c.Key("b"), &r.b).Label(rf.label + " up to").Placeholder(placeholder).Width(110)
		}
		if rf.unit != "" {
			ui.Text(c, rf.unit).TextColor(t.TextMuted)
		}
	case kindSyntax:
		ui.TextInput(c, &r.a).Label("Search syntax").Placeholder("cat -dog score:5..").Grow(1).MinWidth(0)
	}
}
