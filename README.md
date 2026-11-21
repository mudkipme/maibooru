# Maibooru

A desktop app for keeping your media library on an
[Oxibooru](https://github.com/liamw1/oxibooru) or a
[Szurubooru](https://github.com/rr-/szurubooru) server. It is written in Go with
[MyGo](https://github.com/egoist/mygo)'s native UI: one small binary for
macOS, Windows and Linux, drawn on the GPU, with no webview.

Maibooru is made for one person managing their own collection, not for
browsing a public board: drag files in, tag them, sort them into pools, and
drag them back out into other apps.

## Features

- **Library**: every post in a grid that loads as you scroll, with search
  (tag completion, the full query syntax, a cheat sheet), filters by type
  and safety, sorting, adjustable thumbnails, and saved searches in the
  sidebar.
- **Inspector**: the chosen post's tags in their categories' colors, its
  safety, sources, description, flags, related posts and pools, edited in
  place; tags you add bring the tags they imply and suggest. Choose several
  posts to add or remove tags, set the safety, favorite, add to a pool,
  export or delete them all at once.
- **Viewer**: pictures fitted, fitted to their width or zoomed (pinch,
  Ctrl+wheel, + − 0) and panned, animated GIFs and PNGs, videos with
  sound through the system's media framework, and, in the system's web
  engine, AVIF and JPEG XL pictures, PDFs (with pdf.js) and Flash (with
  Ruffle, as Oxibooru's web client plays it). Notes show over the picture,
  and you draw, move, write, copy and paste them, zoomed or not. The
  comments are beside it. ← and → go through the posts of the search.
- **Uploads**: drop files, folders, links or pictures from a browser
  anywhere in the window, paste them, or open them with Maibooru. Tags and
  the pool of the page you drop on come along. Files the server already has
  are found before posting, and posts that look alike are pointed out.
- **Desktop**: drag posts into other apps as files, open them in their
  default app, save them anywhere, show them in the file manager, and keep
  the content of what you viewed on disk for next time.
- **Everything else the server keeps**: tags (aliases, categories,
  implications, suggestions, merging), tag and pool categories, pools
  (reordered by dragging), comments, favorites and scores, featured posts,
  the history of every change, users, your account, avatar and tokens, and
  sign-up and password reset.
- Comments, descriptions and notes in Markdown with the server's own tags:
  `@426` for a post, `#tag`, `+user`, spoilers and Shift JIS art.
- Help: the server's details, the search syntax of every list, the keys,
  the comments' format and the server's terms.
- Several servers, signed in with tokens kept in the system's credential
  store; light and dark themes that follow the system.

## Install

[Releases](https://github.com/mudkipme/maibooru/releases) have the app for
Linux (AppImage), macOS (disk image) and Windows (installer, or a zip to
run as it is), each for x86-64 and ARM64. The `nightly` pre-release is
built from every push to `main`. The apps are not signed yet: on macOS,
open the app with Control-click → Open the first time; on Windows, choose
More info → Run anyway.

Tagging `v<version>`, the version in `mygo.json`, publishes its release
(`.github/workflows/release.yml`).

`go tool mygo build` packages the app for the machine it runs on into
`build/`: an app bundle and disk image on macOS, an installer on Windows,
and `.deb`, `.rpm`, a tarball with `install.sh` and a PKGBUILD on Linux.
Any machine builds for the others:

```sh
go tool mygo build -platform darwin/universal,windows/amd64,linux/amd64
```

The app needs nothing more on macOS and Windows 11 (Windows 10 needs the
WebView2 Runtime for the formats the web engine shows). On Linux it needs
GTK 3, GStreamer's base plugins for videos (with `gstreamer1.0-libav` or
another H.264 decoder for MP4s), WebKitGTK 4.1 for AVIF, JPEG XL, PDFs and
Flash (without it, those show as their thumbnails, and open in other
apps), and a Secret Service (GNOME Keyring, KWallet, KeePassXC…) to stay
signed in. pdf.js and Ruffle load from cdnjs and unpkg the first time a
PDF or a Flash post shows.

## Develop

With [Go](https://go.dev/dl/) 1.27.4 or later:

```sh
go tool mygo dev     # runs the app, rebuilding it as you edit
go test ./...        # the client, the settings, the cache and the views
```

The views are tested without a window, against a fake server. Tests that
need a real server run when told where one is. Oxibooru's
`docker-compose.yml` starts one; make its first user, who becomes an
administrator, then:

```sh
MAIBOORU_TEST_SERVER=http://localhost:8080 \
MAIBOORU_TEST_USER=admin MAIBOORU_TEST_PASSWORD=… \
MAIBOORU_SHOTS=/tmp/shots \
go test ./...
```

`MAIBOORU_SHOTS`, when set, receives a picture of each screen the app test
goes through. The same tests run against a Szurubooru server, whose
resources carry numbered versions and whose posts have no descriptions. `.github/workflows/ci.yml` runs the tests and packages the
app on Linux, macOS and Windows.

The real window is driven from outside through MyGo's automation plugin,
which only builds with the `e2e` tag (`e2e.go`):

```sh
go build -tags e2e -o /tmp/maibooru-e2e .
MYGO_AUTOMATION=$XDG_RUNTIME_DIR/maibooru.sock MAIBOORU_E2E_MEDIA=/tmp/media.png /tmp/maibooru-e2e &
go run github.com/egoist/mygo/plugins/automation/cmd/mygo-automation skills   # how to drive it
```

Its captures show native UI alone; `MAIBOORU_E2E_MEDIA` receives what the
viewer's web view shows, every second.

### Layout

| Package | |
|---|---|
| `internal/oxibooru` | the REST API's client: every endpoint, uploads with progress, the query syntax and privileges |
| `internal/settings` | servers, saved searches and preferences, kept in a JSON file |
| `internal/cache` | the posts' content, downloaded once and kept on disk |
| `internal/gui` | the window: a view for each page, built from the app's state every frame |
| `main.go` | the window, menus, dialogs and what the system opens the app with |

## License

[MIT](LICENSE).
