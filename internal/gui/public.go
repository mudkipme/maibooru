package gui

import "strings"

// What the app's menus, the system and other instances ask of the window.
// Call these on the main thread, as the window's Update runs them.

// Upload queues files and folders to upload, and shows the queue.
func (a *App) Upload(paths ...string) {
	if len(paths) == 0 {
		return
	}
	if a.sess == nil || !a.sess.ready {
		// Opened with the app as it starts: they wait for the server.
		a.waiting = append(a.waiting, paths...)
		return
	}
	a.up.addFiles(paths)
	a.goUploads()
}

// OpenURL takes a link the system opened the app with: a maibooru:// link
// to a page of the app, or a web link to upload.
func (a *App) OpenURL(raw string) {
	if a.sess == nil || !a.sess.ready {
		a.waiting = append(a.waiting, raw)
		return
	}
	if rest, ok := strings.CutPrefix(raw, "maibooru://"); ok {
		a.router.Push("/" + strings.TrimPrefix(rest, "/"))
		return
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		a.up.addURLs([]string{raw})
		a.goUploads()
	}
}

// ChooseUploads asks for files to upload.
func (a *App) ChooseUploads() {
	if a.sess != nil && a.sess.ready {
		a.chooseUploads()
	}
}

// Reload loads the page shown again.
func (a *App) Reload() {
	if a.sess != nil && a.sess.ready {
		a.refresh()
	}
}

// Go shows a page of the app, as "/tags" or "/settings".
func (a *App) Go(path string) {
	if a.sess != nil && a.sess.ready {
		a.router.Push(path)
	}
}

// ToggleInspector shows or hides the inspector beside the library.
func (a *App) ToggleInspector() {
	a.s.HideInspector = !a.s.HideInspector
	a.save()
}

// Close saves what the app keeps.
func (a *App) Close() {
	a.save()
}
