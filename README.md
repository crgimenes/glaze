*Notice*: This is a heavily modified hard fork of the original go-webview by abemedia.
If you are looking for the official version, please use: https://github.com/abemedia/go-webview/


# go-webview

Go bindings for [webview/webview](https://github.com/webview/webview) using [purego](https://github.com/ebitengine/purego), with **no CGO**, and prebuilt native libraries for Windows, macOS, and Linux.

## Features

- No CGO
- Cross-platform (Windows, macOS, Linux)
- Prebuilt dynamic libraries included
- Bind Go functions to JavaScript
- Fully embeddable in pure Go projects

## Basic Example

```go
package main

import (
	"log"

	"github.com/crgimenes/go-webview"
	_ "github.com/crgimenes/go-webview/embedded" // embed native library
)

func main() {
	w, err := webview.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Greetings")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml("Hello World!")
	w.Run()
}
```

See [./examples/bind](./examples/bind) for an example binding Go functions to JavaScript.

## Building for Windows

When building Windows apps, set the following flag: `-ldflags="-H windowsgui"`.

```bash
go build -ldflags="-H windowsgui" .
```

## Testing

Run unit tests (default, headless-safe):

```bash
go test ./...
```

Run GUI integration test (requires desktop session and window support):

```bash
go test -tags=integration -run TestWebview ./...
```

## Embedded Libraries

This package requires native WebView libraries per-platform. To embed them in your app import the `embedded` package.

```go
import _ "github.com/crgimenes/go-webview/embedded"
```

Or you can ship your application with `.dll`, `.so`, or `.dylib` files.
Ensure these are discoverable at runtime by placing them in the same folder as your executable.
For MacOS `.app` bundles, place the `.dylib` file into the `Frameworks` folder.

See the [`embedded`](./embedded) folder for pre-built libraries you can ship with your application.

## Helpers for Desktop Applications

### BindMethods

`BindMethods` automatically binds all exported methods of a Go struct as JavaScript functions. Method names are converted from CamelCase to snake_case with a prefix.

```go
type Store struct{}
func (s *Store) GetItems() []string   { return []string{"a", "b"} }
func (s *Store) AddItem(name string)  { /* ... */ }

// Binds: window.api_get_items(), window.api_add_item(name)
bound, err := webview.BindMethods(w, "api", &Store{})
```

### RenderHTML

`RenderHTML` renders a Go `html/template` to a string, suitable for `SetHtml()`. This lets you reuse Go template definitions without an HTTP server.

```go
tpl := template.Must(template.ParseFiles("ui.html"))
html, err := webview.RenderHTML(tpl, "main", data)
w.SetHtml(html)
```

### AppWindow

`AppWindow` wraps an `http.Handler` in a native window with a local loopback server. This is the easiest way to turn a web application (e.g. a devengine app) into a desktop app:

```go
err := webview.AppWindow(webview.AppOptions{
    Title:   "My App",
    Width:   1280,
    Height:  800,
    Debug:   true,
    Handler: mux, // your http.ServeMux
    OnReady: func(addr string) { fmt.Println("Serving on", addr) },
})
```

The server starts on a random port, the window opens, and when the user closes it the server shuts down automatically.

### Local-First Desktop Pattern

For local desktop apps without an HTTP server, expose Go services directly to JavaScript via `Bind`:

```go
store := &MyStore{}
w, _ := webview.New(true)
webview.BindMethods(w, "store", store) // JS calls Go directly
w.SetHtml(myHTML)
w.Run()
```

See [./examples/desktop](./examples/desktop) and [./examples/filorepl](./examples/filorepl) for complete working examples.

## Acknowledgements

- [webview/webview](https://github.com/webview/webview) — core native UI library
- [purego](https://github.com/ebitengine/purego) — pure-Go `dlopen` magic
