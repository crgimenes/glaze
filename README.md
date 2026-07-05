# Glaze

Glaze is a desktop WebView binding for Go. It is a pure-Go port of [webview/webview](https://github.com/webview/webview) built on [purego](https://github.com/ebitengine/purego), keeping CGo out of the picture. Each backend talks to the WebView framework the OS already ships -- WKWebView on macOS, WebKitGTK on Linux, WebView2 on Windows -- so nothing native is bundled.

It started as a fork of `go-webview` but has diverged enough to live as a separate codebase with its own goals and API.

## Examples

| Desktop | Game of Life | Starfield |
| --- | --- | --- |
| [![Desktop example preview](imgs/desktop.gif)](examples/desktop/) | [![Game of Life example preview](imgs/gameoflife.gif)](examples/gameoflife/) | [![Starfield example preview](imgs/starfield.gif)](examples/starfield/) |

| Doom Fire | Mandelbrot | Falling Sand |
| --- | --- | --- |
| [![Doom Fire example preview](imgs/doomfire.gif)](examples/doomfire/) | [![Mandelbrot example preview](imgs/mandelbrot.gif)](examples/mandelbrot/) | [![Falling Sand example preview](imgs/fallingsand.gif)](examples/fallingsand/) |

| Raycasting | Filo REPL | |
| --- | --- | --- |
| [![Raycasting example preview](imgs/raycasting.gif)](examples/raycasting/) | [![Filo REPL example preview](imgs/filorepl.gif)](examples/filorepl/) | |

## Why no CGo

This is the whole point of the project, and it's the part that's easy to miss. Most native-WebView bindings reach for CGo, which quietly takes back the things that make Go pleasant to ship: cross-compiling suddenly needs a matching **C** cross-compiler for every target (mingw for Windows, a sysroot for Linux), builds stop being reproducible, and `go install` only works for people who already have that C toolchain set up.

glaze keeps CGo out entirely - with [purego](https://github.com/ebitengine/purego) it `dlopen`/`LoadLibrary`s the WebView the OS already ships, so there is no C compiler in the loop. What that buys you:

- **Cross-compile to every desktop from one machine** -- no C cross-toolchain, just `GOOS`/`GOARCH`:

  ```sh
  GOOS=windows GOARCH=amd64 go build   # from a Mac, from Linux, from anywhere
  GOOS=linux   GOARCH=arm64 go build
  GOOS=darwin  GOARCH=arm64 go build
  ```

- **`CGO_ENABLED=0` builds** -- reproducible output, and a `go get` / `go install` that just works for whoever clones the repo, with no compiler to install first.

One caveat, "self-contained" isn't misread: glaze does **not** bundle a browser engine - it is not Electron. The binary ships no native library and stays small, but it uses the *system* WebView at runtime, so the target machine needs that present: WebView2 on Windows (preinstalled on current Windows 10/11), WebKitGTK on Linux (a package), WKWebView on macOS (built in).

## What's in the box

- No CGo
- Windows, macOS, and Linux
- Zero bundled native libraries -- binds the OS WebView directly (WKWebView / WebKitGTK / WebView2)
- JavaScript to Go binding
- Helpers for common desktop patterns: `BindMethods`, `RenderHTML`, `AppWindow`, a Go↔JS `Events` bridge
- Native file dialogs (`OpenFile`/`OpenFiles`/`SaveFile`/`OpenDirectory`) and a reusable native menu bar (`glaze/menu`)
- Window control from Go: `SetTitle`, `SetSize`, `Focus` (explicit keyboard focus into the web content)
- `NewWindow(debug, window)` embeds the WebView into an existing native window (`New` is the create-a-window shortcut)
- Plays nicely with `go.work` multi-module setups

## Related: native

Glaze stays focused on the window and the WebView. OS features that aren't window-bound -- and especially the more platform-specific or less standardized ones, like desktop notifications and the system tray -- live in [`native`](https://github.com/crgimenes/native), a sibling collection of small, cgo-free packages on the same purego foundation: clipboard, single-instance locks, opening a URL or revealing a file, memory-mapped files, keeping the machine awake, and more. The two don't depend on each other -- an application imports each directly for what it needs; where a platform can't support something cleanly, the native package returns a clear `ErrUnsupported` instead of shipping something flaky.

## Install

```bash
go get github.com/crgimenes/glaze@latest
```

## Requirements

Glaze binds the WebView the operating system already provides; there is nothing to bundle, but that runtime must be present:

- **macOS** -- nothing extra. The Cocoa/WebKit frameworks ship with the OS.
- **Linux** -- a system WebKitGTK, GTK4 or GTK3; glaze detects which at runtime. The exact libraries and how to install or debug them are in [Linux shared libraries](#linux-shared-libraries) below.
- **Windows** -- the Microsoft Edge WebView2 Runtime (preinstalled on current Windows 10/11; otherwise install the Evergreen Runtime). It is located via the registry, and `New` returns an error if it is missing. To bundle zero native DLLs, glaze calls the runtime's internal environment-creation export directly instead of shipping `WebView2Loader.dll`; that export is undocumented and could change in a future Edge runtime (in which case `New` returns a clear error). See the note on `createEnvironment` in [webview2_windows.go](webview2_windows.go).

### Linux shared libraries

Linux is the hard case. Every distro packages WebKitGTK a little differently and glaze can't paper over all of it -- but what it needs is concrete. These are the exact sonames it tries to `dlopen` at startup. They have to be loadable by the dynamic linker (on the default search path or in the `ldconfig` cache, or in `LD_LIBRARY_PATH`) and the **same architecture as your binary** -- a 64-bit Go build needs 64-bit libraries.

Always loaded:

- `libglib-2.0.so.0`
- `libgobject-2.0.so.0`

`libwebkitgtk-6.0.so.4` decides the stack: if it loads, glaze uses GTK4; otherwise GTK3. It never loads both -- most desktops have GTK3 and GTK4 installed side by side, and pulling both into one process corrupts GTK's type system and crashes `gtk_init`.

- GTK4: `libgtk-4.so.1`, `libwebkitgtk-6.0.so.4`, `libjavascriptcoregtk-6.0.so.1`
- GTK3: `libgtk-3.so.0`, `libwebkit2gtk-4.1.so.0` (or `libwebkit2gtk-4.0.so.37`), `libjavascriptcoregtk-4.1.so.0` (or `libjavascriptcoregtk-4.0.so.18`)

On the GTK4 stack, the file dialogs additionally load `libgio-2.0.so.0` the
first time a dialog opens (it ships with GLib, so it is present wherever the
libraries above are).

Installing the WebKitGTK package pulls GTK and GLib in as dependencies:

- Debian / Ubuntu: `apt install libwebkit2gtk-4.1-0` (GTK3) or `libwebkitgtk-6.0-4` (GTK4)
- Fedora: `dnf install webkit2gtk4.1` or `webkitgtk6.0`
- Arch: `pacman -S webkit2gtk-4.1` or `webkitgtk-6.0`
- Nix / NixOS: these libraries are not on the default loader path, so a bare `go run` outside a shell that provides them fails to load. Add `webkitgtk_4_1` (or `webkitgtk_6_0`) to your `buildInputs` / dev shell, or expose them through `LD_LIBRARY_PATH` or `nix-ld`.

If `New` returns `webview: none of [...] could be loaded`, the linker can't find that soname. See what's actually visible to it:

```bash
ldconfig -p | grep -E 'libwebkit(2)?gtk|libjavascriptcoregtk|libgtk-[34]'
```

`wrong ELF class: ELFCLASS32` means the library was found but in the wrong architecture -- a 64-bit binary was pointed at 32-bit libraries (check your `LD_LIBRARY_PATH`).

The test suite reflects this: the GUI tests skip themselves when none of these libraries can load, so `go test ./...` stays green on a box without WebKitGTK instead of failing.

## Hello world

```go
package main

import (
	"log"

	"github.com/crgimenes/glaze"
)

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze")
	w.SetSize(800, 600, glaze.HintNone)
	w.SetHtml("<h1>Hello from Glaze</h1>")
	w.Run()
}
```

Glaze pins the goroutine that creates the first window to its current OS thread. Keep direct window calls on that goroutine, and use `Dispatch` to re-enter the UI thread from background work.

## Desktop helpers

### BindMethods

A convenience layer over `Bind` that exposes every exported method of a Go value as a JavaScript-callable function.

What it does:

- Reflects over the exported methods of a struct or pointer receiver.
- Builds JavaScript names with a prefix and snake_case conversion.
  - Example: `GetUserByID` with prefix `api` becomes `api_get_user_by_id`.
- Applies the same signature rules as `Bind`: no return, value, error, value and error.
- Returns the list of registered names so you can log or verify them.

Useful when you have a service object and want to expose a consistent JavaScript API without writing one `Bind` call per method.

```go
type Store struct{}

func (s *Store) GetItems() []string { return []string{"a", "b"} }

bound, err := glaze.BindMethods(w, "store", &Store{})
```

### RenderHTML

Renders a named Go `html/template` to a string you can pass to `SetHtml`.

What it does:

- Runs a specific template (nested calls included).
- Returns the final HTML string.
- Wraps execution errors with template context.

Useful when you want server-style template rendering in a local desktop app without running an HTTP server for that page.

```go
html, err := glaze.RenderHTML(tpl, "page", data)
if err != nil {
	return err
}
w.SetHtml(html)
```

### AppWindow

Wraps an `http.Handler` inside a native desktop window backed by a local loopback HTTP server.

What it does:

- Selectable transport with platform-aware default:
  - `auto` (default): `unix` on macOS/Linux, `tcp` on Windows
  - `tcp`: direct loopback HTTP (`127.0.0.1`)
  - `unix`: handler served on a Unix socket with a lightweight loopback HTTP gateway for browser navigation
- Starts listeners on random free ports/paths by default (or a custom `Addr` / `UnixSocketPath`).
- Creates a native window and navigates it to that local URL.
- Runs the UI loop and shuts down the HTTP server when the window exits.
- Supports window sizing, title, debug mode, and an optional readiness callback.
  - `OnReady` receives the browser URL (loopback; `http://127.0.0.1:...`, or `http://[::1]:...` if you pass an IPv6 `Addr`).
  - `OnReadyInfo` receives the resolved backend details (`Transport`, `Backend`, `Gateway`) so you can verify unix vs tcp from logs.

The shortest path from an existing `net/http` app to a desktop app, with minimal changes to routing, templates, and assets.

```go
err := glaze.AppWindow(glaze.AppOptions{
	Title:     "My App",
	Width:     1280,
	Height:    800,
	Transport: glaze.AppTransportAuto,
	Handler:   mux,
	OnReadyInfo: func(info glaze.AppReadyInfo) {
		log.Printf("transport=%s backend=%s gateway=%s", info.Transport, info.Backend, info.Gateway)
	},
})
```

### Events

A lightweight publish/subscribe bridge between Go and JavaScript, layered on
`Bind`/`Init`/`Eval` with no extra native code. Create one per window, then emit
and subscribe on either side; an event reaches every listener on both sides
exactly once.

```go
ev, err := glaze.NewEvents(w)
if err != nil {
	log.Fatal(err)
}

// Go subscribes; each argument arrives as raw JSON to decode as you like.
ev.On("ui:save", func(args ...json.RawMessage) {
	var name string
	_ = json.Unmarshal(args[0], &name)
	log.Println("save requested for", name)
})

// Go emits to JS — safe to call from any goroutine.
_ = ev.Emit("app:ready", map[string]any{"version": 3})
```

```js
// JS subscribes to Go events and emits its own.
glaze.events.on("app:ready", (info) => console.log("ready", info.version));
glaze.events.emit("ui:save", "untitled.txt");
```

`On` returns a function that cancels that one subscription; `Off(name)` drops all
of them. Go handlers run on the goroutine that emitted (or the binding goroutine
for events coming from JS), so re-enter the UI thread with `Dispatch` if a handler
touches the window. See [examples/events](examples/events/).

### File dialogs

Native open/save/directory dialogs, exposed on the `WebView` interface (a glaze
extension; upstream webview has none):

```go
path, _  := w.OpenFile(glaze.FileDialogOptions{
    Title:   "Open an image",
    Filters: []glaze.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}},
})
paths, _ := w.OpenFiles(glaze.FileDialogOptions{})                     // multi-select
saveTo, _ := w.SaveFile(glaze.FileDialogOptions{Filename: "untitled.txt"})
dir, _   := w.OpenDirectory(glaze.FileDialogOptions{})
```

Backends: `NSOpenPanel`/`NSSavePanel` (macOS), `IFileOpenDialog`/`IFileSaveDialog`
(Windows), `GtkFileChooserNative` (Linux). Each shows the modal dialog, blocks the
calling goroutine, and returns the chosen path(s) or `""` on cancel. Call them
from `Bind` callbacks (a background goroutine), never from the UI thread. See
[examples/filedialog](examples/filedialog/).

### Native menus

[`github.com/crgimenes/glaze/menu`](menu/) installs a native menu bar. It depends
only on purego, not on the WebView, so a game or any other window-owning app can
use it the same way.

```go
menu.Set([]menu.Item{
    {Title: "App", Submenu: []menu.Item{
        {Title: "About", OnClick: showAbout},
        {Separator: true},
        {Title: "Quit", Shortcut: "cmd+q", OnClick: w.Terminate},
    }},
    {Title: "Edit", Submenu: []menu.Item{
        {Title: "Copy", Shortcut: "cmd+c", OnClick: doCopy},
    }},
}, menu.Options{Window: w.Window()})
```

macOS (`NSMenu`) and Windows (Win32 menu bar) are implemented; Linux returns
`ErrUnsupported`. See [examples/menu](examples/menu/).

## Running the examples

`examples/` is a separate Go module (it keeps the library's `go.mod`
purego-only), so run the examples from inside it:

```bash
cd examples
go run ./simple
go run ./bind
go run ./zero_tcp
```

Or from each example directory:

```bash
cd examples/appwindow && go run .
cd examples/desktop && go run .
cd examples/filorepl && go run .
```

`examples/zero_tcp` shows a local-first UI with no HTTP server and no loopback
TCP gateway: it stages the frontend to disk, navigates to a `file://` URL, and
talks to Go through `BindMethods` alone.

## Testing

```bash
go test ./...
```

This runs the pure-logic unit tests (binding marshalling, transport selection)
plus the per-platform GUI smoke tests, which drive a real WebView
(WKWebView / WebKitGTK / WebView2). Those GUI tests **skip themselves** when the
system WebView can't run here -- no display, or the libraries aren't installed
(WebKitGTK on Linux, the Edge WebView2 Runtime on Windows) -- so the command
above stays green on a headless or minimal box instead of failing.

For a fast, headless run, `-short` skips the GUI scenarios on every platform
(each drives a real run loop and can take a few seconds):

```bash
go test -short ./...
```

To actually exercise the GUI tests on Linux, install WebKitGTK and run under a
virtual display:

```bash
xvfb-run -a go test ./...
```

## Building on Windows

Use `windowsgui` to hide the console window:

```bash
go build -ldflags="-H windowsgui" .
```

## Project layout

- `webview_common.go` -- the `WebView` interface, function-wrapper, and JS marshalling
- `webview_bridge.go` / `webview_bridge_webkit.go` -- the injected JS bridge (init/bind scripts)
- `webview_darwin.go` / `webview_linux.go` / `webview_windows.go` (+ `webview2_windows.go`, `putbounds_amd64.go`, `putbounds_arm64.go`) -- the per-OS pure-Go backends
- `appwindow.go` -- desktop window + local HTTP server helper
- `dialog.go` / `dialog_darwin.go` / `dialog_windows.go` / `dialog_linux.go` -- native file dialogs
- `helpers.go` -- utility helpers (`BindMethods`, `RenderHTML`)
- `events.go` -- the Go↔JS publish/subscribe events bridge (`NewEvents`)
- `menu/` -- the standalone native menu-bar package (`github.com/crgimenes/glaze/menu`)
- `examples/` -- runnable sample applications (their own Go module)

Glaze loads the OS WebView framework directly and bundles or extracts no native library, so there is no extracted file to verify or swap.

## Acknowledgments

- [abemedia/go-webview](https://github.com/abemedia/go-webview) for the original Go binding base
- [webview/webview](https://github.com/webview/webview) for the original C++ WebView implementation this is ported from
- [purego](https://github.com/ebitengine/purego) for dynamic linking without CGo

---

## More of my projects

- [filo](https://github.com/crgimenes/filo): a small scripting language safe to embed in Go programs.
- [kutta](https://github.com/crgimenes/kutta): a 2D wind tunnel; watch air misbehave around an airfoil.
- [neko](https://github.com/crgimenes/neko): the classic desktop cat chasing your pointer, in Go.
- [minigui](https://github.com/crgimenes/minigui): a tiny immediate-mode GUI for Ebitengine.

More at [github.com/crgimenes](https://github.com/crgimenes) and [crg.eti.br](https://crg.eti.br).
