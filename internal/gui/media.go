package gui

import (
	"html"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/oxibooru"
)

// MediaPage is a web page in the window that shows the content native UI
// can't: Flash with Ruffle, as Oxibooru's web client plays it, PDFs with
// pdf.js, and the pictures only the system's web engine decodes, as AVIF
// and JPEG XL.
type MediaPage interface {
	ui.NativeWebView
	// Load shows html, as a page at baseURL.
	Load(html, baseURL string)
}

// mediaHTML returns the page that shows a post's content, "" for content
// the page doesn't show.
func mediaHTML(p *oxibooru.Post, contentURL string) string {
	src := html.EscapeString(contentURL)
	var body string
	switch {
	case p.Type == oxibooru.TypeFlash:
		body = `<div id="stage"></div>
<script src="https://unpkg.com/@ruffle-rs/ruffle"></script>
<script>
window.RufflePlayer = window.RufflePlayer || {};
window.RufflePlayer.config = {autoplay: "on", unmuteOverlay: "hidden", letterbox: "on", backgroundColor: "#121214"};
window.addEventListener("load", () => {
  const player = window.RufflePlayer.newest().createPlayer();
  player.style.width = "100%"; player.style.height = "100%";
  document.getElementById("stage").appendChild(player);
  player.load("` + jsString(contentURL) + `");
});
</script>`
	case p.MimeType == "application/pdf":
		body = `<div id="pages"></div>
<script src="https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/pdf.min.js"></script>
<script>
pdfjsLib.GlobalWorkerOptions.workerSrc = "https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/pdf.worker.min.js";
pdfjsLib.getDocument("` + jsString(contentURL) + `").promise.then(async doc => {
  const pages = document.getElementById("pages");
  const ratio = window.devicePixelRatio || 1;
  for (let i = 1; i <= doc.numPages; i++) {
    const page = await doc.getPage(i);
    const width = Math.min(pages.clientWidth, 1100);
    const viewport = page.getViewport({scale: width / page.getViewport({scale: 1}).width});
    const canvas = document.createElement("canvas");
    canvas.width = viewport.width * ratio; canvas.height = viewport.height * ratio;
    canvas.style.width = viewport.width + "px"; canvas.style.height = viewport.height + "px";
    pages.appendChild(canvas);
    await page.render({canvasContext: canvas.getContext("2d"), viewport, transform: [ratio, 0, 0, ratio, 0, 0]}).promise;
  }
}).catch(e => { document.getElementById("pages").textContent = "This PDF doesn't open: " + e.message; });
</script>`
	case p.Type == oxibooru.TypeImage || p.Type == oxibooru.TypeAnimation:
		body = `<img src="` + src + `" alt="">`
	default:
		return ""
	}
	return `<!doctype html><html><head><meta charset="utf-8">
<style>
html, body { margin: 0; height: 100%; background: #121214; color: #ddd; font: 14px system-ui, sans-serif; }
body { display: flex; align-items: center; justify-content: center; overflow: auto; }
img { max-width: 100%; max-height: 100%; object-fit: contain; }
#stage { width: 100%; height: 100%; }
#pages { align-self: flex-start; width: 100%; display: flex; flex-direction: column; align-items: center; gap: 12px; padding: 12px 0; }
canvas { box-shadow: 0 2px 12px rgba(0,0,0,.5); background: #fff; }
</style></head><body>` + body + `</body></html>`
}

// jsString escapes s for a JavaScript string between double quotes, in a
// script element.
func jsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "<", `\x3c`, " ", ` `, " ", ` `)
	return r.Replace(s)
}

// webMedia shows a post's content in the media page, when the app has one;
// it reports whether it does.
func (a *App) webMedia(m *media, p *oxibooru.Post) bool {
	if a.opts.MediaPage == nil {
		return false
	}
	page := a.mediaPage
	if page == nil {
		var err error
		if page, err = a.opts.MediaPage(); err != nil || page == nil {
			return false
		}
		a.mediaPage = page
	}
	doc := mediaHTML(p, a.sess.client.DataURL(p.ContentURL))
	if doc == "" {
		return false
	}
	// The page is at the site, so that it reads the post's data as its
	// own, which the site serves without CORS.
	page.Load(doc, a.sess.client.SiteURL())
	m.web, m.blank = page, a.blankMedia
	return true
}

// blankMedia empties the media page, stopping what plays in it.
func (a *App) blankMedia() {
	if a.mediaPage != nil {
		a.mediaPage.Load(`<!doctype html><html><body style="background:#121214"></body></html>`, "")
	}
}
