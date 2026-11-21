package oxibooru

import "strings"

// Escape escapes a value of a search query, as a tag's name, so that it
// matches itself: its colons, asterisks, commas, quotes and backslashes,
// and a minus sign it starts with.
func Escape(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '\\', r == ':', r == '*', r == ',', r == '"':
			b.WriteByte('\\')
		case r == '-' && i == 0:
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Unescape undoes Escape.
func Unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if r == '\\' && !escaped {
			escaped = true
			continue
		}
		escaped = false
		b.WriteRune(r)
	}
	return b.String()
}

// Tokens splits a search query into its tokens, keeping escapes and
// quoted strings whole.
func Tokens(q string) []string {
	var tokens []string
	var cur strings.Builder
	escaped, quoted := false, false
	for _, r := range q {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case !quoted && (r == ' ' || r == '\t' || r == '\n'):
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// Token is a token of a search query, split into its parts.
type Token struct {
	Negated bool
	// Key is the name of a named token, as "sort" or "type", empty for an
	// anonymous one.
	Key   string
	Value string
}

// ParseToken splits a token of a query into its parts.
func ParseToken(s string) Token {
	var t Token
	if strings.HasPrefix(s, "-") {
		t.Negated, s = true, s[1:]
	}
	escaped, quoted := false, false
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case r == ':' && !quoted:
			t.Key, t.Value = s[:i], s[i+1:]
			return t
		}
	}
	t.Value = s
	return t
}

// String joins the token's parts.
func (t Token) String() string {
	s := t.Value
	if t.Key != "" {
		s = t.Key + ":" + s
	}
	if t.Negated {
		s = "-" + s
	}
	return s
}

// SetToken returns the query with the named token key set to value: in
// place of the one it has, added when it has none, and taken out when value
// is empty.
func SetToken(q, key, value string) string {
	var out []string
	done := false
	for _, tok := range Tokens(q) {
		t := ParseToken(tok)
		if strings.EqualFold(t.Key, key) {
			if !done && value != "" {
				out = append(out, Token{Key: key, Value: value}.String())
			}
			done = true
			continue
		}
		out = append(out, tok)
	}
	if !done && value != "" {
		out = append(out, Token{Key: key, Value: value}.String())
	}
	return strings.Join(out, " ")
}

// TokenValue returns the value of the named token key of the query, ""
// when it has none.
func TokenValue(q, key string) string {
	for _, tok := range Tokens(q) {
		if t := ParseToken(tok); !t.Negated && strings.EqualFold(t.Key, key) {
			return t.Value
		}
	}
	return ""
}
