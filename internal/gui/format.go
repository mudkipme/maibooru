package gui

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Oxibooru's text is Markdown with a few tags of its own: @426 links to a
// post, #tag to a tag's posts, +name to a user, [spoiler]…[/spoiler] hides
// what it holds until clicked, [sjis]…[/sjis] is art in a fixed font, and
// [icon]…[/icon] a link.
var (
	reMention = regexp.MustCompile(`(^|[\s(\[<>])([@#+])([a-zA-Z0-9_-]+)`)
	reSpoiler = regexp.MustCompile(`(?is)\[spoiler\](.+?)\[/spoiler\]`)
	reSJIS    = regexp.MustCompile(`(?is)\[sjis\](.+?)\[/sjis\]`)
	reIcon    = regexp.MustCompile(`(?is)\[icon\](.+?)\[/icon\]`)
)

// spoilerScheme is the scheme of the links that show a spoiler.
const spoilerScheme = "maibooru-spoiler:"

// booruMarkdown turns Oxibooru's text into the Markdown it means: its
// mentions into links into the app, its spoilers into links revealing them,
// hidden unless shown(i), the i-th spoiler of the text.
func booruMarkdown(text string, shown func(i int) bool) string {
	text = reSJIS.ReplaceAllStringFunc(text, func(m string) string {
		inner := reSJIS.FindStringSubmatch(m)[1]
		return "\n```\n" + strings.Trim(inner, "\n") + "\n```\n"
	})
	text = reIcon.ReplaceAllString(text, "<$1>")
	i := 0
	text = reSpoiler.ReplaceAllStringFunc(text, func(m string) string {
		inner := reSpoiler.FindStringSubmatch(m)[1]
		n := i
		i++
		if shown != nil && shown(n) {
			return inner
		}
		return "[▒▒ spoiler ▒▒](" + spoilerScheme + strconv.Itoa(n) + ")"
	})
	// Code keeps its text: split it out of what is linked.
	parts := strings.Split(text, "```")
	for k := 0; k < len(parts); k += 2 {
		parts[k] = reMention.ReplaceAllStringFunc(parts[k], func(m string) string {
			s := reMention.FindStringSubmatch(m)
			lead, sigil, name := s[1], s[2], s[3]
			var target string
			switch sigil {
			case "@":
				if _, err := strconv.ParseInt(name, 10, 64); err != nil {
					return m
				}
				target = "/post/" + name
			case "#":
				target = postsURL(tagQuery(name))
			case "+":
				target = "/user/" + url.PathEscape(name)
			}
			return fmt.Sprintf("%s[%s%s](%s)", lead, sigil, name, target)
		})
	}
	return strings.Join(parts, "```")
}

// richText shows Oxibooru's text, as comments, descriptions and notes hold
// it, its spoilers revealed as they are clicked.
func (a *App) richText(c *ui.Context, key, text string) ui.Element {
	if a.spoilers == nil {
		a.spoilers = map[string]bool{}
	}
	shown := func(i int) bool { return a.spoilers[key+"#"+strconv.Itoa(i)] }
	return markdown.View(c.Key("md:"+key), booruMarkdown(text, shown), &markdown.Options{
		OnLink: func(dest string) {
			switch {
			case strings.HasPrefix(dest, spoilerScheme):
				a.spoilers[key+"#"+strings.TrimPrefix(dest, spoilerScheme)] = true
			case strings.HasPrefix(dest, "/"):
				a.router.Push(dest)
			default:
				a.openURL(dest)
			}
		},
	})
}
