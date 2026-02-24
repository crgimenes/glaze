// AppWindow Example
//
// This example demonstrates how to use webview.AppWindow to wrap a standard
// HTTP application as a native desktop window. It uses devengine's embedded
// Bootstrap 5 assets and Go templates rendered server-side — the same
// pattern used by edev and rpgstudios.
//
// The key difference from the other examples is that ALL rendering happens
// on the server side via Go templates served over HTTP. The webview is just
// a browser window pointing to http://127.0.0.1:{port}. No Bind is needed.
package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime"
	"strings"

	"github.com/crgimenes/devengine/assets"
	webview "github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

// pageData is passed to every template — mirrors the pattern used in edev/rpgstudios.
type pageData struct {
	Title   string
	Version string
	Items   []todoItem
}

type todoItem struct {
	ID   int
	Text string
	Done bool
}

// store is a simple in-memory TODO list (in a real app, use devengine/db).
var (
	store  []todoItem
	nextID = 1
)

// templates uses devengine's Bootstrap CSS/JS by referencing /assets/... paths,
// exactly like the partials in devengine/templates/partials/head.go.tmpl.
var templates = template.Must(template.New("").Parse(`
{{define "head"}}
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/assets/bootstrap/css/bootstrap.min.css">
<link rel="stylesheet" href="/assets/style.css">
<script defer src="/assets/bootstrap/js/bootstrap.bundle.min.js"></script>
{{end}}

{{define "index"}}
<!doctype html>
<html lang="en" data-bs-theme="dark">
<head>
  {{template "head"}}
  <title>{{.Title}}</title>
</head>
<body>
  <nav class="navbar navbar-expand-lg border-bottom">
    <div class="container">
      <span class="navbar-brand fw-bold">✅ {{.Title}}</span>
      <span class="navbar-text small text-muted">
        {{.Version}} • {{len .Items}} items
      </span>
    </div>
  </nav>

  <main class="container py-4">
    <div class="row justify-content-center">
      <div class="col-lg-6">

        <form method="POST" action="/add" class="mb-4">
          <div class="input-group">
            <input type="text" name="text" class="form-control form-control-lg"
                   placeholder="What needs to be done?" autofocus required>
            <button type="submit" class="btn btn-primary btn-lg">Add</button>
          </div>
        </form>

        {{if .Items}}
        <div class="list-group">
          {{range .Items}}
          <div class="list-group-item d-flex align-items-center gap-3">
            <form method="POST" action="/toggle" class="m-0">
              <input type="hidden" name="id" value="{{.ID}}">
              <button type="submit" class="btn btn-sm {{if .Done}}btn-success{{else}}btn-outline-secondary{{end}}">
                {{if .Done}}✓{{else}}&nbsp;{{end}}
              </button>
            </form>
            <span class="flex-grow-1 {{if .Done}}text-decoration-line-through text-muted{{end}}">
              {{.Text}}
            </span>
            <form method="POST" action="/delete" class="m-0">
              <input type="hidden" name="id" value="{{.ID}}">
              <button type="submit" class="btn btn-sm btn-outline-danger">✕</button>
            </form>
          </div>
          {{end}}
        </div>
        {{else}}
        <div class="text-center text-muted py-5">
          <div class="fs-1 mb-2">📋</div>
          <p>No tasks yet. Add one above!</p>
        </div>
        {{end}}

      </div>
    </div>
  </main>

  <footer class="border-top mt-5 py-3 text-center text-muted">
    <div class="container">
      <p class="mb-0 small">
        AppWindow example • devengine assets • glaze • Go {{.Version}}
      </p>
    </div>
  </footer>
</body>
</html>
{{end}}
`))

func render(w http.ResponseWriter, items []todoItem) {
	data := pageData{
		Title:   "Todo Desktop",
		Version: runtime.Version(),
		Items:   items,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "index", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func findByID(id int) int {
	for i, item := range store {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func main() {
	mux := http.NewServeMux()

	// Serve devengine's embedded static assets (Bootstrap CSS/JS, style.css).
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(assets.FS)))

	// Index page — renders the TODO list.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		render(w, store)
	})

	// Add a new item.
	mux.HandleFunc("POST /add", func(w http.ResponseWriter, r *http.Request) {
		text := strings.TrimSpace(r.FormValue("text"))
		if text != "" {
			store = append(store, todoItem{ID: nextID, Text: text})
			nextID++
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Toggle done state.
	mux.HandleFunc("POST /toggle", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscanf(r.FormValue("id"), "%d", &id)
		if i := findByID(id); i >= 0 {
			store[i].Done = !store[i].Done
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Delete an item.
	mux.HandleFunc("POST /delete", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscanf(r.FormValue("id"), "%d", &id)
		if i := findByID(id); i >= 0 {
			store = append(store[:i], store[i+1:]...)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// That's it! AppWindow does everything else.
	err := webview.AppWindow(webview.AppOptions{
		Title:   "Todo Desktop",
		Width:   800,
		Height:  600,
		Debug:   true,
		Handler: mux,
		OnReady: func(addr string) {
			log.Println("Serving on", addr)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
