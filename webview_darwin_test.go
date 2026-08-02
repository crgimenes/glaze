package glaze

import (
	"errors"
	"flag"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// AppKit runs on one OS thread, so the GUI scenarios run in TestMain (the main
// goroutine) and stash results; the TestXxx functions only assert. The macOS CI
// runner has a window server, so no virtual display is needed.

var (
	resBridge      atomic.Value // string
	resErrorUnbind atomic.Value // string
	resRichTypes   atomic.Value // string
	resMultiWindow atomic.Value // string
	resEmbed       atomic.Value // string
	resOpenPanel   atomic.Value // string
	resDialogCfg   atomic.Value // string
	resFirstMouse  atomic.Value // string
)

func TestMain(m *testing.M) {
	// Honor -short so `go test -short ./...` is a fast, headless run: each GUI
	// scenario drives a real NSApplication run loop and can take a few seconds, so
	// running all of them unconditionally makes a plain `go test` slow and fragile
	// under a tight timeout. The assertions skip when their scenario didn't run.
	flag.Parse()
	if !testing.Short() {
		runtime.LockOSThread()
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resMultiWindow.Store(multiWindowScenario())
		resEmbed.Store(embedScenario())
		resOpenPanel.Store(openPanelCompletionScenario())
		resDialogCfg.Store(dialogConfigScenario())
		resFirstMouse.Store(firstMouseScenario())
	}
	os.Exit(m.Run())
}

// requireGUI skips a GUI assertion when its scenario did not run (e.g. -short).
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("GUI scenarios skipped (-short)")
	}
}

// openPanelCompletionScenario exercises the WKUIDelegate file-chooser
// completion path (invokeOpenPanelCompletion / NSInvocation "v@?@") without
// presenting the modal panel, by invoking it with a Go block and a cancelled
// (nil) selection. The full panel UI is exercised manually via
// examples/filepicker.
func openPanelCompletionScenario() string {
	done := make(chan objc.ID, 1)
	block := objc.NewBlock(func(_ objc.Block, urls objc.ID) {
		select {
		case done <- urls:
		default:
		}
	})
	invokeOpenPanelCompletion(objc.ID(uintptr(block)), 0)
	select {
	case urls := <-done:
		if urls != 0 {
			return "urls=nonnil (want nil for cancel)"
		}
		return "panel-ok"
	case <-time.After(2 * time.Second):
		return "completion handler not invoked"
	}
}

// multiWindowScenario verifies window ref-count bookkeeping across two engines
// and that full Destroy returns the count to its baseline (no run loop needed).
func multiWindowScenario() string {
	start := atomic.LoadInt32(&windowCount)
	w1, err := New(false)
	if err != nil {
		return "w1 error: " + err.Error()
	}
	w2, err := NewWindow(false, nil)
	if err != nil {
		return "w2 error: " + err.Error()
	}
	peak := atomic.LoadInt32(&windowCount)
	w1.Destroy()
	w2.Destroy()
	end := atomic.LoadInt32(&windowCount)
	return strconv.Itoa(int(start)) + "->" + strconv.Itoa(int(peak)) + "->" + strconv.Itoa(int(end))
}

// embedScenario embeds a web view into a caller-provided NSWindow and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func embedScenario() string {
	host := class("NSWindow").Send(sel("alloc"))
	host = host.Send(sel("initWithContentRect:styleMask:backing:defer:"),
		cgRect{cgPoint{0, 0}, cgSize{400, 300}},
		uint(nsWindowStyleMaskTitled), nsBackingStoreBuffered, false)
	host = host.Send(sel("retain"))

	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host)) // objc.ID -> unsafe.Pointer
	w, err := NewWindow(false, hostPtr)
	if err != nil {
		return "new error: " + err.Error()
	}
	owns := w.(*webview).ownsWindow // concrete type (same package)
	w.Destroy()

	// Host must still be alive after Destroy (this would crash on a released
	// object), then tear it down ourselves.
	host.Send(sel("setTitle:"), nsstr("still alive"))
	host.Send(sel("close"))
	host.Send(sel("release"))

	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestMultiWindowRefCount(t *testing.T) {
	const want = "0->2->0"
	got, _ := resMultiWindow.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("window ref-count = %q, want %q", got, want)
	}
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestOpenPanelCompletion(t *testing.T) {
	got, _ := resOpenPanel.Load().(string)
	requireGUI(t, got)
	if got != "panel-ok" {
		t.Fatalf("open-panel completion = %q, want %q", got, "panel-ok")
	}
}

