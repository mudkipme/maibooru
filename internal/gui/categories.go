package gui

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type categoriesPage struct {
	newTag  newCategory
	newPool newCategory
	busy    bool
}

type newCategory struct {
	name  string
	color ui.Color
}

// hexColor writes a color as CSS's #rrggbb.
func hexColor(c ui.Color) string {
	c = c.SRGB()
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func (a *App) categoriesView(c *ui.Context) {
	s := a.sess
	t := c.Theme()
	cp := &a.cats
	if cp.newTag.color.A == 0 {
		cp.newTag.color = ui.Hex("#6b7280")
		cp.newPool.color = ui.Hex("#6b7280")
	}
	ui.Scroll(c.Key("cats")).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(28).MaxWidth(760).Children(func() {
			ui.Column(c).Gap(10).Children(func() {
				ui.Text(c, "Tag Categories").FontSize(18).Bold()
				ui.Text(c, "Tags show in the colors of their categories, in the categories' order.").TextColor(t.TextMuted)
				card(c).Gap(0).Padding(0).Children(func() {
					for i := range s.tagCats {
						cat := s.tagCats[i]
						a.categoryRow(c, cat.Name, cat.Color, cat.Usages, "tags", cat.Default, &cat.Order, func(f oxibooru.CategoryFields) {
							f.Version = cat.Version
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								_, err := cl.UpdateTagCategory(ctx, cat.Name, f)
								return err
							}, a.refreshCategories)
						}, func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								_, err := cl.SetDefaultTagCategory(ctx, cat.Name)
								return err
							}, a.refreshCategories)
						}, func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								return cl.DeleteTagCategory(ctx, cat.Name, cat.Version)
							}, a.refreshCategories)
						}, "tag_category")
					}
				})
				if s.can("tag_category_create") {
					a.newCategoryRow(c, "tag", &cp.newTag, func(name, color string) {
						a.do(func(ctx context.Context, cl *oxibooru.Client) error {
							order := len(s.tagCats) + 1
							_, err := cl.CreateTagCategory(ctx, oxibooru.CategoryFields{Name: &name, Color: &color, Order: &order})
							return err
						}, func() {
							cp.newTag.name = ""
							a.refreshCategories()
						})
					})
				}
			})
			ui.Column(c).Gap(10).Children(func() {
				ui.Text(c, "Pool Categories").FontSize(18).Bold()
				card(c).Gap(0).Padding(0).Children(func() {
					for i := range s.poolCats {
						cat := s.poolCats[i]
						a.categoryRow(c, cat.Name, cat.Color, cat.Usages, "pools", cat.Default, nil, func(f oxibooru.CategoryFields) {
							f.Version = cat.Version
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								_, err := cl.UpdatePoolCategory(ctx, cat.Name, f)
								return err
							}, a.refreshCategories)
						}, func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								_, err := cl.SetDefaultPoolCategory(ctx, cat.Name)
								return err
							}, a.refreshCategories)
						}, func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								return cl.DeletePoolCategory(ctx, cat.Name, cat.Version)
							}, a.refreshCategories)
						}, "pool_category")
					}
				})
				if s.can("pool_category_create") {
					a.newCategoryRow(c, "pool", &cp.newPool, func(name, color string) {
						a.do(func(ctx context.Context, cl *oxibooru.Client) error {
							_, err := cl.CreatePoolCategory(ctx, oxibooru.CategoryFields{Name: &name, Color: &color})
							return err
						}, func() {
							cp.newPool.name = ""
							a.refreshCategories()
						})
					})
				}
			})
		})
	})
}

// categoryRow shows a category, editing its name, color and order in
// place.
func (a *App) categoryRow(c *ui.Context, name, css string, usages int, of string, isDefault bool, order *int,
	update func(oxibooru.CategoryFields), makeDefault, remove func(), priv string) {
	s := a.sess
	t := c.Theme()
	color := categoryColor(css, t)
	ui.Row(c.Key(name)).Padding(10, 14).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		well := ui.ColorWell(c.Key("color"), &color).Label("Color of " + name).Disabled(!s.can(priv + "_edit_color"))
		if well.Changed() {
			hex := hexColor(color)
			update(oxibooru.CategoryFields{Color: &hex})
		}
		newName := name
		et := ui.EditableText(c.Key("name"), &newName).Label("Name").Grow(1).Bold().TextColor(color)
		if !s.can(priv + "_edit_name") {
			et.Disabled(true)
		}
		if et.Changed() && strings.TrimSpace(newName) != "" && newName != name {
			nn := strings.TrimSpace(newName)
			update(oxibooru.CategoryFields{Name: &nn})
		}
		ui.Text(c, pluralize(usages, strings.TrimSuffix(of, "s"), of)).FontSize(12).TextColor(t.TextMuted).Width(90)
		if order != nil {
			v := float64(*order)
			if ui.NumberInput(c.Key("order"), &v, -999, 999, 1).Width(90).Label("Order").Disabled(!s.can(priv + "_edit_order")).Changed() {
				o := int(v)
				update(oxibooru.CategoryFields{Order: &o})
			}
		}
		ui.Row(c).Width(150).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if isDefault {
				ui.Badge(c, "Default")
			} else if s.can(priv + "_set_default") {
				if ui.Button(c.Key("default"), "Make Default").Clicked() {
					makeDefault()
				}
			}
			if s.can(priv+"_delete") && !isDefault {
				if iconButton(c.Key("rm"), iconTrash, "Delete "+name).Tooltip("Delete").Clicked() {
					a.confirm("Delete the category "+name+"?", "Its "+of+" go to the default category.", "Delete", remove)
				}
			}
		})
	})
}

func (a *App) newCategoryRow(c *ui.Context, of string, nc *newCategory, create func(name, color string)) {
	ui.Row(c.Key("new" + of)).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.ColorWell(c.Key("color"), &nc.color).Label("Color of the new category")
		in := ui.TextInput(c.Key("name"), &nc.name).Placeholder("New " + of + " category").Label("Name of the new " + of + " category").Width(240)
		ok := strings.TrimSpace(nc.name) != ""
		if (ui.Button(c.Key("add"), "Add").Disabled(!ok).Clicked() || in.Submitted()) && ok {
			create(strings.TrimSpace(nc.name), hexColor(nc.color))
		}
	})
}
