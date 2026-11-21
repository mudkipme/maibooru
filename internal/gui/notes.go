package gui

import (
	"context"
	"encoding/json/v2"
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// noteEditor edits a post's notes: rectangles drawn over the picture by
// dragging, moved by dragging them, and their texts.
type noteEditor struct {
	open   bool
	postID int64
	notes  []oxibooru.Note
	sel    int
	busy   bool

	// The gesture under way: drawing a new note from start to cur, or
	// moving the note chosen by cur−start.
	dragging bool
	moving   bool
	start    oxibooru.Point
	cur      oxibooru.Point
	orig     []oxibooru.Point
}

func (ne *noteEditor) begin(p *oxibooru.Post) {
	*ne = noteEditor{open: true, postID: p.ID, sel: -1}
	for _, n := range p.Notes {
		ne.notes = append(ne.notes, oxibooru.Note{Polygon: slices.Clone(n.Polygon), Text: n.Text})
	}
}

// clamp01 keeps a coordinate in the picture, to four decimals, which is
// finer than a pixel of pictures up to 10,000 pixels.
func clamp01(v float64) float64 { return math.Round(min(1, max(0, v))*1e4) / 1e4 }

// handle takes the pointer over the picture, at x and y in fractions of it.
func (ne *noteEditor) handle(ev ui.InputEvent, area ui.Rect) bool {
	if area.W <= 0 || area.H <= 0 {
		return false
	}
	pt := oxibooru.Point{clamp01(float64((ev.X - area.X) / area.W)), clamp01(float64((ev.Y - area.Y) / area.H))}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		hit := -1
		for i, n := range ne.notes {
			if pointInNote(n, float32(pt[0]), float32(pt[1])) {
				hit = i
			}
		}
		ne.dragging, ne.start, ne.cur = true, pt, pt
		ne.moving = hit >= 0
		ne.sel = hit
		if hit >= 0 {
			ne.orig = slices.Clone(ne.notes[hit].Polygon)
		}
		return true
	case ui.InputPointerMove:
		if !ne.dragging {
			return false
		}
		ne.cur = pt
		if ne.moving && ne.sel >= 0 {
			dx, dy := pt[0]-ne.start[0], pt[1]-ne.start[1]
			minX, minY, maxX, maxY := bounds(ne.orig)
			dx = min(max(dx, -minX), 1-maxX)
			dy = min(max(dy, -minY), 1-maxY)
			for i, p := range ne.orig {
				ne.notes[ne.sel].Polygon[i] = oxibooru.Point{clamp01(p[0] + dx), clamp01(p[1] + dy)}
			}
		}
		return true
	case ui.InputPointerUp:
		if !ne.dragging {
			return false
		}
		ne.dragging = false
		if !ne.moving && !ev.Canceled {
			x0, x1 := min(ne.start[0], pt[0]), max(ne.start[0], pt[0])
			y0, y1 := min(ne.start[1], pt[1]), max(ne.start[1], pt[1])
			if x1-x0 > 0.01 && y1-y0 > 0.01 {
				ne.notes = append(ne.notes, oxibooru.Note{Polygon: []oxibooru.Point{{x0, y0}, {x0, y1}, {x1, y1}, {x1, y0}}})
				ne.sel = len(ne.notes) - 1
			}
		}
		ne.moving = false
		return true
	}
	return false
}

// validNotes reports whether notes, as pasted, are polygons in the picture.
func validNotes(notes []oxibooru.Note) bool {
	if len(notes) == 0 {
		return false
	}
	for _, n := range notes {
		if len(n.Polygon) < 3 {
			return false
		}
		for _, p := range n.Polygon {
			if p[0] < 0 || p[0] > 1 || p[1] < 0 || p[1] > 1 {
				return false
			}
		}
	}
	return true
}

func bounds(pts []oxibooru.Point) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = 1, 1, 0, 0
	for _, p := range pts {
		minX, maxX = min(minX, p[0]), max(maxX, p[0])
		minY, maxY = min(minY, p[1]), max(maxY, p[1])
	}
	return
}

