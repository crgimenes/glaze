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

	w.SetTitle("Glaze - Simple")
	w.SetSize(480, 320, glaze.HintNone)
	w.SetHtml(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glaze Basic Example</title>
  <style>
    * { box-sizing: border-box; }
    html, body { width: 100%; overflow-x: hidden; }
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #111827;
      color: #e5e7eb;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
    }
    .container { width: min(100%, 860px); margin: 0 auto; padding: 0 16px; }
    .border-bottom { border-bottom: 1px solid #374151; }
    .border-top { border-top: 1px solid #374151; }
    .navbar { padding: 12px 0; }
    .nav-content {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 16px;
      flex-wrap: wrap;
    }
    .navbar-brand { font-weight: 700; color: #e5e7eb; }
    .navbar-text {
      color: #9ca3af;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      overflow-wrap: anywhere;
      text-align: right;
    }
    .page { flex: 1; padding-top: 24px; }
    .card { background: #1f2937; border: 1px solid #374151; border-radius: 10px; padding: 16px; }
    h1 { margin: 0 0 8px 0; font-size: 20px; }
    .muted { color: #9ca3af; }
    .footer { padding: 14px 0; color: #9ca3af; font-size: 12px; }
    .small { margin: 2px 0; }
    @media (max-width: 560px) {
      .page { padding-top: 16px; }
      .card { padding: 12px; }
      h1 { font-size: 18px; }
      .navbar-text { text-align: left; }
    }
  </style>
</head>
<body>
  <nav class="navbar border-bottom">
    <div class="container nav-content">
      <span class="navbar-brand">Glaze - Simple</span>
      <span class="navbar-text">sethtml only</span>
    </div>
  </nav>

  <main class="container page">
    <div class="card">
      <h1>Hello from Glaze</h1>
      <p class="muted">This is the smallest example using SetHtml only.</p>
    </div>
  </main>

  <footer class="border-top footer">
    <div class="container">
      <p class="small">Glaze simple example</p>
      <p class="small">No bindings and no HTTP server</p>
    </div>
  </footer>
</body>
</html>`)
	w.Run()
}
