// Native menu example.
//
// Builds a glaze window and installs a native menu bar with the standalone
// github.com/crgimenes/glaze/menu package. Choosing a menu item runs a Go
// callback that talks back to the page; Quit ends the app. The menu package
// does not depend on glaze's WebView, so a game or any other NSApplication-based
// app would call menu.Set the same way against its own run loop.
//
//	go run ./examples/menu   (macOS; the menu bar is at the top of the screen)
package main

import (
	"log"
	"runtime"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/menu"
)

func init() { runtime.LockOSThread() }

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - Menu")
	w.SetSize(640, 420, glaze.HintNone)
	w.SetHtml(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Menu</title>
<style>
  body { font-family: -apple-system, system-ui, sans-serif; background:#111827; color:#e5e7eb; padding:2rem; }
  h1 { font-size: 20px; } code { color:#93c5fd; }
  #out { margin-top:1rem; padding:1rem; background:#1f2937; border:1px solid #374151; border-radius:8px; }
</style></head>
<body>
  <h1>Native menu bar</h1>
  <p>Use the menu bar at the top of the screen: <code>Glaze</code> and <code>Demo</code>.
     Each item calls back into Go.</p>
  <div id="out">No menu item chosen yet.</div>
  <script>function show(s){ document.getElementById('out').textContent = s; }</script>
</body></html>`)

	// Set on the main thread before Run, so no Dispatch is needed; the callbacks
	// then fire on the main thread when the app is running.
	_, err = menu.Set([]menu.Item{
		{Title: "Glaze", Submenu: []menu.Item{
			{Title: "About This Demo", OnClick: func() { w.Eval(`show('About: glaze/menu demo')`) }},
			{Separator: true},
			{Title: "Quit", Shortcut: "cmd+q", OnClick: func() { w.Terminate() }},
		}},
		{Title: "Demo", Submenu: []menu.Item{
			{Title: "Say Hello", Shortcut: "cmd+h", OnClick: func() { w.Eval(`show('Hello from the native menu!')`) }},
			{Title: "Clear", Shortcut: "cmd+k", OnClick: func() { w.Eval(`show('cleared')`) }},
			{Separator: true},
			{Title: "Disabled item", Disabled: true},
		}},
	}, menu.Options{})
	if err != nil {
		log.Fatal(err)
	}

	w.Run()
}
