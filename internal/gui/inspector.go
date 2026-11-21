package gui

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// postDraft is what the inspector edits of a post, before it saves it.
type postDraft struct {
	tags        []string
	safety      oxibooru.Safety
	source      string
	description string
	loop, sound bool
	relations   []int64
}

func draftOf(p *oxibooru.Post) postDraft {
	return postDraft{
		tags:        p.TagNames(),
		safety:      p.Safety,
		source:      p.Source,
		description: p.Description,
		loop:        p.HasFlag(oxibooru.FlagLoop),
		sound:       p.HasFlag(oxibooru.FlagSound),
		relations:   p.RelationIDs(),
	}
}

// fields returns what the draft changes of the post, nil when nothing.
func (d *postDraft) fields(p *oxibooru.Post) *oxibooru.PostFields {
	f := &oxibooru.PostFields{Version: p.Version}
	changed := false
	if tags := uniq(d.tags); !slices.Equal(tags, p.TagNames()) {
		f.Tags, changed = &tags, true
	}
	if d.safety != p.Safety && d.safety != "" {
		f.Safety, changed = &d.safety, true
	}
	if d.source != p.Source {
		src := d.source
		f.Source, changed = &src, true
	}
	if d.description != p.Description {
		desc := d.description
		f.Description, changed = &desc, true
	}
	if d.loop != p.HasFlag(oxibooru.FlagLoop) || d.sound != p.HasFlag(oxibooru.FlagSound) {
		flags := []string{}
		if d.loop {
			flags = append(flags, oxibooru.FlagLoop)
		}
		if d.sound {
			flags = append(flags, oxibooru.FlagSound)
		}
		f.Flags, changed = &flags, true
	}
	if rel := d.relations; !slices.Equal(rel, p.RelationIDs()) {
		rel = slices.Clone(rel)
		f.Relations, changed = &rel, true
	}
	if !changed {
		return nil
	}
	return f
}

// inspector shows and edits the post chosen in the library, or several.
type inspector struct {
	id      int64
	post    *oxibooru.Post
	loading bool
	err     error
	gen     int

	draft     postDraft
	saving    bool
	addTag    string
	tagInput  ui.Handle
	tags      completer
	suggested []string
	relation  string

	bulk     bulkDraft
	bulkBusy bool

	poolPicker poolPicker
	merge      mergeDialog
	similar    similarDialog
}

type bulkDraft struct {
	add    []string
	tags   completer
	safety int
}

// follow shows the post the feed's cursor is on, saving the one shown
// before when it was changed.
func (in *inspector) follow(a *App, f *feed) {
	var id int64
	if f.chosen.Len() <= 1 {
		if p := f.current(); p != nil {
			id = p.ID
		}
	}
	if id == in.id {
		return
	}
	in.leave(a)
	in.id, in.post, in.err, in.suggested = id, nil, nil, nil
	in.addTag, in.relation = "", ""
	in.gen++
	if id != 0 {
		in.load(a)
	}
}

// leave saves the post shown, if changed.
func (in *inspector) leave(a *App) {
	if in.post != nil && in.draft.fields(in.post) != nil {
		in.save(a)
	}
}

func (in *inspector) load(a *App) {
	id, gen := in.id, in.gen
	in.loading = true
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		return c.Post(ctx, id)
	}, func(p *oxibooru.Post, err error) {
		if gen != in.gen {
			return
		}
		in.loading = false
		if err != nil {
			in.err = err
			return
		}
		in.post, in.draft = p, draftOf(p)
		a.prefetch(p)
	})
}

// reload loads the post shown again, dropping the changes not saved.
func (in *inspector) reload(a *App) {
	if in.id != 0 {
		in.gen++
		in.load(a)
	}
}

