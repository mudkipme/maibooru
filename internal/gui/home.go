package gui

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// homePage is the overview of the server and the library.
type homePage struct {
	loaded bool
	info   *oxibooru.Info
	recent []oxibooru.Post
	tags   []oxibooru.Tag
	err    error
}

type commentsPage struct {
	search string
	help   bool
	pg     pager[oxibooru.Comment]
	list   ui.ListState
}

func (cp *commentsPage) reload(a *App) { cp.pg.reload(a) }

func (a *App) homeView(c *ui.Context) {
	h := &a.home
	t := c.Theme()
	s := a.sess
	if !h.loaded {
		h.loaded = true
		type overview struct {
			info   *oxibooru.Info
			recent []oxibooru.Post
			tags   []oxibooru.Tag
		}
		async(a, func(ctx context.Context, cl *oxibooru.Client) (overview, error) {
			var o overview
			var err error
			if o.info, err = cl.Info(ctx); err != nil {
				return o, err
			}
			page, err := cl.Posts(ctx, oxibooru.Search{Limit: 24, Fields: []string{"id", "thumbnailUrl", "type", "safety"}})
			if err != nil {
				return o, err
			}
			o.recent = page.Results
			if s.can("tag_list") {
				tags, err := cl.Tags(ctx, oxibooru.Search{Query: "usages:1.. sort:usages", Limit: 40, Fields: []string{"names", "category", "usages"}})
				if err == nil {
					o.tags = tags.Results
				}
			}
			return o, nil
		}, func(o overview, err error) {
			h.err = err
			if err == nil {
				h.info, h.recent, h.tags = o.info, o.recent, o.tags
				s.info.PostCount, s.info.DiskUsage = o.info.PostCount, o.info.DiskUsage
			}
		})
	}
	ui.Scroll(c.Key("home")).Fill().Children(func() {
		ui.Column(c).Padding(24, 28).Gap(24).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				greeting := "Welcome"
				if s.user != nil {
					greeting = "Welcome back, " + s.user.Name
				}
				ui.Text(c, greeting).FontSize(24).Bold()
				ui.Text(c, cmpOr(s.profile.Name, "Oxibooru")+" · "+s.profile.URL).TextColor(t.TextMuted)
			})
			if h.err != nil {
				ui.Text(c, h.err.Error()).TextColor(t.Danger)
			}
			info := h.info
			if info == nil {
				info = s.info
			}
			ui.Row(c).Gap(12).Wrap().Children(func() {
				tile := func(label, value string, to string) {
					b := ui.ButtonBase(c.Key(label)).Column().Gap(2).Padding(14, 18).MinWidth(150).Radius(t.Radius+4).
						Background(t.Surface).Border(1, t.Border)
					if b.Hovered() && to != "" {
						b.Background(t.SurfaceHover)
					}
					b.Children(func() {
						ui.Text(c, value).FontSize(22).Bold().FontFeatures("tnum")
						ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
					})
					if b.Clicked() && to != "" {
						a.router.Push(to)
					}
				}
				tile("Posts", formatCount(info.PostCount), "/posts")
				tile("Disk Used", formatBytes(info.DiskUsage), "")
				if s.user != nil {
					tile("Your Uploads", formatCount(s.user.UploadedPostCount), postsURL("uploader:"+oxibooru.Escape(s.user.Name)))
					tile("Your Favorites", formatCount(s.user.FavoritePostCount), postsURL("special:fav"))
				}
				if n := a.up.pendingCount(); n > 0 {
					tile("Waiting to Upload", formatCount(n), "/uploads")
				}
			})
			if len(h.recent) > 0 {
				section(c, "Latest Posts", func() {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						for _, p := range h.recent {
							b := ui.ButtonBase(c.Key(p.ID)).Size(112, 112).Radius(t.Radius).Clip().Background(t.Surface).Label("Post " + strconv.FormatInt(p.ID, 10))
							b.Children(func() {
								netimage.Image(c, s.client.DataURL(p.ThumbnailURL), netimage.Options{MaxSide: 240}).Fill()
							})
							if b.Clicked() {
								a.openPost(nil, p.ID)
							}
						}
					})
					ui.Link(c, "All posts →", "/posts")
				})
			}
			if info.FeaturedPost != nil {
				fp := info.FeaturedPost
				section(c, "Featured", func() {
					ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
						b := ui.ButtonBase(c.Key("featured")).Size(200, 200).Radius(t.Radius + 2).Clip().Background(t.Surface).Label("Featured post")
						b.Children(func() {
							netimage.Image(c, s.client.DataURL(fp.ThumbnailURL), netimage.Options{MaxSide: 400}).Fill()
						})
						if b.Clicked() {
							a.openPost(nil, fp.ID)
						}
						ui.Column(c).Gap(4).Children(func() {
							ui.Text(c, "Post "+strconv.FormatInt(fp.ID, 10)).Bold()
							by := ""
							if info.FeaturingUser != "" {
								by = " by " + info.FeaturingUser
							}
							ui.Text(c, "Featured "+formatTime(info.FeaturingTime)+by).TextColor(t.TextMuted)
						})
					})
				})
			}
			if len(h.tags) > 0 {
				section(c, "Popular Tags", func() {
					ui.Row(c).Gap(6).Wrap().Children(func() {
						for _, tag := range h.tags {
							color := s.tagColor(tag.Category, t)
							ui.Link(c.Key(tag.Name()), a.tagLabel(tag.Name())+" "+formatCount(tag.Usages), postsURL(tagQuery(tag.Name()))).
								TextColor(color).FontSize(13)
						}
					})
				})
			}
			saved := a.s.Saved[s.profile.ID]
			if len(saved) > 0 {
				section(c, "Saved Searches", func() {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						for i, ss := range saved {
							if ui.Button(c.Key(i), ss.Name).Tooltip(ss.Query).Clicked() {
								a.router.Push(postsURL(ss.Query))
							}
						}
					})
				})
			}
		})
	})
}

