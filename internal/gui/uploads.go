package gui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type uploadState int

const (
	upQueued uploadState = iota
	upSending
	upChecking
	upCreating
	upReview // posts look like it: the user says what to take from them
	upDone
	upDuplicate
	upFailed
)

// upload is a file, a link or a picture to make a post of.
type upload struct {
	id      int
	path    string
	url     string
	temp    bool // path is a file the app made, deleted once done
	name    string
	size    int64
	preview ui.ImageSource

	tags     []string
	tc       completer
	safety   int // -1 for the queue's
	source   string
	state    uploadState
	progress float64
	exact    *oxibooru.Post
	similar  []oxibooru.SimilarPost
	// lookalikes are the similar posts under review, with what the post
	// takes of each; reviewed is set once the user went through them.
	lookalikes []lookalike
	reviewed   bool
	post       *oxibooru.Post
	err        error
}

// lookalike is a post that looks like an upload: the upload copies its
// tags, and is related to it, as the user chooses.
type lookalike struct {
	post     oxibooru.Post
	distance float64
	copyTags bool
	relate   bool
}

// inFlight reports whether an upload is on its way.
func (it *upload) inFlight() bool {
	return it.state == upSending || it.state == upChecking || it.state == upCreating
}

// uploads is the queue of uploads, and what they share.
type uploads struct {
	app     *App
	profile string
	items   []*upload
	next    int

	tags      []string
	tc        completer
	safety    int
	source    string
	pool      int64
	poolName  string
	anonymous bool
	running   bool
	created   []int64
	selected  int
	list      ui.ListState
	tagsTyped string
	poolFind  completer
	poolOpen  bool
	fresh     bool
	paused    bool
	linkOpen  bool
	link      string
}

var mediaExts = []string{".jpg", ".jpeg", ".jpe", ".jfif", ".png", ".gif", ".webp", ".bmp", ".avif", ".jxl",
	".mp4", ".m4v", ".mov", ".webm", ".swf", ".pdf"}

func isMedia(path string) bool {
	return slices.Contains(mediaExts, strings.ToLower(filepath.Ext(path)))
}

func (u *uploads) pendingCount() int {
	n := 0
	for _, it := range u.items {
		if it.state < upDone || it.state == upFailed {
			n++
		}
	}
	return n
}

// defaults takes the tags and the pool of the page files were dropped on:
// for a new batch in place of the last one's, else where the queue has
// none yet.
func (u *uploads) defaults(tags []string, pool int64) {
	if u.running {
		return
	}
	if u.fresh {
		u.fresh = false
		u.tags, u.pool, u.poolName, u.source = nil, 0, "", ""
	}
	if len(u.tags) == 0 {
		u.tags = slices.Clone(tags)
	}
	if pool != 0 && u.pool == 0 {
		u.pool = pool
		u.poolName = "#" + strconv.FormatInt(pool, 10)
		if p := u.app.pools.find(pool); p != nil {
			u.poolName = p.Name()
		}
	}
}

func (u *uploads) add(it *upload) {
	if u.pendingCount() == 0 && len(u.items) > 0 {
		// What was there is done: this starts a new batch.
		u.fresh = true
	}
	u.next++
	it.id, it.safety = u.next, -1
	u.items = append(u.items, it)
}

// addFiles queues files, and the media in folders.
func (u *uploads) addFiles(paths []string) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			var inner []string
			filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && isMedia(p) {
					inner = append(inner, p)
				}
				return nil
			})
			slices.Sort(inner)
			u.addFiles(inner)
			continue
		}
		if slices.ContainsFunc(u.items, func(it *upload) bool { return it.path == path && it.state != upDone }) {
			continue
		}
		it := &upload{path: path, name: filepath.Base(path), size: info.Size()}
		u.add(it)
		u.makePreview(it)
	}
}

// addURLs queues links, for the server to download.
func (u *uploads) addURLs(urls []string) {
	for _, raw := range urls {
		if slices.ContainsFunc(u.items, func(it *upload) bool { return it.url == raw && it.state != upDone }) {
			continue
		}
		name := raw
		if i := strings.LastIndexByte(strings.TrimRight(raw, "/"), '/'); i >= 0 {
			name = raw[i+1:]
		}
		u.add(&upload{url: raw, name: name, size: -1, source: raw})
	}
}

// addImage queues a picture, as a pasted or dropped one.
func (u *uploads) addImage(png []byte) {
	f, err := os.CreateTemp("", "maibooru-paste-*.png")
	if err != nil {
		u.app.fail(err)
		return
	}
	_, err = f.Write(png)
	f.Close()
	if err != nil {
		u.app.fail(err)
		return
	}
	it := &upload{path: f.Name(), temp: true, name: "Pasted picture.png", size: int64(len(png))}
	u.add(it)
	u.makePreview(it)
}

