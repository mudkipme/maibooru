package gui

import (
	"context"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

type usersPage struct {
	search string
	help   bool
	pg     pager[oxibooru.User]
	list   ui.ListState
	row    int
}

func (up *usersPage) reload(a *App) { up.pg.reload(a) }

func (a *App) usersView(c *ui.Context) {
	up := &a.users
	t := c.Theme()
	if up.pg.fetch == nil {
		up.row = -1
		up.pg.fetch = func(ctx context.Context, cl *oxibooru.Client, s oxibooru.Search) (*oxibooru.Page[oxibooru.User], error) {
			return cl.Users(ctx, s)
		}
	}
	q := strings.TrimSpace(up.search)
	if q != "" && !strings.ContainsAny(q, "*:") {
		q = "*" + oxibooru.Escape(q) + "*"
	}
	up.pg.search(a, strings.TrimSpace(q+" sort:name"))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(8, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.SearchField(c.Key("q"), &up.search).Placeholder("Find users").Label("Find users").Width(240)
			syntaxHelp(c, &up.help, userSyntax, &up.search)
			ui.Spacer(c)
			ui.Text(c, pluralize(up.pg.total, "user", "users")).TextColor(t.TextMuted).SingleLine()
		})
		if up.pg.err != nil && len(up.pg.items) == 0 {
			ui.Text(c, up.pg.err.Error()).TextColor(t.Danger).Padding(16)
			return
		}
		cols := []ui.TableColumn{
			{Title: "Name", MinWidth: 160},
			{Title: "Rank", Width: 120},
			{Title: "Uploads", Width: 90, Align: ui.End},
			{Title: "Comments", Width: 90, Align: ui.End},
			{Title: "Joined", Width: 120},
			{Title: "Last Seen", Width: 120},
		}
		up.list.Selected = &up.row
		up.list.Key = func(i int) any { return up.pg.items[i].Name }
		tbl := ui.Table(c.Key("users"), &up.list, cols, len(up.pg.items), func(row, col int) {
			up.pg.near(a, row)
			u := &up.pg.items[row]
			switch col {
			case 0:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					pic, _ := netimage.Load(c, a.sess.client.DataURL(u.AvatarURL), netimage.Options{MaxSide: 64})
					avatar(c, u.Name, pic, 22)
					ui.Text(c, u.Name).SingleLine()
				})
			case 1:
				ui.Text(c, string(u.Rank)).TextColor(t.TextMuted)
			case 2:
				ui.Text(c, formatCount(u.UploadedPostCount)).FontFeatures("tnum")
			case 3:
				ui.Text(c, formatCount(u.CommentCount)).FontFeatures("tnum")
			case 4:
				ui.Text(c, formatTime(u.CreationTime)).TextColor(t.TextMuted)
			case 5:
				ui.Text(c, formatTime(u.LastLoginTime)).TextColor(t.TextMuted)
			}
		}).Grow(1).Label("Users")
		if tbl.Submitted() && up.row >= 0 && up.row < len(up.pg.items) {
			a.router.Push("/user/" + url.PathEscape(up.pg.items[up.row].Name))
		}
	})
}

// accountPage is the page of a user: theirs, or the account of the user
// signed in, which they edit.
type accountPage struct {
	name    string
	user    *oxibooru.User
	err     error
	gen     int
	loading bool

	newName  string
	email    string
	password string
	rank     string
	busy     bool

	tokens       []oxibooru.UserToken
	tokensErr    error
	tokenNote    string
	tokenExpires int
	newToken     string
}

func (ap *accountPage) load(a *App) {
	ap.gen++
	gen, name := ap.gen, ap.name
	ap.loading = true
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.User, error) {
		return cl.User(ctx, name, false)
	}, func(u *oxibooru.User, err error) {
		if gen != ap.gen {
			return
		}
		ap.loading, ap.err = false, err
		if err == nil {
			ap.setUser(u)
		}
	})
	if a.sess.isMe(name) && a.sess.can("user_token_list_self") || a.sess.can("user_token_list_any") {
		ap.loadTokens(a)
	}
}

func (ap *accountPage) setUser(u *oxibooru.User) {
	ap.user = u
	ap.newName, ap.rank, ap.password = u.Name, string(u.Rank), ""
	ap.email = ""
	if u.Email.Value != nil {
		ap.email = *u.Email.Value
	}
}

func (ap *accountPage) loadTokens(a *App) {
	name := ap.name
	async(a, func(ctx context.Context, cl *oxibooru.Client) ([]oxibooru.UserToken, error) {
		return cl.UserTokens(ctx, name)
	}, func(tokens []oxibooru.UserToken, err error) {
		if name != ap.name {
			return
		}
		ap.tokens, ap.tokensErr = tokens, err
	})
}