// notesEditingOverlay draws the notes being edited over the picture, and
// takes the pointer drawing and moving them.
func (a *App) notesEditingOverlay(c *ui.Context, w, h float32, grow bool) {
	ne := &a.viewer.notes
	overlay := ui.Box(c.Key("noteedit")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Cursor(ui.CursorCrosshair)
	box := overlay.LayoutBounds()
	area := fitRect(ui.Rect{W: box.W, H: box.H}, w, h, grow)
	overlay.Focusable().TabStop(false).Label("Notes")
	overlay.HandleInput(func(ev ui.InputEvent) bool { return ne.handle(ev, area) })
	if (overlay.Shortcut(0, ui.KeyDelete) || overlay.Shortcut(0, ui.KeyBackspace)) && ne.sel >= 0 {
		ne.notes = slices.Delete(ne.notes, ne.sel, ne.sel+1)
		ne.sel = -1
	}
	overlay.Draw(func(p *ui.Painter, r ui.Rect) {
		ar := fitRect(r, w, h, grow)
		for i, n := range ne.notes {
			path := notePath(n, ar)
			if path == nil {
				continue
			}
			fill, stroke := ui.RGBA(255, 236, 140, 0.22), ui.RGBA(255, 214, 10, 0.9)
			if i == ne.sel {
				fill, stroke = ui.RGBA(120, 180, 255, 0.28), ui.RGBA(60, 140, 255, 1)
			}
			p.FillPath(path, fill)
			p.StrokePath(path, 2, stroke)
		}
		if ne.dragging && !ne.moving {
			x0, x1 := min(ne.start[0], ne.cur[0]), max(ne.start[0], ne.cur[0])
			y0, y1 := min(ne.start[1], ne.cur[1]), max(ne.start[1], ne.cur[1])
			rect := ui.Rect{X: ar.X + float32(x0)*ar.W, Y: ar.Y + float32(y0)*ar.H, W: float32(x1-x0) * ar.W, H: float32(y1-y0) * ar.H}
			p.Fill(rect, ui.RGBA(120, 180, 255, 0.2), 0)
			p.StrokeDashed(rect, ui.RGBA(60, 140, 255, 1), 0, 1.5)
		}
	})
}

// notesPanel lists the notes being edited, with their texts.
func (a *App) notesPanel(c *ui.Context, p *oxibooru.Post) {
	ne := &a.viewer.notes
	t := c.Theme()
	ui.Scroll(c.Key("notes")).Grow(1).Children(func() {
		ui.Column(c).Padding(4, 14, 14).Gap(12).Children(func() {
			ui.Text(c, "Drag over the picture to add a note; drag a note to move it.").FontSize(12).TextColor(t.TextMuted)
			if len(ne.notes) == 0 {
				ui.Text(c, "No notes").TextColor(t.TextMuted)
			}
			removed := -1
			for i := range ne.notes {
				row := ui.Column(c.Key(i)).Gap(6).Padding(8).Radius(t.Radius+2).Border(1, t.Border)
				if i == ne.sel {
					row.Border(2, t.Accent)
				}
				if row.PressedInside() {
					ne.sel = i
				}
				row.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Note "+itoa(i+1)).Bold().Grow(1)
						if iconButton(c.Key("rm"), iconTrash, "Delete note").Clicked() {
							removed = i
						}
					})
					ui.TextArea(c.Key("text"), &ne.notes[i].Text).Placeholder("Markdown").Label("Text of note "+itoa(i+1)).Lines(2, 8)
				})
			}
			if removed >= 0 {
				ne.notes = slices.Delete(ne.notes, removed, removed+1)
				ne.sel = -1
			}
		})
	})
	ui.Row(c).Padding(10, 14).Gap(8).Justify(ui.End).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
		iconButton(c.Key("notesmenu"), iconMore, "More").PopoverMenuButton(func(m *ui.Menu) {
			if m.Item("Copy Notes").Disabled(len(ne.notes) == 0).Chosen() {
				if b, err := json.Marshal(ne.notes); err == nil {
					a.copyText(string(b))
				}
			}
			if m.Item("Paste Notes").Chosen() {
				var pasted []oxibooru.Note
				if err := json.Unmarshal([]byte(c.ReadClipboard()), &pasted); err != nil || !validNotes(pasted) {
					a.toast("The clipboard holds no notes")
				} else {
					ne.notes = append(ne.notes, pasted...)
				}
			}
		}, ui.PopoverMenuButtonOptions{At: ui.AnchorTopStart, Self: ui.AnchorBottomStart})
		ui.Spacer(c)
		if ui.Button(c, "Cancel").Clicked() {
			ne.open = false
		}
		if ui.PrimaryButton(c, "Save Notes").Disabled(ne.busy).Clicked() {
			notes := slices.Clone(ne.notes)
			id, version := p.ID, p.Version
			ne.busy = true
			async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.Post, error) {
				return cl.UpdatePost(ctx, id, oxibooru.PostFields{Version: version, Notes: &notes})
			}, func(np *oxibooru.Post, err error) {
				ne.busy = false
				if err != nil {
					a.fail(err)
					return
				}
				ne.open = false
				if cur := a.viewer.post(); cur != nil && cur.ID == np.ID {
					cur.Notes, cur.NoteCount, cur.Version = np.Notes, np.NoteCount, np.Version
				}
				a.postChanged(np)
				a.toast("Saved the notes")
			})
		}
	})
}