// makePreview decodes a small picture of an image file.
func (u *uploads) makePreview(it *upload) {
	switch strings.ToLower(filepath.Ext(it.path)) {
	case ".jpg", ".jpeg", ".jpe", ".jfif", ".png", ".gif", ".webp", ".bmp":
	default:
		return
	}
	if it.size > 128<<20 {
		return
	}
	path := it.path
	go func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		bm, err := ui.DecodeBitmapScaled(data, 256)
		if err != nil {
			return
		}
		u.app.post(func() { it.preview = bm })
	}()
}

func (u *uploads) remove(id int) {
	u.items = slices.DeleteFunc(u.items, func(it *upload) bool {
		if it.id == id && it.temp {
			os.Remove(it.path)
		}
		return it.id == id
	})
}

func (u *uploads) clearFinished() {
	u.items = slices.DeleteFunc(u.items, func(it *upload) bool {
		done := it.state == upDone || it.state == upDuplicate
		if done && it.temp {
			os.Remove(it.path)
		}
		return done
	})
}

// start uploads the queue, two at a time.
func (u *uploads) start() {
	if u.running {
		return
	}
	for _, it := range u.items {
		switch it.state {
		case upFailed:
			it.state, it.err, it.progress = upQueued, nil, 0
		case upReview:
			it.state, it.reviewed, it.progress = upQueued, true, 0
		}
	}
	u.running, u.paused = true, false
	u.created = nil
	for range 2 {
		u.pump()
	}
}

// pump starts the next upload queued, or finishes the queue.
func (u *uploads) pump() {
	a := u.app
	i := slices.IndexFunc(u.items, func(it *upload) bool { return it.state == upQueued })
	if i < 0 || u.paused {
		if !slices.ContainsFunc(u.items, (*upload).inFlight) {
			u.finish()
		}
		return
	}
	it := u.items[i]
	it.state = upSending
	tags := uniq(append(slices.Clone(u.tags), it.tags...))
	safety := oxibooru.Safeties[max(0, u.safety)]
	if it.safety >= 0 {
		safety = oxibooru.Safeties[it.safety]
	}
	if !a.sess.safetyEnabled() {
		safety = oxibooru.Safe
	}
	source := strings.TrimSpace(cmp.Or(it.source, u.source))
	anonymous := u.anonymous
	keepDuplicates, askSimilar := a.s.KeepDuplicates, !a.s.UploadSimilar && !it.reviewed
	var relations []int64
	for _, l := range it.lookalikes {
		if l.copyTags {
			tags = uniq(append(tags, l.post.TagNames()...))
		}
		if l.relate {
			relations = append(relations, l.post.ID)
		}
	}
	path, rawURL := it.path, it.url
	report := func(st uploadState, progress float64) {
		a.post(func() { it.state, it.progress = st, progress })
	}
	type result struct {
		post    *oxibooru.Post
		exact   *oxibooru.Post
		similar []oxibooru.SimilarPost
	}
	async(a, func(ctx context.Context, c *oxibooru.Client) (result, error) {
		var r result
		f := oxibooru.PostFields{Tags: &tags, Safety: &safety}
		if source != "" {
			f.Source = &source
		}
		if anonymous {
			f.Anonymous = &anonymous
		}
		if len(relations) > 0 {
			f.Relations = &relations
		}
		if path != "" {
			file, err := os.Open(path)
			if err != nil {
				return r, err
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				return r, err
			}
			token, err := c.Upload(ctx, filepath.Base(path), file, info.Size(), func(done, total int64) {
				if total > 0 {
					report(upSending, float64(done)/float64(total))
				}
			})
			if err != nil {
				return r, err
			}
			report(upChecking, 1)
			found, err := c.ReverseSearch(ctx, token)
			if err == nil {
				r.exact, r.similar = found.ExactPost, found.SimilarPosts
				if r.exact != nil && !keepDuplicates {
					return r, nil
				}
				if len(r.similar) > 0 && askSimilar {
					return r, errLookalikes
				}
			}
			f.ContentToken = token
		} else {
			f.ContentURL = rawURL
		}
		report(upCreating, 1)
		post, err := c.CreatePost(ctx, f)
		r.post = post
		return r, err
	}, func(r result, err error) {
		it.exact, it.similar = r.exact, r.similar
		switch {
		case err == errLookalikes:
			it.state = upReview
			it.lookalikes = nil
			for _, s := range r.similar {
				it.lookalikes = append(it.lookalikes, lookalike{post: s.Post, distance: s.Distance, relate: true})
			}
		case err != nil:
			it.state, it.err = upFailed, err
			if !a.s.KeepGoingOnError {
				u.paused = true
			}
		case r.post == nil:
			it.state = upDuplicate
		default:
			it.state, it.post = upDone, r.post
			u.created = append(u.created, r.post.ID)
			a.s.AddRecentTags(a.sess.profile.ID, tags...)
		}
		u.pump()
	})
}