func (a *App) userView(c *ui.Context, name string) {
	ap := &a.account
	t := c.Theme()
	s := a.sess
	if ap.name != name {
		*ap = accountPage{name: name, gen: ap.gen}
		ap.load(a)
	}
	u := ap.user
	if u == nil {
		ui.Column(c).Fill().Center().Children(func() {
			if ap.err != nil {
				ui.Text(c, ap.err.Error()).TextColor(t.Danger)
			} else {
				ui.Spinner(c)
			}
		})
		return
	}
	me := s.isMe(u.Name)
	self := func(priv string) bool {
		if me {
			return s.can("user_edit_self_" + priv)
		}
		return s.can("user_edit_any_" + priv)
	}
	ui.Scroll(c.Key("user")).Fill().Children(func() {
		ui.Column(c).Padding(24).Gap(22).MaxWidth(720).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				pic, _ := netimage.Load(c, s.client.DataURL(u.AvatarURL), netimage.Options{MaxSide: 256})
				avatar(c, u.Name, pic, 72)
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, u.Name).FontSize(22).Bold()
					ui.Text(c, string(u.Rank)+" · joined "+formatTime(u.CreationTime)+" · last seen "+formatTime(u.LastLoginTime)).TextColor(t.TextMuted)
				})
				if self("avatar") {
					ui.Column(c).Gap(6).AlignItems(ui.End).Children(func() {
						if ui.Button(c, "Change Picture…").Clicked() {
							a.changeAvatar(u)
						}
						if u.AvatarStyle != "gravatar" && ui.Link(c, "Use Gravatar", "").FontSize(12).Clicked() {
							a.useGravatar(u)
						}
					})
				}
			})
			ui.Row(c).Gap(10).Wrap().Children(func() {
				stat := func(label string, n int, q string) {
					b := ui.ButtonBase(c.Key(label)).Column().Padding(10, 16).Radius(t.Radius+2).Background(t.Surface).Border(1, t.Border).Gap(2)
					if b.Hovered() {
						b.Background(t.SurfaceHover)
					}
					b.Children(func() {
						ui.Text(c, formatCount(n)).FontSize(20).Bold().FontFeatures("tnum")
						ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
					})
					if b.Clicked() && q != "" {
						a.router.Push(postsURL(q))
					}
				}
				stat("Uploads", u.UploadedPostCount, "uploader:"+oxibooru.Escape(u.Name))
				stat("Favorites", u.FavoritePostCount, "fav:"+oxibooru.Escape(u.Name))
				stat("Comments", u.CommentCount, "comment:"+oxibooru.Escape(u.Name))
				if !u.LikedPostCount.Hidden {
					stat("Liked", u.LikedPostCount.Value, map[bool]string{true: "special:liked", false: ""}[me])
				}
				if !u.DislikedPostCount.Hidden {
					stat("Disliked", u.DislikedPostCount.Value, map[bool]string{true: "special:disliked", false: ""}[me])
				}
			})
			if self("name") || self("email") || self("pass") || self("rank") {
				a.userEditor(c, u, self)
			}
			if me && s.can("user_token_list_self") || s.can("user_token_list_any") {
				a.tokensView(c, u)
			}
			if me && s.can("user_delete_self") || !me && s.can("user_delete_any") {
				section(c, "Delete Account", func() {
					if ui.Button(c, "Delete Account…").TextColor(t.Danger).Clicked() {
						target := *u
						a.confirm("Delete the account "+target.Name+"?", "Their posts stay. This can't be undone.", "Delete", func() {
							a.do(func(ctx context.Context, cl *oxibooru.Client) error {
								return cl.DeleteUser(ctx, target.Name, target.Version)
							}, func() {
								if me {
									a.switchServer()
									return
								}
								a.users.pg.reload(a)
								a.router.Replace("/users")
							})
						})
					}
				})
			}
		})
	})
}

