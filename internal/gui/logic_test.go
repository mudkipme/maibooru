package gui

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

func TestDraftSendsOnlyChangedFields(t *testing.T) {
	p := &oxibooru.Post{Version: "v", Safety: oxibooru.Safe, Source: "s",
		Tags:  []oxibooru.MicroTag{{Names: []string{"a"}}, {Names: []string{"b"}}},
		Flags: []string{oxibooru.FlagLoop}}
	d := draftOf(p)
	if d.fields(p) != nil {
		t.Fatal("an untouched draft changes the post")
	}
	d.tags = []string{"a", "b", " ", "a"}
	if d.fields(p) != nil {
		t.Error("repeated and blank tags count as a change")
	}
	d.tags = append(d.tags, "c")
	d.sound = true
	f := d.fields(p)
	if f == nil || f.Safety != nil || f.Source != nil || f.Version != "v" {
		t.Fatalf("fields %+v", f)
	}
	if !slices.Equal(*f.Tags, []string{"a", "b", "c"}) || !slices.Equal(*f.Flags, []string{"loop", "sound"}) {
		t.Errorf("tags %v flags %v", *f.Tags, *f.Flags)
	}
}

func TestLastToken(t *testing.T) {
	for _, tc := range []struct {
		q, prefix, term string
		ok              bool
	}{
		{"ca", "", "ca", true},
		{"dog ca", "dog ", "ca", true},
		{"dog -ca", "dog -", "ca", true},
		{"dog tag:ca", "dog tag:", "ca", true},
		{"dog sort:sc", "dog ", "sort:sc", false},
		{"dog ", "dog ", "", false},
	} {
		prefix, term, ok := lastToken(tc.q)
		if prefix != tc.prefix || term != tc.term || ok != tc.ok {
			t.Errorf("lastToken(%q) = %q, %q, %v", tc.q, prefix, term, ok)
		}
	}
}

func TestNotesGeometry(t *testing.T) {
	square := oxibooru.Note{Polygon: []oxibooru.Point{{0.1, 0.1}, {0.1, 0.5}, {0.5, 0.5}, {0.5, 0.1}}}
	if !pointInNote(square, 0.3, 0.3) || pointInNote(square, 0.6, 0.3) {
		t.Error("point in note")
	}
	r := containRect(ui.Rect{W: 400, H: 400}, 200, 100)
	if r != (ui.Rect{X: 100, Y: 150, W: 200, H: 100}) {
		t.Errorf("a small picture is not enlarged: %+v", r)
	}
	r = containRect(ui.Rect{W: 400, H: 400}, 800, 400)
	if r != (ui.Rect{X: 0, Y: 100, W: 400, H: 200}) {
		t.Errorf("a large picture fits: %+v", r)
	}
}

func TestNoteEditorDrawsAndMovesNotes(t *testing.T) {
	ne := &noteEditor{open: true, sel: -1}
	area := ui.Rect{X: 0, Y: 0, W: 100, H: 100}
	ev := func(kind ui.InputKind, x, y float32) { ne.handle(ui.InputEvent{Kind: kind, X: x, Y: y}, area) }
	ev(ui.InputPointerDown, 10, 10)
	ev(ui.InputPointerMove, 30, 40)
	ev(ui.InputPointerUp, 30, 40)
	if len(ne.notes) != 1 || ne.sel != 0 {
		t.Fatalf("notes %v, chosen %d", ne.notes, ne.sel)
	}
	minX, minY, maxX, maxY := bounds(ne.notes[0].Polygon)
	if minX != 0.1 || minY != 0.1 || maxX != 0.3 || maxY != 0.4 {
		t.Errorf("drawn %v", ne.notes[0].Polygon)
	}
	// Dragging it moves it, but not out of the picture.
	ev(ui.InputPointerDown, 20, 20)
	ev(ui.InputPointerMove, 120, 20)
	ev(ui.InputPointerUp, 120, 20)
	minX, _, maxX, _ = bounds(ne.notes[0].Polygon)
	if maxX != 1 || minX < 0.79 || minX > 0.81 || len(ne.notes) != 1 {
		t.Errorf("moved to %v", ne.notes[0].Polygon)
	}
	// A click without a drag adds nothing.
	ev(ui.InputPointerDown, 50, 90)
	ev(ui.InputPointerUp, 50, 90)
	if len(ne.notes) != 1 || ne.sel != -1 {
		t.Errorf("a click made %d notes, chose %d", len(ne.notes), ne.sel)
	}
}

