package gui

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// completer looks tags up on the server as the user types them, once they
// pause.
type completer struct {
	// Query is the text typed, which a field binds.
	Query string
	asked string
	typed time.Time
	found []oxibooru.MicroTag
	// pools completes pools' names in place of tags'.
	pools bool
}

const typingPause = 250 * time.Millisecond

// names returns the names of the tags found.
func (tc *completer) names() []string {
	out := make([]string, 0, len(tc.found))
	for _, t := range tc.found {
		out = append(out, t.Name())
	}
	return out
}

// tag returns the tag found of a name.
func (tc *completer) tag(name string) (oxibooru.MicroTag, bool) {
	for _, t := range tc.found {
		for _, n := range t.Names {
			if strings.EqualFold(n, name) {
				return t, true
			}
		}
	}
	return oxibooru.MicroTag{}, false
}

// update notes typing, and looks up what was typed once the user pauses,
// asking for a frame then.
func (tc *completer) update(a *App, c *ui.Context, changed bool) {
	if changed {
		tc.typed = c.Now()
	}
	term := strings.TrimSpace(tc.Query)
	if term == tc.asked {
		return
	}
	if wait := typingPause - c.Now().Sub(tc.typed); wait > 0 {
		c.After(wait)
		return
	}
	tc.asked = term
	if term == "" {
		tc.found = nil
		return
	}
	asked := term
	if tc.pools {
		async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.MicroTag, error) {
			page, err := cl.Pools(ctx, oxibooru.Search{Query: oxibooru.Escape(asked) + "* sort:post-count", Limit: 15,
				Fields: []string{"id", "names", "category", "postCount"}})
			if err != nil {
				return nil, err
			}
			out := make([]oxibooru.MicroTag, 0, len(page.Results))
			for _, p := range page.Results {
				out = append(out, oxibooru.MicroTag{Names: p.Names, Category: p.Category, Usages: p.PostCount})
			}
			return out, nil
		}, func(found []oxibooru.MicroTag, err error) {
			if err == nil && tc.asked == asked {
				tc.found = found
			}
		})
		return
	}
	async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.MicroTag, error) {
		page, err := cl.Tags(ctx, oxibooru.Search{Query: oxibooru.Escape(asked) + "* sort:usages", Limit: 15,
			Fields: []string{"names", "category", "usages"}})
		if err != nil {
			return nil, err
		}
		out := make([]oxibooru.MicroTag, 0, len(page.Results))
		for _, t := range page.Results {
			out = append(out, t.Micro())
		}
		return out, nil
	}, func(found []oxibooru.MicroTag, err error) {
		if err == nil && tc.asked == asked {
			tc.found = found
		}
	})
}

// tagField is a token field of tag names, suggesting tags of the server and
// the recent ones as the user types.
func (a *App) tagField(c *ui.Context, tags *[]string, tc *completer, label string) ui.Element {
	suggestions := tc.names()
	if strings.TrimSpace(tc.Query) == "" {
		suggestions = a.recentTags()
	}
	f := ui.TokenField(c, tags, suggestions).Label(label).Placeholder("Add a tag…").Query(&tc.Query)
	tc.update(a, c, f.Changed())
	return f
}

// recentTags returns the tags used last on this server.
func (a *App) recentTags() []string {
	if a.sess == nil {
		return nil
	}
	return a.s.Recent[a.sess.profile.ID]
}

// lastToken returns the query's last token being typed, as a tag to
// complete, and what comes before it; ok is false when the last token is
// not a tag.
func lastToken(q string) (prefix, term string, ok bool) {
	if q == "" || strings.HasSuffix(q, " ") {
		return q, "", false
	}
	i := strings.LastIndexByte(q, ' ')
	prefix, term = q[:i+1], q[i+1:]
	if strings.HasPrefix(term, "-") {
		prefix, term = prefix+"-", term[1:]
	}
	tok := oxibooru.ParseToken(term)
	if tok.Key != "" && tok.Key != "tag" {
		return prefix, term, false
	}
	if tok.Key == "tag" {
		prefix, term = prefix+"tag:", tok.Value
	}
	return prefix, term, term != ""
}
