*Notice*: This is a heavily modified fork of the original go-webview by abemedia.
If you are looking for the official version, please use: https://github.com/abemedia/go-webview


# go-webview

[![Go Reference](https://pkg.go.dev/badge/github.com/abemedia/go-webview.svg)](https://pkg.go.dev/github.com/abemedia/go-webview)
[![CI](https://github.com/abemedia/go-webview/actions/workflows/test.yml/badge.svg)](https://github.com/abemedia/go-webview/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/abemedia/go-webview)](https://goreportcard.com/report/github.com/abemedia/go-webview)

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
	"github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded" // embed native library
)

func main() {
	w := webview.New(true)
	w.SetTitle("Greetings")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml("Hello World!")
	w.Run()
	w.Destroy()
}
```

See [./examples/bind](./examples/bind) for an example binding Go functions to JavaScript.

## Building for Windows

When building Windows apps, set the following flag: `-ldflags="-H windowsgui"`.

```bash
go build -ldflags="-H windowsgui" .
```

## Embedded Libraries

This package requires native WebView libraries per-platform. To embed them in your app import the `embedded` package.

```go
import _ "github.com/abemedia/go-webview/embedded"
```

Or you can ship your application with `.dll`, `.so`, or `.dylib` files.
Ensure these are discoverable at runtime by placing them in the same folder as your executable.
For MacOS `.app` bundles, place the `.dylib` file into the `Frameworks` folder.

See the [`embedded`](./embedded) folder for pre-built libraries you can ship with your application.

## Acknowledgements

- [webview/webview](https://github.com/webview/webview) — core native UI library
- [purego](https://github.com/ebitengine/purego) — pure-Go `dlopen` magic
