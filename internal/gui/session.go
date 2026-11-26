package gui

import (
	"cmp"
	"context"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
	"github.com/mudkipme/maibooru/internal/settings"
)

// session is the connection to a server, as a user or anonymously.
type session struct {
	ctx      context.Context
	cancel   context.CancelFunc
	profile  settings.Profile
	client   *oxibooru.Client
	info     *oxibooru.Info
	user     *oxibooru.User
	tagCats  []oxibooru.TagCategory
	poolCats []oxibooru.PoolCategory
	ready    bool
	err      error
	counts   map[string]*postCount
}

// rank returns the rank of the user, anonymous for none.
func (s *session) rank() oxibooru.Rank {
	if s.user == nil {
		return oxibooru.Anonymous
	}
	return s.user.Rank
}

// can reports whether the user has a privilege.
func (s *session) can(privilege string) bool {
	if s.info == nil {
		return false
	}
	return s.info.Config.Can(s.rank(), privilege)
}

// isMe reports whether name is the user signed in.
func (s *session) isMe(name string) bool {
	return s.user != nil && strings.EqualFold(s.user.Name, name)
}

// safetyEnabled reports whether the server rates posts' safety.
func (s *session) safetyEnabled() bool { return s.info != nil && s.info.Config.EnableSafety }

// tagColor returns the color of a tag category.
func (s *session) tagColor(category string, t *ui.Theme) ui.Color {
	for _, cat := range s.tagCats {
		if cat.Name == category {
			return categoryColor(cat.Color, t)
		}
	}
	return t.Text
}

// tagCategoryNames returns the names of the tag categories, in order.
func (s *session) tagCategoryNames() []string {
	names := make([]string, 0, len(s.tagCats))
	for _, c := range s.tagCats {
		names = append(names, c.Name)
	}
	return names
}

// defaultTagCategory returns the category new tags get.
func (s *session) defaultTagCategory() string {
	for _, c := range s.tagCats {
		if c.Default {
			return c.Name
		}
	}
	if len(s.tagCats) > 0 {
		return s.tagCats[0].Name
	}
	return ""
}

// defaultPoolCategory returns the category new pools get.
func (s *session) defaultPoolCategory() string {
	for _, c := range s.poolCats {
		if c.Default {
			return c.Name
		}
	}
	if len(s.poolCats) > 0 {
		return s.poolCats[0].Name
	}
	return ""
}

// categoryColor turns a category's color, as CSS writes it, into a color
// that reads on the theme's background.
func categoryColor(css string, t *ui.Theme) ui.Color {
	css = strings.TrimSpace(css)
	if css == "" || css == "default" {
		return t.Text
	}
	if !strings.HasPrefix(css, "#") {
		if hex, ok := cssColors[strings.ToLower(css)]; ok {
			css = hex
		} else {
			return t.Text
		}
	}
	c := ui.Hex(css)
	if c.A == 0 && css != "#00000000" {
		return t.Text
	}
	return c
}

var cssColors = map[string]string{
	"black": "#000000", "white": "#ffffff", "red": "#ff0000", "green": "#008000", "blue": "#0000ff",
	"yellow": "#ffff00", "orange": "#ffa500", "purple": "#800080", "pink": "#ffc0cb", "gray": "#808080",
	"grey": "#808080", "brown": "#a52a2a", "cyan": "#00ffff", "magenta": "#ff00ff", "lime": "#00ff00",
	"navy": "#000080", "teal": "#008080", "maroon": "#800000", "olive": "#808000", "silver": "#c0c0c0",
	"violet": "#ee82ee", "indigo": "#4b0082", "gold": "#ffd700", "crimson": "#dc143c", "coral": "#ff7f50",
}

func tokenKey(profileID string) string { return "token/" + profileID }

// resume connects to a saved server, with the user's token.
func (a *App) resume(p settings.Profile) {
	creds := oxibooru.Credentials{}
	if p.User != "" {
		tok, err := a.opts.Secrets.Get(tokenKey(p.ID))
		if err != nil || len(tok) == 0 {
			// Signed out, or the token is gone: sign in again.
			a.connect = connectPage{step: stepSignIn, url: p.URL, profile: p.ID, user: p.User,
				endpoint: &oxibooru.Endpoint{API: p.API, Site: p.Site}, serverName: p.Name}
			return
		}
		creds = oxibooru.Credentials{User: p.User, Token: string(tok)}
	}
	a.start(p, creds)
}

