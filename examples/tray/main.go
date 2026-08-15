// Tray + WebView: a native/tray menu-bar icon whose menu opens glaze windows.
//
// The tray owns the process's UI event loop (tray.Run on the locked main
// goroutine); glaze detects the already-running loop and cooperates: New
// builds the window on the UI thread and Run pumps events until the window
// closes. That is why the OnClick below can drive a webview synchronously —
// the tray stays responsive while the window lives, and closing the window
// does not stop the tray's loop.
//
// macOS and Windows only: native/tray has no Linux backend (a
// StatusNotifierItem tray means a D-Bus dependency it avoids), so tray.Run
// reports ErrUnsupported there.
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"runtime"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/native/tray"
)

func main() {
	runtime.LockOSThread()

	cfg := tray.Config{
		Icon:    discPNG(),
		Tooltip: "glaze tray example",
		Items: []tray.Item{
			{Title: "Open window", OnClick: openWindow},
			{Separator: true},
			{Title: "Quit", OnClick: func() { tray.Stop() }},
		},
	}
	err := tray.Run(cfg)
	if err != nil {
		log.Fatalf("tray: %v", err)
	}
}

// openWindow runs a full webview lifecycle from inside the tray's OnClick.
// Run returns when the user closes the window; the tray keeps running.
func openWindow() {
	w, err := glaze.New(false)
	if err != nil {
		log.Printf("webview: %v", err)
		return
	}
	defer w.Destroy()

	w.SetTitle("Glaze from the tray")
	w.SetSize(480, 320, glaze.HintNone)
	w.SetHtml(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Glaze from the tray</title>
  <style>
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #111827;
      color: #e5e7eb;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .card {
      background: #1f2937;
      border: 1px solid #374151;
      border-radius: 10px;
      padding: 24px 32px;
    }
    h1 { margin: 0 0 8px 0; font-size: 20px; }
    p { margin: 0; color: #9ca3af; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Opened from the tray</h1>
    <p>Close this window — the tray icon stays.</p>
  </div>
</body>
</html>`)
	w.Run()
}

// discPNG returns a small filled-circle PNG so the example has a tray icon
// without shipping an asset file.
func discPNG() []byte {
	const s = 44
	img := image.NewRGBA(image.Rect(0, 0, s, s))
	cx, cy, r := float64(s)/2, float64(s)/2, float64(s)/2-1
	for y := range s {
		for x := range s {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, color.RGBA{R: 45, G: 124, B: 240, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		log.Fatalf("encode icon: %v", err)
	}
	return buf.Bytes()
}
