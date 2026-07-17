// Command scheme demonstrates glaze's custom URL-scheme handler API
// (NewWithOptions + Options.SchemeHandlers). It serves an embedded single-page
// app from a portless "app://" origin that WebKit and WebView2 treat as a
// SECURE CONTEXT, so localStorage, crypto.subtle, and path-based routing all
// work with no loopback HTTP server and no TCP port.
//
// Run it from the examples module:
//
//	cd examples && go run ./scheme
package main

import (
	"embed"
	"log"
	"mime"
	"net/url"
	"path"
	"strings"

	"github.com/crgimenes/glaze"
)

//go:embed ui
var uiFS embed.FS

// serveAsset is the SchemeHandler: it maps a request URL to bytes + content
// type from the embedded ui/ directory, falling back to index.html for the root
// and for unknown extension-less routes (so client-side routing works). The
// same URL format arrives here on every platform (e.g. "app://home/app.js"),
// so the handler needs to understand only one shape.
func serveAsset(req *glaze.SchemeRequest) *glaze.SchemeResponse {
	name := assetName(req.URL)
	data, err := uiFS.ReadFile("ui/" + name)
	if err != nil {
		if path.Ext(name) != "" {
			return nil // a missing file with an extension is a real 404
		}
		data, err = uiFS.ReadFile("ui/index.html") // SPA fallback
		if err != nil {
			return nil
		}
		name = "index.html"
	}
	ct := mime.TypeByExtension(path.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return &glaze.SchemeResponse{Body: data, MIMEType: ct}
}

// assetName turns a request URL ("app://home/app.js" or a bare "/app.js") into
// a clean embedded-FS name, defaulting the root to index.html.
func assetName(reqURL string) string {
	p := reqURL
	if u, err := url.Parse(reqURL); err == nil && u.Path != "" {
		p = u.Path
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" || p == "." {
		return "index.html"
	}
	return p
}

func main() {
	w, err := glaze.NewWithOptions(glaze.Options{
		Debug: true,
		SchemeHandlers: map[string]glaze.SchemeHandler{
			"app": serveAsset,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - Custom URL scheme")
	w.SetSize(720, 560, glaze.HintNone)
	// A custom-scheme URL. The authority ("home") and path are preserved for the
	// handler on every backend; relative sub-resources (app.js) load from the
	// same secure origin.
	w.Navigate("app://home/index.html")
	w.Run()
}