// changed takes a post's new state, keeping the edits of fields it didn't
// change.
func (in *inspector) changed(p *oxibooru.Post) {
	if in.post == nil || in.post.ID != p.ID {
		return
	}
	old := draftOf(in.post)
	merged := *in.post
	mergePost(&merged, p)
	if p.Relations != nil {
		merged.Relations = p.Relations
	}
	if p.Description != "" || p.LastEditTime.After(merged.LastEditTime) {
		merged.Description = p.Description
	}
	if p.Comments != nil {
		merged.Comments = p.Comments
	}
	if p.Pools != nil {
		merged.Pools = p.Pools
	}
	merged.LastEditTime = p.LastEditTime
	in.post = &merged
	fresh := draftOf(&merged)
	// Fields the user did not touch follow the server.
	if slices.Equal(in.draft.tags, old.tags) {
		in.draft.tags = fresh.tags
	}
	if in.draft.safety == old.safety {
		in.draft.safety = fresh.safety
	}
	if in.draft.source == old.source {
		in.draft.source = fresh.source
	}
	if in.draft.description == old.description {
		in.draft.description = fresh.description
	}
	if in.draft.loop == old.loop && in.draft.sound == old.sound {
		in.draft.loop, in.draft.sound = fresh.loop, fresh.sound
	}
	if slices.Equal(in.draft.relations, old.relations) {
		in.draft.relations = fresh.relations
	}
}

func (in *inspector) deleted(ids []int64) {
	if slices.Contains(ids, in.id) {
		in.id, in.post = 0, nil
		in.gen++
	}
}

// save sends the draft's changes.
func (in *inspector) save(a *App) {
	p := in.post
	if p == nil || in.saving {
		// A save on its way has the version the next one needs.
		return
	}
	f := in.draft.fields(p)
	if f == nil {
		return
	}
	in.saving = true
	id := p.ID
	added := []string{}
	if f.Tags != nil {
		for _, t := range *f.Tags {
			if !slices.Contains(p.TagNames(), t) {
				added = append(added, t)
			}
		}
	}
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		return c.UpdatePost(ctx, id, *f)
	}, func(np *oxibooru.Post, err error) {
		in.saving = false
		if err != nil {
			a.fail(err)
			return
		}
		if len(added) > 0 {
			a.s.AddRecentTags(a.sess.profile.ID, added...)
			a.save()
		}
		if in.post != nil && in.post.ID == np.ID {
			in.post = np
			in.draft = draftOf(np)
		}
		a.postChanged(np)
	})
}

// prefetch downloads a post's content in the background, so that it can be
// dragged out of the app as a file.
func (a *App) prefetch(p *oxibooru.Post) {
	const limit = 64 << 20
	if p.FileSize > limit || p.ContentURL == "" {
		return
	}
	post := *p
	s := a.sess
	go a.opts.Cache.Fetch(s.ctx, s.client, &post, nil)
}

func (a *App) inspectorView(c *ui.Context, f *feed) {
	in := &a.insp
	t := c.Theme()
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		in.save(a)
	}
	ui.Scroll(c.Key("inspector")).Grow(1).Children(func() {
		ui.Column(c).Padding(14).Gap(14).Children(func() {
			switch {
			case f.chosen.Len() > 1:
				a.bulkView(c, f)
			case in.id == 0:
				ui.Column(c).PaddingY(40).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconInfo).FontSize(28).TextColor(t.TextMuted)
					ui.Text(c, "Choose a post to see and edit it").TextColor(t.TextMuted).TextAlign(ui.Center)
					ui.Text(c, "Shift and Ctrl choose several").FontSize(12).TextColor(t.TextMuted)
				})
			case in.post == nil && in.err != nil:
				ui.Text(c, in.err.Error()).TextColor(t.Danger)
				if ui.Button(c, "Try Again").Clicked() {
					in.reload(a)
				}
			case in.post == nil:
				if p := f.current(); p != nil {
					a.previewBox(c, f, p)
				}
				ui.Spinner(c)
			default:
				a.postEditor(c, in, f, in.post, true)
			}
		})
	})
	if in.post != nil && in.draft.fields(in.post) != nil {
		ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
			ui.Text(c, "Edited").FontSize(12).TextColor(t.TextMuted).Grow(1)
			if ui.Button(c, "Revert").Disabled(in.saving).Clicked() {
				in.draft = draftOf(in.post)
			}
			if ui.PrimaryButton(c, "Save").Disabled(in.saving).KeyShortcuts(ui.Cmd, ui.KeyS).Clicked() {
				in.save(a)
			}
		})
	}
}

