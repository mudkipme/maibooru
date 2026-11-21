// Package gui is Maibooru's interface, drawn in native UI: a library of the
// posts of an Oxibooru server, with the tags, pools and the rest of what the
// server keeps around them.
package gui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
	"github.com/mudkipme/maibooru/internal/cache"
	"github.com/mudkipme/maibooru/internal/oxibooru"
	"github.com/mudkipme/maibooru/internal/settings"
)

// Secrets keeps the users' tokens, as the system's credential store does.
type Secrets interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

// Desktop is what the app asks of the desktop: dialogs and other apps.
type Desktop interface {
	// OpenFiles asks the user for files to upload.
	OpenFiles() ([]string, error)
	// ChooseFolder asks the user for a folder to save into.
	ChooseFolder() (string, error)
	// SaveFile asks the user where to save a file named name.
	SaveFile(name string) (string, error)
	// OpenPath opens a file in the app the system has for it.
	OpenPath(path string) error
	// ShowInFolder shows a file in the file manager.
	ShowInFolder(path string)
	// OpenURL opens a link in the browser.
	OpenURL(url string) error
	// Notify tells the user something happened while they were in another
	// app; a click on it runs onClick on the main thread.
	Notify(title, body string, onClick func())
}

// Options are what the app is made of.
type Options struct {
	Store    *settings.Store
	Settings *settings.Settings
	Secrets  Secrets
	Cache    *cache.Cache
	Desktop  Desktop
	// Post runs a function on the main thread, then builds a frame: the
	// window's Update.
	Post func(func())
	// HTTP sends the app's requests, http.DefaultClient's transport when
	// nil.
	HTTP *http.Client
	// Version is the app's version, as shown in the settings.
	Version string
	// MediaPage makes the web page showing the content native UI can't,
	// nil where the system has no web view.
	MediaPage func() (MediaPage, error)
}

// App is the state of the window.
type App struct {
	opts   Options
	s      *settings.Settings
	router *ui.Router

	sess    *session
	connect connectPage

	pending    []ui.Toast
	mediaPage  MediaPage
	autoResume *settings.Profile
	spoilers   map[string]bool
	feeds      []*feed
	insp       inspector
	viewer     viewer
	up         uploads
	tags       tagsPage
	cats       categoriesPage
	pools      poolsPage
	users      usersPage
	account    accountPage
	history    historyPage
	comms      commentsPage
	prefs      settingsPage
	help       helpPage
	home       homePage

	searchText    string
	searchFocus   ui.Handle
	searchTags    completer
	lastQuery     string
	queryShown    bool
	savedOpen     bool
	waiting       []string // files and links opened before the session was ready
	active        bool     // the window has the keyboard
	clipboard     string
	saveAt        time.Time
	knownTags     map[string]string
	confirmDelete confirmation
	saveSearch    saveSearchDialog
	shortcutsOpen bool
	searchHelp    bool
}

// New returns the app, connecting to the server last used.
func New(o Options) *App {
	if o.HTTP == nil {
		o.HTTP = &http.Client{}
	}
	if o.Post == nil {
		o.Post = func(fn func()) { fn() }
	}
	a := &App{opts: o, s: o.Settings, router: ui.NewRouter("/"), knownTags: map[string]string{}}
	a.router.Transition = ui.TransitionNone
	a.up.app = a
	netimage.SetClient(o.HTTP)
	if p, ok := a.s.Profile(a.s.Active); ok {
		a.autoResume = p
	}
	return a
}

// save writes the settings.
func (a *App) save() {
	if err := a.opts.Store.Save(a.s); err != nil {
		slog.Error("saving the settings", "err", err)
	}
}

// post runs fn on the main thread.
func (a *App) post(fn func()) { a.opts.Post(fn) }

// toast shows a message at the bottom of the window.
func (a *App) toast(msg string) {
	a.pending = append(a.pending, ui.Toast{Title: msg})
}

// fail shows what went wrong.
func (a *App) fail(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	msg := err.Error()
	if errors.Is(err, oxibooru.ErrConflict) {
		msg = "Someone else changed this meanwhile. Reload and try again."
	}
	slog.Warn("request failed", "err", err)
	a.pending = append(a.pending, ui.Toast{Title: msg, Type: "error", Timeout: 8 * time.Second})
}

// async runs work on a goroutine with the session's client, then done on the
// main thread, unless the session ended meanwhile.
func async[T any](a *App, work func(ctx context.Context, c *oxibooru.Client) (T, error), done func(T, error)) {
	s := a.sess
	if s == nil {
		return
	}
	go func() {
		v, err := work(s.ctx, s.client)
		a.post(func() {
			if a.sess != s {
				return
			}
			done(v, err)
		})
	}()
}

// do runs work for its error alone, showing it if any, then done when it
// succeeded.
func (a *App) do(work func(ctx context.Context, c *oxibooru.Client) error, done func()) {
	async(a, func(ctx context.Context, c *oxibooru.Client) (struct{}, error) {
		return struct{}{}, work(ctx, c)
	}, func(_ struct{}, err error) {
		if err != nil {
			a.fail(err)
			return
		}
		if done != nil {
			done()
		}
	})
}

// View builds the window.
func (a *App) View(c *ui.Context) {
	switch a.s.Theme {
	case settings.ThemeLight:
		c.SetTheme(ui.LightTheme().WithPreferences(c.Preferences()))
	case settings.ThemeDark:
		c.SetTheme(ui.DarkTheme().WithPreferences(c.Preferences()))
	}
	c.SetMenuPresentation(ui.PopoverMenus)
	a.active = c.WindowActive()
	for _, t := range a.pending {
		c.AddToast(t)
	}
	a.pending = a.pending[:0]
	if a.clipboard != "" {
		c.WriteClipboard(a.clipboard)
		a.clipboard = ""
	}
	a.flushSave(c)
	if p := a.autoResume; p != nil {
		a.autoResume = nil
		a.resume(*p)
	}

	if a.sess == nil || !a.sess.ready {
		a.connectView(c)
		return
	}
	a.mainView(c)
}

// confirmation is an alert asking before something that can't be undone.
type confirmation struct {
	open    bool
	title   string
	message string
	button  string
	run     func()
}

func (a *App) confirm(title, message, button string, run func()) {
	a.confirmDelete = confirmation{open: true, title: title, message: message, button: button, run: run}
}

func (a *App) confirmView(c *ui.Context) {
	d := &a.confirmDelete
	if !d.open {
		return
	}
	switch ui.AlertDialog(c, &d.open, d.title, d.message, "Cancel", d.button) {
	case 1:
		d.open = false
		if d.run != nil {
			d.run()
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// pluralize returns "1 post" or "3 posts".
func pluralize(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%s %s", formatCount(n), many)
}

// formatCount writes a count with thousands separated.
func formatCount(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + formatCount(-n)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatBytes writes a size in bytes in the largest unit under 1024.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatTime writes a time as how long ago it was, for recent times, or
// its date.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return pluralize(int(d.Minutes()), "minute", "minutes") + " ago"
	case d < 24*time.Hour:
		return pluralize(int(d.Hours()), "hour", "hours") + " ago"
	case d < 7*24*time.Hour:
		return pluralize(int(d.Hours()/24), "day", "days") + " ago"
	case t.Year() == time.Now().Year():
		return t.Local().Format("Jan 2")
	}
	return t.Local().Format("Jan 2, 2006")
}

// uniq returns the strings without repeats, in their order, trimmed and
// without empty ones.
func uniq(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
