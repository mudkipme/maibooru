package gui

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type historyPage struct {
	kind string
	op   string
	user string
	pg   pager[oxibooru.Snapshot]
	list ui.ListState
}

func (hp *historyPage) reload(a *App) { hp.pg.reload(a) }

var snapshotKinds = []struct{ label, value string }{
	{"Everything", ""}, {"Posts", "post"}, {"Tags", "tag"}, {"Tag Categories", "tag_category"},
	{"Pools", "pool"}, {"Pool Categories", "pool_category"},
}

var snapshotOps = []string{"Any Change", "created", "modified", "deleted", "merged"}

func (a *App) historyView(c *ui.Context) {
	hp := &a.history
	t := c.Theme()
	if hp.pg.fetch == nil {
		hp.pg.fetch = func(ctx context.Context, cl *oxibooru.Client, s oxibooru.Search) (*oxibooru.Page[oxibooru.Snapshot], error) {
			return cl.Snapshots(ctx, s)
		}
	}
	var q []string
	if hp.kind != "" {
		q = append(q, "type:"+hp.kind)
	}
	if hp.op != "" && hp.op != snapshotOps[0] {
		q = append(q, "operation:"+hp.op)
	}
	if u := strings.TrimSpace(hp.user); u != "" {
		q = append(q, "user:"+oxibooru.Escape(u))
	}
	hp.pg.search(a, strings.Join(q, " "))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			labels := make([]string, len(snapshotKinds))
			kind := "Everything"
			for i, k := range snapshotKinds {
				labels[i] = k.label
				if k.value == hp.kind {
					kind = k.label
				}
			}
			if ui.Select(c.Key("kind"), &kind, labels).Label("Kind").Changed() {
				hp.kind = snapshotKinds[slices.Index(labels, kind)].value
			}
			op := cmpOr(hp.op, snapshotOps[0])
			if ui.Select(c.Key("op"), &op, snapshotOps).Label("Change").Changed() {
				hp.op = op
			}
			ui.SearchField(c.Key("user"), &hp.user).Placeholder("By user").Label("By user").Width(180)
			ui.Spacer(c)
			ui.Text(c, pluralize(hp.pg.total, "change", "changes")).TextColor(t.TextMuted).SingleLine()
		})
		if hp.pg.err != nil && len(hp.pg.items) == 0 {
			ui.Text(c, hp.pg.err.Error()).TextColor(t.Danger).Padding(16)
			return
		}
		ui.List(c.Key("snapshots"), &hp.list, len(hp.pg.items), func(i int) {
			hp.pg.near(a, i)
			a.snapshotRow(c, &hp.pg.items[i])
		}).Grow(1).Label("Changes")
	})
}

func (a *App) snapshotRow(c *ui.Context, s *oxibooru.Snapshot) {
	t := c.Theme()
	ui.Column(c).Padding(10, 16).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
			who := "Someone"
			if s.User != nil {
				who = s.User.Name
			}
			ui.Text(c, who).Bold()
			ui.Text(c, s.Operation).TextColor(opColor(s.Operation, t))
			kind := strings.ReplaceAll(s.Type, "_", " ")
			id := s.ResourceID()
			ui.Text(c, kind).TextColor(t.TextMuted)
			switch s.Type {
			case "post":
				ui.Link(c, "#"+id, "/post/"+id)
			case "pool":
				ui.Link(c, "#"+id, "/pool/"+id)
			case "tag":
				ui.Link(c, id, postsURL(tagQuery(id)))
			default:
				ui.Text(c, id)
			}
			ui.Spacer(c)
			ui.Text(c, formatTime(s.Time)).FontSize(12).TextColor(t.TextMuted)
		})
		for _, line := range describeSnapshot(s) {
			ui.Text(c, line).FontSize(12).TextColor(t.TextMuted).MaxLines(3)
		}
	})
}

func opColor(op string, t *ui.Theme) ui.Color {
	switch op {
	case "created":
		return t.Success
	case "deleted":
		return t.Danger
	case "merged":
		return t.Warning
	}
	return t.Accent
}

// describeSnapshot writes what a snapshot changed, a line a field.
func describeSnapshot(s *oxibooru.Snapshot) []string {
	var data any
	if len(s.Data) == 0 || json.Unmarshal(s.Data, &data) != nil {
		return nil
	}
	switch s.Operation {
	case "modified":
		obj, ok := data.(map[string]any)
		if !ok {
			return nil
		}
		value, _ := obj["value"].(map[string]any)
		var lines []string
		for _, k := range sortedKeys(value) {
			lines = append(lines, k+": "+describeDiff(value[k]))
		}
		return lines
	case "merged":
		if arr, ok := data.([]any); ok && len(arr) == 2 {
			return []string{fmt.Sprintf("into %v %v", arr[0], formatValue(arr[1]))}
		}
	case "created", "deleted":
		obj, ok := data.(map[string]any)
		if !ok {
			return nil
		}
		var lines []string
		for _, k := range sortedKeys(obj) {
			v := formatValue(obj[k])
			if v == "" || v == "[]" || v == `""` || v == "none" || v == "false" || k == "checksum" || k == "notes" {
				continue
			}
			lines = append(lines, k+": "+v)
		}
		return lines
	}
	return nil
}

func describeDiff(d any) string {
	m, ok := d.(map[string]any)
	if !ok {
		return formatValue(d)
	}
	switch m["type"] {
	case "primitive change":
		return formatValue(m["old-value"]) + " → " + formatValue(m["new-value"])
	case "list change":
		var parts []string
		if added, _ := m["added"].([]any); len(added) > 0 {
			parts = append(parts, "+"+joinValues(added, " +"))
		}
		if removed, _ := m["removed"].([]any); len(removed) > 0 {
			parts = append(parts, "−"+joinValues(removed, " −"))
		}
		return strings.Join(parts, " ")
	case "added property":
		return "+" + formatValue(m["value"])
	case "deleted property":
		return "−" + formatValue(m["value"])
	case "object change":
		v, _ := m["value"].(map[string]any)
		var parts []string
		for _, k := range sortedKeys(v) {
			parts = append(parts, k+" "+describeDiff(v[k]))
		}
		return strings.Join(parts, "; ")
	}
	return formatValue(d)
}

func joinValues(vs []any, sep string) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = formatValue(v)
	}
	return strings.Join(parts, sep)
}

func formatValue(v any) string {
	switch v := v.(type) {
	case nil:
		return "none"
	case string:
		if v == "" {
			return "\"\""
		}
		if len(v) > 80 {
			return v[:77] + "…"
		}
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		return "[" + joinValues(v, ", ") + "]"
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return "note “" + formatValue(text) + "”"
		}
		return fmt.Sprintf("{%d fields}", len(v))
	}
	return fmt.Sprint(v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