// previewBox shows a post's thumbnail at the inspector's width, opening the
// viewer on a click.
func (a *App) previewBox(c *ui.Context, f *feed, p *oxibooru.Post) {
	t := c.Theme()
	ratio := float32(1)
	if p.CanvasWidth > 0 && p.CanvasHeight > 0 {
		ratio = max(0.5, min(2.5, float32(p.CanvasWidth)/float32(p.CanvasHeight)))
	}
	box := ui.ButtonBase(c.Key("preview")).Label("Open post").AspectRatio(ratio).Radius(t.Radius + 2).Clip().Background(t.Surface)
	if box.Clicked() {
		a.openPost(f, p.ID)
	}
	box.DragDataFrom(func() transfer.Data { return a.dragData([]*oxibooru.Post{p}) })
	box.Children(func() {
		netimage.Image(c, a.sess.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 900, Fit: ui.Contain}).Fill()
	})
}

// postEditor shows a post's details, editing what the user may.
func (a *App) postEditor(c *ui.Context, in *inspector, f *feed, p *oxibooru.Post, preview bool) {
	t := c.Theme()
	s := a.sess
	d := &in.draft
	if preview {
		a.previewBox(c, f, p)
	}

	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		if s.user != nil && s.can("post_favorite") {
			ic := iconHeart
			if p.OwnFavorite {
				ic = iconHeartFilled
			}
			fav := ui.ButtonBase(c.Key("fav")).Height(30).Padding(0, 8).Gap(5).Radius(t.Radius).AlignItems(ui.Center).Label("Favorite").Tooltip("Favorite (L)")
			if fav.Hovered() {
				fav.Background(t.SurfaceHover)
			}
			fav.Children(func() {
				ui.Icon(c, ic).FontSize(16)
				ui.Text(c, strconv.Itoa(p.FavoriteCount)).FontFeatures("tnum")
			})
			if p.OwnFavorite {
				fav.TextColor(t.Danger)
			}
			if fav.Clicked() {
				a.toggleFavorites([]*oxibooru.Post{p})
			}
		}
		if s.user != nil && s.can("post_score") {
			up := iconButton(c.Key("up"), iconThumbUp, "Like")
			if p.OwnScore > 0 {
				up.TextColor(t.Accent)
			}
			if up.Clicked() {
				a.ratePost(p.ID, map[bool]int{true: 0, false: 1}[p.OwnScore > 0])
			}
			ui.Text(c, strconv.Itoa(p.Score)).FontFeatures("tnum").MinWidth(16).TextAlign(ui.Center)
			down := iconButton(c.Key("down"), iconThumbDown, "Dislike")
			if p.OwnScore < 0 {
				down.TextColor(t.Danger)
			}
			if down.Clicked() {
				a.ratePost(p.ID, map[bool]int{true: 0, false: -1}[p.OwnScore < 0])
			}
		}
		ui.Spacer(c)
		if iconButton(c.Key("ext"), iconExternal, "Open in default app").Clicked() {
			a.openExternally(p)
		}
		if iconButton(c.Key("dl"), iconDownload, "Save as").Tooltip("Save as…").Clicked() {
			a.export([]*oxibooru.Post{p})
		}
		iconButton(c.Key("more"), iconMore, "More").PopoverMenuButton(func(m *ui.Menu) {
			a.postsMenu(m, f, []*oxibooru.Post{p})
		}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
	})

	editTags := s.can("post_edit_tag")
	section(c, "Tags", func() {
		a.tagChips(c, &d.tags, editTags)
		if editTags {
			a.addTagInput(c, &d.tags, &in.addTag, &in.tags, &in.suggested, &in.tagInput)
		}
	})

	if s.safetyEnabled() {
		section(c, "Safety", func() {
			i := slices.Index(oxibooru.Safeties, d.safety)
			seg := ui.Segmented(c.Key("safety"), &i, "Safe", "Sketchy", "Unsafe").Label("Safety").FillWidth()
			seg.Disabled(!s.can("post_edit_safety"))
			if seg.Changed() && i >= 0 {
				d.safety = oxibooru.Safeties[i]
			}
		})
	}

	section(c, "Source", func() {
		ui.TextArea(c.Key("source"), &d.source).Placeholder("Where it comes from, a link a line").Label("Source").Lines(1, 6).
			ReadOnly(!s.can("post_edit_source"))
		for line := range strings.SplitSeq(d.source, "\n") {
			line = strings.TrimSpace(line)
			if u, err := url.Parse(line); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				ui.Link(c.Key("src:"+line), u.Host+u.Path, line).SingleLine().FontSize(12)
			}
		}
	})

	if !s.info.Config.Szurubooru() {
		section(c, "Description", func() {
			ui.TextArea(c.Key("desc"), &d.description).Placeholder("Markdown").Label("Description").Lines(2, 12).
				ReadOnly(!s.can("post_edit_description"))
		})
	}

	if p.Type == oxibooru.TypeVideo || p.Type == oxibooru.TypeAnimation {
		section(c, "Flags", func() {
			ui.Row(c).Gap(16).Children(func() {
				ui.Checkbox(c.Key("loop"), &d.loop, "Loops").Disabled(!s.can("post_edit_flag"))
				ui.Checkbox(c.Key("sound"), &d.sound, "Has sound").Disabled(!s.can("post_edit_flag"))
			})
		})
	}

	section(c, "Related Posts", func() {
		a.relationsEditor(c, &d.relations, &in.relation, s.can("post_edit_relation"))
	})

	section(c, "Pools", func() {
		for _, pool := range p.Pools {
			ui.Row(c.Key(pool.ID)).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconLayers).TextColor(t.TextMuted)
				if ui.Link(c, pool.Name(), "").SingleLine().Clicked() {
					a.router.Push("/pool/" + strconv.FormatInt(pool.ID, 10))
				}
				ui.Text(c, pluralize(pool.PostCount, "post", "posts")).FontSize(12).TextColor(t.TextMuted)
			})
		}
		if s.can("pool_edit_post") || s.can("pool_create") {
			if ui.Button(c.Key("addpool"), "Add to Pool…").Clicked() {
				in.poolPicker = poolPicker{open: true, posts: []int64{p.ID}}
			}
		}
	})

	section(c, "Details", func() {
		info := [][2]string{
			{"ID", strconv.FormatInt(p.ID, 10)},
			{"Type", typeLabel(p)},
			{"Size", fmt.Sprintf("%d × %d · %s", p.CanvasWidth, p.CanvasHeight, formatBytes(p.FileSize))},
			{"Uploaded", formatTime(p.CreationTime)},
			{"Edited", formatTime(p.LastEditTime)},
		}
		if p.User != nil {
			info = append(info, [2]string{"Uploader", p.User.Name})
		}
		info = append(info,
			[2]string{"Comments", strconv.Itoa(p.CommentCount)},
			[2]string{"Notes", strconv.Itoa(p.NoteCount)},
			[2]string{"Featured", pluralize(p.FeatureCount, "time", "times")},
			[2]string{"Checksum", p.Checksum},
		)
		ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(12).GapY(6).Children(func() {
			for _, row := range info {
				ui.Text(c, row[0]).FontSize(12).TextColor(t.TextMuted)
				v := ui.Text(c, row[1]).FontSize(12).Selectable().MinWidth(0)
				if row[0] == "Checksum" {
					v.TruncateMiddle()
				}
			}
		})
	})
}

