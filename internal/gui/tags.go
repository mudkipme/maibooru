package gui

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

var tagSorts = []struct{ label, token string }{
	{"Most Used", "usages"}, {"Name", "name"}, {"Recently Created", "creation-date"},
	{"Recently Edited", "edit-date"}, {"Category", "category"}, {"Most Implications", "implication-count"},
}

type tagsPage struct {
	search string
	help   bool
	cat    string
	sort   string
	pg     pager[oxibooru.Tag]
	list   ui.ListState
	row    int
	// edit names a tag to open in the editor, as asked from another page.
	edit string
	ed   tagEditor
}

// tagEditor edits a tag, or makes a new one when tag is nil and open.
type tagEditor struct {
	open         bool
	tag          *oxibooru.Tag
	names        []string
	namesTC      completer
	category     string
	description  string
	implications []string
	impTC        completer
	suggestions  []string
	sugTC        completer
	siblings     []oxibooru.TagSibling
	mergeInto    string
	mergeTC      completer
	busy         bool
	gen          int
}

func (tp *tagsPage) query() string {
	q := strings.TrimSpace(tp.search)
	if q != "" && !strings.ContainsAny(q, "*:") {
		q = "*" + oxibooru.Escape(q) + "*"
	}
	if tp.cat != "" {
		q = strings.TrimSpace(q + " category:" + oxibooru.Escape(tp.cat))
	}
	sort := tp.sort
	if sort == "" {
		sort = "usages"
	}
	return strings.TrimSpace(q + " sort:" + sort)
}

func (tp *tagsPage) reload(a *App) { tp.pg.reload(a) }

func (a *App) tagsView(c *ui.Context) {
	tp := &a.tags
	t := c.Theme()
	if tp.pg.fetch == nil {
		tp.row = -1
		tp.pg.fetch = func(ctx context.Context, cl *oxibooru.Client, s oxibooru.Search) (*oxibooru.Page[oxibooru.Tag], error) {
			return cl.Tags(ctx, s)
		}
	}
	tp.pg.search(a, tp.query())
	if tp.edit != "" {
		a.openTag(tp.edit)
		tp.edit = ""
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.SearchField(c.Key("q"), &tp.search).Placeholder("Find tags").Label("Find tags").Width(240)
				syntaxHelp(c, &tp.help, tagSyntax, &tp.search)
				cats := append([]string{"All Categories"}, a.sess.tagCategoryNames()...)
				cat := tp.cat
				if cat == "" {
					cat = "All Categories"
				}
				if ui.Select(c.Key("cat"), &cat, cats).Label("Category").Changed() {
					tp.cat = cat
					if cat == "All Categories" {
						tp.cat = ""
					}
				}
				sortLabel := "Most Used"
				for _, so := range tagSorts {
					if so.token == tp.sort {
						sortLabel = so.label
					}
				}
				menuButton(c.Key("sort"), iconSort, sortLabel, "Sort", func(m *ui.Menu) {
					for _, so := range tagSorts {
						if m.Item(so.label).Radio(so.label == sortLabel).Chosen() {
							tp.sort = so.token
						}
					}
				})
				ui.Spacer(c)
				ui.Text(c, pluralize(tp.pg.total, "tag", "tags")).TextColor(t.TextMuted).SingleLine()
				if a.sess.can("tag_create") && toolButton(c.Key("new"), iconPlus, "New Tag").Clicked() {
					a.newTag()
				}
			})
			a.tagsTable(c)
		})
		if tp.ed.open {
			ui.Box(c).Width(1).Background(t.Border)
			ui.Scroll(c.Key("editor")).Width(380).Shrink(0).Children(func() {
				ui.Column(c).Padding(14).Gap(14).Children(func() { a.tagEditorView(c) })
			})
		}
	})
}

