// Native file-dialog example: the programmatic path driven from Go.
//
// Demonstrates glaze's file-dialog API: OpenFile, OpenFiles, SaveFile and
// OpenDirectory. Each HTML button calls a bound Go function that opens the
// native dialog and returns the chosen path(s) back to the page.
//
// Unlike an in-page <input type="file"> (see examples/filepicker), these run
// from Go and return full filesystem PATHS, and can also save a file or choose
// a directory -- things the browser file input cannot do.
//
// The dialog methods block the calling goroutine, so they are called from Bind
// callbacks (which run on a background goroutine) - never from the UI thread.
//
// Native on every platform: macOS NSOpenPanel/NSSavePanel, Windows IFileOpenDialog/
// IFileSaveDialog (Common Item Dialog COM), Linux GtkFileChooserNative.
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

	w.SetTitle("Glaze - File Dialogs")
	w.SetSize(560, 460, glaze.HintNone)

	imageFilter := []glaze.FileFilter{
		{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "gif"}},
	}

	must(w.Bind("openFile", func() (string, error) {
		return w.OpenFile(glaze.FileDialogOptions{
			Title:   "Open an image",
			Filters: imageFilter,
		})
	}))
	must(w.Bind("openFiles", func() ([]string, error) {
		return w.OpenFiles(glaze.FileDialogOptions{Title: "Open one or more files"})
	}))
	must(w.Bind("saveFile", func() (string, error) {
		return w.SaveFile(glaze.FileDialogOptions{
			Title:    "Save as",
			Filename: "untitled.txt",
		})
	}))
	must(w.Bind("openDir", func() (string, error) {
		return w.OpenDirectory(glaze.FileDialogOptions{Title: "Choose a folder"})
	}))

	w.SetHtml(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glaze File Dialogs</title>
  <style>
    body { font-family: -apple-system, system-ui, "Segoe UI", Roboto, sans-serif;
           background: #111827; color: #e5e7eb; padding: 2rem; }
    h1 { font-size: 20px; margin: 0 0 .25rem; }
    p { color: #9ca3af; margin: 0 0 1rem; }
    button { font: inherit; margin: 0 .5rem .5rem 0; padding: .5rem .9rem;
             background: #2563eb; color: #fff; border: 0; border-radius: 8px; cursor: pointer; }
    button:hover { background: #1d4ed8; }
    #out { margin-top: 1rem; padding: 1rem; background: #1f2937; border: 1px solid #374151;
           border-radius: 8px; white-space: pre-wrap; min-height: 3rem; }
    .err { color: #fca5a5; }
  </style>
</head>
<body>
  <h1>File dialogs</h1>
  <p>Each button opens a native dialog from Go and shows the full path(s) it returned.</p>
  <button onclick="run(window.openFile, 'Open file')">Open file</button>
  <button onclick="run(window.openFiles, 'Open files')">Open files</button>
  <button onclick="run(window.saveFile, 'Save file')">Save file</button>
  <button onclick="run(window.openDir, 'Open directory')">Open directory</button>
  <div id="out">No dialog opened yet.</div>
  <script>
    const out = document.getElementById("out");
    async function run(fn, label) {
      out.className = "";
      out.textContent = label + ": opening...";
      try {
        const r = await fn();
        const text = Array.isArray(r) ? (r.length ? r.join("\n") : "(cancelled)")
                                      : (r ? r : "(cancelled)");
        out.textContent = label + ":\n" + text;
      } catch (e) {
        out.className = "err";
        out.textContent = label + " failed: " + e;
      }
    }
  </script>
</body>
</html>`)

	w.Run()
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