func (a *App) userEditor(c *ui.Context, u *oxibooru.User, can func(string) bool) {
	ap := &a.account
	s := a.sess
	section(c, "Account", func() {
		card(c).Children(func() {
			ui.Form(c, func() {
				if can("name") {
					ui.Field(c, "Name", func() { ui.TextInput(c.Key("name"), &ap.newName).Width(300) })
				}
				if can("email") && !u.Email.Hidden {
					ui.Field(c, "Email", func() { ui.TextInput(c.Key("email"), &ap.email).Width(300) })
				}
				if can("pass") {
					ui.Field(c, "New password", func() {
						ui.TextInput(c.Key("pass"), &ap.password).Password().Placeholder("Unchanged").Width(300).Autofill(ui.AutofillNewPassword)
					})
				}
				if can("rank") {
					var ranks []string
					for _, r := range oxibooru.Ranks[1:] {
						if r != oxibooru.Nobody && r.Level() <= s.rank().Level() {
							ranks = append(ranks, string(r))
						}
					}
					ui.Field(c, "Rank", func() { ui.Select(c.Key("rank"), &ap.rank, ranks).Width(300) })
				}
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, "Save").Disabled(ap.busy).Clicked() {
					a.saveUser(u)
				}
			})
		})
	})
}

func (a *App) saveUser(u *oxibooru.User) {
	ap := &a.account
	f := oxibooru.UserFields{Version: u.Version}
	if n := strings.TrimSpace(ap.newName); n != "" && n != u.Name {
		f.Name = &n
	}
	old := ""
	if u.Email.Value != nil {
		old = *u.Email.Value
	}
	if e := strings.TrimSpace(ap.email); e != old && !u.Email.Hidden {
		f.Email = &e
	}
	if ap.password != "" {
		p := ap.password
		f.Password = &p
	}
	if r := oxibooru.Rank(ap.rank); r != u.Rank && r != "" {
		f.Rank = &r
	}
	ap.busy = true
	name, me := u.Name, a.sess.isMe(u.Name)
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.User, error) {
		return cl.UpdateUser(ctx, name, f)
	}, func(nu *oxibooru.User, err error) {
		ap.busy = false
		if err != nil {
			a.fail(err)
			return
		}
		ap.setUser(nu)
		a.toast("Saved")
		if me {
			a.sess.user = nu
			if nu.Name != name {
				// The token belongs to the new name now.
				creds := a.sess.client.Credentials()
				creds.User = nu.Name
				a.sess.client.SetCredentials(creds)
				a.sess.profile.User = nu.Name
				a.s.PutProfile(a.sess.profile)
				a.save()
			}
		}
		if nu.Name != name {
			ap.name = nu.Name
			a.router.Replace("/user/" + url.PathEscape(nu.Name))
		}
	})
}

// useGravatar shows the picture Gravatar has for the user's email.
func (a *App) useGravatar(u *oxibooru.User) {
	name, version, style := u.Name, u.Version, "gravatar"
	async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.User, error) {
		return cl.UpdateUser(ctx, name, oxibooru.UserFields{Version: version, AvatarStyle: &style})
	}, func(nu *oxibooru.User, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		a.account.setUser(nu)
		if a.sess.isMe(nu.Name) {
			a.sess.user = nu
		}
	})
}

// changeAvatar uploads a picture as the user's avatar.
func (a *App) changeAvatar(u *oxibooru.User) {
	desk := a.opts.Desktop
	if desk == nil {
		return
	}
	name, version := u.Name, u.Version
	go func() {
		files, err := desk.OpenFiles()
		if err != nil || len(files) == 0 {
			a.post(func() { a.fail(err) })
			return
		}
		path := files[0]
		a.post(func() {
			async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.User, error) {
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				defer f.Close()
				info, err := f.Stat()
				if err != nil {
					return nil, err
				}
				token, err := cl.Upload(ctx, path, f, info.Size(), nil)
				if err != nil {
					return nil, err
				}
				style := "manual"
				return cl.UpdateUser(ctx, name, oxibooru.UserFields{Version: version, AvatarStyle: &style, AvatarToken: token})
			}, func(nu *oxibooru.User, err error) {
				if err != nil {
					a.fail(err)
					return
				}
				netimage.Forget(a.sess.client.DataURL(nu.AvatarURL))
				a.account.setUser(nu)
				if a.sess.isMe(nu.Name) {
					a.sess.user = nu
				}
			})
		})
	}()
}

var tokenLifetimes = []struct {
	label string
	d     time.Duration
}{{"Never expires", 0}, {"1 week", 7 * 24 * time.Hour}, {"1 month", 30 * 24 * time.Hour}, {"1 year", 365 * 24 * time.Hour}}

