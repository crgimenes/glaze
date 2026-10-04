package glaze

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
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
	resBridge       atomic.Value // string
	resErrorUnbind  atomic.Value // string
	resRichTypes    atomic.Value // string
	resMultiWindow  atomic.Value // string
	resEmbed        atomic.Value // string
	resOpenPanel    atomic.Value // string
	resDialogCfg    atomic.Value // string
	resFirstMouse   atomic.Value // string
	resHitTest      atomic.Value // string
	resRaise        atomic.Value // string
	resEditor       atomic.Value // string
	resExternalLoop atomic.Value // string
)

func TestMain(m *testing.M) {
	// Honor -short so `go test -short ./...` is a fast, headless run: each GUI
	// scenario drives a real NSApplication run loop and can take a few seconds, so
	// running all of them unconditionally makes a plain `go test` slow and fragile
	// under a tight timeout. The assertions skip when their scenario didn't run.
	if os.Getenv(nsappFirstEnv) == "1" {
		os.Exit(nsappFirstChild())
	}
	flag.Parse()
	if !testing.Short() {
		runtime.LockOSThread()
		// First: the web view that starts the application owns its delegate.
		resOpenURLs.Store(openURLsScenario())
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resMultiWindow.Store(multiWindowScenario())
		resEmbed.Store(embedScenario())
		resOpenPanel.Store(openPanelCompletionScenario())
		resDialogCfg.Store(dialogConfigScenario())
		resFirstMouse.Store(firstMouseScenario())
		resHitTest.Store(hitTestFirstMouseScenario())
		resRaise.Store(raiseScenario())
		resEditor.Store(editorScenario())
		resNoBridge.Store(noBridgeScenario())
		resHideUntilLoaded.Store(hideUntilLoadedScenario())
		resRevealTiming.Store(revealTimingScenario())
		resSpinner.Store(spinnerScenario())
		resHeldBackInput.Store(heldBackInputScenario())
		resContextMenu.Store(contextMenuScenario())
		resMediaCapture.Store(mediaCaptureScenario())
		resNavigation.Store(navigationScenario())
		resURLChange.Store(urlChangeScenario())
		resMediaOrigin.Store(mediaOriginScenario())
		resSchemeReuse.Store(schemeReuseScenario())
		resHistory.Store(historyScenario())
		resNewWindow.Store(newWindowScenario())
		resFind.Store(findScenario())
		resZoom.Store(zoomScenario())
		resDownload.Store(downloadScenario())
		resEphemeral.Store(ephemeralScenario())
		resPanelOnMain.Store(panelOnMainScenario())
		// Last: this scenario runs its own [NSApp run] as the "external" host.
		resExternalLoop.Store(externalLoopScenario())
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
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()

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
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()

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
	defer time.AfterFunc(15*time.Second, w.Terminate).Stop()

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

// hitTestFirstMouseScenario checks the property that actually decides whether
// the opt-in works: AppKit asks the view its HIT TEST lands on, not the one we
// happen to hold a pointer to. If WebKit ever puts an internal subview in front
// of ours, the override would still answer YES to us and NO to the user's
// click — a silent, untestable-by-name regression.
func hitTestFirstMouseScenario() string {
	w, err := NewWithOptions(Options{AcceptsFirstMouse: true})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	wv := w.(*webview).webView
	win := w.(*webview).window
	content := win.Send(sel("contentView"))
	hit := content.Send(sel("hitTest:"), cgPoint{200, 200})

	name := func(id objc.ID) string {
		if id == 0 {
			return "<nil>"
		}
		return cstr(id.Send(sel("className")).Send(sel("UTF8String")))
	}
	accepts := func(id objc.ID) string {
		if id == 0 {
			return "-"
		}
		if id.Send(sel("respondsToSelector:"), sel("acceptsFirstMouse:")) == 0 {
			return "no-selector"
		}
		if id.Send(sel("acceptsFirstMouse:"), objc.ID(0)) != 0 {
			return "YES"
		}
		return "NO"
	}
	if accepts(hit) != "YES" {
		return "the view under the cursor refuses the first mouse: hit=" +
			name(hit) + "/" + accepts(hit) + " webView=" + name(wv) + "/" + accepts(wv)
	}
	_ = content
	return "hit-test-ok"
}

func TestTheViewUnderTheCursorAcceptsTheFirstMouse(t *testing.T) {
	const want = "hit-test-ok"
	got, _ := resHitTest.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("hit-test first mouse: got %q, want %q", got, want)
	}
}

// raiseScenario checks that Raise leaves the window KEY. A window that rose but
// is not key is exactly the state Raise exists to escape: the next click on it
// is spent activating instead of pressing what it landed on.
func raiseScenario() string {
	w, err := NewWithOptions(Options{})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	win := w.(*webview).window

	// Order it out first, so "already key" cannot pass for a working Raise.
	win.Send(sel("orderOut:"), objc.ID(0))
	if win.Send(sel("isKeyWindow")) != 0 {
		return "the window is still key after orderOut: the fixture proves nothing"
	}

	w.Raise()
	if !becameKey(w.(*webview), win, 2*time.Second) {
		return "Raise left the window not key"
	}
	return "raise-ok"
}

// becameKey pumps AppKit events until the window is key or limit passes.
// Raise asks the window server to activate the application, and the window
// only becomes key once that is processed, so the result is not observable on
// the line after the call: asserting it there passes or fails depending on how
// busy the machine is. The scenarios hold the main thread with no run loop of
// their own, which is why this pumps instead of sleeping.
func becameKey(w *webview, win objc.ID, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if win.Send(sel("isKeyWindow")) != 0 {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		autorelease(func() {
			until := class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.02)
			ev := w.app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, until, nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

func TestRaiseMakesTheWindowKey(t *testing.T) {
	const want = "raise-ok"
	got, _ := resRaise.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("Raise: got %q, want %q", got, want)
	}
}

// externalLoopScenario reproduces issue #31: a host (native/tray) already
// drives [NSApp run] on the main thread and the webview is created from a
// plain goroutine. Before the fix, New hung forever in a second [NSApp run]
// waiting for an applicationDidFinishLaunching that had already fired. The
// scenario drives the full lifecycle — create, configure, Run, Terminate,
// Destroy — and checks the host loop survives the window.
func externalLoopScenario() string {
	app := class("NSApplication").Send(sel("sharedApplication"))
	res := make(chan string, 1)

	go func() {
		verdict := func() string {
			for i := 0; app.Send(sel("isRunning")) == 0; i++ {
				if i > 500 {
					return "host loop never started"
				}
				time.Sleep(10 * time.Millisecond)
			}
			done := make(chan string, 1)
			go func() {
				w, err := New(false)
				if err != nil {
					done <- "new error: " + err.Error()
					return
				}
				defer w.Destroy()
				w.SetTitle("external loop")
				w.SetHtml("<html><body>issue 31</body></html>")
				go func() {
					time.Sleep(500 * time.Millisecond)
					w.Terminate()
				}()
				w.Run()
				done <- "external-loop-ok"
			}()
			select {
			case s := <-done:
				if s != "external-loop-ok" {
					return s
				}
			case <-time.After(15 * time.Second):
				return "timeout: New or Run blocked under a running loop (issue #31)"
			}

			// Second shape: the whole lifecycle issued ON the UI thread from
			// inside a run-loop callout — what a tray OnClick does when it
			// calls glaze synchronously. Run must pump events instead of
			// block-waiting, or it deadlocks the very loop that would deliver
			// the close.
			syncRes := make(chan string, 1)
			dispatchMain(func() {
				w, err := New(false)
				if err != nil {
					syncRes <- "sync new error: " + err.Error()
					return
				}
				defer w.Destroy()
				w.SetHtml("<html><body>issue 31, sync shape</body></html>")
				go func() {
					time.Sleep(300 * time.Millisecond)
					w.Terminate()
				}()
				w.Run()
				syncRes <- "external-loop-ok"
			})
			select {
			case s := <-syncRes:
				return s
			case <-time.After(15 * time.Second):
				return "timeout: sync (OnClick-shaped) lifecycle hung"
			}
		}()
		if app.Send(sel("isRunning")) == 0 {
			// The webview must not have stopped the host's loop on its way out.
			verdict += " (webview close stopped the host loop)"
		}
		res <- verdict
		// Stop the host loop; the scenario owns it, glaze must not.
		dispatchMain(func() {
			autorelease(func() {
				app.Send(sel("stop:"), objc.ID(0))
				postWakeEvent(app)
			})
		})
	}()

	app.Send(sel("run")) // the "tray": owns the run loop on the main thread
	return <-res
}

func TestNewUnderAnExternalRunLoop(t *testing.T) {
	const want = "external-loop-ok"
	got, _ := resExternalLoop.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("external run loop: got %q, want %q", got, want)
	}
}

var resPanelOnMain atomic.Value // string

// panelOnMainScenario opens a save panel from the main thread -- as a menu
// item or a WebKit callback does -- and has it abort itself after a second.
// Dispatching the panel and waiting for it from there used to hang forever.
func panelOnMainScenario() string {
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	result := "panel never returned"
	w.Dispatch(func() {
		// The panel's modal loop runs only NSModalPanelRunLoopMode: a block on
		// the main queue would wait for it to end. Schedule the abort there.
		app := class("NSApplication").Send(sel("sharedApplication"))
		modes := class("NSArray").Send(sel("arrayWithObject:"), nsstr("NSModalPanelRunLoopMode"))
		app.Send(sel("performSelector:withObject:afterDelay:inModes:"), sel("abortModal"), objc.ID(0), 1.0, modes)
		path, err := w.SaveFile(FileDialogOptions{Filename: "x.txt"})
		result = fmt.Sprintf("returned %q err=%v", path, err)
		w.Terminate()
	})
	w.Run()
	return result
}

func TestFilePanelFromMainThread(t *testing.T) {
	got, _ := resPanelOnMain.Load().(string)
	requireGUI(t, got)
	if got != `returned "" err=<nil>` {
		t.Fatalf("save panel from the main thread: %s", got)
	}
}

var resOpenURLs atomic.Value // string

// openURLsScenario hands the app delegate what the system sends when another
// app opens links with this one: application:openURLs: with NSURLs.
func openURLsScenario() string {
	var got []string
	v, err := NewWithOptions(Options{OnOpenURLs: func(urls []string) { got = urls }})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer v.Destroy()
	w := v.(*webview)
	// The application's delegate, installed by this first web view.
	delegate := w.app.Send(sel("delegate"))
	if delegate == 0 {
		return "no app delegate"
	}
	result := ""
	w.Dispatch(func() {
		autorelease(func() {
			a := class("NSURL").Send(sel("URLWithString:"), nsstr("https://example.com/a"))
			b := class("NSURL").Send(sel("URLWithString:"), nsstr("https://example.com/b?q=1"))
			urls := class("NSArray").Send(sel("arrayWithObjects:count:"), unsafe.Pointer(&[2]objc.ID{a, b}), 2)
			delegate.Send(sel("application:openURLs:"), w.app, urls)
		})
		result = strings.Join(got, " ")
		w.Terminate()
	})
	w.Run()
	return result
}

func TestOnOpenURLs(t *testing.T) {
	got, _ := resOpenURLs.Load().(string)
	requireGUI(t, got)
	want := "https://example.com/a https://example.com/b?q=1"
	if got != want {
		t.Fatalf("OnOpenURLs: got %q, want %q", got, want)
	}
}

func spinnerVisible(w *webview) bool {
	return w.spinner != 0 && !objc.Send[bool](w.spinner, sel("isHidden"))
}

// enableMockCapture gives the view simulated cameras and microphones (a
// private WebKit preference, for tests only), so the permission path runs
// without devices or the system's privacy prompt.
func enableMockCapture(w *webview) {
	performOnMain(func() {
		prefs := w.webView.Send(sel("configuration")).Send(sel("preferences"))
		prefs.Send(sel("_setMockCaptureDevicesEnabled:"), true)
	})
}

var resContextMenu atomic.Value // string

// contextMenuScenario hands a menu with WebKit's download items to both web
// view classes the way AppKit does before showing it, and reports what is
// left: the download items WKWebView would not carry out are gone.
func contextMenuScenario() string {
	var out []string
	for _, firstMouse := range []bool{false, true} {
		v, err := NewWithOptions(Options{AcceptsFirstMouse: firstMouse})
		if err != nil {
			return "new error: " + err.Error()
		}
		w := v.(*webview)
		performOnMain(func() {
			autorelease(func() {
				menu := class("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsstr("")).Send(sel("autorelease"))
				for _, id := range []string{"WKMenuItemIdentifierDownloadLinkedFile", "WKMenuItemIdentifierCopyLink", "WKMenuItemIdentifierDownloadImage", "WKMenuItemIdentifierDownloadMedia"} {
					item := menu.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsstr(id), objc.SEL(0), nsstr(""))
					item.Send(sel("setIdentifier:"), nsstr(id))
				}
				w.webView.Send(sel("willOpenMenu:withEvent:"), menu, objc.ID(0))
				var left []string
				for i := range objc.Send[int](menu, sel("numberOfItems")) {
					left = append(left, cstr(menu.Send(sel("itemAtIndex:"), i).Send(sel("identifier")).Send(sel("UTF8String"))))
				}
				out = append(out, fmt.Sprintf("firstMouse=%v left=%s", firstMouse, strings.Join(left, ",")))
			})
		})
		v.Destroy()
	}
	return strings.Join(out, " | ")
}

func TestContextMenuDropsDeadDownloads(t *testing.T) {
	got, _ := resContextMenu.Load().(string)
	requireGUI(t, got)
	want := "firstMouse=false left=WKMenuItemIdentifierCopyLink | firstMouse=true left=WKMenuItemIdentifierCopyLink"
	if got != want {
		t.Fatalf("context menu: got %q, want %q", got, want)
	}
}

var resHeldBackInput atomic.Value // string

// heldBackInputScenario checks where a click at the window's center and the
// keys go while HideUntilLoaded holds the page back, and after it shows.
func heldBackInputScenario() string {
	v, err := NewWithOptions(Options{HideUntilLoaded: true})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer v.Destroy()
	w := v.(*webview)
	var out []string
	probe := func(stage string) {
		performOnMain(func() {
			f := objc.Send[cgRect](w.widget, sel("frame"))
			hit := w.widget.Send(sel("hitTest:"), cgPoint{f.Origin.X + f.Size.Width/2, f.Origin.Y + f.Size.Height/2})
			inPage := hit != 0 && objc.Send[bool](hit, sel("isDescendantOf:"), w.webView)
			keys := w.window.Send(sel("firstResponder")) == w.webView
			out = append(out, fmt.Sprintf("%s click-in-page=%v keys-in-page=%v", stage, inPage, keys))
		})
	}
	probe("held")
	performOnMain(w.revealContent)
	probe("shown")
	return strings.Join(out, " | ")
}

func TestHeldBackInput(t *testing.T) {
	got, _ := resHeldBackInput.Load().(string)
	requireGUI(t, got)
	want := "held click-in-page=false keys-in-page=false | shown click-in-page=true keys-in-page=true"
	if got != want {
		t.Fatalf("input while held back:\n got %s\nwant %s", got, want)
	}
}