// section is a titled part of a panel.
func section(c *ui.Context, title string, fn func()) {
	t := c.Theme()
	ui.Column(c.Key("section:" + title)).Gap(6).Children(func() {
		ui.Text(c, strings.ToUpper(title)).FontSize(11).Bold().TextColor(t.TextMuted).LetterSpacing(0.4)
		fn()
	})
}

func typeLabel(p *oxibooru.Post) string {
	label := map[oxibooru.PostType]string{
		oxibooru.TypeImage: "Image", oxibooru.TypeAnimation: "Animation", oxibooru.TypeVideo: "Video",
		oxibooru.TypeFlash: "Flash", oxibooru.TypeDocument: "Document",
	}[p.Type]
	if label == "" {
		label = string(p.Type)
	}
	if p.MimeType != "" {
		label += " · " + p.MimeType
	}
	return label
}

// tagChips shows tags in the colors of their categories, grouped by them,
// each searching for its posts when clicked and taken out by its button.
func (a *App) tagChips(c *ui.Context, tags *[]string, editable bool) {
	t := c.Theme()
	cats := a.tagCategoriesOf(*tags)
	if len(*tags) == 0 {
		ui.Text(c, "No tags").FontSize(12).TextColor(t.TextMuted)
		return
	}
	var removed string
	ui.Row(c).Wrap().Gap(4).Children(func() {
		for _, name := range sortTags(uniq(*tags), cats, a.sess) {
			color := a.sess.tagColor(cats[name], t)
			chip := ui.Row(c.Key(name)).Gap(2).Padding(2, 4, 2, 8).Radius(999).AlignItems(ui.Center).
				Background(color.Alpha(0.14)).Border(1, color.Alpha(0.35))
			chip.Children(func() {
				lbl := ui.ButtonBase(c.Key("l")).Children(func() {
					ui.Text(c, a.tagLabel(name)).FontSize(12).TextColor(color).SingleLine()
				}).Label("Search " + name).Tooltip(cmpOr(cats[name], "new tag"))
				if lbl.Clicked() {
					a.router.Push(postsURL(tagQuery(name)))
				}
				lbl.ContextMenu(func(m *ui.Menu) {
					if m.Item("Search “" + name + "”").Chosen() {
						a.router.Push(postsURL(tagQuery(name)))
					}
					if m.Item("Edit Tag…").Chosen() {
						a.tags.edit = name
						a.router.Push("/tags")
					}
					if m.Item("Copy Name").Chosen() {
						a.clipboard = name
					}
					if editable {
						m.Separator()
						if m.Item("Remove").Danger().Chosen() {
							removed = name
						}
					}
				})
				if editable {
					x := ui.ButtonBase(c.Key("x")).Size(16, 16).Radius(8).Center().Label("Remove " + name).Children(func() {
						ui.Icon(c, iconClose).FontSize(10).TextColor(color)
					})
					if x.Hovered() {
						x.Background(color.Alpha(0.2))
					}
					if x.Clicked() {
						removed = name
					}
				} else {
					ui.Box(c).Width(4)
				}
			})
		}
	})
	if removed != "" {
		*tags = slices.DeleteFunc(*tags, func(s string) bool { return s == removed })
	}
}