func TestDescribeSnapshot(t *testing.T) {
	s := &oxibooru.Snapshot{Operation: "modified", Data: jsontext.Value(`{"type":"object change","value":{
		"source":{"type":"primitive change","old-value":null,"new-value":"https://x"},
		"tags":{"type":"list change","added":["b"],"removed":["a"]}}}`)}
	got := describeSnapshot(s)
	want := []string{"source: none → https://x", "tags: +b −a"}
	if !slices.Equal(got, want) {
		t.Errorf("modified: %q", got)
	}
	s = &oxibooru.Snapshot{Operation: "created", Data: jsontext.Value(`{"names":["cat"],"category":"default","description":"","implications":[]}`)}
	if got := describeSnapshot(s); !slices.Equal(got, []string{"category: default", "names: [cat]"}) {
		t.Errorf("created: %q", got)
	}
}

func TestFormatting(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1200: "-1,200"} {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%d) = %q", in, got)
		}
	}
	if got := formatBytes(1536); got != "1.5 KB" {
		t.Errorf("formatBytes = %q", got)
	}
	if got := pluralize(1, "post", "posts"); got != "1 post" {
		t.Errorf("pluralize = %q", got)
	}
	if got := hexColor(ui.Hex("#a855f7")); got != "#a855f7" {
		t.Errorf("hexColor = %q", got)
	}
}

func TestMediaHTML(t *testing.T) {
	for _, tc := range []struct {
		p    oxibooru.Post
		want string
	}{
		{oxibooru.Post{Type: oxibooru.TypeFlash}, "RufflePlayer"},
		{oxibooru.Post{Type: oxibooru.TypeDocument, MimeType: "application/pdf"}, "pdfjsLib"},
		{oxibooru.Post{Type: oxibooru.TypeImage, MimeType: "image/avif"}, `<img src="https://b/x.avif?a=1&amp;b=2"`},
	} {
		got := mediaHTML(&tc.p, "https://b/x.avif?a=1&b=2")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s %s: no %q", tc.p.Type, tc.p.MimeType, tc.want)
		}
	}
	if mediaHTML(&oxibooru.Post{Type: oxibooru.TypeVideo}, "x") != "" {
		t.Error("videos play natively")
	}
	evil := `https://b/"</script><script>alert(1)//`
	got := mediaHTML(&oxibooru.Post{Type: oxibooru.TypeFlash}, evil)
	if strings.Contains(got, "</script><script>alert") || strings.Contains(got, `/"<`) {
		t.Errorf("the URL escapes its string: %s", got)
	}
}

func TestBooruMarkdown(t *testing.T) {
	got := booruMarkdown("See @426 and #Dragon_Ball by +Pirate, not mail@x or (@abc).", nil)
	want := "See [@426](/post/426) and [#Dragon_Ball](/posts?q=Dragon_Ball) by [+Pirate](/user/Pirate), not mail@x or (@abc)."
	if got != want {
		t.Errorf("mentions:\n got %q\nwant %q", got, want)
	}
	got = booruMarkdown("a [spoiler]one[/spoiler] b [SPOILER]two[/SPOILER]", func(i int) bool { return i == 1 })
	if got != "a [▒▒ spoiler ▒▒](maibooru-spoiler:0) b two" {
		t.Errorf("spoilers: %q", got)
	}
	got = booruMarkdown("[sjis]@426 #x[/sjis] [icon]https://a.b[/icon]", nil)
	if !strings.Contains(got, "```\n@426 #x\n```") || !strings.Contains(got, "<https://a.b>") {
		t.Errorf("sjis and icon: %q", got)
	}
}
