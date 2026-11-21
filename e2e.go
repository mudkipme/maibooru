//go:build e2e

package main

import (
	"os"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"
)

// Built with -tags e2e, the app takes automation through the socket that
// $MYGO_AUTOMATION names, for end-to-end tests of the real window. Captures
// of native UI leave web views out: $MAIBOORU_E2E_MEDIA names a PNG file
// the viewer's web view writes what it shows into, every second.
func init() {
	out := os.Getenv("MAIBOORU_E2E_MEDIA")
	if out == "" {
		return
	}
	mediaPageMade = func(wv *mygo.WebView) {
		go func() {
			for range time.Tick(time.Second) {
				if wv.IsDestroyed() {
					return
				}
				if png, err := wv.CapturePage(); err == nil {
					os.WriteFile(out, png, 0o600)
				}
			}
		}()
	}
}