// tagLabel shows a tag's name as the settings ask: its underscores as
// spaces, or as they are.
func (a *App) tagLabel(name string) string {
	if a.s.UnderscoresAsSpaces {
		return strings.ReplaceAll(name, "_", " ")
	}
	return name
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// tagCategoriesOf returns the categories of tags, as the posts shown and
// the completer know them.
func (a *App) tagCategoriesOf(tags []string) map[string]string {
	out := map[string]string{}
	add := func(mt []oxibooru.MicroTag) {
		for _, tag := range mt {
			for _, n := range tag.Names {
				if slices.Contains(tags, n) {
					out[n] = tag.Category
				}
			}
		}
	}
	if a.insp.post != nil {
		add(a.insp.post.Tags)
	}
	if p := a.viewer.post(); p != nil {
		add(p.Tags)
	}
	add(a.insp.tags.found)
	add(a.viewer.ed.tags.found)
	add(a.up.tc.found)
	for _, f := range a.feeds {
		if p := f.current(); p != nil {
			add(p.Tags)
		}
	}
	for n, cat := range a.knownTags {
		if _, ok := out[n]; !ok && slices.Contains(tags, n) {
			out[n] = cat
		}
	}
	maps.Copy(a.knownTags, out)
	return out
}

// sortTags orders tags by their categories' order, then by name.
func sortTags(tags []string, cats map[string]string, s *session) []string {
	order := map[string]int{}
	for i, c := range s.tagCats {
		order[c.Name] = i
	}
	out := slices.Clone(tags)
	slices.SortStableFunc(out, func(x, y string) int {
		ox, okx := order[cats[x]]
		oy, oky := order[cats[y]]
		if !okx {
			ox = len(order)
		}
		if !oky {
			oy = len(order)
		}
		if ox != oy {
			return ox - oy
		}
		return strings.Compare(x, y)
	})
	return out
}

// addTagInput adds the tags typed, with what they imply, and suggests what
// they suggest.
func (a *App) addTagInput(c *ui.Context, tags *[]string, text *string, tc *completer, suggested *[]string, h *ui.Handle) {
	t := c.Theme()
	suggestions := tc.names()
	if strings.TrimSpace(*text) == "" {
		suggestions = slices.DeleteFunc(slices.Clone(a.recentTags()), func(s string) bool { return slices.Contains(*tags, s) })
	}
	in := ui.Autocomplete(c.Key("addtag"), text, suggestions).Placeholder("Add tags…").Label("Add tags").Bind(h)
	tc.Query = *text
	tc.update(a, c, in.Changed())
	if in.Submitted() || (in.Changed() && slices.Contains(suggestions, *text)) {
		var added []string
		for _, name := range strings.FieldsFunc(*text, func(r rune) bool { return r == ' ' || r == ',' }) {
			if !slices.Contains(*tags, name) {
				*tags = append(*tags, name)
				added = append(added, name)
			}
		}
		*text = ""
		if len(added) > 0 {
			a.implications(added, tags, suggested)
		}
	}
	var sugg []string
	for _, s := range *suggested {
		if !slices.Contains(*tags, s) {
			sugg = append(sugg, s)
		}
	}
	if len(sugg) > 0 {
		ui.Row(c).Wrap().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Suggested:").FontSize(12).TextColor(t.TextMuted)
			for _, s := range sugg {
				if ui.Link(c.Key("sugg:"+s), "+"+s, "").FontSize(12).Clicked() {
					*tags = append(*tags, s)
					a.implications([]string{s}, tags, suggested)
				}
			}
		})
	}
}

