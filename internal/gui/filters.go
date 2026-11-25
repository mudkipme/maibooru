package gui

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type filterOption struct{ label, value string }

// postFilter is a named token of the search that the filter bar sets from
// a few choices. Its keys are the token's name and then its aliases.
type postFilter struct {
	name    string
	keys    []string
	options func(env filterEnv) []filterOption
}

// filterEnv is what the choices of a filter depend on.
type filterEnv struct {
	now   time.Time
	szuru bool
}

func fixedOptions(opts ...filterOption) func(filterEnv) []filterOption {
	return func(filterEnv) []filterOption { return opts }
}

func presence(with, without string) func(filterEnv) []filterOption {
	return fixedOptions(filterOption{with, "1.."}, filterOption{without, "0"})
}

var postFilters = []postFilter{
	{"Shape", []string{"ar", "aspect-ratio", "image-ar", "image-aspect-ratio"}, fixedOptions(
		filterOption{"Landscape", "1.05.."},
		filterOption{"Portrait", "..0.95"},
		filterOption{"Square", "0.95..1.05"},
		filterOption{"Panorama", "2.."},
	)},
	{"Resolution", []string{"area", "image-area"}, fixedOptions(
		filterOption{"Under 0.5 MP", "..500000"},
		filterOption{"0.5–2 MP", "500000..2000000"},
		filterOption{"2–8 MP", "2000000..8000000"},
		filterOption{"8 MP or More", "8000000.."},
	)},
	{"File Size", []string{"file-size"}, fileSizeOptions},
	{"Posted", []string{"date", "time", "creation-date", "creation-time"}, dateOptions},
	{"Tags", []string{"tag-count"}, fixedOptions(filterOption{"Untagged", "0"}, filterOption{"Tagged", "1.."})},
	{"Notes", []string{"note-count"}, presence("With Notes", "Without Notes")},
	{"Comments", []string{"comment-count"}, presence("With Comments", "Without Comments")},
}

// fileSizeOptions are ranges of file sizes: Oxibooru takes them only with
// a unit, Szurubooru only in bytes.
func fileSizeOptions(env filterEnv) []filterOption {
	mb := func(n int) string {
		if env.szuru {
			return strconv.Itoa(n * 1000000)
		}
		return strconv.Itoa(n) + "MB"
	}
	return []filterOption{
		{"Under 1 MB", ".." + mb(1)},
		{"1–10 MB", mb(1) + ".." + mb(10)},
		{"10–100 MB", mb(10) + ".." + mb(100)},
		{"100 MB or More", mb(100) + ".."},
	}
}

// dateOptions are periods by name, as the server takes them. Months and
// years are written out, so a saved search keeps the period it was made
// with.
func dateOptions(env filterEnv) []filterOption {
	now := env.now.UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return []filterOption{
		{"Today", "today"},
		{"Yesterday", "yesterday"},
		{"This Month", month.Format("2006-01")},
		{"Last Month", month.AddDate(0, -1, 0).Format("2006-01")},
		{"This Year", strconv.Itoa(now.Year())},
		{"Last Year", strconv.Itoa(now.Year() - 1)},
	}
}

// value returns the filter's token in the query, and whether it is negated.
func (pf postFilter) value(q string) (string, bool) {
	for _, tok := range oxibooru.Tokens(q) {
		t := oxibooru.ParseToken(tok)
		for _, k := range pf.keys {
			if strings.EqualFold(t.Key, k) {
				return t.Value, t.Negated
			}
		}
	}
	return "", false
}

// set returns the query with the filter's token, under any of its names,
// replaced by value, or taken out when value is empty.
func (pf postFilter) set(q, value string) string {
	for _, k := range pf.keys[1:] {
		q = oxibooru.SetToken(q, k, "")
	}
	return oxibooru.SetToken(q, pf.keys[0], value)
}

// label is what the filter's button shows for the query: the filter's name
// when unset, the option chosen, or the token as written.
func (pf postFilter) label(q string, env filterEnv) string {
	v, neg := pf.value(q)
	if v == "" && !neg {
		return pf.name
	}
	if !neg {
		for _, o := range pf.options(env) {
			if o.value == v {
				return o.label
			}
		}
		if t, err := time.Parse("2006-01", v); err == nil && pf.keys[0] == "date" {
			return t.Format("January 2006")
		}
	}
	if neg {
		v = "not " + v
	}
	return pf.name + " " + v
}

// filtersActive reports whether the query has a token of the filter bar.
func filtersActive(q string) bool {
	for _, pf := range postFilters {
		if v, neg := pf.value(q); v != "" || neg {
			return true
		}
	}
	return false
}

// filterBar shows a button for each filter, each choosing its token in the
// query.
func (a *App) filterBar(c *ui.Context, q string) {
	t := c.Theme()
	env := filterEnv{now: time.Now(), szuru: a.sess.info != nil && a.sess.info.Config.Szurubooru()}
	ui.Row(c).Padding(4, 12).Gap(4).AlignItems(ui.Center).Wrap().BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		for _, pf := range postFilters {
			v, neg := pf.value(q)
			b := menuButton(c.Key(pf.name), nil, pf.label(q, env), pf.name, func(m *ui.Menu) {
				if m.Item("Any").Radio(v == "" && !neg).Chosen() {
					a.router.Replace(postsURL(pf.set(q, "")))
				}
				m.Separator()
				for _, o := range pf.options(env) {
					if m.Item(o.label).Radio(!neg && o.value == v).Chosen() {
						a.router.Replace(postsURL(pf.set(q, o.value)))
					}
				}
			})
			on(b, v != "" || neg, c)
		}
		if filtersActive(q) {
			if toolButton(c.Key("clear"), nil, "Clear").Clicked() {
				for _, pf := range postFilters {
					q = pf.set(q, "")
				}
				a.router.Replace(postsURL(q))
			}
		}
	})
}