func (a *App) commentsView(c *ui.Context) {
	cp := &a.comms
	t := c.Theme()
	if cp.pg.fetch == nil {
		cp.pg.limit = 30
		cp.pg.fetch = func(ctx context.Context, cl *oxibooru.Client, s oxibooru.Search) (*oxibooru.Page[oxibooru.Comment], error) {
			return cl.Comments(ctx, s)
		}
	}
	q := strings.TrimSpace(cp.search)
	if q != "" && !strings.ContainsAny(q, "*:") {
		q = "*" + oxibooru.Escape(q) + "*"
	}
	cp.pg.search(a, strings.TrimSpace(q+" sort:creation-date"))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.SearchField(c.Key("q"), &cp.search).Placeholder("Find comments").Label("Find comments").Width(240)
			syntaxHelp(c, &cp.help, commentSyntax, &cp.search)
			ui.Spacer(c)
			ui.Text(c, pluralize(cp.pg.total, "comment", "comments")).TextColor(t.TextMuted).SingleLine()
		})
		if cp.pg.err != nil && len(cp.pg.items) == 0 {
			ui.Text(c, cp.pg.err.Error()).TextColor(t.Danger).Padding(16)
			return
		}
		ui.List(c.Key("comments"), &cp.list, len(cp.pg.items), func(i int) {
			cp.pg.near(a, i)
			cm := &cp.pg.items[i]
			ui.Row(c.Key(cm.ID)).Padding(12, 16).Gap(14).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				b := ui.ButtonBase(c.Key("post")).Size(72, 72).Radius(t.Radius).Clip().Background(t.Surface).Label("Post " + strconv.FormatInt(cm.PostID, 10))
				b.Children(func() {
					ui.Text(c, "#"+strconv.FormatInt(cm.PostID, 10)).FontSize(12).Margin(ui.Auto)
				})
				if b.Clicked() {
					a.openPost(nil, cm.PostID)
				}
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					a.commentView(c, cm, func(n *oxibooru.Comment) {
						if n == nil {
							cp.pg.reload(a)
							return
						}
						*cm = *n
					})
				})
			})
		}).Grow(1).Label("Comments")
	})
}