// implications adds to tags what the tags added imply, and notes what they
// suggest.
func (a *App) implications(added []string, tags *[]string, suggested *[]string) {
	async(a, func(ctx context.Context, c *oxibooru.Client) ([]*oxibooru.Tag, error) {
		var out []*oxibooru.Tag
		for _, name := range added {
			tag, err := c.Tag(ctx, name)
			if err != nil {
				continue // a new tag
			}
			out = append(out, tag)
		}
		return out, nil
	}, func(found []*oxibooru.Tag, _ error) {
		for _, tag := range found {
			a.knownTags[tag.Name()] = tag.Category
			for _, imp := range tag.Implications {
				if n := imp.Name(); !slices.Contains(*tags, n) {
					*tags = append(*tags, n)
					a.knownTags[n] = imp.Category
				}
			}
			for _, sg := range tag.Suggestions {
				if n := sg.Name(); !slices.Contains(*suggested, n) {
					*suggested = append(*suggested, n)
					a.knownTags[n] = sg.Category
				}
			}
		}
	})
}

// relationsEditor shows related posts as thumbnails, and adds them by ID.
func (a *App) relationsEditor(c *ui.Context, rel *[]int64, input *string, editable bool) {
	t := c.Theme()
	thumbs := map[int64]string{}
	if a.insp.post != nil {
		for _, r := range a.insp.post.Relations {
			thumbs[r.ID] = r.ThumbnailURL
		}
	}
	if p := a.viewer.post(); p != nil {
		for _, r := range p.Relations {
			thumbs[r.ID] = r.ThumbnailURL
		}
	}
	if len(*rel) > 0 {
		var removed int64
		ui.Row(c).Wrap().Gap(6).Children(func() {
			for _, id := range *rel {
				ui.Box(c.Key(id)).Size(64, 64).Children(func() {
					b := ui.ButtonBase(c.Key("open")).Fill().Radius(t.Radius).Clip().Background(t.Surface).Label("Post " + strconv.FormatInt(id, 10)).Tooltip("Post " + strconv.FormatInt(id, 10))
					b.Children(func() {
						if u := thumbs[id]; u != "" {
							netimage.Image(c, a.sess.client.DataURL(u), netimage.Options{MaxSide: 160}).Fill()
						} else {
							ui.Text(c, "#"+strconv.FormatInt(id, 10)).FontSize(11).Margin(ui.Auto)
						}
					})
					if b.Clicked() {
						a.openPost(nil, id)
					}
					if editable {
						x := ui.ButtonBase(c.Key("x")).Absolute().Top(2).Right(2).Size(18, 18).Radius(9).Center().
							Background(ui.RGBA(0, 0, 0, 0.6)).Label("Remove relation").Children(func() {
							ui.Icon(c, iconClose).FontSize(11).TextColor(ui.RGB(255, 255, 255))
						})
						if x.Clicked() {
							removed = id
						}
					}
				})
			}
		})
		if removed != 0 {
			*rel = slices.DeleteFunc(*rel, func(x int64) bool { return x == removed })
		}
	} else {
		ui.Text(c, "None").FontSize(12).TextColor(t.TextMuted)
	}
	if editable {
		in := ui.TextInput(c.Key("rel"), input).Placeholder("Add by post ID…").Label("Add a related post")
		if in.Submitted() {
			for _, part := range strings.FieldsFunc(*input, func(r rune) bool { return r == ' ' || r == ',' }) {
				id, err := strconv.ParseInt(strings.TrimPrefix(part, "#"), 10, 64)
				if err == nil && id > 0 && !slices.Contains(*rel, id) {
					*rel = append(*rel, id)
				}
			}
			*input = ""
		}
	}
}

