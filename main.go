// Maibooru is a desktop library for the posts of an Oxibooru server: browse,
// search, tag and organize them, upload files by dragging them in, and drag
// posts out into other apps.
package main

import (
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/cache"
	"github.com/mudkipme/maibooru/internal/gui"
	"github.com/mudkipme/maibooru/internal/settings"
)

// mediaPageMade, when set, hears of the viewer's web view as it is made,
// for end-to-end tests.
var mediaPageMade func(*mygo.WebView)

func main() {
	mygo.App.SetName("Maibooru")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	store, prefs, err := settings.Open(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		slog.Error("reading the settings, starting afresh", "err", err)
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		cacheDir = filepath.Join(os.TempDir(), "maibooru-cache")
	}
	content := cache.New(filepath.Join(cacheDir, "content"))
	go content.Prune(int64(prefs.CacheLimitMB) << 20)

	// What goroutines post before the window is made waits for it.
	var (
		mu     sync.Mutex
		win    *mygo.Window
		queued []func()
	)
	post := func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		if win == nil {
			queued = append(queued, fn)
			return
		}
		win.Update(fn)
	}
	window := func() *mygo.Window {
		mu.Lock()
		defer mu.Unlock()
		return win
	}
	app := gui.New(gui.Options{
		Store:    store,
		Settings: prefs,
		Secrets:  mygo.Secrets,
		Cache:    content,
		Desktop:  desktop{win: window},
		Post:     post,
		Version:  mygo.App.Version(), // mygo.json's, which mygo build links in
		MediaPage: func() (gui.MediaPage, error) {
			w := window()
			if w == nil || !mygo.WebViewAvailable() {
				return nil, errors.ErrUnsupported
			}
			wv, err := w.NewWebView(mygo.WebViewOptions{
				Page:            mygo.PageOptions{Untrusted: true, Autoplay: mygo.AutoplayAllow},
				BackgroundColor: "#121214",
			})
			if err != nil {
				return nil, err
			}
			if mediaPageMade != nil {
				mediaPageMade(wv)
			}
			return mediaPage{wv}, nil
		},
	})

	// Files opened with the app, and given to a second instance, go to
	// the uploads.
	upload := func(paths []string) {
		var files []string
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				files = append(files, p)
			}
		}
		post(func() { app.Upload(files...) })
	}
	mygo.App.OnOpenFile(func(path string) { upload([]string{path}) })
	mygo.App.OnOpenURL(func(u string) { post(func() { app.OpenURL(u) }) })
	mygo.App.OnSecondInstance(func(args []string, dir string) {
		if w := window(); w != nil {
			w.Focus()
		}
		var paths []string
		for _, a := range args[min(1, len(args)):] {
			if !filepath.IsAbs(a) {
				a = filepath.Join(dir, a)
			}
			paths = append(paths, a)
		}
		upload(paths)
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { app.Close() })

	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			mygo.App.SetMenu(menu(app, post))
		}
		opts := mygo.WindowOptions{
			Title:     "Maibooru",
			Width:     1280,
			Height:    820,
			MinWidth:  880,
			MinHeight: 560,
			StateKey:  "main",
			Content:   ui.View(app.View),
		}
		if runtime.GOOS == "darwin" {
			opts.TitleBarStyle = mygo.TitleBarHidden
		}
		w := mygo.NewWindow(opts)
		mu.Lock()
		win = w
		for _, fn := range queued {
			w.Update(fn)
		}
		queued = nil
		mu.Unlock()
		if len(os.Args) > 1 {
			upload(os.Args[1:])
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// menu is the menu bar of macOS; the window takes the same keys elsewhere.
func menu(app *gui.App, post func(func())) *mygo.Menu {
	do := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { post(fn) }
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Upload Files…", Accelerator: "CmdOrCtrl+U", Click: do(app.ChooseUploads)},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Reload", Accelerator: "CmdOrCtrl+R", Click: do(app.Reload)},
			{Label: "Show or Hide Inspector", Click: do(app.ToggleInspector)},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Go", Submenu: []*mygo.MenuItem{
			{Label: "Overview", Accelerator: "CmdOrCtrl+1", Click: do(func() { app.Go("/") })},
			{Label: "All Posts", Accelerator: "CmdOrCtrl+2", Click: do(func() { app.Go("/posts") })},
			{Label: "Uploads", Accelerator: "CmdOrCtrl+3", Click: do(func() { app.Go("/uploads") })},
			{Label: "Tags", Accelerator: "CmdOrCtrl+4", Click: do(func() { app.Go("/tags") })},
			{Label: "Pools", Accelerator: "CmdOrCtrl+5", Click: do(func() { app.Go("/pools") })},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

// desktop is what the app asks of the desktop, through MyGo.
type desktop struct{ win func() *mygo.Window }

var mediaFilter = mygo.FileFilter{
	Name:       "Pictures, Videos and Documents",
	Extensions: []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "avif", "jxl", "mp4", "m4v", "mov", "webm", "swf", "pdf"},
}

func (d desktop) OpenFiles() ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: d.win(), Title: "Upload Files", ButtonLabel: "Upload", Multiple: true,
		Filters: []mygo.FileFilter{mediaFilter, {Name: "All Files", Extensions: []string{"*"}}},
	})
}

func (d desktop) ChooseFolder() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: d.win(), Title: "Save to Folder", ButtonLabel: "Save Here", Directory: true, CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (d desktop) SaveFile(name string) (string, error) {
	dir, _ := mygo.App.Path(mygo.PathDownloads)
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: d.win(), DefaultPath: filepath.Join(dir, name), CreateDirectories: true})
}

func (desktop) OpenPath(path string) error { return mygo.Shell.OpenPath(path) }

func (desktop) ShowInFolder(path string) { mygo.Shell.ShowItemInFolder(path) }

// mediaPage is the web view the viewer shows Flash, PDFs, AVIF and JPEG XL
// in.
type mediaPage struct{ *mygo.WebView }

func (p mediaPage) Load(html, baseURL string) { p.Page().LoadHTML(html, baseURL) }

func (desktop) OpenURL(url string) error { return mygo.Shell.OpenExternal(url) }

func (d desktop) Notify(title, body string, onClick func()) {
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "maibooru-uploads", Title: title, Body: body})
	n.OnClick(func() {
		if w := d.win(); w != nil {
			w.Focus()
			w.Update(onClick)
		}
	})
	go n.Show()
}
