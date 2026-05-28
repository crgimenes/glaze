# Glaze

Glaze is a desktop WebView binding for Go. It sits on top of [webview/webview](https://github.com/webview/webview) and [purego](https://github.com/ebitengine/purego) to keep CGo out of the picture.

It started as a fork of `go-webview` but has diverged enough to live as a separate codebase with its own goals and API.

## Why no CGo

Dragging a C toolchain into a Go project just to open a window with HTML breaks too much of what I like about the Go ecosystem -- easy cross-compilation, reproducible builds, `go install` that works for whoever clones the repo. With `purego` the native library is loaded at runtime via dlopen / LoadLibrary, and the binary ships everything embedded.

## What's in the box

- No CGo
- Windows, macOS, and Linux
- Native libraries embedded in the binary, extracted at runtime with BLAKE2b-256 integrity verification
- JavaScript to Go binding
- Helpers for common desktop patterns: `BindMethods`, `RenderHTML`, `AppWindow`
- Plays nicely with `go.work` multi-module setups

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

## Install

```bash
go get github.com/crgimenes/glaze@latest
```

To use the embedded native libraries:

```go
import _ "github.com/crgimenes/glaze/embedded"
```

## Hello world

```go
package main

import (
	"log"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
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
  - `OnReady` receives the browser URL (always `http://127.0.0.1:...`).
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

## Running the examples

From the repository root:

```bash
go run ./examples/simple
go run ./examples/bind
go run ./examples/zero_tcp
```

From each example directory:

```bash
cd examples/appwindow && go run .
cd examples/desktop && go run .
cd examples/filorepl && go run .
```

`examples/zero_tcp` shows a local-first UI built with `SetHtml + BindMethods` only -- no HTTP server, no loopback TCP gateway.

## Testing

Default tests (headless-safe):

```bash
go test ./...
```

GUI integration test:

```bash
go test -tags=integration -run TestWebview ./...
```

## Building on Windows

Use `windowsgui` to hide the console window:

```bash
go build -ldflags="-H windowsgui" .
```

## Project layout

- `webview.go` -- core API and binding internals
- `appwindow.go` -- desktop window + local HTTP server helper
- `helpers.go` -- utility helpers (`BindMethods`, `RenderHTML`)
- `embedded/` -- embedded native library assets per platform
- `examples/` -- runnable sample applications

## Library integrity verification

Glaze embeds native libraries and extracts them to disk before loading. By default the extraction target is a temp directory that may be writable by other processes -- which would let an attacker swap the library file.

To handle that, Glaze computes a BLAKE2b-256 hash of the embedded library bytes at runtime and verifies every extracted (or pre-existing) file against that hash before loading. If the hash doesn't match, extraction fails with an integrity error and the library is **not** loaded.

Extracted files get restricted permissions (`0500`, owner read+execute) inside a `0700` directory.

### Custom library directory

In production, use `ExtractTo` to place the library in a secure, application-controlled directory instead of the system temp:

```go
package main

import (
	"log"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/embedded"
)

func main() {
	// Extract to a directory with restricted access.
	if err := embedded.ExtractTo("/opt/myapp/lib"); err != nil {
		log.Fatal(err)
	}

	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Secure App")
	w.SetSize(800, 600, glaze.HintNone)
	w.SetHtml("<h1>Hello</h1>")
	w.Run()
}
```

When using `ExtractTo`, **don't** also `import _ "github.com/crgimenes/glaze/embedded"` -- the blank import fires `init()` which extracts to the default temp directory. Call `ExtractTo` explicitly instead.

## Acknowledgments

- [abemedia/go-webview](https://github.com/abemedia/go-webview) for the original Go binding base
- [webview/webview](https://github.com/webview/webview) for the native WebView implementation
- [purego](https://github.com/ebitengine/purego) for dynamic linking without CGo
