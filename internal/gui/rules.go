package gui

import (
	"strconv"
	"strings"

	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type ruleKind int

const (
	kindTags ruleKind = iota
	kindChoice
	kindNames
	kindText
	kindNumber
	kindDate
	kindSyntax
)

type ruleField struct {
	label string
	// keys are the token's name and then its aliases.
	keys  []string
	kind  ruleKind
	group string
	// choices are what a choice takes, as the server names them.
	choices func(env filterEnv) []filterOption
	// single is a choice that takes one value.
	single bool
	// unit is shown after a number.
	unit     string
	oxibooru bool
}

func (rf *ruleField) key() string { return rf.keys[0] }

var ruleFields = []*ruleField{
	{label: "Tag", keys: []string{"tag"}, kind: kindTags},
	{label: "Type", keys: []string{"type"}, kind: kindChoice, choices: postTypes},
	{label: "Safety", keys: []string{"safety", "rating"}, kind: kindChoice, choices: fixedOptions(
		filterOption{"Safe", "safe"}, filterOption{"Sketchy", "sketchy"}, filterOption{"Unsafe", "unsafe"})},
	{label: "Special", keys: []string{"special"}, kind: kindChoice, single: true, choices: specials},
	{label: "Flag", keys: []string{"flag"}, kind: kindChoice, choices: fixedOptions(
		filterOption{"Loop", "loop"}, filterOption{"Sound", "sound"})},

	{label: "Uploader", keys: []string{"uploader", "upload", "submit"}, kind: kindNames, group: "people"},
	{label: "Favorited By", keys: []string{"fav"}, kind: kindNames, group: "people"},
	{label: "Commented By", keys: []string{"comment"}, kind: kindNames, group: "people"},
	{label: "Pool", keys: []string{"pool"}, kind: kindNames, group: "people"},

	{label: "Source", keys: []string{"source"}, kind: kindText, group: "text"},
	{label: "Description", keys: []string{"description"}, kind: kindText, group: "text", oxibooru: true},
	{label: "Note Text", keys: []string{"note-text"}, kind: kindText, group: "text"},

	{label: "Width", keys: []string{"width", "image-width"}, kind: kindNumber, group: "Size", unit: "px"},
	{label: "Height", keys: []string{"height", "image-height"}, kind: kindNumber, group: "Size", unit: "px"},
	{label: "Pixels", keys: []string{"area", "image-area"}, kind: kindNumber, group: "Size"},
	{label: "Aspect Ratio", keys: []string{"ar", "aspect-ratio", "image-ar", "image-aspect-ratio"}, kind: kindNumber, group: "Size"},
	{label: "File Size", keys: []string{"file-size"}, kind: kindNumber, group: "Size", unit: "MB"},

	{label: "Score", keys: []string{"score"}, kind: kindNumber, group: "Count"},
	{label: "Tag Count", keys: []string{"tag-count"}, kind: kindNumber, group: "Count"},
	{label: "Favorite Count", keys: []string{"fav-count"}, kind: kindNumber, group: "Count"},
	{label: "Comment Count", keys: []string{"comment-count"}, kind: kindNumber, group: "Count"},
	{label: "Note Count", keys: []string{"note-count"}, kind: kindNumber, group: "Count"},
	{label: "Pool Count", keys: []string{"pool-count"}, kind: kindNumber, group: "Count", oxibooru: true},
	{label: "Relation Count", keys: []string{"relation-count"}, kind: kindNumber, group: "Count"},
	{label: "Feature Count", keys: []string{"feature-count"}, kind: kindNumber, group: "Count"},
	{label: "Post ID", keys: []string{"id"}, kind: kindNumber, group: "Count"},

	{label: "Posted", keys: []string{"date", "time", "creation-date", "creation-time"}, kind: kindDate, group: "Date"},
	{label: "Edited", keys: []string{"edit-date", "edit-time", "last-edit-date", "last-edit-time"}, kind: kindDate, group: "Date"},
	{label: "Favorited", keys: []string{"fav-date", "fav-time"}, kind: kindDate, group: "Date"},
	{label: "Commented", keys: []string{"comment-date", "comment-time"}, kind: kindDate, group: "Date"},
	{label: "Featured", keys: []string{"feature-date", "feature-time"}, kind: kindDate, group: "Date"},

	{label: "Search Syntax", keys: []string{""}, kind: kindSyntax, group: "syntax"},
}

func postTypes(env filterEnv) []filterOption {
	opts := []filterOption{{"Image", "image"}, {"Animation", "animation"}, {"Video", "video"}, {"Flash", "flash"}}
	if !env.szuru {
		opts = append(opts, filterOption{"Document", "document"})
	}
	return opts
}

func specials(filterEnv) []filterOption {
	return []filterOption{{"In My Favorites", "fav"}, {"Liked", "liked"}, {"Disliked", "disliked"}, {"Tumbleweed", "tumbleweed"}}
}

// fieldOf returns the field of a token's name, nil for none.
func fieldOf(key string) *ruleField {
	for _, rf := range ruleFields {
		for _, k := range rf.keys {
			if strings.EqualFold(k, key) {
				return rf
			}
		}
	}
	return nil
}

var syntaxField = ruleFields[len(ruleFields)-1]

// available reports whether the server and the user can search the field.
func (rf *ruleField) available(env filterEnv) bool {
	switch {
	case rf.oxibooru && env.szuru, rf.key() == "safety" && !env.safety, rf.key() == "special" && !env.signedIn:
		return false
	}
	return true
}

// ruleOps are the ways a field's rule matches, by kind.
var ruleOps = map[ruleKind][]filterOption{
	kindTags:   {{"Has All Of", "all"}, {"Has Any Of", "any"}, {"Has None Of", "none"}},
	kindChoice: {{"Is", "is"}, {"Is Not", "not"}},
	kindNames:  {{"Is", "is"}, {"Is Not", "not"}},
	kindText:   {{"Contains", "contains"}, {"Doesn't Contain", "lacks"}, {"Is", "is"}, {"Is Not", "not"}},
	kindNumber: {{"Is", "is"}, {"Is Not", "not"}, {"At Least", "min"}, {"At Most", "max"}, {"Between", "between"}},
	kindDate:   {{"Is", "is"}, {"Since", "min"}, {"Until", "max"}, {"Between", "between"}},
}

// rule is a condition of a saved search, making tokens of its query. Posts
// match all the rules: the search syntax joins its tokens with AND alone.
type rule struct {
	field *ruleField
	op    string
	// values are the tags, choices or names.
	values []string
	// a and b are the text, the number or date, and the end of a range.
	a, b string
	tags completer
}

func newRule(rf *ruleField) *rule {
	r := &rule{field: rf}
	if ops := ruleOps[rf.kind]; len(ops) > 0 {
		r.op = ops[0].value
	}
	return r
}

// tokens returns the tokens of the query the rule makes, none while it is
// incomplete.
func (r *rule) tokens(env filterEnv) []string {
	rf := r.field
	key := rf.key()
	var vals []string
	for _, v := range r.values {
		if v = strings.TrimSpace(v); v != "" {
			vals = append(vals, v)
		}
	}
	a, b := strings.TrimSpace(r.a), strings.TrimSpace(r.b)
	named := func(neg bool, v string) []string {
		return []string{oxibooru.Token{Negated: neg, Key: key, Value: v}.String()}
	}
	switch rf.kind {
	case kindSyntax:
		return oxibooru.Tokens(a)
	case kindTags:
		if len(vals) == 0 {
			return nil
		}
		for i, v := range vals {
			vals[i] = escapeKeepingWildcards(v)
		}
		switch r.op {
		case "any":
			if len(vals) > 1 {
				return named(false, strings.Join(vals, ","))
			}
			return vals
		case "none":
			for i, v := range vals {
				vals[i] = "-" + v
			}
		}
		return vals
	case kindChoice, kindNames:
		if len(vals) == 0 {
			return nil
		}
		if rf.kind == kindNames {
			for i, v := range vals {
				vals[i] = escapeKeepingWildcards(v)
			}
		}
		return named(r.op == "not", strings.Join(vals, ","))
	case kindText:
		if a == "" {
			return nil
		}
		v := escapeText(a)
		if r.op == "contains" || r.op == "lacks" {
			v = "*" + v + "*"
		}
		return named(r.op == "lacks" || r.op == "not", v)
	case kindNumber, kindDate:
		conv := func(s string) (string, bool) {
			if s == "" || rf.kind == kindDate {
				return s, !strings.Contains(s, "..") && !strings.Contains(s, ",")
			}
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return "", false
			}
			if key == "file-size" {
				if env.szuru {
					return strconv.FormatInt(int64(n*1e6+0.5), 10), true
				}
				return s + "MB", true
			}
			return s, true
		}
		a, okA := conv(a)
		b, okB := conv(b)
		if !okA || !okB || a == "" {
			return nil
		}
		switch r.op {
		case "not":
			return named(true, a)
		case "min":
			return named(false, a+"..")
		case "max":
			return named(false, ".."+a)
		case "between":
			if b == "" {
				return named(false, a+"..")
			}
			return named(false, a+".."+b)
		}
		return named(false, a)
	}
	return nil
}

