package gui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// iconButton is a flat button showing an icon, named and explained by
// label.
func iconButton(c *ui.Context, ic *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(30, 30).Shrink(0).Radius(t.Radius).Center().Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.Children(func() { ui.Icon(c, ic).FontSize(16) })
	return b
}

// toolButton is a flat button showing an icon and a text.
func toolButton(c *ui.Context, ic *ui.SVG, text string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(30).Shrink(0).Padding(0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Label(text)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if ic != nil {
			ui.Icon(c, ic).FontSize(15)
		}
		ui.Text(c, text).SingleLine()
	})
	return b
}

// menuButton is a flat button opening a menu, showing an icon, a text and
// an arrow.
func menuButton(c *ui.Context, ic *ui.SVG, text, label string, build func(m *ui.Menu)) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Height(30).Shrink(0).Padding(0, 8, 0, 10).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if ic != nil {
			ui.Icon(c, ic).FontSize(15)
		}
		if text != "" {
			ui.Text(c, text).SingleLine()
		}
		ui.Icon(c, iconChevronD).FontSize(12).TextColor(t.TextMuted)
	})
	b.PopoverMenuButton(build)
	return b
}

// on marks a flat button as switched on.
func on(b ui.Element, active bool, c *ui.Context) ui.Element {
	if active {
		t := c.Theme()
		b.Background(t.Accent.Alpha(0.16)).TextColor(t.Accent)
	}
	return b
}

// syntaxExample is a token of a search, and what it finds.
type syntaxExample struct{ token, about string }

var (
	tagSyntax = []syntaxExample{
		{"cat*", "names starting with cat"},
		{"category:artist", "of a category"},
		{"usages:10..", "used in 10 posts or more"},
		{"implies:animal", "implying a tag"},
		{"suggests:cute", "suggesting a tag"},
		{"implication-count:1..", "with implications"},
		{"creation-date:2024", "made in 2024"},
		{"sort:usages", "sort: name, category, usages, creation-date, edit-date, implication-count, suggestion-count, random"},
	}
	poolSyntax = []syntaxExample{
		{"comic*", "names starting with comic"},
		{"category:series", "of a category"},
		{"post-count:5..", "with 5 posts or more"},
		{"edit-date:today", "edited today"},
		{"sort:post-count", "sort: name, category, post-count, creation-date, edit-date, random"},
	}
	userSyntax = []syntaxExample{
		{"ada*", "names starting with ada"},
		{"creation-date:2024", "joined in 2024"},
		{"last-login-date:yesterday", "seen yesterday"},
		{"sort:last-login-date", "sort: name, creation-date, last-login-date, random"},
	}
	commentSyntax = []syntaxExample{
		{"user:ada", "by a user"},
		{"post:12", "under post 12"},
		{"text:*great*", "saying something"},
		{"score:1..", "scored 1 or more"},
		{"sort:score", "sort: user, post, score, creation-date, edit-date, random"},
	}
)

// syntaxHelp is a button showing the tokens a page's search takes, each
// added to the search by a click.
func syntaxHelp(c *ui.Context, open *bool, examples []syntaxExample, query *string) {
	t := c.Theme()
	b := iconButton(c.Key("syntax"), iconInfo, "Search syntax")
	if b.Clicked() {
		*open = !*open
	}
	ui.Popover(c, b, open, func() {
		ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(14).GapY(6).Width(440).Children(func() {
			for _, ex := range examples {
				if ui.Link(c.Key(ex.token), ex.token, "").FontSize(13).Clicked() {
					q := *query
					if q != "" && !strings.ContainsAny(q, ":*") {
						q = "*" + q + "*"
					}
					*query = strings.TrimSpace(q + " " + ex.token)
					*open = false
				}
				ui.Text(c, ex.about).FontSize(12).TextColor(t.TextMuted)
			}
		})
		ui.Text(c, "A minus before a token excludes it; ranges take a..b, lists a,b,c.").FontSize(12).TextColor(t.TextMuted).Width(440)
	})
}
