package gui

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// palette goes to a pool, a saved search or a tag by a fuzzy search of
// their names.
type palette struct {
	open   bool
	query  string
	shown  string
	cursor int
	moved  bool
	asked  string
	typed  time.Time
	pools  []oxibooru.Pool
	tags   []oxibooru.MicroTag
}

type paletteItem struct {
	name string
	// alias is the other name of a pool or tag that matched, which hits
	// are then of.
	alias string
	hits  []int
	score int
	count int
	icon  *ui.SVG
	color ui.Color
	url   string
}

type paletteGroup struct {
	title string
	items []paletteItem
}

func (a *App) openPalette() {
	a.pal = palette{open: true}
}

// paletteFind looks the pools and tags up on the server once the user pauses
// typing: those starting with the query, and those having its letters in
// order, which the palette then ranks.
func (a *App) paletteFind(c *ui.Context, changed bool) {
	p := &a.pal
	if changed {
		p.typed = c.Now()
	}
	term := strings.Join(strings.Fields(p.query), "")
	if term == p.asked {
		return
	}
	if wait := typingPause - c.Now().Sub(p.typed); wait > 0 {
		c.After(wait)
		return
	}
	p.asked = term
	if term == "" {
		p.pools, p.tags = nil, nil
		return
	}
	var fuzzy strings.Builder
	fuzzy.WriteByte('*')
	for _, r := range term {
		fuzzy.WriteString(oxibooru.Escape(string(r)))
		fuzzy.WriteByte('*')
	}
	queries := []string{oxibooru.Escape(term) + "*", fuzzy.String()}
	async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.MicroTag, error) {
		var out []oxibooru.MicroTag
		for i, q := range queries {
			page, err := cl.Tags(ctx, oxibooru.Search{Query: q + " sort:usages", Limit: 10 + 15*i, Fields: []string{"names", "category", "usages"}})
			if err != nil {
				return nil, err
			}
			for _, t := range page.Results {
				if !slices.ContainsFunc(out, func(o oxibooru.MicroTag) bool { return o.Name() == t.Name() }) {
					out = append(out, t.Micro())
				}
			}
		}
		return out, nil
	}, func(tags []oxibooru.MicroTag, err error) {
		if err == nil && p.asked == term {
			p.tags = tags
		}
	})
	async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.Pool, error) {
		var out []oxibooru.Pool
		for i, q := range queries {
			page, err := cl.Pools(ctx, oxibooru.Search{Query: q + " sort:post-count", Limit: 10 + 15*i, Fields: []string{"id", "names", "category", "postCount"}})
			if err != nil {
				return nil, err
			}
			for _, pl := range page.Results {
				if !slices.ContainsFunc(out, func(o oxibooru.Pool) bool { return o.ID == pl.ID }) {
					out = append(out, pl)
				}
			}
		}
		return out, nil
	}, func(pools []oxibooru.Pool, err error) {
		if err == nil && p.asked == term {
			p.pools = pools
		}
	})
}

// paletteGroups returns what the palette finds for its query, best first:
// with none, the saved searches and the tags used last.
func (a *App) paletteGroups(t *ui.Theme) []paletteGroup {
	p := &a.pal
	s := a.sess
	q := strings.Join(strings.Fields(p.query), "")
	var saved, pools, tags []paletteItem
	for _, ss := range a.s.Saved[s.profile.ID] {
		it := paletteItem{name: ss.Name, count: -1, icon: searchIcon(ss.Icon), color: t.Accent, url: postsURL(ss.Query)}
		if col, ok := searchColor(ss.Color); ok {
			it.color = col
		}
		if n, ok := a.postCount(ss.Query); ok {
			it.count = n
		}
		if it.score, it.hits = fuzzyMatch(ss.Name, q); it.score > 0 {
			saved = append(saved, it)
		}
	}
	match := func(it *paletteItem, names []string) bool {
		for i, n := range names {
			score, hits := fuzzyMatch(n, q)
			if score > it.score {
				it.score, it.hits, it.alias = score, hits, ""
				if i > 0 {
					it.alias = n
				}
			}
		}
		return it.score > 0
	}
	for _, tg := range p.tags {
		it := paletteItem{name: tg.Name(), count: tg.Usages, icon: iconTag, color: s.tagColor(tg.Category, t), url: postsURL(tagQuery(tg.Name()))}
		if match(&it, tg.Names) {
			tags = append(tags, it)
		}
	}
	for _, name := range a.recentTags() {
		it := paletteItem{name: name, count: -1, icon: iconTag, color: s.tagColor(a.knownTags[name], t), url: postsURL(tagQuery(name))}
		if !slices.ContainsFunc(tags, func(o paletteItem) bool { return o.name == name }) && match(&it, []string{name}) {
			tags = append(tags, it)
		}
	}
	for _, pl := range p.pools {
		it := paletteItem{name: pl.Name(), count: pl.PostCount, icon: iconLayers, color: a.poolColor(pl.Category, t), url: "/pool/" + strconv.FormatInt(pl.ID, 10)}
		if match(&it, pl.Names) {
			pools = append(pools, it)
		}
	}
	best := func(items []paletteItem, n int) []paletteItem {
		slices.SortStableFunc(items, func(x, y paletteItem) int {
			return cmp.Or(y.score-x.score, y.count-x.count, strings.Compare(x.name, y.name))
		})
		return items[:min(n, len(items))]
	}
	var groups []paletteGroup
	for _, g := range []paletteGroup{{"Saved Searches", best(saved, 5)}, {"Pools", best(pools, 5)}, {"Tags", best(tags, 8)}} {
		if len(g.items) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

func (a *App) paletteView(c *ui.Context) {
	p := &a.pal
	if !p.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &p.open, func() {
		ui.Column(c).Gap(8).Width(520).Children(func() {
			in := ui.SearchField(c.Key("find"), &p.query).Placeholder("Go to a pool, saved search or tag").Label("Go to").AutoFocus()
			a.paletteFind(c, in.Changed())
			if p.query != p.shown {
				p.shown, p.cursor = p.query, 0
			}
			groups := a.paletteGroups(t)
			var items []paletteItem
			for _, g := range groups {
				items = append(items, g.items...)
			}
			switch {
			case c.Shortcut(0, ui.KeyDown):
				p.cursor, p.moved = p.cursor+1, true
			case c.Shortcut(0, ui.KeyUp):
				p.cursor, p.moved = p.cursor-1, true
			}
			p.cursor = max(0, min(p.cursor, len(items)-1))
			if in.Submitted() && len(items) > 0 {
				a.paletteGo(items[p.cursor])
				return
			}
			if len(items) == 0 {
				if q := strings.TrimSpace(p.query); q != "" && p.asked == strings.Join(strings.Fields(q), "") {
					ui.Text(c, "Nothing has this name").FontSize(12).TextColor(t.TextMuted).Padding(8)
				}
				return
			}
			i := 0
			ui.Scroll(c).MaxHeight(440).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for _, g := range groups {
						title := g.title
						if title == "Tags" && strings.TrimSpace(p.query) == "" {
							title = "Recent Tags"
						}
						ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Padding(6, 10, 2)
						for _, it := range g.items {
							a.paletteRow(c.Key(i), it, i == p.cursor)
							i++
						}
					}
				})
			})
		})
	})
}