// dialogConfigScenario verifies that configureOpenPanel maps FileDialogOptions
// onto the NSOpenPanel correctly, without presenting the modal (runModal is the
// only part that needs UI; the configuration is the part worth asserting). The
// full dialog is exercised manually via examples/filedialog.
func dialogConfigScenario() string {
	res := "dialog-config-ok"
	autorelease(func() {
		// File mode: choose files, multiple selection, a two-extension filter
		// (one of them carrying a leading dot, which must be stripped).
		p := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(p, true, false, true, FileDialogOptions{
			Title:   "Pick a file",
			Filters: []FileFilter{{Name: "Images", Extensions: []string{"png", ".jpg"}}},
		})
		switch {
		case p.Send(sel("canChooseFiles")) == 0:
			res = "file: canChooseFiles=false"
		case p.Send(sel("canChooseDirectories")) != 0:
			res = "file: canChooseDirectories=true (want false)"
		case p.Send(sel("allowsMultipleSelection")) == 0:
			res = "file: allowsMultipleSelection=false"
		case int(p.Send(sel("allowedFileTypes")).Send(sel("count"))) != 2:
			res = "file: allowedFileTypes count != 2"
		}
		if res != "dialog-config-ok" {
			return
		}
		// Directory mode: files off, dirs on, and a wildcard filter must leave
		// the type restriction unset (allowedFileTypes nil).
		d := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(d, false, true, false, FileDialogOptions{
			Filters: []FileFilter{{Extensions: []string{"*"}}},
		})
		switch {
		case d.Send(sel("canChooseFiles")) != 0:
			res = "dir: canChooseFiles=true (want false)"
		case d.Send(sel("canChooseDirectories")) == 0:
			res = "dir: canChooseDirectories=false"
		case d.Send(sel("allowedFileTypes")) != 0:
			res = "dir: allowedFileTypes set (want nil for wildcard)"
		}
	})
	return res
}

func TestDialogConfig(t *testing.T) {
	got, _ := resDialogCfg.Load().(string)
	requireGUI(t, got)
	if got != "dialog-config-ok" {
		t.Fatalf("dialog config = %q, want %q", got, "dialog-config-ok")
	}
}

func TestFirstOr(t *testing.T) {
	got := firstOr(nil, "def")
	if got != "def" {
		t.Fatalf("firstOr(nil) = %q, want %q", got, "def")
	}
	got = firstOr([]string{"a", "b"}, "def")
	if got != "a" {
		t.Fatalf("firstOr([a b]) = %q, want %q", got, "a")
	}
}

func bridgeScenario() string {
	w, err := New(true)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.SetSize(700, 500, HintNone)

	done := make(chan string, 1)
	_ = w.Bind("add", func(a, b float64) float64 { return a + b })
	_ = w.Bind("hello", func(s string) string { return "hi " + s })
	_ = w.Bind("done", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	time.AfterFunc(15*time.Second, w.Terminate)

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var s = await window.add(20, 22);
    var h = await window.hello("x");
    window.done(s + "|" + h);
  } catch(e) { window.done("ERR:" + e); }
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func errorUnbindScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	done := make(chan string, 1)
	_ = w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	_ = w.Bind("boom", func() (string, error) { return "", errors.New("kaboom") })
	_ = w.Bind("temp", func() string { return "x" })
	_ = w.Unbind("temp")
	time.AfterFunc(15*time.Second, w.Terminate)

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  var msg = 'temp=' + (typeof window.temp);
  try { await window.boom(); msg += ' boom=nope'; }
  catch(e){ msg += ' boom=' + e; }
  window.report(msg);
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

type point struct{ X, Y int }

func richTypesScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()

	done := make(chan string, 1)
	_ = w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	_ = w.Bind("echoPoint", func(p point) point { return point{p.X + 1, p.Y + 1} })
	_ = w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(15*time.Second, w.Terminate)

	w.SetHtml(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var p = await window.echoPoint({X:1, Y:2});
    var s = await window.sum([1,2,3,4]);
    window.report('p=' + p.X + ',' + p.Y + ' s=' + s);
  } catch(e) { window.report('ERR:' + e); }
});
</script></body></html>`)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func TestBridge(t *testing.T) {
	got, _ := resBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

// firstMouseScenario checks the opt-in end to end against the Objective-C
// runtime: the view a first-mouse web view is built from must ANSWER YES to
// acceptsFirstMouse:, and a default one must keep AppKit's NO. Asking the
// object itself is the point — a test that only compared class names would
// pass while the method was never installed.
func firstMouseScenario() string {
	ask := func(opts Options) (string, bool) {
		w, err := NewWithOptions(opts)
		if err != nil {
			return "new error: " + err.Error(), false
		}
		defer w.Destroy()
		view := w.(*webview).webView
		if view == 0 {
			return "no web view was created", false
		}
		if view.Send(sel("respondsToSelector:"), sel("acceptsFirstMouse:")) == 0 {
			return "the view does not respond to acceptsFirstMouse:", false
		}
		// A nil NSEvent is what AppKit passes when it asks about a view that is
		// not in a window yet, and neither implementation reads it.
		return "", bool(view.Send(sel("acceptsFirstMouse:"), objc.ID(0)) != 0)
	}

	msg, on := ask(Options{AcceptsFirstMouse: true})
	if msg != "" {
		return msg
	}
	if !on {
		return "opted in, but the view still refuses the first mouse"
	}
	msg, off := ask(Options{})
	if msg != "" {
		return msg
	}
	if off {
		return "not opted in, but the view accepts the first mouse (the default must stay AppKit's)"
	}
	return "first-mouse-ok"
}

func TestAcceptsFirstMouseIsOptIn(t *testing.T) {
	const want = "first-mouse-ok"
	got, _ := resFirstMouse.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("acceptsFirstMouse: got %q, want %q", got, want)
	}
}
