// File picker example: the in-page web path via <input type="file">.
//
// The native file dialog here is provided by the webview, not by glaze:
//   - macOS:   by glaze's WKUIDelegate (WKWebView shows no dialog on its own)
//   - Linux:   by WebKitGTK's default run-file-chooser handler
//   - Windows: by WebView2 itself
//
// Selecting files updates the page and calls a bound Go function, so this also
// exercises the JS->Go bridge end to end with a slice argument.
//
// Note the browser only exposes the file NAMES to JavaScript, never their
// filesystem paths (a web security boundary). When you need full paths -- or
// want to save a file or choose a directory -- drive the dialog from Go with the
// programmatic API instead; see examples/filedialog.
package main

import (
	"log"
	"runtime"

	"github.com/crgimenes/glaze"
)

func init() { runtime.LockOSThread() }

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - File Picker")
	w.SetSize(560, 440, glaze.HintNone)

	// Bound so the selection round-trips to Go as well.
	if err := w.Bind("picked", func(names []string) {
		log.Println("picked files:", names)
	}); err != nil {
		log.Fatal(err)
	}

	w.SetHtml(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glaze File Picker</title>
  <style>
    body { font-family: -apple-system, system-ui, "Segoe UI", Roboto, sans-serif;
           background: #111827; color: #e5e7eb; padding: 2rem; }
    h1 { font-size: 20px; margin: 0 0 .5rem; }
    p { color: #9ca3af; }
    .btn { display: inline-block; margin-top: .5rem; }
    input[type=file] { color: #e5e7eb; }
    #out { margin-top: 1rem; padding: 1rem; background: #1f2937; border: 1px solid #374151;
           border-radius: 8px; white-space: pre-wrap; min-height: 2rem; }
  </style>
</head>
<body>
  <h1>File picker</h1>
  <p>Click below to open the native file dialog (multiple selection allowed).
     The browser exposes only file names here, not full paths.</p>
  <input type="file" id="f" multiple>
  <div id="out">No file selected yet.</div>
  <script>
    const f = document.getElementById("f");
    const out = document.getElementById("out");
    f.addEventListener("change", function () {
      const files = Array.from(f.files);
      out.textContent = files.length
        ? files.map(function (x) { return x.name + "  (" + x.size + " bytes)"; }).join("\n")
        : "(no file selected)";
      window.picked(files.map(function (x) { return x.name; }));
    });
  </script>
</body>
</html>`)

	w.Run()
}