// start opens a session on a profile with credentials, loading what every
// page needs.
func (a *App) start(p settings.Profile, creds oxibooru.Credentials) {
	if a.sess != nil {
		a.sess.cancel()
	}
	client, err := oxibooru.New(p.API, p.Site, oxibooru.WithHTTPClient(a.opts.HTTP), oxibooru.WithCredentials(creds))
	if err != nil {
		a.connect.err = err.Error()
		a.sess = nil
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if a.up.profile != p.ID {
		// The queue was for another server.
		a.up = uploads{app: a, profile: p.ID}
	}
	s := &session{ctx: ctx, cancel: cancel, profile: p, client: client}
	a.sess = s
	a.feeds, a.insp, a.viewer = nil, inspector{}, viewer{}
	a.tags, a.cats, a.pools, a.users, a.account, a.history, a.comms, a.home = tagsPage{}, categoriesPage{}, poolsPage{}, usersPage{}, accountPage{}, historyPage{}, commentsPage{}, homePage{}
	a.router = ui.NewRouter("/posts")
	a.router.Transition = ui.TransitionNone
	a.connect.busy = true
	a.connect.err = ""

	type loaded struct {
		info     *oxibooru.Info
		user     *oxibooru.User
		tagCats  []oxibooru.TagCategory
		poolCats []oxibooru.PoolCategory
	}
	async(a, func(ctx context.Context, c *oxibooru.Client) (loaded, error) {
		var l loaded
		var err error
		if l.info, err = c.Info(ctx); err != nil {
			return l, err
		}
		if creds.User != "" {
			if l.user, err = c.User(ctx, creds.User, true); err != nil {
				return l, err
			}
		}
		if l.tagCats, err = c.TagCategories(ctx); err != nil {
			return l, err
		}
		if l.poolCats, err = c.PoolCategories(ctx); err != nil && oxibooru.StatusOf(err) != http.StatusForbidden {
			return l, err
		}
		return l, nil
	}, func(l loaded, err error) {
		a.connect.busy = false
		if err != nil {
			s.err = err
			a.sess = nil
			a.connect.err = err.Error()
			a.connect.url, a.connect.profile = p.URL, p.ID
			a.connect.endpoint = &oxibooru.Endpoint{API: p.API, Site: p.Site}
			a.connect.serverName = p.Name
			switch oxibooru.StatusOf(err) {
			case http.StatusUnauthorized, http.StatusForbidden:
				a.connect.step, a.connect.user = stepSignIn, p.User
				a.connect.err = "Your sign-in has expired. Sign in again."
				_ = a.opts.Secrets.Delete(tokenKey(p.ID))
			default:
				a.connect.step = stepServers
				a.connect.retry = &p
			}
			return
		}
		s.info, s.user = l.info, l.user
		a.setTagCategories(l.tagCats)
		s.poolCats = l.poolCats
		s.ready = true
		if l.info.Config.Name != "" && l.info.Config.Name != p.Name {
			s.profile.Name = l.info.Config.Name
			a.s.PutProfile(s.profile)
		}
		a.s.Active = p.ID
		a.save()
		a.connect = connectPage{}
		a.flushWaiting()
	})
}

// flushWaiting opens the files and links that waited for the server or
// the PIN.
func (a *App) flushWaiting() {
	if a.sess == nil || !a.sess.ready || a.lock.locked {
		return
	}
	waiting := a.waiting
	a.waiting = nil
	for _, w := range waiting {
		if strings.Contains(w, "://") {
			a.OpenURL(w)
		} else {
			a.Upload(w)
		}
	}
}

// setTagCategories keeps the tag categories in their order.
func (a *App) setTagCategories(cats []oxibooru.TagCategory) {
	slices.SortStableFunc(cats, func(x, y oxibooru.TagCategory) int {
		return cmp.Or(cmp.Compare(x.Order, y.Order), strings.Compare(x.Name, y.Name))
	})
	a.sess.tagCats = cats
}

// refreshCategories loads the categories again, after they changed.
func (a *App) refreshCategories() {
	type both struct {
		tags  []oxibooru.TagCategory
		pools []oxibooru.PoolCategory
	}
	async(a, func(ctx context.Context, c *oxibooru.Client) (both, error) {
		t, err := c.TagCategories(ctx)
		if err != nil {
			return both{}, err
		}
		p, err := c.PoolCategories(ctx)
		return both{t, p}, err
	}, func(b both, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		a.setTagCategories(b.tags)
		a.sess.poolCats = b.pools
	})
}

// signOut deletes the session's token and goes back to the servers.
func (a *App) signOut() {
	s := a.sess
	if s == nil {
		return
	}
	if creds := s.client.Credentials(); creds.Token != "" {
		go s.client.DeleteUserToken(context.WithoutCancel(s.ctx), creds.User, creds.Token)
	}
	_ = a.opts.Secrets.Delete(tokenKey(s.profile.ID))
	s.cancel()
	p := s.profile
	a.sess = nil
	a.s.Active = ""
	a.save()
	a.connect = connectPage{step: stepSignIn, url: p.URL, profile: p.ID, user: p.User,
		endpoint: &oxibooru.Endpoint{API: p.API, Site: p.Site}, serverName: p.Name}
}

// switchServer leaves the session for the list of servers.
func (a *App) switchServer() {
	if a.sess != nil {
		a.sess.cancel()
	}
	a.sess = nil
	a.s.Active = ""
	a.save()
	a.connect = connectPage{}
}

// Connecting.

type connectStep int

const (
	stepServers connectStep = iota
	stepSignIn
)

type authMode int

const (
	modeSignIn authMode = iota
	modeRegister
	modeReset
)

type connectPage struct {
	step       connectStep
	url        string
	profile    string // the ID of the profile signing in again, if any
	endpoint   *oxibooru.Endpoint
	serverName string
	retry      *settings.Profile

	mode       int
	user       string
	password   string
	password2  string
	email      string
	resetToken string
	resetSent  bool
	newPass    string

	busy bool
	err  string
}

func (a *App) connectView(c *ui.Context) {
	t := c.Theme()
	cp := &a.connect
	if a.sess != nil && !a.sess.ready {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Connecting to "+cmp.Or(a.sess.profile.Name, a.sess.profile.URL)+"…").TextColor(t.TextMuted)
			if ui.Button(c, "Cancel").Clicked() {
				a.switchServer()
			}
		})
		return
	}
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Height(max(bar.Height, 0)).DragWindow()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Width(420).Margin(48, ui.Auto, 32).Gap(20).Children(func() {
				ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconLibrary).FontSize(40).TextColor(t.Accent)
					ui.Text(c, "Maibooru").FontSize(26).Bold()
					ui.Text(c, "Your media library on Oxibooru").TextColor(t.TextMuted)
				})
				switch cp.step {
				case stepServers:
					a.serversStep(c)
				case stepSignIn:
					a.signInStep(c)
				}
				if cp.err != "" {
					ui.Row(c).Gap(8).Padding(10, 12).Radius(t.Radius).Background(t.Danger.Alpha(0.12)).Children(func() {
						ui.Icon(c, iconAlert).TextColor(t.Danger)
						ui.Text(c, cp.err).Wrap().Grow(1).Selectable()
					})
				}
			})
		})
	})
}

