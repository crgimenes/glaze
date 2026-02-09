package main

import (
	"time"

	"github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded"
)

const html = `
<div>
  <button id="increment">+</button>
  <button id="decrement">-</button>
  <span>Counter: <span id="counterResult">0</span></span>
</div>
<hr />
<div>
  <button id="compute">Compute</button>
  <span>Result: <span id="computeResult">(not started)</span></span>
</div>
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
    ui.computeResult.textContent = "(pending)";
    ui.computeResult.textContent = await window.compute(6, 7);
    ui.compute.disabled = false;
  });
</script>
`

func main() {
	var count int64

	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("Bind Example")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml(html)

	// Binding for count which immediately returns.
	err := w.Bind("count", func(delta int64) int64 {
		count += delta
		return count
	})
	if err != nil {
		panic(err)
	}

	// Binding for compute which simulates a long computation.
	err = w.Bind("compute", func(a, b int) int {
		time.Sleep(1 * time.Second)
		return a * b
	})
	if err != nil {
		panic(err)
	}

	w.Run()
}