func (a *App) paletteRow(c *ui.Context, it paletteItem, current bool) {
	t := c.Theme()
	p := &a.pal
	row := ui.ButtonBase(c).Row().Padding(6, 10).Gap(10).Radius(t.Radius).AlignItems(ui.Center).Label(it.name)
	switch {
	case current:
		row.Background(t.Accent.Alpha(0.16))
		if p.moved {
			row.ScrollIntoView()
			p.moved = false
		}
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Icon(c, it.icon).TextColor(it.color)
		if it.alias == "" {
			ui.RichText(c, highlight(it.name, it.hits)...).SingleLine().Shrink(1).MinWidth(0)
		} else {
			ui.Text(c, it.name).SingleLine().Shrink(1).MinWidth(0)
			ui.RichText(c, highlight(it.alias, it.hits)...).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		if it.count >= 0 {
			ui.Text(c, formatCount(it.count)).FontSize(12).FontFeatures("tnum").TextColor(t.TextMuted).SingleLine()
		}
	})
	if row.Clicked() {
		a.paletteGo(it)
	}
}

func (a *App) paletteGo(it paletteItem) {
	a.pal.open = false
	a.router.Push(it.url)
}

// highlight makes spans of a name, bold at the runes hit.
func highlight(name string, hits []int) []ui.Span {
	var spans []ui.Span
	i := 0
	for _, r := range name {
		bold := 400
		if slices.Contains(hits, i) {
			bold = 700
		}
		if n := len(spans); n > 0 && spans[n-1].Weight == bold {
			spans[n-1].Text += string(r)
		} else {
			spans = append(spans, ui.Span{Text: string(r), Weight: bold})
		}
		i++
	}
	return spans
}

// fuzzyMatch scores how well q matches name, 0 for not at all, and returns
// the runes of name it hit. Its letters must come in order; whole names,
// their starts, their words' starts and runs of letters score higher. An
// empty q matches every name a little.
func fuzzyMatch(name, q string) (int, []int) {
	if q == "" {
		return 1, nil
	}
	n := []rune(strings.ToLower(name))
	qr := []rune(strings.ToLower(q))
	if i := runeIndex(n, qr); i >= 0 {
		hits := make([]int, len(qr))
		for k := range hits {
			hits[k] = i + k
		}
		switch {
		case len(n) == len(qr):
			return 1000, hits
		case i == 0:
			return 900 - len(n), hits
		case wordStart(n, i):
			return 800 - len(n), hits
		}
		return 700 - i - len(n), hits
	}
	var best []int
	score := 0
	for _, words := range []bool{false, true} {
		if hits := subsequence(n, qr, words); hits != nil {
			if sc := subsequenceScore(n, hits); sc > score {
				best, score = hits, sc
			}
		}
	}
	return score, best
}

// subsequence returns where the runes of q come in order in n, nil when
// they don't: the first of each, or the first starting a word when the
// rest still follow it.
func subsequence(n, q []rune, words bool) []int {
	hits := make([]int, 0, len(q))
	at := 0
	for k, r := range q {
		j := slices.Index(n[at:], r)
		if j < 0 {
			return nil
		}
		j += at
		if words && !wordStart(n, j) {
			for w := j + 1; w < len(n); w++ {
				if n[w] == r && wordStart(n, w) && subsequence(n[w+1:], q[k+1:], false) != nil {
					j = w
					break
				}
			}
		}
		hits = append(hits, j)
		at = j + 1
	}
	return hits
}

func subsequenceScore(n []rune, hits []int) int {
	score, prev := 100, -2
	for _, i := range hits {
		switch {
		case i == prev+1:
			score += 8
		case wordStart(n, i):
			score += 6
		default:
			score -= min(i-prev, 6)
		}
		prev = i
	}
	return max(score-len(n)/4, 2)
}

func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func wordStart(s []rune, i int) bool {
	return i == 0 || !unicode.IsLetter(s[i-1]) && !unicode.IsDigit(s[i-1])
}