func (a *App) serversStep(c *ui.Context) {
	t := c.Theme()
	cp := &a.connect
	if len(a.s.Profiles) > 0 {
		card(c).Gap(0).Padding(4).Children(func() {
			for _, p := range a.s.Profiles {
				row := ui.ButtonBase(c.Key(p.ID)).Row().Gap(12).Padding(10, 12).Radius(t.Radius - 2).AlignItems(ui.Center).Label("Connect to " + cmp.Or(p.Name, p.URL))
				if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					ui.Icon(c, iconServer).FontSize(20).TextColor(t.TextMuted)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, cmp.Or(p.Name, p.URL)).Bold().SingleLine()
						who := "Guest"
						if p.User != "" {
							who = p.User
						}
						ui.Text(c, who+" · "+p.URL).FontSize(12).TextColor(t.TextMuted).SingleLine()
					})
					rm := ui.Button(c.Key("rm"), "").Label("Forget " + cmp.Or(p.Name, p.URL)).Tooltip("Forget this server").Children(func() {
						ui.Icon(c, iconClose)
					})
					if rm.Clicked() {
						id := p.ID
						a.confirm("Forget this server?", "Maibooru forgets "+cmp.Or(p.Name, p.URL)+" and its saved searches. The server keeps everything.", "Forget", func() {
							_ = a.opts.Secrets.Delete(tokenKey(id))
							a.s.RemoveProfile(id)
							a.save()
						})
					}
				})
				if row.Clicked() && !cp.busy {
					a.resume(p)
				}
			}
		})
	}
	if cp.retry != nil {
		p := *cp.retry
		if ui.Button(c, "Try again").Clicked() {
			cp.retry = nil
			a.resume(p)
		}
	}
	card(c).Children(func() {
		ui.Text(c, "Add a server").Bold()
		in := ui.TextInput(c, &cp.url).Placeholder("https://booru.example.com").Label("Server address").AutoFocus()
		next := ui.PrimaryButton(c, "Continue").Disabled(cp.busy || strings.TrimSpace(cp.url) == "")
		if (in.Submitted() || next.Clicked()) && !cp.busy && strings.TrimSpace(cp.url) != "" {
			a.discover()
		}
		if cp.busy {
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Looking for the server…").TextColor(t.TextMuted)
			})
		}
	})
	a.confirmView(c)
}

