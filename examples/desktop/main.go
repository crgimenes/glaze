// Desktop Example with devengine
//
// This example demonstrates a local-first desktop application that uses
// devengine components directly from a webview window:
//
//   - devengine/db: SQLite database (WAL, RW/RO pools, no CGO)
//   - devengine/assets: Bootstrap 5 CSS/JS, style.css (dark theme)
//   - devengine/templates: Go HTML templates with partials (head, footer, scripts)
//
// A lightweight local HTTP server serves the embedded assets so that
// devengine's template partials (which reference /assets/...) work unmodified.
// No session, auth, or middleware is needed — the app runs on loopback.
package main

import (
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/crgimenes/devengine/assets"
	"github.com/crgimenes/devengine/db"
	webview "github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

// NoteService wraps devengine's SQLite to provide note CRUD operations.
// Its exported methods are bound to JavaScript via BindMethods.
type NoteService struct {
	store *db.SQLite
}

type Note struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
}

func (s *NoteService) Add(text string) (Note, error) {
	err := s.store.Exec(
		`INSERT INTO notes (text) VALUES (?)`, text,
	)
	if err != nil {
		return Note{}, err
	}
	var note Note
	row := s.store.QueryRowRW(`SELECT id, text FROM notes ORDER BY id DESC LIMIT 1`)
	if err := row.Scan(&note.ID, &note.Text); err != nil {
		return Note{}, err
	}
	return note, nil
}

func (s *NoteService) List() ([]Note, error) {
	rows, err := s.store.Query(`SELECT id, text FROM notes ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Text); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func (s *NoteService) Delete(id int64) error {
	return s.store.Exec(`DELETE FROM notes WHERE id = ?`, id)
}

func (s *NoteService) Count() (int, error) {
	var count int
	row := s.store.QueryRow(`SELECT COUNT(*) FROM notes`)
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// pageTemplate is the main HTML template using Bootstrap 5 dark mode,
// served via the local HTTP server alongside devengine's embedded assets.
var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en" data-bs-theme="dark">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="/assets/bootstrap/css/bootstrap.min.css">
  <link rel="stylesheet" href="/assets/style.css">
  <title>Desktop Notes</title>
  <style>
    .note-item { transition: all 0.2s ease; }
    .note-item:hover { transform: translateX(4px); }
    .btn-del { opacity: 0.5; transition: opacity 0.2s; }
    .btn-del:hover { opacity: 1; }
  </style>
</head>
<body>
  <nav class="navbar navbar-expand-lg border-bottom mb-4">
    <div class="container">
      <span class="navbar-brand fw-bold">📝 Desktop Notes</span>
      <span class="navbar-text small text-muted">devengine/db + glaze</span>
    </div>
  </nav>

  <main class="container">
    <div class="row justify-content-center">
      <div class="col-lg-8">
        <div class="card border-0 shadow-sm mb-4">
          <div class="card-body">
            <div class="input-group">
              <input type="text" id="input" class="form-control form-control-lg"
                     placeholder="What's on your mind?" autofocus>
              <button id="add" class="btn btn-primary btn-lg px-4">Add</button>
            </div>
          </div>
        </div>

        <div id="notes"></div>

        <p id="count" class="text-muted text-center mt-3 small"></p>
      </div>
    </div>
  </main>

  <footer class="border-top mt-5 py-3 text-center text-muted">
    <div class="container">
      <p class="mb-0 small">Powered by devengine (SQLite, no CGO) • glaze (purego, no CGO)</p>
    </div>
  </footer>

  <script defer src="/assets/bootstrap/js/bootstrap.bundle.min.js"></script>
  <script type="module">
    const input = document.getElementById("input");
    const addBtn = document.getElementById("add");
    const notesList = document.getElementById("notes");
    const countEl = document.getElementById("count");

    async function refresh() {
      const notes = await window.notes_list();
      const count = await window.notes_count();

      if (!notes || notes.length === 0) {
        notesList.innerHTML =
          '<div class="text-center text-muted py-5">' +
            '<div class="fs-1 mb-2">📋</div>' +
            '<p>No notes yet. Type something above!</p>' +
          '</div>';
      } else {
        notesList.innerHTML = notes.map(n =>
          '<div class="card border-0 shadow-sm mb-2 note-item">' +
            '<div class="card-body d-flex align-items-center py-2 px-3">' +
              '<span class="flex-grow-1">' + escapeHtml(n.text) + '</span>' +
              '<button class="btn btn-sm btn-outline-danger btn-del ms-2" data-id="' + n.id + '">' +
                '✕' +
              '</button>' +
            '</div>' +
          '</div>'
        ).join("");

        notesList.querySelectorAll(".btn-del").forEach(btn => {
          btn.addEventListener("click", async () => {
            await window.notes_delete(Number(btn.dataset.id));
            await refresh();
          });
        });
      }

      const label = count === 1 ? "note" : "notes";
      countEl.textContent = count + " " + label + " stored in SQLite";
    }

    function escapeHtml(text) {
      const div = document.createElement("div");
      div.textContent = text;
      return div.innerHTML;
    }

    async function addNote() {
      const text = input.value.trim();
      if (!text) return;
      await window.notes_add(text);
      input.value = "";
      await refresh();
      input.focus();
    }

    addBtn.addEventListener("click", addNote);
    input.addEventListener("keydown", e => {
      if (e.key === "Enter") addNote();
    });

    refresh();
  </script>
</body>
</html>`))

// startAssetServer starts a local HTTP server that serves devengine's
// embedded assets and the main page template. Returns the base URL.
func startAssetServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	mux := http.NewServeMux()

	// Serve devengine embedded assets (Bootstrap CSS/JS, style.css, etc.)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(assets.FS)))

	// Serve the main page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pageTemplate.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	go func() {
		srv := &http.Server{Handler: mux}
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), nil
}

func main() {
	// Open SQLite using devengine's db package — same as edev/rpgstudios use.
	store, err := db.NewWithPath("desktop_notes.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Create the notes table if it doesn't exist.
	err = store.Exec(`CREATE TABLE IF NOT EXISTS notes (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		text TEXT NOT NULL
	)`)
	if err != nil {
		log.Fatal(err)
	}

	svc := &NoteService{store: store}

	// Start a minimal local HTTP server for devengine's embedded assets.
	baseURL, err := startAssetServer()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Asset server:", baseURL)

	w, err := webview.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Desktop Notes")
	w.SetSize(640, 600, webview.HintNone)

	// Bind all exported methods of NoteService as JS functions:
	// window.notes_add, window.notes_list, window.notes_delete, window.notes_count
	bound, err := webview.BindMethods(w, "notes", svc)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Bound functions:", strings.Join(bound, ", "))

	// Navigate to the local server (templates reference /assets/... for Bootstrap).
	w.Navigate(baseURL)
	w.Run()
}

// Ensure NoteService implements the methods at compile time.
var _ interface {
	Add(string) (Note, error)
	List() ([]Note, error)
	Delete(int64) error
	Count() (int, error)
} = (*NoteService)(nil)