// errLookalikes stops an upload that looks like posts there already, for
// the user to say what to take from them.
var errLookalikes = errors.New("posts look like it")

// finish adds what was made to the pool chosen, and shows it.
func (u *uploads) finish() {
	a := u.app
	u.running = false
	a.save()
	if u.paused {
		a.toast("Uploads paused after an error")
	}
	if len(u.created) == 0 {
		return
	}
	a.postsAdded()
	n := len(u.created)
	if u.pool != 0 {
		created, pool := slices.Clone(u.created), u.pool
		async(a, func(ctx context.Context, c *oxibooru.Client) (*oxibooru.Pool, error) {
			p, err := c.Pool(ctx, pool)
			if err != nil {
				return nil, err
			}
			ids := append(p.PostIDs(), created...)
			return c.UpdatePool(ctx, pool, oxibooru.PoolFields{Version: p.Version, Posts: &ids})
		}, func(p *oxibooru.Pool, err error) {
			if err != nil {
				a.fail(err)
				return
			}
			a.poolChanged(p)
		})
	}
	show := func() { a.router.Push(postsURL("")) }
	a.pending = append(a.pending, ui.Toast{Title: "Uploaded " + pluralize(n, "post", "posts"), Action: "Show", OnAction: show})
	if !a.active && a.opts.Desktop != nil {
		a.opts.Desktop.Notify("Uploads finished", "Uploaded "+pluralize(n, "post", "posts")+" to "+cmpOr(a.sess.profile.Name, "the server"), show)
	}
}

func (a *App) uploadsView(c *ui.Context) {
	u := &a.up
	t := c.Theme()
	a.linkView(c)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, "Uploads").FontSize(16).Bold()
				ui.Text(c, pluralize(len(u.items), "file", "files")).TextColor(t.TextMuted)
				ui.Spacer(c)
				if toolButton(c.Key("add"), iconPlus, "Add Files…").Clicked() {
					a.chooseUploads()
				}
				if toolButton(c.Key("link"), iconLink, "Add Link…").Clicked() {
					u.linkOpen, u.link = true, ""
				}
				if ui.Button(c, "Clear Finished").Disabled(!slices.ContainsFunc(u.items, func(it *upload) bool {
					return it.state == upDone || it.state == upDuplicate
				})).Clicked() {
					u.clearFinished()
				}
			})
			if len(u.items) == 0 {
				ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
					ui.Icon(c, iconUpload).FontSize(44).TextColor(t.TextMuted)
					ui.Text(c, "Drop files or folders here").FontSize(18).Bold()
					ui.Text(c, "or links and pictures from a browser, or paste them with Ctrl+V").TextColor(t.TextMuted)
					if ui.PrimaryButton(c, "Choose Files…").Clicked() {
						a.chooseUploads()
					}
				})
				return
			}
			u.list.Selected = &u.selected
			ui.List(c.Key("queue"), &u.list, len(u.items), func(i int) {
				a.uploadRow(c, u.items[i], i == u.selected)
			}).Grow(1).Label("Files to upload")
		})
		ui.Box(c).Width(1).Background(t.Border)
		ui.Scroll(c.Key("upopts")).Width(340).Shrink(0).Children(func() {
			ui.Column(c).Padding(14).Gap(14).Children(func() { a.uploadOptions(c) })
		})
	})
}

// linkView asks for the address of a file the server downloads.
func (a *App) linkView(c *ui.Context) {
	u := &a.up
	if !u.linkOpen {
		return
	}
	ui.Modal(c, &u.linkOpen, func() {
		ui.Text(c, "Add a Link").FontSize(16).Bold()
		ui.Text(c, "The server downloads what it points to, as a picture, a video, or a page its downloader knows.").
			TextColor(c.Theme().TextMuted).MaxWidth(400)
		in := ui.TextInput(c.Key("url"), &u.link).Placeholder("https://").Label("Address").AutoFocus().Width(400)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				u.linkOpen = false
			}
			ok := strings.HasPrefix(u.link, "http://") || strings.HasPrefix(u.link, "https://")
			if (ui.PrimaryButton(c, "Add").Disabled(!ok).Clicked() || in.Submitted()) && ok {
				u.addURLs(strings.Fields(u.link))
				u.linkOpen = false
			}
		})
	})
}

