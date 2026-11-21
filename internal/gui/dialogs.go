package gui

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// poolPicker adds posts to a pool the user finds or makes.
type poolPicker struct {
	open    bool
	posts   []int64
	find    completer
	newName string
	busy    bool
}

func (a *App) poolPickerView(c *ui.Context) {
	pp := &a.insp.poolPicker
	if !pp.open {
		return
	}
	t := c.Theme()
	pp.find.pools = true
	ui.Modal(c, &pp.open, func() {
		ui.Text(c, "Add "+pluralize(len(pp.posts), "Post", "Posts")+" to a Pool").FontSize(16).Bold()
		in := ui.SearchField(c.Key("find"), &pp.find.Query).Placeholder("Find a pool").Label("Find a pool").AutoFocus().Width(380)
		pp.find.update(a, c, in.Changed())
		ui.Scroll(c).MaxHeight(260).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				for _, p := range pp.find.found {
					row := ui.ButtonBase(c.Key(p.Name())).Row().Padding(8, 10).Gap(8).Radius(t.Radius).AlignItems(ui.Center).Disabled(pp.busy)
					if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Icon(c, iconLayers).TextColor(a.poolColor(p.Category, t))
						ui.Text(c, p.Name()).Grow(1).SingleLine()
						ui.Text(c, pluralize(p.Usages, "post", "posts")).FontSize(12).TextColor(t.TextMuted)
					})
					if row.Clicked() {
						a.addToPool(p.Name(), pp.posts)
					}
				}
				if strings.TrimSpace(pp.find.Query) != "" && len(pp.find.found) == 0 {
					ui.Text(c, "No pool has this name").FontSize(12).TextColor(t.TextMuted).Padding(8)
				}
			})
		})
		if a.sess.can("pool_create") {
			ui.Divider(c)
			ui.Row(c).Gap(8).Children(func() {
				in := ui.TextInput(c.Key("new"), &pp.newName).Placeholder("New pool name").Label("New pool name").Grow(1)
				ok := strings.TrimSpace(pp.newName) != "" && !pp.busy
				if (ui.Button(c, "Create").Disabled(!ok).Clicked() || in.Submitted()) && ok {
					a.createPoolWith(strings.ReplaceAll(strings.TrimSpace(pp.newName), " ", "_"), pp.posts)
				}
			})
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				pp.open = false
			}
		})
	})
}

// poolColor returns the color of a pool category.
func (a *App) poolColor(category string, t *ui.Theme) ui.Color {
	for _, pc := range a.sess.poolCats {
		if pc.Name == category {
			return categoryColor(pc.Color, t)
		}
	}
	return t.TextMuted
}

// addToPool adds posts to the end of the pool of a name or an ID.
func (a *App) addToPool(name string, posts []int64) {
	pp := &a.insp.poolPicker
	pp.busy = true
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Pool, error) {
		page, err := c.Pools(ctx, oxibooru.Search{Query: "name:" + oxibooru.Escape(name), Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Results) == 0 {
			return nil, errNotFound("pool " + name)
		}
		pool := page.Results[0]
		ids := pool.PostIDs()
		for _, id := range posts {
			if !containsID(ids, id) {
				ids = append(ids, id)
			}
		}
		return c.UpdatePool(ctx, pool.ID, oxibooru.PoolFields{Version: pool.Version, Posts: &ids})
	}, func(pool *oxibooru.Pool, err error) {
		pp.busy = false
		if err != nil {
			a.fail(err)
			return
		}
		pp.open = false
		a.toast("Added to " + pool.Name())
		a.poolChanged(pool)
	})
}

func (a *App) createPoolWith(name string, posts []int64) {
	pp := &a.insp.poolPicker
	pp.busy = true
	cat := a.sess.defaultPoolCategory()
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Pool, error) {
		return c.CreatePool(ctx, oxibooru.PoolFields{Names: &[]string{name}, Category: &cat, Posts: &posts})
	}, func(pool *oxibooru.Pool, err error) {
		pp.busy = false
		if err != nil {
			a.fail(err)
			return
		}
		pp.open = false
		a.toast("Made the pool " + pool.Name())
		a.poolChanged(pool)
	})
}