func (a *App) discover() {
	cp := &a.connect
	cp.busy, cp.err = true, ""
	raw := strings.TrimSpace(cp.url)
	httpClient := a.opts.HTTP
	go func() {
		ep, err := oxibooru.Discover(context.Background(), raw, oxibooru.WithHTTPClient(httpClient))
		a.post(func() {
			cp.busy = false
			if err != nil {
				cp.err = err.Error()
				return
			}
			cp.endpoint, cp.serverName, cp.step, cp.profile = ep, ep.Info.Config.Name, stepSignIn, ""
			cp.mode = int(modeSignIn)
		})
	}()
}

func (a *App) signInStep(c *ui.Context) {
	t := c.Theme()
	cp := &a.connect
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if iconButton(c, iconChevronL, "Back").Tooltip("Other servers").Clicked() {
			*cp = connectPage{url: cp.url}
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, cmp.Or(cp.serverName, "Oxibooru")).Bold().SingleLine()
			ui.Text(c, cp.endpoint.Site).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
	})
	card(c).Children(func() {
		ui.Segmented(c, &cp.mode, "Sign in", "Create account", "Reset password").FillWidth()
		switch authMode(cp.mode) {
		case modeSignIn:
			ui.TextInput(c.Key("user"), &cp.user).Placeholder("User name").Label("User name").AutoFocus().Autofill(ui.AutofillUsername)
			pw := ui.TextInput(c.Key("password"), &cp.password).Placeholder("Password").Label("Password").Password().Autofill(ui.AutofillPassword)
			ok := cp.user != "" && cp.password != "" && !cp.busy
			if (ui.PrimaryButton(c, "Sign in").Disabled(!ok).Clicked() || pw.Submitted()) && ok {
				a.signIn(false)
			}
		case modeRegister:
			ui.TextInput(c.Key("ruser"), &cp.user).Placeholder("User name").Label("User name").AutoFocus()
			ui.TextInput(c.Key("remail"), &cp.email).Placeholder("Email (optional)").Label("Email")
			ui.TextInput(c.Key("rpass"), &cp.password).Placeholder("Password").Label("Password").Password().Autofill(ui.AutofillNewPassword)
			ui.TextInput(c.Key("rpass2"), &cp.password2).Placeholder("Password again").Label("Password again").Password()
			ok := cp.user != "" && cp.password != "" && cp.password == cp.password2 && !cp.busy
			if cp.password2 != "" && cp.password != cp.password2 {
				ui.Text(c, "The passwords differ.").FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(4).Wrap().Children(func() {
				ui.Text(c, "Creating an account, you agree to the server's").FontSize(12).TextColor(t.TextMuted)
				ui.Link(c, "Terms of Service", strings.TrimSuffix(cp.endpoint.Site, "/")+"/help/tos").FontSize(12)
			})
			if ui.PrimaryButton(c, "Create account").Disabled(!ok).Clicked() && ok {
				a.signIn(true)
			}
		case modeReset:
			a.resetView(c)
		}
		if cp.busy {
			ui.Spinner(c)
		}
	})
	if ui.Button(c, "Continue as guest").Disabled(cp.busy).Clicked() {
		p := settings.Profile{ID: cp.profile, Name: cp.serverName, URL: cmp.Or(cp.url, cp.endpoint.Site), API: cp.endpoint.API, Site: cp.endpoint.Site}
		p.ID = a.s.PutProfile(p)
		a.save()
		a.start(p, oxibooru.Credentials{})
	}
}