func (a *App) uploadRow(c *ui.Context, it *upload, chosen bool) {
	t := c.Theme()
	muted, warn, danger, ok := t.TextMuted, t.Warning, t.Danger, t.Success
	if chosen {
		muted = t.AccentText.Alpha(0.75)
		warn, danger, ok = t.AccentText, t.AccentText, t.AccentText
	}
	u := &a.up
	ui.Row(c.Key(it.id)).Padding(8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(56, 56).Radius(t.Radius).Clip().Background(t.Surface).Center().Shrink(0).Children(func() {
			switch {
			case it.preview != nil:
				ui.Image(c, it.preview).Fill().Fit(ui.Cover)
			case it.post != nil:
				netimage.Image(c, a.sess.client.DataURL(it.post.ThumbnailURL), netimage.Options{MaxSide: 160}).Fill()
			case it.url != "":
				ui.Icon(c, iconLink).FontSize(20).TextColor(muted)
			default:
				ic := iconFile
				switch strings.ToLower(filepath.Ext(it.name)) {
				case ".mp4", ".m4v", ".mov", ".webm":
					ic = iconVideo
				}
				ui.Icon(c, ic).FontSize(20).TextColor(muted)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, it.name).SingleLine().TruncateMiddle()
			meta := it.url
			if it.size >= 0 {
				meta = formatBytes(it.size)
			}
			if len(it.tags) > 0 || it.safety >= 0 || it.source != "" && it.url == "" {
				meta += " · own details"
			}
			switch it.state {
			case upSending:
				ui.Progress(c, it.progress).Width(200)
			case upChecking:
				meta = "Looking for duplicates…"
			case upCreating:
				meta = "Making the post…"
			case upFailed:
				meta = it.err.Error()
			case upReview:
				meta = "Posts there look like it. Choose what it takes from them, then upload again."
			}
			color := muted
			if it.state == upFailed {
				color = danger
			}
			if it.state != upSending {
				ui.Text(c, meta).FontSize(12).TextColor(color).MaxLines(2)
			}
			if it.state == upDuplicate && it.exact != nil {
				id := it.exact.ID
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, "Already there:").FontSize(12).TextColor(warn)
					ui.Link(c, "post "+strconv.FormatInt(id, 10), "/post/"+strconv.FormatInt(id, 10)).FontSize(12)
				})
			}
			if it.state == upReview {
				ui.Row(c).Wrap().Gap(10).PaddingY(4).Children(func() {
					for i := range it.lookalikes {
						l := &it.lookalikes[i]
						ui.Row(c.Key(l.post.ID)).Gap(8).Padding(6).Radius(t.Radius).Border(1, t.Border).Background(t.Background).Children(func() {
							b := ui.ButtonBase(c.Key("open")).Size(56, 56).Radius(t.Radius).Clip().Label("Post " + strconv.FormatInt(l.post.ID, 10))
							b.Children(func() {
								netimage.Image(c, a.sess.client.DataURL(l.post.ThumbnailURL), netimage.Options{MaxSide: 160}).Fill()
							})
							if b.Clicked() {
								a.openPost(nil, l.post.ID)
							}
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, fmt.Sprintf("#%d · %d%% alike", l.post.ID, int((1-l.distance)*100))).FontSize(12).TextColor(t.Text)
								ui.Checkbox(c.Key("tags"), &l.copyTags, "Copy tags")
								ui.Checkbox(c.Key("rel"), &l.relate, "Add relation")
							})
						})
					}
				})
			} else if len(it.similar) > 0 && it.state != upDuplicate {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Looks like").FontSize(12).TextColor(muted)
					for i, sp := range it.similar {
						if i >= 4 {
							break
						}
						ui.Link(c.Key(sp.Post.ID), fmt.Sprintf("#%d (%d%%)", sp.Post.ID, int((1-sp.Distance)*100)), "/post/"+strconv.FormatInt(sp.Post.ID, 10)).FontSize(12)
					}
				})
			}
		})
		switch it.state {
		case upDone:
			ui.Icon(c, iconCheck).TextColor(ok)
			if it.post != nil {
				id := it.post.ID
				if ui.Button(c.Key("open"), "Open").Clicked() {
					a.openPost(nil, id)
				}
			}
		case upDuplicate:
			ui.Icon(c, iconAlert).TextColor(warn)
		case upSending, upChecking, upCreating:
			ui.Spinner(c)
		default:
			id := it.id
			if iconButton(c.Key("rm"), iconClose, "Remove "+it.name).Tooltip("Remove").Clicked() {
				u.remove(id)
			}
		}
	})
}

