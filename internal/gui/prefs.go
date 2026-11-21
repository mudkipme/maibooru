package gui

import (
	"cmp"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/settings"
)

type settingsPage struct {
	cacheSize    int64
	cacheChecked bool
}

func (a *App) settingsView(c *ui.Context) {
	sp := &a.prefs
	t := c.Theme()
	s := a.s
	changed := false
	if !sp.cacheChecked {
		sp.cacheChecked = true
		go func() {
			n := a.opts.Cache.Size()
			a.post(func() { sp.cacheSize = n })
		}()
	}
	ui.Scroll(c.Key("settings")).Fill().Children(func() {
		ui.Column(c).Padding(24, 28).Gap(26).MaxWidth(680).Children(func() {
			ui.Text(c, "Settings").FontSize(22).Bold()

			section(c, "Servers", func() {
				card(c).Gap(0).Padding(0).Children(func() {
					for _, p := range s.Profiles {
						ui.Row(c.Key(p.ID)).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
							ui.Icon(c, iconServer).TextColor(t.TextMuted)
							ui.Column(c).Grow(1).MinWidth(0).Children(func() {
								ui.Text(c, cmp.Or(p.Name, p.URL)).SingleLine()
								ui.Text(c, cmp.Or(p.User, "Guest")+" · "+p.URL).FontSize(12).TextColor(t.TextMuted).SingleLine()
							})
							if a.sess != nil && a.sess.profile.ID == p.ID {
								ui.Badge(c, "Connected")
							} else if ui.Button(c.Key("connect"), "Connect").Clicked() {
								a.resume(p)
							}
						})
					}
				})
				if ui.Button(c, "Add a Server…").Clicked() {
					a.switchServer()
				}
			})

			section(c, "Appearance", func() {
				themes := []settings.Theme{settings.ThemeSystem, settings.ThemeLight, settings.ThemeDark}
				i := slices.Index(themes, s.Theme)
				if ui.Segmented(c.Key("theme"), &i, "System", "Light", "Dark").Label("Theme").Width(300).Changed() {
					s.Theme = themes[i]
					changed = true
				}
			})

			section(c, "Lock", func() {
				changed = a.lockSettings(c) || changed
			})

			section(c, "Library", func() {
				size := s.ThumbnailSize
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Thumbnail size").Width(160)
					if ui.Slider(c.Key("thumb"), &size, 100, 360).Width(220).Label("Thumbnail size").Changed() {
						s.ThumbnailSize = size
						a.saveLater()
					}
				})
				pages := []string{"50", "100", "200"}
				ps := map[int]string{50: "50", 100: "100", 200: "200"}[s.PageSize]
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Posts loaded at once").Width(160)
					if ui.Select(c.Key("page"), &ps, pages).Label("Posts loaded at once").Width(100).Changed() {
						s.PageSize = map[string]int{"50": 50, "100": 100, "200": 200}[ps]
						changed = true
					}
				})
				changed = ui.Checkbox(c.Key("blur"), &s.BlurUnsafe, "Blur unsafe thumbnails").Changed() || changed
				changed = ui.Checkbox(c.Key("spaces"), &s.UnderscoresAsSpaces, "Show underscores in tags as spaces").Changed() || changed
				changed = ui.Checkbox(c.Key("notes"), &s.ShowNotes, "Show notes over posts").Changed() || changed
			})

			section(c, "Keyboard", func() {
				on := !s.NoKeyShortcuts
				if ui.Checkbox(c.Key("keys"), &on, "Single keys act as shortcuts (L, F, E, A, D…)").Changed() {
					s.NoKeyShortcuts = !on
					changed = true
				}
			})

			section(c, "Viewer", func() {
				changed = ui.Checkbox(c.Key("upscale"), &s.UpscaleSmall, "Enlarge small pictures to fit the window").Changed() || changed
				changed = ui.Checkbox(c.Key("grid"), &s.TransparencyGrid, "Show a checkerboard behind transparent pictures").Changed() || changed
			})

			section(c, "Videos", func() {
				changed = ui.Checkbox(c.Key("autoplay"), &s.AutoplayVideos, "Play videos as they open").Changed() || changed
				changed = ui.Checkbox(c.Key("mute"), &s.MuteVideos, "Start videos muted").Changed() || changed
			})

			section(c, "Uploads", func() {
				changed = ui.Checkbox(c.Key("dups"), &s.KeepDuplicates, "Upload files the server has already").Changed() || changed
				ui.Text(c, "Otherwise Maibooru skips them, and shows the post that has them.").FontSize(12).TextColor(t.TextMuted)
			})

			section(c, "Downloaded Files", func() {
				ui.Text(c, "Maibooru keeps the files of the posts it showed, to show them again at once and to drag them into other apps.").
					TextColor(t.TextMuted)
				limits := []string{"512 MB", "1 GB", "2 GB", "5 GB", "10 GB"}
				mbs := []int{512, 1024, 2048, 5120, 10240}
				cur := limits[max(0, slices.Index(mbs, s.CacheLimitMB))]
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Keep at most").Width(160)
					if ui.Select(c.Key("cache"), &cur, limits).Label("Keep at most").Width(120).Changed() {
						s.CacheLimitMB = mbs[slices.Index(limits, cur)]
						changed = true
						limit := int64(s.CacheLimitMB) << 20
						go a.opts.Cache.Prune(limit)
					}
				})
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, formatBytes(sp.cacheSize)+" kept").TextColor(t.TextMuted).Width(160)
					if ui.Button(c, "Clear").Clicked() {
						go func() {
							err := a.opts.Cache.Clear()
							a.post(func() {
								if err != nil {
									a.fail(err)
								}
								netimage.Clear()
								sp.cacheChecked = false
							})
						}()
					}
					if a.opts.Desktop != nil && ui.Button(c, "Show in Folder").Clicked() {
						go a.opts.Desktop.OpenPath(a.opts.Cache.Root())
					}
				})
			})

			section(c, "About", func() {
				ui.Text(c, "Maibooru "+cmp.Or(a.opts.Version, "dev"))
				ui.Text(c, "A desktop library for Oxibooru servers.").TextColor(t.TextMuted)
			})
		})
	})
	a.pinSetupView(c)
	if changed {
		a.save()
	}
}
