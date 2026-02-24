// Filo REPL — Desktop
//
// A graphical REPL for the Filo language built with glaze.
// Uses devengine's embedded Bootstrap 5 for the UI and all Filo
// extension packages (math, rand, str, print).
//
// The left panel is the code editor and the right panel shows results.
// State (globals) persists across evaluations within the session.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/crgimenes/devengine/assets"
	"github.com/crgimenes/filo"
	"github.com/crgimenes/filo/filomath"
	"github.com/crgimenes/filo/filoprint"
	"github.com/crgimenes/filo/filorand"
	"github.com/crgimenes/filo/filostrings"
	webview "github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

// FiloService wraps a Filo engine and maintains REPL state.
type FiloService struct {
	mu      sync.Mutex
	engine  *filo.Engine
	globals map[string]filo.Value
	cfg     filo.EvalConfig
	output  *captureWriter
}

// captureWriter captures output from filoprint to include in results.
type captureWriter struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *captureWriter) Flush() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.buf.String()
	w.buf.Reset()
	return s
}

// EvalResult holds the result of a Filo evaluation for JSON serialization.
type EvalResult struct {
	Output string `json:"output"` // print output
	Result string `json:"result"` // final value
	Error  string `json:"error"`  // error message, if any
}

// Eval evaluates a Filo script and returns the result.
func (s *FiloService) Eval(script string) EvalResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	script = strings.TrimSpace(script)
	if script == "" {
		return EvalResult{}
	}

	// Flush any previous output.
	s.output.Flush()

	ctx := context.Background()
	result, newGlobals, err := s.engine.RunScript(ctx, script, s.globals, s.cfg)

	// Capture any print output.
	printOutput := s.output.Flush()

	if err != nil {
		return EvalResult{
			Output: printOutput,
			Error:  err.Error(),
		}
	}

	if newGlobals != nil {
		s.globals = newGlobals
	}

	return EvalResult{
		Output: printOutput,
		Result: formatResult(result),
	}
}

// Reset clears all globals, giving a fresh session.
func (s *FiloService) Reset() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globals = make(map[string]filo.Value)
	return "Session reset."
}

func formatResult(v filo.Value) string {
	switch v.Kind {
	case filo.KNumber:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v.Num), "0"), ".")
	case filo.KBool:
		if v.Bool {
			return "#t"
		}
		return "#f"
	case filo.KString:
		return v.Str
	case filo.KList:
		var items []string
		for _, item := range v.List {
			items = append(items, formatResult(item))
		}
		return "(" + strings.Join(items, " ") + ")"
	case filo.KTuple:
		var items []string
		for _, item := range v.Tup {
			items = append(items, formatResult(item))
		}
		return "(values " + strings.Join(items, " ") + ")"
	case filo.KFunc:
		return "<fn>"
	default:
		return v.String()
	}
}