func (a *App) bulkView(c *ui.Context, f *feed) {
	t := c.Theme()
	s := a.sess
	in := &a.insp
	posts := f.chosenPosts()
	b := &in.bulk
	ui.Text(c, pluralize(len(posts), "post", "posts")+" chosen").FontSize(16).Bold()
	ui.Row(c).Wrap().Gap(4).Children(func() {
		for i, p := range posts {
			if i >= 12 {
				ui.Box(c).Size(44, 44).Radius(t.Radius).Center().Background(t.Surface).Children(func() {
					ui.Text(c, fmt.Sprintf("+%d", len(posts)-12)).FontSize(12)
				})
				break
			}
			netimage.Image(c.Key(p.ID), s.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 120}).Size(44, 44).Radius(t.Radius)
		}
	})

	// The tags every post chosen has, taken out of all by their buttons.
	common := map[string]int{}
	for _, p := range posts {
		for _, n := range p.TagNames() {
			common[n]++
		}
	}
	var shared []string
	for n, k := range common {
		if k == len(posts) {
			shared = append(shared, n)
		}
	}
	slices.Sort(shared)
	if s.can("post_edit_tag") {
		section(c, "Tags All Have", func() {
			before := slices.Clone(shared)
			a.tagChips(c, &shared, true)
			if len(shared) < len(before) && !in.bulkBusy {
				var gone []string
				for _, n := range before {
					if !slices.Contains(shared, n) {
						gone = append(gone, n)
					}
				}
				a.bulkEdit(posts, bulkChange{remove: gone})
			}
		})
		section(c, "Add Tags to All", func() {
			a.tagField(c, &b.add, &b.tags, "Tags to add")
			if ui.PrimaryButton(c, "Add Tags").Disabled(len(b.add) == 0 || in.bulkBusy).Clicked() {
				a.bulkEdit(posts, bulkChange{add: uniq(b.add)})
				b.add = nil
			}
		})
	}
	if s.safetyEnabled() && s.can("post_edit_safety") {
		section(c, "Safety", func() {
			ui.Row(c).Gap(6).Children(func() {
				for _, sf := range oxibooru.Safeties {
					if ui.Button(c.Key(sf), safetyLabel(sf)).Disabled(in.bulkBusy).Clicked() {
						a.bulkEdit(posts, bulkChange{safety: sf})
					}
				}
			})
		})
	}
	section(c, "Actions", func() {
		ui.Column(c).Gap(6).AlignItems(ui.Start).Children(func() {
			if s.user != nil && ui.Button(c, "Add to Favorites").Clicked() {
				var notFav []*oxibooru.Post
				for _, p := range posts {
					if !p.OwnFavorite {
						notFav = append(notFav, p)
					}
				}
				a.toggleFavorites(notFav)
			}
			if (s.can("pool_edit_post") || s.can("pool_create")) && ui.Button(c, "Add to Pool…").Clicked() {
				in.poolPicker = poolPicker{open: true, posts: postIDs(posts)}
			}
			if ui.Button(c, "Save to Folder…").Clicked() {
				a.export(posts)
			}
			if s.can("post_delete") && ui.Button(c, "Delete…").TextColor(t.Danger).Clicked() {
				a.deletePosts(posts)
			}
		})
	})
	if in.bulkBusy {
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Saving…").TextColor(t.TextMuted)
		})
	}
}