// buildQuery returns the query of rules and a sort.
func buildQuery(rules []*rule, sort string, env filterEnv) string {
	var toks []string
	for _, r := range rules {
		toks = append(toks, r.tokens(env)...)
	}
	if sort != "" {
		toks = append(toks, "sort:"+sort)
	}
	return strings.Join(toks, " ")
}

// parseRules returns the rules of a query, and its sort. The tokens no rule
// shows whole stay as search syntax, so the rules make the same query.
func parseRules(q string, env filterEnv) (rules []*rule, sort string) {
	var all, none *rule
	syntax := func(tok string) {
		r := newRule(syntaxField)
		r.a = tok
		rules = append(rules, r)
	}
	for _, tok := range oxibooru.Tokens(q) {
		t := oxibooru.ParseToken(tok)
		if t.Key == "" {
			if !plainValue(t.Value) {
				syntax(tok)
				continue
			}
			target := &all
			op := "all"
			if t.Negated {
				target, op = &none, "none"
			}
			if *target == nil {
				*target = newRule(ruleFields[0])
				(*target).op = op
				rules = append(rules, *target)
			}
			(*target).values = append((*target).values, oxibooru.Unescape(t.Value))
			continue
		}
		if strings.EqualFold(t.Key, "sort") && !t.Negated && sort == "" {
			sort = t.Value
			continue
		}
		rf := fieldOf(t.Key)
		if rf == nil {
			syntax(tok)
			continue
		}
		r, ok := parseRule(rf, t, env)
		if !ok {
			syntax(tok)
			continue
		}
		rules = append(rules, r)
	}
	return rules, sort
}