const html = `<!doctype html>
<html lang="en" data-bs-theme="dark">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="/assets/bootstrap/css/bootstrap.min.css">
  <link rel="stylesheet" href="/assets/style.css">
  <title>Filo REPL</title>
  <style>
    body { display: flex; flex-direction: column; height: 100vh; overflow: hidden; }
    .repl-container { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
    .panels { flex: 1; display: flex; gap: 0; overflow: hidden; }
    .panel { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
    .panel-header {
      padding: 8px 16px; font-size: 0.8em; font-weight: 600;
      text-transform: uppercase; letter-spacing: 0.05em;
      border-bottom: 1px solid var(--bs-border-color);
      color: var(--bs-secondary-color);
    }
    textarea#code {
      flex: 1; resize: none; border: none; border-radius: 0;
      font-family: "SF Mono", "Fira Code", "Cascadia Code", monospace;
      font-size: 14px; line-height: 1.6;
      padding: 16px; background: var(--bs-body-bg);
      color: var(--bs-body-color); outline: none;
    }
    #output {
      flex: 1; overflow-y: auto; padding: 16px;
      font-family: "SF Mono", "Fira Code", "Cascadia Code", monospace;
      font-size: 14px; line-height: 1.6;
    }
    .divider {
      width: 1px; background: var(--bs-border-color); flex-shrink: 0;
    }
    .entry { margin-bottom: 12px; }
    .entry-input {
      color: var(--bs-secondary-color); font-size: 0.85em;
      white-space: pre-wrap; word-break: break-all;
    }
    .entry-input::before { content: "filo> "; color: #6c757d; }
    .entry-result { color: #20c997; white-space: pre-wrap; word-break: break-all; }
    .entry-error { color: #dc3545; white-space: pre-wrap; }
    .entry-print { color: #0dcaf0; white-space: pre-wrap; opacity: 0.85; }
    .toolbar {
      display: flex; align-items: center; gap: 8px;
      padding: 6px 16px;
      border-top: 1px solid var(--bs-border-color);
    }
    .toolbar .badge { font-weight: 500; }
    .key-hint { font-size: 0.75em; color: #fff; }
    .key-hint kbd {
      font-size: 0.9em; padding: 1px 5px; border-radius: 3px;
      background: #495057; border: 1px solid #6c757d; color: #fff;
    }
  </style>
</head>
<body>
  <nav class="navbar border-bottom py-1">
    <div class="container-fluid">
      <span class="navbar-brand fw-bold mb-0 fs-6">λ Filo REPL</span>
      <div class="d-flex gap-2">
        <button id="btnRun" class="btn btn-sm btn-primary">▶ Run</button>
        <button id="btnClear" class="btn btn-sm btn-outline-secondary">Clear Output</button>
        <button id="btnReset" class="btn btn-sm btn-outline-warning">Reset Session</button>
      </div>
    </div>
  </nav>

  <div class="repl-container">
    <div class="panels">
      <div class="panel">
        <div class="panel-header">Code</div>
        <textarea id="code" placeholder="Type Filo code here...&#10;&#10;Examples:&#10;  (+ 1 2)&#10;  (define x 42)&#10;  (map (lambda (n) (* n n)) (list 1 2 3 4 5))" spellcheck="false" autofocus></textarea>
      </div>
      <div class="divider"></div>
      <div class="panel">
        <div class="panel-header">Output</div>
        <div id="output"></div>
      </div>
    </div>
    <div class="toolbar">
      <span class="badge bg-success">math</span>
      <span class="badge bg-info">str</span>
      <span class="badge bg-warning text-dark">rand</span>
      <span class="badge bg-secondary">print</span>
      <span class="flex-grow-1"></span>
      <span class="key-hint"><kbd>Ctrl</kbd>+<kbd>Enter</kbd> to run</span>
    </div>
  </div>

  <script defer src="/assets/bootstrap/js/bootstrap.bundle.min.js"></script>
  <script type="module">
    const code = document.getElementById("code");
    const output = document.getElementById("output");
    const btnRun = document.getElementById("btnRun");
    const btnClear = document.getElementById("btnClear");
    const btnReset = document.getElementById("btnReset");

    function escapeHtml(text) {
      const div = document.createElement("div");
      div.textContent = text;
      return div.innerHTML;
    }

    function appendEntry(input, result) {
      const entry = document.createElement("div");
      entry.className = "entry";

      // Show input
      const inputEl = document.createElement("div");
      inputEl.className = "entry-input";
      inputEl.textContent = input.replace(/\n/g, "\n...   ");
      entry.appendChild(inputEl);

      // Show print output if any
      if (result.output) {
        const printEl = document.createElement("div");
        printEl.className = "entry-print";
        printEl.textContent = result.output;
        entry.appendChild(printEl);
      }

      // Show error or result
      if (result.error) {
        const errEl = document.createElement("div");
        errEl.className = "entry-error";
        errEl.textContent = "error: " + result.error;
        entry.appendChild(errEl);
      } else if (result.result) {
        const resEl = document.createElement("div");
        resEl.className = "entry-result";
        resEl.textContent = result.result;
        entry.appendChild(resEl);
      }

      output.appendChild(entry);
      output.scrollTop = output.scrollHeight;
    }

    async function runCode() {
      const script = code.value.trim();
      if (!script) return;

      btnRun.disabled = true;
      btnRun.textContent = "⏳";

      try {
        const result = await window.filo_eval(script);
        appendEntry(script, result);
        code.value = "";
      } catch (e) {
        appendEntry(script, { error: String(e) });
      }

      btnRun.disabled = false;
      btnRun.textContent = "▶ Run";
      code.focus();
    }

    btnRun.addEventListener("click", runCode);

    btnClear.addEventListener("click", () => {
      output.innerHTML = "";
      code.focus();
    });

    btnReset.addEventListener("click", async () => {
      const msg = await window.filo_reset();
      output.innerHTML = "";
      appendEntry(".reset", { result: msg });
      code.focus();
    });

    code.addEventListener("keydown", e => {
      // Ctrl+Enter or Cmd+Enter to run
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        runCode();
        return;
      }
      // Tab inserts 2 spaces
      if (e.key === "Tab") {
        e.preventDefault();
        const start = code.selectionStart;
        const end = code.selectionEnd;
        code.value = code.value.substring(0, start) + "  " + code.value.substring(end);
        code.selectionStart = code.selectionEnd = start + 2;
      }
    });
  </script>
</body>
</html>`

func startAssetServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(assets.FS)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	go func() {
		srv := &http.Server{Handler: mux}
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), nil
}

func main() {
	// Create Filo engine with all extension packages.
	engine := filo.NewEngine()
	output := &captureWriter{}

	filomath.RegisterBuiltins(engine)
	filorand.RegisterBuiltins(engine)
	filostrings.RegisterBuiltins(engine)
	filoprint.RegisterBuiltins(engine)
	filoprint.SetOutput(output)

	svc := &FiloService{
		engine:  engine,
		globals: make(map[string]filo.Value),
		cfg: filo.EvalConfig{
			StepLimit:      100000,
			RecursionLimit: 128,
			Timeout:        30 * time.Second,
		},
		output: output,
	}

	// Start asset server.
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

	w.SetTitle("Filo REPL")
	w.SetSize(900, 600, webview.HintNone)

	// Bind: window.filo_eval(script), window.filo_reset()
	bound, err := webview.BindMethods(w, "filo", svc)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Bound:", strings.Join(bound, ", "))

	w.Navigate(baseURL)
	w.Run()
}
