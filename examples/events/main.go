// Command events demonstrates github.com/crgimenes/glaze events: a publish/
// subscribe bridge between Go and JavaScript. The page emits "ui:greet" when you
// click the button (JS -> Go), Go replies with "go:reply" (Go -> JS), and a
// background goroutine emits "go:tick" once a second to show Emit is safe to call
// from any goroutine. Run it with:
//
//	cd examples && go run ./events
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/crgimenes/glaze"
)

const html = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Glaze - Events</title>
  <style>
    body {
      margin: 0; min-height: 100vh; display: flex; flex-direction: column;
      gap: 16px; align-items: center; justify-content: center;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #111827; color: #e5e7eb;
    }
    input, button { font-size: 16px; padding: 8px 12px; border-radius: 8px; border: 1px solid #374151; }
    input { background: #1f2937; color: #e5e7eb; }
    button { background: #2563eb; color: white; border: none; cursor: pointer; }
    .muted { color: #9ca3af; }
    #reply { min-height: 1.4em; font-weight: 600; }
  </style>
</head>
<body>
  <div>tick from Go: <span id="tick">-</span></div>
  <div>
    <input id="name" value="world" autofocus>
    <button id="greet">Greet from JS</button>
  </div>
  <div id="reply" class="muted">(no reply yet)</div>

  <script>
    // Go -> JS: react to events emitted by the Go side.
    glaze.events.on("go:tick", (n) => {
      document.getElementById("tick").textContent = n;
    });
    glaze.events.on("go:reply", (message) => {
      const el = document.getElementById("reply");
      el.textContent = message;
      el.classList.remove("muted");
    });

    // JS -> Go: emit an event the Go side is subscribed to.
    document.getElementById("greet").addEventListener("click", () => {
      const name = document.getElementById("name").value;
      glaze.events.emit("ui:greet", name);
    });
  </script>
</body>
</html>`

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - Events")
	w.SetSize(640, 480, glaze.HintNone)

	ev, err := glaze.NewEvents(w)
	if err != nil {
		log.Fatal(err)
	}

	// JS -> Go: when the page greets us, reply back to the page.
	ev.On("ui:greet", func(args ...json.RawMessage) {
		var name string
		if len(args) > 0 {
			_ = json.Unmarshal(args[0], &name)
		}
		log.Printf("greeted from JS with %q", name)
		_ = ev.Emit("go:reply", fmt.Sprintf("Hello, %s, from Go!", name))
	})

	w.SetHtml(html)

	// Go -> JS from a background goroutine: a once-a-second counter.
	go func() {
		for n := 1; ; n++ {
			time.Sleep(1 * time.Second)
			_ = ev.Emit("go:tick", n)
		}
	}()

	w.Run()
}