func parseRule(rf *ruleField, t oxibooru.Token, env filterEnv) (*rule, bool) {
	r := newRule(rf)
	v := t.Value
	if v == "" || strings.ContainsRune(v, '"') {
		return nil, false
	}
	switch rf.kind {
	case kindTags, kindNames:
		r.values = splitUnescaped(v, ',')
		for i, s := range r.values {
			if !plainValue(s) {
				return nil, false
			}
			r.values[i] = oxibooru.Unescape(s)
		}
		switch {
		case rf.kind == kindNames && t.Negated:
			r.op = "not"
		case rf.kind == kindTags && t.Negated:
			r.op = "none"
		case rf.kind == kindTags:
			r.op = "any"
		}
	case kindChoice:
		opts := rf.choices(env)
		for _, s := range splitUnescaped(v, ',') {
			i := indexOption(opts, s)
			if i < 0 {
				return nil, false
			}
			r.values = append(r.values, opts[i].value)
		}
		if rf.single && (len(r.values) != 1 || t.Negated) {
			return nil, false
		}
		if t.Negated {
			r.op = "not"
		}
	case kindText:
		if len(splitUnescaped(v, ',')) > 1 {
			return nil, false
		}
		stars := splitUnescaped(v, '*')
		switch {
		case len(stars) == 1:
			r.op = "is"
			r.a = oxibooru.Unescape(v)
		case len(stars) == 3 && stars[0] == "" && stars[2] == "" && stars[1] != "":
			r.op = "contains"
			r.a = oxibooru.Unescape(stars[1])
		default:
			return nil, false
		}
		if t.Negated {
			r.op = map[string]string{"is": "not", "contains": "lacks"}[r.op]
		}
	case kindNumber, kindDate:
		if strings.Contains(v, ",") {
			return nil, false
		}
		conv := func(s string) (string, bool) {
			if s == "" || rf.kind == kindDate {
				return s, true
			}
			if rf.key() == "file-size" {
				return megabytes(s, env)
			}
			_, err := strconv.ParseFloat(s, 64)
			return s, err == nil
		}
		lo, hi, ranged := strings.Cut(v, "..")
		var okA, okB bool
		switch {
		case !ranged:
			r.a, okA = conv(v)
			okB = true
			if t.Negated {
				r.op = "not"
			}
		case t.Negated:
		case lo == "":
			r.op = "max"
			r.a, okA = conv(hi)
			okB = true
		case hi == "":
			r.op = "min"
			r.a, okA = conv(lo)
			okB = true
		default:
			r.op = "between"
			r.a, okA = conv(lo)
			r.b, okB = conv(hi)
		}
		if !okA || !okB || r.a == "" || (rf.kind == kindDate && r.op == "not") {
			return nil, false
		}
	}
	return r, true
}

// megabytes returns a file size of the query in MB, as the rule shows it.
func megabytes(s string, env filterEnv) (string, bool) {
	if env.szuru {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n%1000 != 0 {
			return "", false
		}
		return strconv.FormatFloat(float64(n)/1e6, 'f', -1, 64), true
	}
	num, ok := strings.CutSuffix(s, "MB")
	if !ok {
		return "", false
	}
	_, err := strconv.ParseFloat(num, 64)
	return num, err == nil
}

func indexOption(opts []filterOption, v string) int {
	for i, o := range opts {
		if strings.EqualFold(o.value, v) {
			return i
		}
	}
	return -1
}

// plainValue reports whether a value reads the same once unescaped and
// escaped again: it escapes no wildcard, and has no list or range.
func plainValue(v string) bool {
	return !strings.Contains(v, `\*`) && !strings.Contains(v, `\ `) && len(splitUnescaped(v, ',')) == 1 &&
		!strings.Contains(v, "..") && !strings.ContainsRune(v, '"')
}

// splitUnescaped splits s around the sep not escaped with a backslash.
func splitUnescaped(s string, sep rune) []string {
	var out []string
	start, escaped := 0, false
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == sep:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// escapeKeepingWildcards escapes a tag or a name, keeping its asterisks as
// wildcards.
func escapeKeepingWildcards(s string) string {
	return strings.ReplaceAll(oxibooru.Escape(s), `\*`, "*")
}

// escapeText escapes a text to find, with its spaces.
func escapeText(s string) string {
	return strings.ReplaceAll(oxibooru.Escape(s), " ", `\ `)
}