func (a *App) resetView(c *ui.Context) {
	t := c.Theme()
	cp := &a.connect
	if cp.newPass != "" {
		ui.Text(c, "Your new password:").TextColor(t.TextMuted)
		ui.Text(c, cp.newPass).Bold().Selectable().FontSize(18)
		ui.Text(c, "Sign in with it, then change it in your account.").FontSize(12).TextColor(t.TextMuted).Wrap()
		if ui.Button(c, "Copy").Clicked() {
			c.WriteClipboard(cp.newPass)
		}
		return
	}
	ui.TextInput(c.Key("who"), &cp.user).Placeholder("User name or email").Label("User name or email").AutoFocus()
	if !cp.resetSent {
		ui.Text(c, "The server mails you a link with a token.").FontSize(12).TextColor(t.TextMuted)
		if ui.PrimaryButton(c, "Send the link").Disabled(cp.user == "" || cp.busy).Clicked() {
			a.requestReset()
		}
		return
	}
	ui.Text(c, "Paste the token from the link the server mailed you.").FontSize(12).TextColor(t.TextMuted).Wrap()
	ui.TextInput(c.Key("token"), &cp.resetToken).Placeholder("Token").Label("Token")
	if ui.PrimaryButton(c, "Reset password").Disabled(cp.resetToken == "" || cp.busy).Clicked() {
		a.confirmReset()
	}
}

func (a *App) anonymousClient() (*oxibooru.Client, error) {
	return oxibooru.New(a.connect.endpoint.API, a.connect.endpoint.Site, oxibooru.WithHTTPClient(a.opts.HTTP))
}

func (a *App) requestReset() {
	cp := &a.connect
	client, err := a.anonymousClient()
	if err != nil {
		cp.err = err.Error()
		return
	}
	cp.busy, cp.err = true, ""
	who := strings.TrimSpace(cp.user)
	go func() {
		err := client.RequestPasswordReset(context.Background(), who)
		a.post(func() {
			cp.busy = false
			if err != nil {
				cp.err = err.Error()
				return
			}
			cp.resetSent = true
		})
	}()
}

func (a *App) confirmReset() {
	cp := &a.connect
	client, err := a.anonymousClient()
	if err != nil {
		cp.err = err.Error()
		return
	}
	cp.busy, cp.err = true, ""
	who, tok := strings.TrimSpace(cp.user), strings.TrimSpace(cp.resetToken)
	if i := strings.LastIndex(tok, ":"); i >= 0 {
		// The link reads password-reset/<name>:<token>.
		tok = tok[i+1:]
	}
	go func() {
		pass, err := client.ConfirmPasswordReset(context.Background(), who, tok)
		a.post(func() {
			cp.busy = false
			if err != nil {
				cp.err = err.Error()
				return
			}
			cp.newPass = pass
		})
	}()
}

// signIn signs in with the password typed, creating the account first when
// register, then keeps a token in its place.
func (a *App) signIn(register bool) {
	cp := &a.connect
	client, err := a.anonymousClient()
	if err != nil {
		cp.err = err.Error()
		return
	}
	cp.busy, cp.err = true, ""
	user, pass, email := strings.TrimSpace(cp.user), cp.password, strings.TrimSpace(cp.email)
	go func() {
		ctx := context.Background()
		var err error
		if register {
			f := oxibooru.UserFields{Name: &user, Password: &pass}
			if email != "" {
				f.Email = &email
			}
			_, err = client.CreateUser(ctx, f)
		}
		var tok *oxibooru.UserToken
		if err == nil {
			client.SetCredentials(oxibooru.Credentials{User: user, Password: pass})
			var me *oxibooru.User
			if me, err = client.User(ctx, user, true); err == nil {
				user = me.Name
				tok, err = client.CreateUserToken(ctx, user, oxibooru.TokenFields{Enabled: new(true), Note: new(tokenNote())})
			}
		}
		a.post(func() {
			cp.busy = false
			if err != nil {
				if oxibooru.StatusOf(err) == http.StatusUnauthorized {
					cp.err = "The user name or the password is wrong."
				} else {
					cp.err = err.Error()
				}
				return
			}
			p := settings.Profile{ID: cp.profile, Name: cp.serverName, URL: cmp.Or(cp.url, cp.endpoint.Site), API: cp.endpoint.API, Site: cp.endpoint.Site, User: user}
			p.ID = a.s.PutProfile(p)
			a.save()
			if err := a.opts.Secrets.Set(tokenKey(p.ID), []byte(tok.Token)); err != nil {
				a.toast("The system keeps no secrets here: you will sign in again next time.")
			}
			cp.password, cp.password2 = "", ""
			a.start(p, oxibooru.Credentials{User: user, Token: tok.Token})
		})
	}()
}

// tokenNote names the tokens Maibooru makes, as the server lists them.
func tokenNote() string {
	host, _ := os.Hostname()
	if host == "" {
		return "Maibooru"
	}
	return "Maibooru on " + host
}

// card is a panel of the surface color.
func card(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(12).Padding(16).Radius(t.Radius+4).Background(t.Surface).Border(1, t.Border)
}
