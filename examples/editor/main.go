// Editor example: the glaze/editor component editing Filo on the left and SQL
// on the right, with the theming hook demonstrated on the SQL side. Everything
// is embedded — no CDN, no framework, no files on disk.
package main

import (
	"fmt"
	"log"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/editor"
)

func main() {
	js, err := editor.JS("filo", "sql")
	if err != nil {
		log.Fatal(err)
	}

	w, err := glaze.New(false)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()
	w.SetTitle("glaze/editor")
	w.SetSize(1000, 560, glaze.HintNone)

	// The app side of the story: the page hands the current buffer to Go.
	err = w.Bind("report", func(kind, text string) {
		fmt.Printf("--- %s (%d bytes) ---\n%s\n", kind, len(text), text)
	})
	if err != nil {
		log.Fatal(err)
	}

	w.SetHtml(`<!DOCTYPE html><html><head><meta charset="utf-8"><style>
` + string(editor.CSS()) + `
body { margin: 0; display: flex; flex-direction: column; height: 100vh;
       background: #0b0f14; color: #c8d6e0;
       font: 13px ui-monospace, Menlo, Consolas, monospace; }
main { flex: 1; display: flex; gap: 8px; padding: 8px; min-height: 0; }
section { flex: 1; display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.ge { flex: 1; }
button { background: #101820; color: #70e0ff; border: 1px solid #22303c; padding: 4px 12px; cursor: pointer; }
/* Theming is CSS variables on the host: the SQL side goes green. */
#sq .ge { --ge-caret: #9fe6a0; --ge-t-k: #9fe6a0; }
</style></head><body>
<main>
  <section><div id="fl"></div><div><button onclick="report('filo', fe.getValue())">Print Filo buffer</button></div></section>
  <section id="sq"><div id="sm"></div><div><button onclick="report('sql', se.getValue())">Print SQL buffer</button></div></section>
</main>
<script>` + string(js) + `</script>
<script>
const fe = new GlazeEditor(document.getElementById('fl'), {
  language: 'filo',
  completions: ['self-x', 'self-y', 'self-hull', 'seek', 'fire', 'enemies'],
  value: '; a tiny filo program\n(def n 2)\n(if (> n 1)\n    (fire "missile" n))\n',
});
const se = new GlazeEditor(document.getElementById('sm'), {
  language: 'sql',
  value: "-- try Ctrl+Space, Cmd+/ and Tab\nSELECT id, name\n  FROM ships\n WHERE hull > 2; /* the\n survivors */\n",
});
fe.focus();
</script></body></html>`)
	w.Run()
}
