package gui

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo/ui"
)

// postSyntax is what a search for posts takes, as the server documents it.
var postSyntax = []syntaxExample{
	{"cat", "tagged cat; a tag's names and its aliases match"},
	{"cat*", "a tag starting with cat"},
	{"-dog", "not tagged dog (any token can be negated)"},
	{"id:12 id:10..20", "posts by number, one or a range"},
	{"tag-category:artist", "having tags of a category"},
	{"type:image,animation", "image, animation, video, flash or document"},
	{"safety:safe", "safe, sketchy or unsafe"},
	{"uploader:ada", "uploaded by ada"},
	{"fav:ada comment:ada", "in ada's favorites, commented by ada"},
	{"pool:12 pool-category:series", "in a pool, by ID or name, or of a pool category"},
	{"score:5..", "scored 5 or more"},
	{"tag-count:0", "without tags; also comment-count, fav-count, note-count, relation-count, feature-count"},
	{"note-text:*hello*", "with notes saying something"},
	{"source:*example.com*", "from a source"},
	{"flag:loop flag:sound", "videos that loop, or have sound"},
	{"content-checksum:…", "of a BLAKE3 checksum"},
	{"file-size:..1MB", "files of 1 MB at most"},
	{"width:1920.. height:..1080 area:1000000.. ar:1.7..1.8", "sizes and aspect ratios"},
	{"date:2024-06 edit-date:today", "posted, edited, commented (comment-date), favorited (fav-date), featured (feature-date)"},
	{"special:fav special:liked special:disliked", "your favorites, likes and dislikes"},
	{"special:tumbleweed", "without scores, comments or favorites"},
	{"sort:score sort:random", "sort: id, score, uploader, pool-count, tag-count, comment-count, fav-count, note-count, relation-count, feature-count, type, flag, source, file-size, width, height, area, aspect-ratio, date, edit-date, comment-date, fav-date, feature-date, safety, random"},
	{"sort:date,asc", "sort the other way (or -sort:date)"},
}

var commentFormat = [][2]string{
	{"*italic*  **bold**  ~~struck~~", "Markdown, as on GitHub"},
	{"@426", "a link to post 426"},
	{"#Dragon_Ball", "a link to the posts of a tag"},
	{"+Pirate", "a link to a user"},
	{"[spoiler]…[/spoiler]", "hidden until clicked"},
	{"[sjis]…[/sjis]", "Shift JIS art, in a fixed font"},
	{"[icon]https://…[/icon]", "a link"},
	{"![alt](https://…)", "a picture"},
}

type helpPage struct {
	tab    int
	syntax int
}

func (a *App) helpView(c *ui.Context) {
	h := &a.help
	t := c.Theme()
	s := a.sess
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(10, 24, 0).Children(func() {
			ui.Tabs(c.Key("tabs"), &h.tab, "About", "Search", "Keyboard", "Comments", "Terms").Label("Help")
		})
		ui.Scroll(c.Key("help")).Grow(1).Children(func() {
			ui.Column(c).Padding(16, 28, 28).Gap(16).MaxWidth(820).Children(func() {
				switch h.tab {
				case 0:
					info := s.info
					rows := [][2]string{
						{"Server", cmp.Or(info.Config.Name, "Oxibooru")},
						{"Address", s.client.SiteURL()},
						{"Posts", formatCount(info.PostCount)},
						{"Disk used", formatBytes(info.DiskUsage)},
					}
					if !info.ServerTime.IsZero() {
						rows = append(rows, [2]string{"Server time", info.ServerTime.Local().Format("Jan 2, 2006 15:04")})
					}
					rows = append(rows, [2]string{"Signed in as", cmp.Or(s.profile.User, "Guest") + " · " + string(s.rank())})
					if r := info.Config.DefaultUserRank; r != "" {
						rows = append(rows, [2]string{"New accounts are", string(r)})
					}
					if info.Config.ContactEmail != "" {
						rows = append(rows, [2]string{"Contact", info.Config.ContactEmail})
					}
					rows = append(rows, [2]string{"Maibooru", cmp.Or(a.opts.Version, "dev")})
					ui.Text(c, cmp.Or(info.Config.Name, "Oxibooru")).FontSize(22).Bold()
					ui.Text(c, "An image board run by Oxibooru, a server compatible with Szurubooru. Maibooru is a desktop app for it.").TextColor(t.TextMuted)
					table(c, rows)
					section(c, "Accounts", func() {
						ui.Text(c, "The email of an account is only used for its Gravatar and for resetting its password; only its owner and the staff see it. You can delete your account at any time; the posts you uploaded stay.")
					})
				case 1:
					ui.Segmented(c.Key("syntax"), &h.syntax, "Posts", "Tags", "Pools", "Users", "Comments").Label("Search syntax")
					examples := [][]syntaxExample{postSyntax, tagSyntax, poolSyntax, userSyntax, commentSyntax}[h.syntax]
					rows := make([][2]string, len(examples))
					for i, ex := range examples {
						rows[i] = [2]string{ex.token, ex.about}
					}
					table(c, rows)
					ui.Text(c, "Tokens join with spaces, and all must match. A minus before a token excludes it. Values take ranges (1..4, 1.., ..4) and lists (a,b,c); dates take today, yesterday, a year, a month or a day (2024-06-30). A backslash escapes : * , and a leading minus; quotes take spaces.").
						TextColor(t.TextMuted)
				case 2:
					table(c, shortcutRows)
				case 3:
					ui.Text(c, "Comments, descriptions and notes are Markdown, with a few tags of the server's:").TextColor(t.TextMuted)
					table(c, commentFormat)
				case 4:
					tos := strings.TrimSuffix(s.client.SiteURL(), "/") + "/help/tos"
					ui.Text(c, "Using "+cmp.Or(info(s), "this server")+", you agree to its terms of service, which its web client shows.")
					if ui.Button(c, "Open the Terms of Service").Clicked() {
						a.openURL(tos)
					}
				}
			})
		})
	})
}

func info(s *session) string {
	if s.info == nil {
		return ""
	}
	return s.info.Config.Name
}

// table shows rows of a key and what it means.
func table(c *ui.Context, rows [][2]string) {
	t := c.Theme()
	ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(20).GapY(8).Children(func() {
		for _, r := range rows {
			ui.Text(c, r[0]).FontFeatures("tnum").Selectable().MaxWidth(320)
			ui.Text(c, r[1]).TextColor(t.TextMuted)
		}
	})
}
