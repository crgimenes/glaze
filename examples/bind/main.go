package main

import (
	"log"
	"time"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

const html = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glaze Bind Example</title>
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
    .card { background: #1f2937; border: 1px solid #374151; border-radius: 10px; padding: 16px; margin-bottom: 12px; }
    .section-title { margin: 0 0 10px 0; font-size: 13px; color: #9ca3af; text-transform: uppercase; letter-spacing: 0.03em; }
    .row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    button { border: 1px solid #374151; border-radius: 8px; background: #2563eb; color: #fff; padding: 8px 12px; cursor: pointer; }
    button.secondary { background: transparent; color: #d1d5db; }
    .value { color: #9ca3af; overflow-wrap: anywhere; }
    .footer { padding: 14px 0; color: #9ca3af; font-size: 12px; }
    .small { margin: 2px 0; }
    @media (max-width: 560px) {
      .navbar-text { text-align: left; }
      .row { align-items: stretch; }
      button { width: 100%; }
      .value { width: 100%; }
    }
  </style>
</head>
<body>
  <nav class="navbar border-bottom">
    <div class="container nav-content">
      <span class="navbar-brand">Glaze - Bind</span>
      <span class="navbar-text">sethtml + bind</span>
    </div>
  </nav>

  <main class="container page">
    <div class="card">
      <p class="section-title">Counter</p>
      <div class="row">
        <button id="increment" class="secondary">Increment</button>
        <button id="decrement" class="secondary">Decrement</button>
        <span class="value">Counter: <strong id="counterResult">0</strong></span>
      </div>
    </div>
    <div class="card">
      <p class="section-title">Compute</p>
      <div class="row">
        <button id="compute">Compute 6 x 7</button>
        <span class="value">Result: <strong id="computeResult">not started</strong></span>
      </div>
    </div>
  </main>

  <footer class="border-top footer">
    <div class="container">
      <p class="small">Glaze bind example</p>
      <p class="small">Bindings: count and compute</p>
    </div>
  </footer>

  <script type="module">
    const getElements = ids => Object.assign({}, ...ids.map(id => ({ [id]: document.getElementById(id) })));
    const ui = getElements(["increment", "decrement", "counterResult", "compute", "computeResult"]);
    ui.increment.addEventListener("click", async () => {
      ui.counterResult.textContent = await window.count(1);
    });
    ui.decrement.addEventListener("click", async () => {
      ui.counterResult.textContent = await window.count(-1);
    });
    ui.compute.addEventListener("click", async () => {
      ui.compute.disabled = true;
      ui.computeResult.textContent = "running";
      ui.computeResult.textContent = await window.compute(6, 7);
      ui.compute.disabled = false;
    });
  </script>
</body>
</html>`

func main() {
	var count int64

	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - Bind")
	w.SetSize(800, 800, glaze.HintNone)
	w.SetHtml(html)

	// Binding for count which immediately returns.
	err = w.Bind("count", func(delta int64) int64 {
		count += delta
		return count
	})
	if err != nil {
		log.Fatal(err)
	}

	// Binding for compute which simulates a long computation.
	err = w.Bind("compute", func(a, b int) int {
		time.Sleep(1 * time.Second)
		return a * b
	})
	if err != nil {
		log.Fatal(err)
	}

	w.Run()
}