func (a *App) uploadOptions(c *ui.Context) {
	u := &a.up
	t := c.Theme()
	s := a.sess
	pending := 0
	for _, it := range u.items {
		if it.state == upQueued || it.state == upFailed || it.state == upReview {
			pending++
		}
	}
	ui.Text(c, "For every file").FontSize(16).Bold()
	section(c, "Tags", func() {
		a.tagField(c, &u.tags, &u.tc, "Tags of every file")
	})
	if s.safetyEnabled() {
		section(c, "Safety", func() {
			ui.Segmented(c.Key("safety"), &u.safety, "Safe", "Sketchy", "Unsafe").Label("Safety").FillWidth()
		})
	}
	section(c, "Source", func() {
		ui.TextInput(c.Key("source"), &u.source).Placeholder("Where they come from").Label("Source")
	})
	section(c, "Pool", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if u.pool == 0 {
				ui.Text(c, "None").TextColor(t.TextMuted).Grow(1)
			} else {
				ui.Icon(c, iconLayers).TextColor(t.TextMuted)
				ui.Text(c, u.poolName).Grow(1).SingleLine()
				if iconButton(c.Key("nopool"), iconClose, "No pool").Clicked() {
					u.pool, u.poolName = 0, ""
				}
			}
			choose := ui.Button(c.Key("choosepool"), "Choose…")
			if choose.Clicked() {
				u.poolOpen = !u.poolOpen
			}
			u.poolFind.pools = true
			ui.Popover(c, choose, &u.poolOpen, func() {
				in := ui.SearchField(c.Key("findpool"), &u.poolFind.Query).Placeholder("Find a pool").Label("Find a pool").AutoFocus().Width(260)
				u.poolFind.update(a, c, in.Changed())
				for _, p := range u.poolFind.found {
					if ui.Button(c.Key(p.Name()), p.Name()).FillWidth().Clicked() {
						u.poolName = p.Name()
						u.poolOpen = false
						name := p.Name()
						async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Pool, error) {
							page, err := cl.Pools(ctx, oxibooru.Search{Query: "name:" + oxibooru.Escape(name), Limit: 1, Fields: []string{"id", "names"}})
							if err != nil || len(page.Results) == 0 {
								return nil, cmp.Or(err, errNotFound("pool "+name))
							}
							return &page.Results[0], nil
						}, func(p *oxibooru.Pool, err error) {
							if err != nil {
								a.fail(err)
								return
							}
							u.pool = p.ID
						})
					}
				}
			})
		})
	})
	if s.can("post_create_anonymous") {
		ui.Checkbox(c.Key("anon"), &u.anonymous, "Upload anonymously")
	}
	ui.Checkbox(c.Key("dups"), &a.s.KeepDuplicates, "Upload files the server has already")
	ui.Checkbox(c.Key("similar"), &a.s.UploadSimilar, "Upload files like posts there without asking")
	ui.Checkbox(c.Key("errors"), &a.s.KeepGoingOnError, "Keep going after an error")

	label := "Upload"
	if pending > 0 {
		label = "Upload " + pluralize(pending, "File", "Files")
	}
	if ui.PrimaryButton(c.Key("go"), label).Disabled(pending == 0 || u.running || !s.can("post_create")).FillWidth().Clicked() {
		a.save()
		u.start()
	}
	if u.running {
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Uploading…").TextColor(t.TextMuted)
		})
	}

	if u.selected >= 0 && u.selected < len(u.items) {
		it := u.items[u.selected]
		if it.state == upQueued || it.state == upFailed || it.state == upReview {
			ui.Divider(c)
			ui.Text(c, "For "+it.name).FontSize(16).Bold().MaxLines(2)
			section(c, "Its Own Tags", func() {
				a.tagField(c.Key(it.id), &it.tags, &it.tc, "Tags of this file")
			})
			if s.safetyEnabled() {
				section(c, "Its Safety", func() {
					i := it.safety + 1
					if ui.Segmented(c.Key("itsafety"), &i, "Same", "Safe", "Sketchy", "Unsafe").Label("Safety of this file").FillWidth().Changed() {
						it.safety = i - 1
					}
				})
			}
			section(c, "Its Source", func() {
				ui.TextInput(c.Key("itsource"), &it.source).Placeholder("Same as every file").Label("Source of this file")
			})
		}
	}
}