func (a *App) tokensView(c *ui.Context, u *oxibooru.User) {
	ap := &a.account
	s := a.sess
	t := c.Theme()
	me := s.isMe(u.Name)
	current := s.client.Credentials().Token
	section(c, "Tokens", func() {
		ui.Text(c, "Tokens sign apps in for "+u.Name+" without the password.").TextColor(t.TextMuted)
		if ap.tokensErr != nil {
			ui.Text(c, ap.tokensErr.Error()).TextColor(t.Danger)
		}
		card(c).Gap(0).Padding(0).Children(func() {
			if len(ap.tokens) == 0 {
				ui.Text(c, "No tokens").TextColor(t.TextMuted).Padding(12)
			}
			for i := range ap.tokens {
				tok := ap.tokens[i]
				ui.Row(c.Key(tok.Token)).Padding(10, 14).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
					ui.Icon(c, iconKey).TextColor(t.TextMuted)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						note := tok.Note
						edit := me && s.can("user_token_edit_self") || s.can("user_token_edit_any")
						et := ui.EditableText(c.Key("note"), &note).Label("Note of the token").Disabled(!edit)
						if et.Changed() {
							name, token, version, n := u.Name, tok.Token, tok.Version, note
							async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.UserToken, error) {
								return cl.UpdateUserToken(ctx, name, token, oxibooru.TokenFields{Version: version, Note: &n})
							}, func(_ *oxibooru.UserToken, err error) {
								if err != nil {
									a.fail(err)
								}
								ap.loadTokens(a)
							})
						}
						if tok.Token == current {
							ui.Text(c, "This app").FontSize(11).TextColor(t.Accent)
						}
						meta := "made " + formatTime(tok.CreationTime) + " · used " + formatTime(tok.LastUsageTime)
						if tok.ExpirationTime != nil {
							meta += " · expires " + tok.ExpirationTime.Local().Format("Jan 2, 2006")
						}
						ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted).SingleLine()
					})
					on := tok.Enabled
					edit := me && s.can("user_token_edit_self") || s.can("user_token_edit_any")
					if ui.Switch(c.Key("on"), &on).Label("Enabled").Disabled(!edit || tok.Token == current).Changed() {
						name, token, version := u.Name, tok.Token, tok.Version
						async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.UserToken, error) {
							return cl.UpdateUserToken(ctx, name, token, oxibooru.TokenFields{Version: version, Enabled: &on})
						}, func(nt *oxibooru.UserToken, err error) {
							if err != nil {
								a.fail(err)
							}
							ap.loadTokens(a)
						})
					}
					del := me && s.can("user_token_delete_self") || s.can("user_token_delete_any")
					if del && tok.Token == current {
						if iconButton(c.Key("rm"), iconLogout, "Delete token and sign out").Clicked() {
							a.confirm("Delete this app's token?", "Maibooru signs out.", "Delete and Sign Out", a.signOut)
						}
					} else if del {
						if iconButton(c.Key("rm"), iconTrash, "Delete token").Tooltip("Delete").Clicked() {
							name, token := u.Name, tok.Token
							a.confirm("Delete this token?", "Apps signed in with it are signed out.", "Delete", func() {
								a.do(func(ctx context.Context, cl *oxibooru.Client) error {
									return cl.DeleteUserToken(ctx, name, token)
								}, func() { ap.loadTokens(a) })
							})
						}
					}
				})
			}
		})
		if me && s.can("user_token_create_self") || s.can("user_token_create_any") {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("note"), &ap.tokenNote).Placeholder("What it's for").Label("Note of the new token").Width(220)
				labels := make([]string, len(tokenLifetimes))
				for i, l := range tokenLifetimes {
					labels[i] = l.label
				}
				life := labels[ap.tokenExpires]
				if ui.Select(c.Key("life"), &life, labels).Label("Lifetime").Changed() {
					ap.tokenExpires = slices.Index(labels, life)
				}
				if ui.Button(c, "Make Token").Clicked() {
					name, note := u.Name, strings.TrimSpace(ap.tokenNote)
					f := oxibooru.TokenFields{Enabled: new(true)}
					if note != "" {
						f.Note = &note
					}
					if d := tokenLifetimes[ap.tokenExpires].d; d > 0 {
						exp := time.Now().Add(d).UTC()
						f.ExpirationTime = &exp
					}
					async(a, func(ctx context.Context, cl *oxibooru.Client) (*oxibooru.UserToken, error) {
						return cl.CreateUserToken(ctx, name, f)
					}, func(tok *oxibooru.UserToken, err error) {
						if err != nil {
							a.fail(err)
							return
						}
						ap.newToken, ap.tokenNote = tok.Token, ""
						ap.loadTokens(a)
					})
				}
			})
			if ap.newToken != "" {
				ui.Row(c).Gap(8).Padding(10, 12).Radius(t.Radius).Background(t.Success.Alpha(0.12)).AlignItems(ui.Center).Children(func() {
					ui.Text(c, ap.newToken).Selectable().Grow(1).FontFeatures("tnum")
					if ui.Button(c, "Copy").Clicked() {
						a.copyText(ap.newToken)
					}
				})
			}
		}
	})
}