// poolChanged shows a pool's new state where it shows, and its posts'
// pools.
func (a *App) poolChanged(pool *oxibooru.Pool) {
	a.pools.changed(pool)
	a.insp.reload(a)
	if a.viewer.post() != nil {
		a.viewer.reload(a)
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

type notFound string

func (e notFound) Error() string { return "no " + string(e) }

func errNotFound(what string) error { return notFound(what) }

// similarDialog shows the posts that look like a post.
type similarDialog struct {
	open  bool
	post  oxibooru.Post
	found []oxibooru.SimilarPost
	busy  bool
	err   error
}

func (a *App) similarView(c *ui.Context) {
	sd := &a.insp.similar
	if !sd.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &sd.open, func() {
		ui.Text(c, "Posts Like Post "+strconv.FormatInt(sd.post.ID, 10)).FontSize(16).Bold()
		switch {
		case sd.busy:
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Looking…").TextColor(t.TextMuted)
			})
		case sd.err != nil:
			ui.Text(c, sd.err.Error()).TextColor(t.Danger).MaxWidth(480)
		case len(sd.found) == 0:
			ui.Text(c, "No post looks like it.").TextColor(t.TextMuted)
		default:
			ui.Scroll(c).MaxHeight(420).Width(560).Children(func() {
				ui.Row(c).Wrap().Gap(10).Children(func() {
					for _, s := range sd.found {
						p := s.Post
						ui.Column(c.Key(p.ID)).Gap(4).Width(130).Children(func() {
							b := ui.ButtonBase(c.Key("open")).Size(130, 130).Radius(t.Radius).Clip().Background(t.Surface).Label("Post " + strconv.FormatInt(p.ID, 10))
							b.Children(func() {
								netimage.Image(c, a.sess.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 280}).Fill()
							})
							if b.Clicked() {
								sd.open = false
								a.openPost(nil, p.ID)
							}
							ui.Text(c, "#"+strconv.FormatInt(p.ID, 10)+" · "+strconv.Itoa(int((1-s.Distance)*100))+"% alike").FontSize(12).TextColor(t.TextMuted)
							if a.sess.can("post_merge") {
								if ui.Link(c.Key("merge"), "Merge…", "").FontSize(12).Clicked() {
									sd.open = false
									a.insp.merge = mergeDialog{open: true, remove: p, into: sd.post}
								}
							}
						})
					}
				})
			})
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "Close").Clicked() {
				sd.open = false
			}
		})
	})
}

// mergeDialog merges a post into another.
type mergeDialog struct {
	open           bool
	remove, into   oxibooru.Post
	replaceContent bool
	busy           bool
}

func (a *App) mergeView(c *ui.Context) {
	md := &a.insp.merge
	if !md.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &md.open, func() {
		ui.Text(c, "Merge Posts").FontSize(16).Bold()
		ui.Text(c, "The post on the left goes; its tags, relations, scores, favorites and comments go to the one on the right.").
			TextColor(t.TextMuted).MaxWidth(440)
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			side := func(p *oxibooru.Post, label string) {
				ui.Column(c.Key(label)).Gap(6).AlignItems(ui.Center).Children(func() {
					netimage.Image(c, a.sess.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 400, Fit: ui.Contain}).Size(180, 180).Radius(t.Radius)
					ui.Text(c, label+" #"+strconv.FormatInt(p.ID, 10)).FontSize(12).TextColor(t.TextMuted)
				})
			}
			side(&md.remove, "Goes")
			ui.Icon(c, iconChevronR).FontSize(24).TextColor(t.TextMuted)
			side(&md.into, "Stays")
		})
		ui.Checkbox(c, &md.replaceContent, "Keep the content of the post that goes")
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Swap").Clicked() {
				md.remove, md.into = md.into, md.remove
			}
			ui.Spacer(c)
			if ui.Button(c, "Cancel").Clicked() {
				md.open = false
			}
			if ui.PrimaryButton(c, "Merge").Disabled(md.busy).Clicked() {
				a.mergePosts(md.remove.ID, md.into.ID, md.replaceContent)
			}
		})
	})
}

func (a *App) mergePosts(remove, into int64, replace bool) {
	md := &a.insp.merge
	md.busy = true
	async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Post, error) {
		r, err := c.Post(ctx, remove, "id", "version")
		if err != nil {
			return nil, err
		}
		t, err := c.Post(ctx, into, "id", "version")
		if err != nil {
			return nil, err
		}
		return c.MergePosts(ctx, r, t, replace)
	}, func(p *oxibooru.Post, err error) {
		md.busy = false
		if err != nil {
			a.fail(err)
			return
		}
		md.open = false
		a.postsDeleted(remove)
		a.postChanged(p)
		a.toast("Merged into post " + strconv.FormatInt(p.ID, 10))
	})
}