func (a *App) tagsTable(c *ui.Context) {
	tp := &a.tags
	t := c.Theme()
	if tp.pg.err != nil && len(tp.pg.items) == 0 {
		ui.Text(c, tp.pg.err.Error()).TextColor(t.Danger).Padding(16)
		return
	}
	cols := []ui.TableColumn{
		{Title: "Name", MinWidth: 160},
		{Title: "Category", Width: 130},
		{Title: "Posts", Width: 80, Align: ui.End},
		{Title: "Implies", Width: 160},
		{Title: "Suggests", Width: 160},
		{Title: "Edited", Width: 110},
	}
	tp.list.Selected = &tp.row
	tp.list.Key = func(i int) any { return tp.pg.items[i].Name() }
	tbl := ui.Table(c.Key("tags"), &tp.list, cols, len(tp.pg.items), func(row, col int) {
		tp.pg.near(a, row)
		tag := &tp.pg.items[row]
		switch col {
		case 0:
			name := a.tagLabel(tag.Name())
			if len(tag.Names) > 1 {
				name += "  (" + a.tagLabel(strings.Join(tag.Names[1:], ", ")) + ")"
			}
			ui.Text(c, name).SingleLine().TextColor(a.sess.tagColor(tag.Category, t))
		case 1:
			ui.Text(c, tag.Category).SingleLine().TextColor(t.TextMuted)
		case 2:
			ui.Text(c, formatCount(tag.Usages)).FontFeatures("tnum")
		case 3:
			var imp []string
			for _, i := range tag.Implications {
				imp = append(imp, i.Name())
			}
			ui.Text(c, strings.Join(imp, ", ")).SingleLine().TextColor(t.TextMuted)
		case 4:
			var sug []string
			for _, s := range tag.Suggestions {
				sug = append(sug, s.Name())
			}
			ui.Text(c, strings.Join(sug, ", ")).SingleLine().TextColor(t.TextMuted)
		case 5:
			ui.Text(c, formatTime(tag.LastEditTime)).SingleLine().TextColor(t.TextMuted)
		}
	}).Grow(1).Label("Tags")
	if tbl.Changed() && tp.row >= 0 && tp.row < len(tp.pg.items) {
		a.editTag(&tp.pg.items[tp.row])
	}
	if tbl.Submitted() && tp.row >= 0 && tp.row < len(tp.pg.items) {
		a.router.Push(postsURL(tagQuery(tp.pg.items[tp.row].Name())))
	}
}

// openTag loads a tag by name into the editor.
func (a *App) openTag(name string) {
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Tag, error) {
		return cl.Tag(ctx, name)
	}, func(tag *oxibooru.Tag, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		a.editTag(tag)
	})
}

// editTag shows a tag in the editor.
func (a *App) editTag(tag *oxibooru.Tag) {
	ed := &a.tags.ed
	ed.gen++
	*ed = tagEditor{open: true, gen: ed.gen}
	cp := *tag
	ed.tag = &cp
	ed.names = slices.Clone(tag.Names)
	ed.category = tag.Category
	ed.description = tag.Description
	for _, i := range tag.Implications {
		ed.implications = append(ed.implications, i.Name())
	}
	for _, s := range tag.Suggestions {
		ed.suggestions = append(ed.suggestions, s.Name())
	}
	gen, name := ed.gen, tag.Name()
	async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.TagSibling, error) {
		return cl.TagSiblings(ctx, name)
	}, func(sib []oxibooru.TagSibling, err error) {
		if err == nil && ed.gen == gen {
			ed.siblings = sib
		}
	})
}

func (a *App) newTag() {
	ed := &a.tags.ed
	ed.gen++
	*ed = tagEditor{open: true, gen: ed.gen, category: a.sess.defaultTagCategory()}
}

func (a *App) tagEditorView(c *ui.Context) {
	ed := &a.tags.ed
	s := a.sess
	t := c.Theme()
	creating := ed.tag == nil
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		title := "New Tag"
		if !creating {
			title = a.tagLabel(ed.tag.Name())
		}
		ui.Text(c, title).FontSize(18).Bold().Grow(1).TextColor(s.tagColor(ed.category, t))
		if iconButton(c, iconClose, "Close").Clicked() {
			ed.open = false
			a.tags.row = -1
		}
	})
	if !creating {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, pluralize(ed.tag.Usages, "post", "posts")).TextColor(t.TextMuted).SingleLine()
			ui.Link(c, "Show posts", postsURL(tagQuery(ed.tag.Name())))
		})
	}
	canName := creating || s.can("tag_edit_name")
	section(c, "Names", func() {
		ui.TokenField(c.Key("names"), &ed.names, nil).Label("Names").Placeholder("Add a name…").Disabled(!canName)
		ui.Text(c, "The first is the tag's name; the others are aliases.").FontSize(12).TextColor(t.TextMuted)
	})
	section(c, "Category", func() {
		ui.Select(c.Key("category"), &ed.category, s.tagCategoryNames()).Label("Category").Disabled(!creating && !s.can("tag_edit_category"))
	})
	section(c, "Description", func() {
		ui.TextArea(c.Key("desc"), &ed.description).Label("Description").Placeholder("Markdown").Lines(3, 12).
			ReadOnly(!creating && !s.can("tag_edit_description"))
	})
	section(c, "Implies", func() {
		a.tagField(c.Key("imp"), &ed.implications, &ed.impTC, "Implied tags").Disabled(!creating && !s.can("tag_edit_implication"))
		ui.Text(c, "Tagging a post with this tag tags it with these too.").FontSize(12).TextColor(t.TextMuted)
	})
	section(c, "Suggests", func() {
		a.tagField(c.Key("sug"), &ed.suggestions, &ed.sugTC, "Suggested tags").Disabled(!creating && !s.can("tag_edit_suggestion"))
	})
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		label := "Save"
		if creating {
			label = "Create"
		}
		if ui.PrimaryButton(c, label).Disabled(ed.busy || len(uniq(ed.names)) == 0).Clicked() {
			a.saveTag()
		}
	})
	if creating {
		return
	}
	if len(ed.siblings) > 0 {
		section(c, "Often Used With", func() {
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for i, sib := range ed.siblings {
					if i >= 30 {
						break
					}
					name := sib.Tag.Name()
					ui.Link(c.Key(name), a.tagLabel(name)+" ("+strconv.Itoa(sib.Occurrences)+")", postsURL(tagQuery(ed.tag.Name())+" "+tagQuery(name))).
						FontSize(12).TextColor(s.tagColor(sib.Tag.Category, t))
				}
			})
		})
	}
	if s.can("tag_merge") {
		section(c, "Merge Into", func() {
			ui.Text(c, "Moves this tag's posts, implications and suggestions to another tag, then deletes it.").FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).Children(func() {
				in := ui.Autocomplete(c.Key("merge"), &ed.mergeInto, ed.mergeTC.names()).Placeholder("Tag").Label("Tag to merge into").Grow(1)
				ed.mergeTC.Query = ed.mergeInto
				ed.mergeTC.update(a, c, in.Changed())
				if ui.Button(c, "Merge…").Disabled(strings.TrimSpace(ed.mergeInto) == "" || ed.busy).Clicked() {
					target, src := strings.TrimSpace(ed.mergeInto), *ed.tag
					a.confirm("Merge "+src.Name()+" into "+target+"?", "This can't be undone.", "Merge", func() {
						a.mergeTag(&src, target)
					})
				}
			})
		})
	}
	if s.can("tag_delete") {
		section(c, "Delete", func() {
			if ed.tag.Usages > 0 {
				ui.Text(c, "Only tags no post uses can be deleted.").FontSize(12).TextColor(t.TextMuted)
			}
			if ui.Button(c, "Delete Tag…").Disabled(ed.tag.Usages > 0 || ed.busy).TextColor(t.Danger).Clicked() {
				tag := *ed.tag
				a.confirm("Delete the tag "+tag.Name()+"?", "This can't be undone.", "Delete", func() {
					a.do(func(ctx context.Context, cl *oxibooru.Client) error {
						return cl.DeleteTag(ctx, tag.Name(), tag.Version)
					}, func() {
						a.tags.ed.open = false
						a.tags.pg.reload(a)
						a.toast("Deleted " + tag.Name())
					})
				})
			}
		})
	}
}

func (a *App) saveTag() {
	ed := &a.tags.ed
	names, imps, sugs := uniq(ed.names), uniq(ed.implications), uniq(ed.suggestions)
	cat, desc := ed.category, ed.description
	ed.busy = true
	if ed.tag == nil {
		f := oxibooru.TagFields{Names: &names, Category: &cat, Implications: &imps, Suggestions: &sugs}
		if desc != "" {
			f.Description = &desc
		}
		async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Tag, error) {
			return cl.CreateTag(ctx, f)
		}, a.tagSaved)
		return
	}
	old := *ed.tag
	f := oxibooru.TagFields{Version: old.Version}
	if !slices.Equal(names, old.Names) {
		f.Names = &names
	}
	if cat != old.Category {
		f.Category = &cat
	}
	if desc != old.Description {
		f.Description = &desc
	}
	var oldImps, oldSugs []string
	for _, i := range old.Implications {
		oldImps = append(oldImps, i.Name())
	}
	for _, s := range old.Suggestions {
		oldSugs = append(oldSugs, s.Name())
	}
	if !slices.Equal(imps, oldImps) {
		f.Implications = &imps
	}
	if !slices.Equal(sugs, oldSugs) {
		f.Suggestions = &sugs
	}
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Tag, error) {
		return cl.UpdateTag(ctx, old.Name(), f)
	}, a.tagSaved)
}

func (a *App) tagSaved(tag *oxibooru.Tag, err error) {
	ed := &a.tags.ed
	ed.busy = false
	if err != nil {
		a.fail(err)
		return
	}
	a.toast("Saved " + tag.Name())
	a.editTag(tag)
	for i := range a.tags.pg.items {
		if a.tags.pg.items[i].Name() == tag.Name() {
			a.tags.pg.items[i] = *tag
			return
		}
	}
	a.tags.pg.reload(a)
}

func (a *App) mergeTag(src *oxibooru.Tag, target string) {
	ed := &a.tags.ed
	ed.busy = true
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Tag, error) {
		into, err := cl.Tag(ctx, target)
		if err != nil {
			return nil, err
		}
		return cl.MergeTags(ctx, src, into)
	}, func(tag *oxibooru.Tag, err error) {
		ed.busy = false
		if err != nil {
			a.fail(err)
			return
		}
		a.toast("Merged into " + tag.Name())
		a.editTag(tag)
		a.tags.pg.reload(a)
	})
}
